package pipeline

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fatContextJSON 构造一份含全部 9 大对象 + 决策小字段的胖上下文（写口入参形态）。
func fatContextJSON(t *testing.T) (string, GateContext) {
	t.Helper()
	orig := GateContext{
		Stages:           []string{StageS1, StageS2},
		Gates:            []GateDecision{{GateID: "G1", Decision: "continue"}},
		Extra:            "note",
		Blueprint:        `{"volumes":[{"id":1,"title":"卷一"}]}`,
		Audit:            "{\"verdict\":\"needs_fix\",\"score\":28,\"summary\":\"含\\n换行与\\\"引号\\\"的体检结论\"}",
		S4Audited:        true,
		LastAuditChapter: 12,
		AuditReason:      "milestone",
		AuditStage:       StageS4,
		AuditKind:        "",
		BlueprintAudit:   `{"verdict":"pass","score":36}`,
		DriftReport:      "第3章与卷纲冲突",
		FixRounds:        2,
		EntryStage:       StageS3,
		ImportAligned:    true,
		StockAudit:       `{"verdict":"pass"}`,
		PatrolReport:     `{"verdict":"needs_fix","issues":[]}`,
		FixStates:        map[string]string{"I1": "fixed"},
		RewriteBacklog: []RewriteItem{
			{ID: "I2", Level: "P1", Area: "节奏", Desc: "中段拖沓", Chapters: []int64{4, 5}, TargetScope: TargetScopeActiveVolume},
		},
		RetconBacklog: []RewriteItem{
			{ID: "I3", Level: "P0", Area: "设定", Desc: "冻结卷冲突", TargetScope: TargetScopeFrozenVolume, Direction: DirectionRetconCandidate},
		},
		RewriteRounds:    1,
		FixLog:           []FixLogEntry{{ID: "I1", Note: "补了动机"}},
		ManualBacklog:    []ManualItem{{ID: "I4", Level: "P1", Area: "工程", Desc: "外部依赖", Reason: "skipped", Chapters: []int64{6}}},
		FixBatchFailures: 1,
		VerifyPending:    true,
	}
	b, err := json.Marshal(orig)
	require.NoError(t, err)
	return string(b), orig
}

// rawRun 绕过 Store 读口直接取库行（验瘦身 blob 与旧行为）。
func rawRun(t *testing.T, db *gorm.DB, runID string) PipelineRun {
	t.Helper()
	var raw PipelineRun
	require.NoError(t, db.Where("run_id = ?", runID).First(&raw).Error)
	return raw
}

// gateRows 取指定 run 的大对象行（kind → payload）。
func gateRows(t *testing.T, db *gorm.DB, runID string) map[string]string {
	t.Helper()
	var rows []GateObject
	require.NoError(t, db.Where("run_id = ?", runID).Find(&rows).Error)
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		m[r.Kind] = r.Payload
	}
	return m
}

func TestSplitMergeGateObjects(t *testing.T) {
	fat, orig := fatContextJSON(t)
	thin, objects := splitGateObjects(fat)

	// 9 大对象全部拆出。
	require.Len(t, objects, len(gateObjectKinds))
	for _, kind := range gateObjectKinds {
		require.Contains(t, objects, kind, "大对象 %s 应拆出", kind)
	}
	// 瘦 blob 只留决策小字段。
	var thinMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(thin), &thinMap))
	for _, kind := range gateObjectKinds {
		_, ok := thinMap[kind]
		require.False(t, ok, "瘦 blob 不应残留大对象键 %s", kind)
	}
	for _, k := range []string{"stages", "gates", "extra", "fix_states", "fix_log", "s4_audited", "audit_reason", "verify_pending"} {
		require.Contains(t, thinMap, k, "决策小字段 %s 应留在瘦 blob", k)
	}
	// merge 回胖视图与原上下文逐字段一致（含转义/换行的字符串键字节保真）。
	merged := mergeGateObjects(thin, objects)
	var got GateContext
	require.NoError(t, json.Unmarshal([]byte(merged), &got))
	require.Equal(t, orig, got)
}

