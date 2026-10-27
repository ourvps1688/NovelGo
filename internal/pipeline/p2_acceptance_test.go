package pipeline

// P2 T5.1 双验收（WBS 验收标准本体）的端到端载体：
//   ①漂移 issue 自动进既有修复/重写路由——active 侧：V 系自动分流进 RewriteBacklog
//     且自动派发豁免（只报告不改写，I 系照常重写）；frozen 侧：V 系自动分流进
//     RetconBacklog→交回人工时自动立案（kind=style，Rollback 只还原 voice 行）。
//   ②冻结卷风格可回滚——由 internal/retcon TestStyleRollbackChainOnlyTouchesVoice
//     承载（style retcon 全链 Rollback 后 voice 行回冻结态、事实层不动）。

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/stretchr/testify/require"
)

// TestP2Acceptance_DriftAutoRouteAndExempt 是验收①的 active 侧端到端：
// 体检 V 系（文风漂移）issue 自动分流进 RewriteBacklog（进既有路由，零新路由），
// 自动章节重写只派发非豁免的 I 系——V 系涉及章节零自动改写。
func TestP2Acceptance_DriftAutoRouteAndExempt(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	issueV := `{"id":"V1","level":"P1","area":"文风漂移·章节#3","desc":"第3章节奏基线偏离","fix_type":"body",` +
		`"target_scope":"active_volume","dimension":"none","direction":"forward_fix"}`
	issueI := `{"id":"I3","level":"P0","area":"开篇过签力","desc":"第4章未立人设","fix_type":"body"}`
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"验收场","issues":[` + issueV + `,` + issueI + `]}`

	var rewrittenIDs []int64
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "chapter-rewrite":
			rewrittenIDs = append(rewrittenIDs, extractChapterIDFromPrompt(t, input.Prompt))
			return SkillOutput{Text: "已整章重写"}, nil
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" +
				`{"verdict":"needs_fix","score":16,"summary":"复评仍报","issues":[` + issueV + `,` + issueI + `]}` +
				"\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 471, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	// 模拟 S4 体检已落发现项工单（R5-2 路由对既有工单行做状态同步，不新建）：否则
	// executeFix 的 syncIssueRow（UpdateIssueStatus）因缺行而静默无操作，V1/I3 落不了 rewrite 态。
	require.NoError(t, svc.store.ReplaceIssues(ctx, 471, run.RunID, 0, parseAudit(auditJSON).Issues),
		"P2 验收：模拟 S4 落发现项工单失败")

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	// R5-2：检测结果路由收口为发现项工单，统一落 paused 交回用户（不自动派发修复/重写）。
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	// 自动进路由：V 系与 I 系都沉淀进 RewriteBacklog（V 系供作者裁决）。
	var backlogIDs []string
	for _, it := range finalCtxt.RewriteBacklog {
		backlogIDs = append(backlogIDs, it.ID)
	}
	require.ElementsMatch(t, []string{"V1", "I3"}, backlogIDs, "V 系应自动分流进 RewriteBacklog")
	// R5-2 自动重写摘除：fix 路由收口零自动派发章节重写（重写由作者在工单面板显式触发）；
	// V 系本就只报告不改写，I 系亦不再自动重写——故 rewrittenIDs 应为空。
	require.Empty(t, rewrittenIDs, "R5-2 摘除自动重写：fix 路由收口零自动派发章节重写")

	// 路由结果已沉淀为发现项工单（rewrite 态），供工单面板处理。
	rows, rerr := svc.store.ListIssues(ctx, 471, run.RunID)
	require.NoError(t, rerr)
	byKey := map[string]string{}
	for _, r := range rows {
		byKey[r.IssueKey] = r.Status
	}
	require.Equal(t, IssueStatusRewrite, byKey["V1"], "V 系路由为重写工单（只报告不改写）")
	require.Equal(t, IssueStatusRewrite, byKey["I3"], "I 系路由为重写工单（待作者显式重写）")
}

// TestP2Acceptance_DriftRetconFiling 是验收①的 frozen 侧端到端：
// 命中冻结卷写保护的 V 系 issue 自动分流进 RetconBacklog，交回人工裁决时自动
// 沉淀为 retcon 候选案（kind=style：Rollback 走 RestoreStyle 只还原 voice 行）。
func TestP2Acceptance_DriftRetconFiling(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(472)
	gate := retconGateOn(t, svc, db)

	runID := startS3Run(t, svc, novelID, "")
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"验收场","issues":[` +
		`{"id":"V1","level":"P1","area":"文风漂移·章节#7","desc":"冻结卷第7章文风指标偏离基线","fix_type":"body",` +
		`"target_scope":"frozen_volume","dimension":"none","direction":"retcon_candidate"}]}`

	var fixDonePayload map[string]any
	defer swapEmitFixDone(func(_ context.Context, p map[string]any) { fixDonePayload = p })()

	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, runID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, runID, "fix", ""))
	waitForStageStatus(t, svc, runID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, runID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Len(t, finalCtxt.RetconBacklog, 1, "frozen V 系应自动分流进 RetconBacklog")
	require.Equal(t, "V1", finalCtxt.RetconBacklog[0].ID)

	// 自动立案：kind=style（风格回改，Rollback 只还原 voice 行）。
	rows, err := gate.ListByNovel(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "V 系应自动沉淀为 retcon 候选案")
	require.Equal(t, retcon.KindStyle, rows[0].Kind, "V 系立案应带 style")
	require.Equal(t, retcon.OriginPipeline, rows[0].Origin)
	require.Equal(t, retcon.StatusCandidate, rows[0].Status)

	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["retcon_handoff"], "fix_done 应带 retcon_handoff（前端零改动）")
}
