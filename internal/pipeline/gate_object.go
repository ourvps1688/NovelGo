package pipeline

// gate_object.go 是 R3d「GateContext 瘦身」的大对象表化层
// （pipeline-core-redesign-proposal.md §4.4：GateContext 瘦身为纯决策记录，
// 大对象独立表化）。GateContext 序列化于 pipeline_runs.context_json，其中 9 个
// 大对象（体检/蓝图/巡检结果 JSON 与三个回流清单）单独表化进 gate_objects
// （run_id+kind 唯一），context_json 只留决策记录（stages/gates/各小字段）。
//
// 拆合完全收口在 Store 读写层（变体 C）：
//   - 写口（Create/SetStatus/SetStageStatus/AppendGateDecision）拆 9 键→表
//     +落瘦身 blob，整袋替换语义保留（传入的新上下文为全量，键缺即清）；
//   - 读口（GetByRunID/GetByNovel/GetActiveVolumeRun/ListByStatus/
//     ListActiveScopedChildren）合并表行回胖视图（表行优先、blob 兜底）。
//
// 调用方（service/orchestrator/patrol/API/前端）继续按胖 GateContext 语义
// 读写，marshal/unmarshal 点、测试、前端、API 零改动。
//
// 兼容与降级（宁缺勿错、降级零提示）：
//   - 历史胖 blob（无表行）读路径原样兜底可见；启动时 MigrateGateObjects
//     一次性表化存量胖 blob（幂等，瘦 blob 无键可拆自动跳过）；
//   - 大对象表不可用（如表缺失）时写口退回「整袋落胖 blob」的 R3d 前行为，
//     读口退回 blob 原样，行为逐字一致；
//   - payload 存原始 JSON 值（字符串键存 JSON 串字面量、数组键存数组），
//     merge 时以 json.RawMessage 注回，值字节保真。

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

// 9 大对象键（= GateContext 的 JSON 键，也用作 gate_objects.kind）。
const (
	gateObjBlueprint      = "blueprint"       // S2 卷结构概览 JSON
	gateObjAudit          = "audit"           // S4 签约体检结果 JSON（AuditData）
	gateObjBlueprintAudit = "blueprint_audit" // 蓝图体检结果 JSON
	gateObjDriftReport    = "drift_report"    // G3 一致性巡检结论
	gateObjStockAudit     = "stock_audit"     // 全量对齐现状体检 JSON
	gateObjPatrolReport   = "patrol_report"   // 全书张力报告 JSON（AuditData 形状）
	gateObjRewriteBacklog = "rewrite_backlog" // S3 重写回路清单（[]RewriteItem）
	gateObjRetconBacklog  = "retcon_backlog"  // retcon 候选清单（[]RewriteItem）
	gateObjManualBacklog  = "manual_backlog"  // 转人工工单清单（[]ManualItem）
)

// gateObjectKinds 是拆合的键全集（split 遍历顺序固定）。
var gateObjectKinds = []string{
	gateObjBlueprint,
	gateObjAudit,
	gateObjBlueprintAudit,
	gateObjDriftReport,
	gateObjStockAudit,
	gateObjPatrolReport,
	gateObjRewriteBacklog,
	gateObjRetconBacklog,
	gateObjManualBacklog,
}

