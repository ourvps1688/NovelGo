package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/novelgo/novelgo/internal/contextasm"
)

// ── 全书一致性巡检（WBS 3.3「张力报告」）────────────────────────────────
//
// 系列长期连载只允许「前向适配」时设定会随卷数慢慢失真（每一处局部修补都合理，
// 合起来渐行渐远）。本巡检是累计漂移的兜底：全书视角对照「已冻结卷既定事实」与
// 当前台账/正文，产出**建议性张力报告**——只报告不改写、绝不进 S5 自动修复路由，
// 改不改由作者定（retcon 候选也只走 3.1 人工审批闸门）。
//
// 触发（WBS 建议「卷末 + 手动」而非实时，控成本）：
//   - 卷末自动：G_freeze 自动冻结成功（freezeVolumeWithNote）后跑一次；
//   - 手动：控制指令 "patrol"（ControlPipeline）。

// patrolPrompt 构造全书一致性巡检（张力报告）的任务提示词：只读、只报告不改写。
// blocks 尾部装配上下文块（巡检基准线，WBS 2.3；P0 §0.5 patrol=Facts+Voice）。
// issue JSON 契约片段由 contract.go 单源生成（全量契约：张力需标
// target_scope/dimension/direction 供报告视图展示建议方向）。
func patrolPrompt(novelID int64, blocks contextasm.ContextBlocks) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 consistency-patrol 方法论做一次**全书一致性巡检（张力报告）**："+
		"用 get_chapter_list 列出全部章节，对照「已冻结卷既定事实」与当前台账（角色/关系/地点/时间线/弧线/读者认知）通读全书，"+
		"只报**张力**（前向适配累计出来的漂移）：设定失真、承诺未兑现、读者认知被悄悄改写、时间线漂移、retcon 衔接裂缝；"+
		"每条张力按落点标 target_scope、按四维标 dimension、给建议 direction（默认 forward_fix；仅硬矛盾且无法前向消解才 retcon_candidate）"+
		"与 fix_type（正文↔台账对账冲突**不预设谁对**：可能台账记错该修台账（ledger），也可能正文记错该修正文（body），按证据判方向）；"+
		"每条必须给具体证据，不报无据臆测，不报单章文笔/节奏类质量问题。"+
		"这是**只报告不改写**的兜底巡检：你不改任何数据，verdict 只是给作者的参考信号（有张力 → needs_fix，无 → pass），"+
		"不会触发任何自动修复，改不改由作者定。"+
		"最终回复末尾**必须**附一个 ```json 代码块，格式："+auditFormatLine(true, issueSamplePatrol)+"——"+issueFieldRules(),
		novelID) + blocks.Render(contextasm.PurposePatrol)
}

// runConsistencyPatrol 执行一次全书一致性巡检（张力报告）：跑 consistency-patrol
// 技能 → 解析结构化张力清单 → 写入 context.PatrolReport → 广播
// pipeline:consistency_patrol 事件通知前端刷新巡检报告视图。
//
// 只报告不改写：结果绝不进 Audit/FixStates 等修复路由字段；失败只记日志
// （不落库、不广播、不阻断）——兜底巡检不给流水线添堵。
func (s *Service) runConsistencyPatrol(ctx context.Context, runID string, novelID int64) {
	bg := s.appCtx
	// 检测进行态注销（T1.3）：巡检·结构检测派发时按容器 runID 登记，收口（含失败）注销；
	// 流水线侧巡检无登记条目，注销为 no-op。
	defer s.endDetectByRunID(runID)
	out, err := s.dispatchSkill(bg, "consistency-patrol", SkillInput{
		NovelID:   novelID,
		Prompt:    patrolPrompt(novelID, s.buildBlocks(bg, novelID, contextasm.PurposePatrol)),
		MaxTurns:  12,
		SessionID: "pipeline:patrol:" + strconv.FormatInt(novelID, 10),
	})
	if err != nil {
		s.logger.Warn("一致性巡检执行失败（非阻断，跳过）", "run_id", runID, "err", err)
		return
	}
	// 拒绝 / 安全审核拦截：上游把拒绝语当正常 content 返回（err == nil），必须在此拦截，
	// 否则会被 parseAudit 兜底包装成 needs_fix 的假张力报告污染界面。命中时落地清晰的
	// 「巡检失败」状态（verdict=error）而非垃圾张力项，并广播事件让面板刷新展示。
	if refused, reason := isLLMRefusal(out.Text); refused {
		s.logger.Warn("一致性巡检：模型拒绝/安全审核拦截（落地失败态，非阻断）",
			"run_id", runID, "reason", reason, "snippet", firstN(out.Text, 120))
		if serr := s.storePatrolReport(bg, runID, AuditData{
			Verdict: "error",
			Summary: "一致性巡检未能完成：模型拒绝了本次请求（" + reason + "）。" +
				"这通常是因为发送给模型的章节/台账内容触发了上游模型的安全审核。" +
				"建议：①更换内容审核更宽松的模型或供应商；②确认当前模型支持长上下文（全书巡检会拼入大量章节）；" +
				"③如确为内容命中，可先对该卷做局部脱敏后再巡检。本次原始返回已忽略，未写入张力清单。",
			Issues: []AuditIssue{},
		}); serr != nil {
			s.logger.Warn("一致性巡检：写回失败态失败", "err", serr)
		}
		emitConsistencyPatrol(ctx, map[string]any{"novel_id": novelID, "run_id": runID})
		return
	}
	data := parseFreshAudit(out.Text)
	if err := s.storePatrolReport(bg, runID, data); err != nil {
		s.logger.Warn("一致性巡检：写回张力报告失败", "err", err)
	}
	emitConsistencyPatrol(ctx, map[string]any{
		"novel_id": novelID,
		"run_id":   runID,
	})
}

// storePatrolReport 把张力报告写入 run 的 ContextJSON.PatrolReport（只读展示用，
// 不进任何修复路由）。仿 storeStockAudit：写前重读 run，保留当前 stage/status。
// 同步打落库时间戳 PatrolReportAt（T1.3）：GetPatrolReport 双源合并按时间戳最新者胜。
func (s *Service) storePatrolReport(ctx context.Context, runID string, data AuditData) error {
	cur, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		return err
	}
	var ctxt GateContext
	if cur.ContextJSON != "" {
		_ = json.Unmarshal([]byte(cur.ContextJSON), &ctxt)
	}
	reportJSON, _ := json.Marshal(data)
	now := time.Now()
	ctxt.PatrolReport = string(reportJSON)
	ctxt.PatrolReportAt = &now
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		return merr
	}
	return s.store.SetStageStatus(ctx, cur.RunID, cur.CurrentStage, cur.Status, string(updated))
}
