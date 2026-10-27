package pipeline

// detect_test.go — R5-3 T1.1 检测-only 执行核心验收（WBS r5-3 §1 T1.1）：
// ① 核心跑完落工单/报告但零 auto-advance/fix/gate（run 状态不被推进、零事件广播）；
// ② executeAudit/executeFinalAudit 流水线路径行为零变化由既有测试网锁定
//    （TestServiceExecuteAudit* / TestOrchestratorS4* / issue_test / quality_dual_test 等）；
// ③ 降级逐字一致（检测失败=原样返回 err、零半截落库、零广播）。

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// detectAuditFixture 是体检结构化产出（契约字段齐全，供落工单/清单断言）。
const detectAuditFixture = `{"verdict":"needs_fix","score":20,"summary":"检测出2条问题","issues":[` +
	`{"id":"I1","level":"P0","area":"开篇","desc":"前三章未立人设","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[1,2,3]},` +
	`{"id":"I2","level":"P1","area":"长线","desc":"伏笔未回收","fix_type":"ledger","target_scope":"active_volume","dimension":"timeline","direction":"forward_fix","chapter_ids":[]}` +
	`]}`

// TestRunDetectionLandsTicketsWithoutAdvancing 验收①：检测-only 核心跑完——
// issue 工单整批落表、结果写回上下文（含断点重置/清单重算），但零 auto-advance、
// 零修复派发、零闸门广播：run 阶段/状态/上下文一概不被核心推进或改写（持久化
// 归调用方裁决路径），检测与流水线阶段解耦。
func TestRunDetectionLandsTicketsWithoutAdvancing(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		require.Equal(t, "signability-audit", name)
		return SkillOutput{Text: "结论\n```json\n" + detectAuditFixture + "\n```"}, nil
	}

	run, err := svc.StartPipeline(ctx, 480, "new", nil, false)
	require.NoError(t, err)
	origCtxt := `{"fix_states":{"I9":"fixed"},"fix_log":[{"id":"I9","note":"旧日志"}]}`
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, origCtxt))

	// 事件捕获在建 run 之后注册（StartPipeline 自身会广播启动阶段事件，不属检测核心）。
	var stageEmits, gateEmits, fixStarts, fixDones int
	t.Cleanup(swapEmitStage(func(_ context.Context, _ StagePayload) { stageEmits++ }))
	t.Cleanup(swapEmitGate(func(_ context.Context, _ GatePayload) { gateEmits++ }))
	t.Cleanup(swapEmitFixStart(func(_ context.Context, _ map[string]any) { fixStarts++ }))
	t.Cleanup(swapEmitFixDone(func(_ context.Context, _ map[string]any) { fixDones++ }))

	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(origCtxt), &ctxt))
	out, err := svc.runDetection(ctx, detectRequest{
		runID:     run.RunID,
		novelID:   run.NovelID,
		skill:     "signability-audit",
		prompt:    "体检提示词",
		sessionID: "detect:test",
		maxTurns:  4,
		kind:      "",
		landing:   detectLandNewRound,
		label:     "检测任务",
	}, &ctxt, origCtxt)
	require.NoError(t, err)

	// 落工单/报告：新一轮清单落法全量生效。
	rows, lerr := svc.store.ListIssues(ctx, run.NovelID, run.RunID)
	require.NoError(t, lerr)
	require.Len(t, rows, 2, "检测结果应整批落发现项工单")
	require.Equal(t, "I1", rows[0].IssueKey)
	require.Equal(t, "needs_fix", out.Audit.Verdict)
	require.Contains(t, out.Context.Audit, "检测出2条问题", "报告应写回上下文")
	require.Nil(t, out.Context.FixStates, "新一轮清单应清修复断点")
	require.Nil(t, out.Context.FixLog, "新一轮清单应清修复日志")
	require.Len(t, out.Context.RewriteBacklog, 1, "正文级问题应重算进重写清单")
	require.Equal(t, "I1", out.Context.RewriteBacklog[0].ID)
	require.Len(t, out.Context.RetconBacklog, 0)

	// 零 auto-advance/fix/gate：run 状态不被推进、上下文不被核心持久化。
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StageS3, got.CurrentStage, "检测-only 不得推进流水线阶段")
	require.Equal(t, StatusRunning, got.Status, "检测-only 不得改写运行状态")
	require.JSONEq(t, origCtxt, got.ContextJSON, "检测-only 核心不持久化上下文（裁决归调用方）")
	require.Zero(t, stageEmits, "检测-only 零阶段事件")
	require.Zero(t, gateEmits, "检测-only 零闸门广播")
	require.Zero(t, fixStarts+fixDones, "检测-only 零修复派发/回执")
}