func TestSplitGateObjectsEmptyValues(t *testing.T) {
	// 空串/空数组/null 不产行（等价 omitempty），且不残留瘦 blob。
	thin, objects := splitGateObjects(`{"audit":"","rewrite_backlog":[],"retcon_backlog":null,"stages":["S3"]}`)
	require.Empty(t, objects)
	require.NotContains(t, thin, "audit")
	require.NotContains(t, thin, "rewrite_backlog")
	require.NotContains(t, thin, "retcon_backlog")
	require.Contains(t, thin, "stages")
}

func TestSplitGateObjectsDegradesOnInvalidJSON(t *testing.T) {
	// 非法 JSON：原样留在 blob（宁缺勿错，读路径可见原值）。
	for _, bad := range []string{`not-json`, `[]`, `{`, ``} {
		thin, objects := splitGateObjects(bad)
		require.Equal(t, bad, thin)
		require.Empty(t, objects)
	}
}

func TestMergeGateObjectsRowPriorityAndFallback(t *testing.T) {
	// 表行优先、blob 兜底。
	merged := mergeGateObjects(`{"audit":"old","stages":["S4"]}`, map[string]string{
		gateObjAudit: `"new"`,
	})
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(merged), &m))
	require.JSONEq(t, `"new"`, string(m["audit"]), "表行应遮蔽 blob 同键旧值")
	require.JSONEq(t, `["S4"]`, string(m["stages"]), "blob 键应保留")
	// 无表行：blob 原样字节返回。
	require.Equal(t, `{"audit":"old"}`, mergeGateObjects(`{"audit":"old"}`, nil))
}

func TestStoreGateObjectRoundTrip(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	fat, orig := fatContextJSON(t)
	run := &PipelineRun{NovelID: 1, RunID: "r1", CurrentStage: StageS4, Status: StatusRunning, ContextJSON: fat}
	require.NoError(t, s.Create(ctx, run))
	// 调用方持有的 run 保持胖视图（不被落库瘦身）。
	require.Equal(t, fat, run.ContextJSON)

	// 落库形态：blob 瘦身、9 大行独立成行。
	raw := rawRun(t, db, "r1")
	var thinMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw.ContextJSON), &thinMap))
	for _, kind := range gateObjectKinds {
		_, ok := thinMap[kind]
		require.False(t, ok, "库行瘦 blob 不应残留大对象键 %s", kind)
	}
	rows := gateRows(t, db, "r1")
	require.Len(t, rows, len(gateObjectKinds))

	// 读口合并回胖视图，与原上下文逐字段一致。
	got, err := s.GetByRunID(ctx, "r1")
	require.NoError(t, err)
	var back GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &back))
	require.Equal(t, orig, back)

	// 状态/阶段写口同样拆合。
	require.NoError(t, s.SetStageStatus(ctx, "r1", StageS5, StatusRunning, got.ContextJSON))
	raw = rawRun(t, db, "r1")
	require.Equal(t, StageS5, raw.CurrentStage)
	require.NoError(t, json.Unmarshal([]byte(raw.ContextJSON), &thinMap))
	_, hasAudit := thinMap[gateObjAudit]
	require.False(t, hasAudit)
	require.Len(t, gateRows(t, db, "r1"), len(gateObjectKinds))
}

func TestGateObjectWholeBagReplacement(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	require.NoError(t, s.Create(ctx, &PipelineRun{
		NovelID: 2, RunID: "r2", CurrentStage: StageS4, Status: StatusRunning,
		ContextJSON: `{"audit":"A","rewrite_backlog":[{"id":"I1"}],"stages":["S4"]}`,
	}))
	require.Len(t, gateRows(t, db, "r2"), 2)

	// 整袋替换：新上下文键缺即清（等价 R3d 前 context_json 全量覆盖语义）。
	require.NoError(t, s.SetStatus(ctx, "r2", StatusPaused, `{"stages":["S5"]}`))
	require.Empty(t, gateRows(t, db, "r2"), "键缺即清：大对象行应随之删除")
	got, err := s.GetByRunID(ctx, "r2")
	require.NoError(t, err)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Empty(t, ctxt.Audit)
	require.Empty(t, ctxt.RewriteBacklog)
	require.Equal(t, []string{StageS5}, ctxt.Stages)
}

