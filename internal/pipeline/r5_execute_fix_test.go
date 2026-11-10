package pipeline

// R5-2 测试迁移载体（提案 §10.3）：S5 检测结果路由收口为「发现项工单」，修复执行改工单
// 修复一律由 chat 人工完成，复验由用户显式触发；后台自动修复执行体已整体下线。
// 本文件覆盖 R5-2 执行路径的关键语义：executeFix 四分区路由、工单修复执行（台账级 /
// 正文级重定向重写 / V 系与写保护退回）、熔断对修复派发的护栏、修复后蓝图巡检。
//
// 工单状态机 / 面板动作 / VerifyIssueTickets 的验收见 issue_tickets_test.go；陈旧体检
// 守卫见 service_test.go 的 TestServiceFixRejectsStaleAudit*；retcon 交回见
// service_test.go 的 TestServiceFixRetconHandoffWhenAllProtected 与
// TestServiceAutoRewriteSkipsProtectedBodyItems。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// startS5Run 把流水线钉在 S5/running 并写入给定审计 JSON（绕过 S1-S4 异步路径以便精确控制）。
// 同时按真实流程把审计清单落为发现项工单（模拟 S4 体检的 ReplaceIssues 契约）——R5-2 的 fix
// 路由对台账级 pending 问题只流转既有工单、不新建。
func startS5Run(t *testing.T, svc *Service, novelID int64, auditJSON string) string {
	t.Helper()
	run, err := svc.StartPipeline(context.Background(), novelID, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(context.Background(), run.RunID, StageS5, StatusRunning, string(ctxtJSON)))
	if auditData := parseAudit(auditJSON); len(auditData.Issues) > 0 {
		require.NoError(t, svc.store.ReplaceIssues(context.Background(), novelID, run.RunID, 0, auditData.Issues),
			"startS5Run：模拟体检落发现项工单失败")
	}
	return run.RunID
}

// TestServiceExecuteFixRoutesToTickets 验收 R5-2 四分区路由：台账级→待修工单、正文级→重写
// 工单、V 系文风漂移→重写工单（只报告不改写）、冻结卷写保护→retcon 候选工单；路由落盘后
// 统一交回用户（settleFixToTickets：落 paused + fix_done tickets:true），**全程零自动派发**
// （不派发任何修复 agent / 复评，修复动作由人显式触发）。
func TestServiceExecuteFixRoutesToTickets(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(701)

	var dispatched []string
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		dispatched = append(dispatched, name)
		return SkillOutput{Text: "ok"}, nil
	}

	// I1 台账级（active）→ 待修；I2 正文级（active）→ 重写；V1 文风漂移（active）→ 重写（豁免派发）；
	// I3 冻结卷（frozen）→ retcon 候选。
	auditJSON := `{"verdict":"needs_fix","score":12,"summary":"混合清单","issues":[` +
		`{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P0","area":"开篇过签力","desc":"前三章未立人设","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[3]},` +
		`{"id":"V1","level":"P1","area":"文风漂移·章节#4","desc":"第4章文风指标偏离基线","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I3","level":"P1","area":"设定自洽","desc":"与前卷冲突","fix_type":"ledger","target_scope":"frozen_volume","dimension":"character","direction":"retcon_candidate"}]}`

	var fixDonePayload map[string]any
	t.Cleanup(swapEmitFixDone(func(_ context.Context, p map[string]any) { fixDonePayload = p }))

	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID)

	// 零自动派发（修复/重写/复评全由人显式触发）。
	require.Empty(t, dispatched, "executeFix 路由收口不得自动派发任何修复/重写/复评")

	got, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, StageS5, got.CurrentStage)
	require.Equal(t, StatusPaused, got.Status, "路由收口应统一落 paused 交回用户")

	require.NotNil(t, fixDonePayload, "应广播 fix_done")
	require.Equal(t, true, fixDonePayload["tickets"], "fix_done 应带 tickets 标记（去工单面板处理）")

	rows, lerr := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, lerr)
	byKey := map[string]string{}
	for _, r := range rows {
		byKey[r.IssueKey] = r.Status
	}
	require.Equal(t, IssueStatusPending, byKey["I1"], "台账级→待修工单")
	require.Equal(t, IssueStatusRewrite, byKey["I2"], "正文级→重写工单")
	require.Equal(t, IssueStatusRewrite, byKey["V1"], "V 系→重写工单（只报告不改写）")
	require.Equal(t, IssueStatusRetcon, byKey["I3"], "冻结卷→retcon 候选工单")
}

// TestServiceExecuteFixSettleAllResolved 验收断点续路由边界：审计清单全部已解决
// （fixed/rewrite/retcon）时，executeFix 不派发、不假报完成，统一收口（total=0）落 paused +
// tickets:true，提示「已全部收口」。
func TestServiceExecuteFixSettleAllResolved(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(702)

	var dispatched []string
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		dispatched = append(dispatched, name)
		return SkillOutput{Text: "ok"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"成稿质量","desc":"x","fix_type":"ledger","target_scope":"active_volume"}]}`
	run, rerr := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, rerr)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4, FixStates: map[string]string{"I1": "fixed"}}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning, string(ctxtJSON)))

	var fixDonePayload map[string]any
	t.Cleanup(swapEmitFixDone(func(_ context.Context, p map[string]any) { fixDonePayload = p }))

	svc.executeFix(run.RunID, novelID)

	require.Empty(t, dispatched, "全已解决不得派发")
	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StatusPaused, got.Status)
	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["tickets"], "收口仍带 tickets 标记")
	require.Contains(t, fmt.Sprint(fixDonePayload["error"]), "已全部收口", "全收口应提示去面板查看历史工单")
}