// TestRunDetectionReportOnlyKeepsBreakpoints 验收①配套：report-only 落法（S6 终检
// 现行为）只写报告字段，不动工单/断点/清单——与新一轮落法严格区分，流水线行为零变化。
func TestRunDetectionReportOnlyKeepsBreakpoints(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "```json\n" + detectAuditFixture + "\n```"}, nil
	}
	run, err := svc.StartPipeline(ctx, 481, "new", nil, false)
	require.NoError(t, err)

	origCtxt := `{"fix_states":{"I9":"fixed"},"manual_backlog":[{"id":"I8","level":"P1","area":"a","desc":"d","reason":"r","chapters":[]}]}`
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(origCtxt), &ctxt))
	out, err := svc.runDetection(ctx, detectRequest{
		runID: run.RunID, novelID: run.NovelID,
		skill: "signability-audit", prompt: "终检提示词", sessionID: "detect:test2", maxTurns: 4,
		kind: "", landing: detectLandReportOnly, label: "S6 终检",
	}, &ctxt, origCtxt)
	require.NoError(t, err)

	require.Contains(t, out.Context.Audit, "检测出2条问题", "报告应写回上下文")
	require.Equal(t, map[string]string{"I9": "fixed"}, out.Context.FixStates, "report-only 不得清修复断点")
	require.Len(t, out.Context.ManualBacklog, 1, "report-only 不得清人工工单")
	require.Nil(t, out.Context.RewriteBacklog, "report-only 不得重算重写清单")

	rows, lerr := svc.store.ListIssues(ctx, run.NovelID, run.RunID)
	require.NoError(t, lerr)
	require.Empty(t, rows, "report-only 不落发现项工单")
}

// TestRunDetectionDegradesOnFailure 验收③：派发失败原样返回 err，零半截落库、
// 零事件广播（降级逐字一致：失败不改检测口径、不给流水线添堵）。
func TestRunDetectionDegradesOnFailure(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{}, errors.New("llm down")
	}
	run, err := svc.StartPipeline(ctx, 482, "new", nil, false)
	require.NoError(t, err)

	var gateEmits, stageEmits int
	t.Cleanup(swapEmitGate(func(_ context.Context, _ GatePayload) { gateEmits++ }))
	t.Cleanup(swapEmitStage(func(_ context.Context, _ StagePayload) { stageEmits++ }))

	ctxt := GateContext{}
	out, derr := svc.runDetection(ctx, detectRequest{
		runID: run.RunID, novelID: run.NovelID,
		skill: "signability-audit", prompt: "p", sessionID: "detect:test3", maxTurns: 4,
		kind: "", landing: detectLandNewRound, label: "检测任务",
	}, &ctxt, "")
	require.Error(t, derr, "派发失败应原样返回错误，由调用方按自身失败通道落地")
	require.Empty(t, out.Context.Audit, "失败不得落半截报告")
	require.Zero(t, gateEmits+stageEmits, "失败零广播")

	rows, lerr := svc.store.ListIssues(ctx, run.NovelID, run.RunID)
	require.NoError(t, lerr)
	require.Empty(t, rows, "失败不得落半截工单")
}