func TestGateObjectLegacyBlobFallbackAndMigrate(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	fat, orig := fatContextJSON(t)
	// 直接落库模拟 R3d 前的历史胖 blob（无表行）。
	require.NoError(t, db.Create(&PipelineRun{
		NovelID: 3, RunID: "r3", CurrentStage: StageS4, Status: StatusRunning, ContextJSON: fat,
	}).Error)

	// 读路径兜底：无表行时胖 blob 原样可见。
	got, err := s.GetByRunID(ctx, "r3")
	require.NoError(t, err)
	var back GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &back))
	require.Equal(t, orig, back)

	// 一次性迁移：胖 blob → 表行 + 瘦 blob，视图不变；再跑幂等跳过。
	n, err := s.MigrateGateObjects(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	raw := rawRun(t, db, "r3")
	var thinMap map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(raw.ContextJSON), &thinMap))
	_, hasAudit := thinMap[gateObjAudit]
	require.False(t, hasAudit, "迁移后 blob 应瘦身")
	require.Len(t, gateRows(t, db, "r3"), len(gateObjectKinds))
	got, err = s.GetByRunID(ctx, "r3")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &back))
	require.Equal(t, orig, back)

	n, err = s.MigrateGateObjects(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "迁移应幂等")
}

func TestGateObjectAppendKeepsLargeObjects(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	fat, orig := fatContextJSON(t)
	require.NoError(t, s.Create(ctx, &PipelineRun{
		NovelID: 4, RunID: "r4", CurrentStage: StageS4, Status: StatusAwaitingGate, ContextJSON: fat,
	}))
	require.NoError(t, s.AppendGateDecision(ctx, "r4", "G4", "continue", "ok"))
	require.NoError(t, s.AppendGateDecision(ctx, "r4", "G终", "finish", "done"))

	got, err := s.GetByRunID(ctx, "r4")
	require.NoError(t, err)
	var back GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &back))
	require.Len(t, back.Gates, 3, "原有 G1 + 追加 G4/G终 应累积")
	// 大对象经追加决策后仍完整（追加读改写走拆合，不丢行）。
	orig.Gates = back.Gates
	require.Equal(t, orig, back)
	require.Len(t, gateRows(t, db, "r4"), len(gateObjectKinds))
}

func TestGateObjectDegradesWithoutTable(t *testing.T) {
	// 大对象表缺失（如旧库未迁移）：写口退回 R3d 前「整袋落胖 blob」行为，
	// 读口胖视图照常可见——降级零提示、语义逐字一致。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&PipelineRun{}))
	s := NewStore(db)
	ctx := context.Background()

	fat, orig := fatContextJSON(t)
	require.NoError(t, s.Create(ctx, &PipelineRun{
		NovelID: 5, RunID: "r5", CurrentStage: StageS4, Status: StatusRunning, ContextJSON: fat,
	}))
	raw := rawRun(t, db, "r5")
	require.Equal(t, fat, raw.ContextJSON, "表缺失时应整袋落胖 blob")

	got, err := s.GetByRunID(ctx, "r5")
	require.NoError(t, err)
	var back GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &back))
	require.Equal(t, orig, back)

	require.NoError(t, s.AppendGateDecision(ctx, "r5", "G4", "continue", "ok"))
	got, err = s.GetByRunID(ctx, "r5")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &back))
	require.Len(t, back.Gates, 2, "原有 G1 + 追加 G4 应累积")
	require.Equal(t, orig.Audit, back.Audit, "追加决策不丢大对象")
}