// GateObject 是 GateContext 大对象的一行（R3d 表化）：(run_id, kind) 唯一，
// payload 存原始 JSON 值（字符串键=JSON 串字面量，数组键=数组字面量）。
type GateObject struct {
	ID        int64     `gorm:"primaryKey;autoIncrement"`
	RunID     string    `gorm:"column:run_id;uniqueIndex:idx_gate_objects_run_kind;not null"`
	Kind      string    `gorm:"column:kind;uniqueIndex:idx_gate_objects_run_kind;size:64;not null"`
	Payload   string    `gorm:"column:payload;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName 指定物理表名。
func (GateObject) TableName() string { return "gate_objects" }

// gatePayloadEmpty 判定大对象值是否等价 omitempty 空（键缺/空串/null/空数组
// → 不产出对象行，与 GateContext 序列化时的 omitempty 语义一致）。
func gatePayloadEmpty(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == `""` || s == "[]"
}

// splitGateObjects 把胖 ContextJSON 拆成瘦身 blob 与 9 大对象的原始 JSON 值映射。
// 空值（键缺/空串/null/空数组）不产出对象且从 blob 剔除（等价 omitempty）；
// 完全无大对象键或输入非合法 JSON 对象时原样返回输入（字节保真降级，宁缺勿错）。
func splitGateObjects(contextJSON string) (string, map[string]string) {
	if strings.TrimSpace(contextJSON) == "" {
		return contextJSON, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(contextJSON), &m); err != nil || m == nil {
		return contextJSON, nil
	}
	objects := make(map[string]string, len(gateObjectKinds))
	touched := false
	for _, kind := range gateObjectKinds {
		raw, ok := m[kind]
		if !ok {
			continue
		}
		touched = true
		delete(m, kind)
		if gatePayloadEmpty(raw) {
			continue
		}
		objects[kind] = string(raw)
	}
	if !touched {
		return contextJSON, nil
	}
	thin, err := json.Marshal(m)
	if err != nil {
		return contextJSON, nil
	}
	return string(thin), objects
}

// mergeGateObjects 把表行的大对象并回瘦身 blob 得到胖视图：表行优先、blob 兜底
// （历史胖 blob 无表行时原样可见）。无对象行时原样返回 blob（字节保真）。
func mergeGateObjects(contextJSON string, objects map[string]string) string {
	if len(objects) == 0 {
		return contextJSON
	}
	m := map[string]json.RawMessage{}
	if strings.TrimSpace(contextJSON) != "" {
		if err := json.Unmarshal([]byte(contextJSON), &m); err != nil {
			// 瘦 blob 不可解析（写路径保证恒合法，仅外部直改可能触发）：
			// 原样返回，宁缺勿错。
			return contextJSON
		}
	}
	for kind, payload := range objects {
		m[kind] = json.RawMessage(payload)
	}
	b, err := json.Marshal(m)
	if err != nil {
		return contextJSON
	}
	return string(b)
}

// replaceGateObjects 整袋替换指定 run 的大对象行（键缺即清，与写口整袋语义一致）。
// 与 context_json 更新同事务调用，保证拆合原子。
func replaceGateObjects(ctx context.Context, tx *gorm.DB, runID string, objects map[string]string) error {
	if err := tx.WithContext(ctx).Where("run_id = ?", runID).Delete(&GateObject{}).Error; err != nil {
		return err
	}
	for _, kind := range gateObjectKinds {
		payload, ok := objects[kind]
		if !ok {
			continue
		}
		if err := tx.WithContext(ctx).Create(&GateObject{
			RunID: runID, Kind: kind, Payload: payload,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// hydrateGateObjects 把表行大对象合并回各 run 的 ContextJSON（胖视图，读口收口）。
// 表行查询失败按无行降级（blob 原样，宁缺勿错）。
func (s *Store) hydrateGateObjects(ctx context.Context, runs []*PipelineRun) {
	if len(runs) == 0 {
		return
	}
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.RunID)
	}
	var rows []GateObject
	if err := s.db.WithContext(ctx).Where("run_id IN ?", ids).Find(&rows).Error; err != nil {
		return
	}
	if len(rows) == 0 {
		return
	}
	byRun := make(map[string]map[string]string, len(ids))
	for _, row := range rows {
		m := byRun[row.RunID]
		if m == nil {
			m = map[string]string{}
			byRun[row.RunID] = m
		}
		m[row.Kind] = row.Payload
	}
	for _, r := range runs {
		r.ContextJSON = mergeGateObjects(r.ContextJSON, byRun[r.RunID])
	}
}

// MigrateGateObjects 把历史胖 ContextJSON 的 9 大对象一次性表化（启动时调用，幂等）：
// 瘦 blob 无键可拆自动跳过，返回迁移的 run 数。迁移只搬大对象，不触碰状态/阶段语义。
func (s *Store) MigrateGateObjects(ctx context.Context) (int, error) {
	var runs []PipelineRun
	if err := s.db.WithContext(ctx).Select("run_id", "context_json").Find(&runs).Error; err != nil {
		return 0, err
	}
	n := 0
	for i := range runs {
		thin, objects := splitGateObjects(runs[i].ContextJSON)
		if len(objects) == 0 {
			continue
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := replaceGateObjects(ctx, tx, runs[i].RunID, objects); err != nil {
				return err
			}
			return tx.WithContext(ctx).Model(&PipelineRun{}).
				Where("run_id = ?", runs[i].RunID).
				Update("context_json", thin).Error
		})
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// MigrateGateObjects 是启动期一次性存量迁移入口（app initialize 调用）：
// 失败只告警不阻断启动（读路径有胖 blob 兜底，不影响正确性）。
func (s *Service) MigrateGateObjects() {
	n, err := s.store.MigrateGateObjects(s.appCtx)
	if err != nil {
		s.logger.Warn("GateContext 大对象表化迁移失败（读路径胖 blob 兜底，非阻断）", "err", err)
		return
	}
	if n > 0 {
		s.logger.Info("GateContext 大对象表化迁移完成", "runs", n)
	}
}
