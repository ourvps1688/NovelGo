package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// 测试中无 Wails runtime 上下文，将事件发射器替换为无操作，避免
// "invalid context" 报错；真实运行仍走 wails 事件总线。
func init() {
	emitStage = func(_ context.Context, _ StagePayload) {}
	emitGate = func(_ context.Context, _ GatePayload) {}
	emitControlAck = func(_ context.Context, _ ControlAckPayload) {}
	emitStockAudit = func(_ context.Context, _ map[string]any) {}
	emitConsistencyPatrol = func(_ context.Context, _ map[string]any) {}
	emitFixStart = func(_ context.Context, _ map[string]any) {}
	emitFixDone = func(_ context.Context, _ map[string]any) {}
	emitRewriteBacklog = func(_ context.Context, _ map[string]any) {}
}

func TestServiceStartAndResume(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 100, "new", nil, false)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if run.CurrentStage != StageS1 || run.Status != StatusRunning {
		t.Fatalf("unexpected run: %+v", run)
	}
	// S1 -> 确认 -> 应进入 S2 并异步执行蓝图，最终落到 G1 闸门（awaiting_gate）
	if err := svc.ResumePipeline(ctx, run.RunID, "continue", ""); err != nil {
		t.Fatalf("resume1: %v", err)
	}
	// S2 蓝图为异步执行，轮询等待闸门落定（无 agent 时 skillRunner 为空操作，应很快完成）。
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	got, _ := svc.GetPipelineState(ctx, 100)
	if got.CurrentStage != StageS2 || got.Status != StatusAwaitingGate {
		t.Fatalf("after resume1: %+v", got)
	}
	// 闸门确认 -> 进入 S3 写作
	if err := svc.ResumePipeline(ctx, run.RunID, "continue", ""); err != nil {
		t.Fatalf("resume2: %v", err)
	}
	got, _ = svc.GetPipelineState(ctx, 100)
	if got.CurrentStage != StageS3 || got.Status != StatusRunning {
		t.Fatalf("after resume2: %+v", got)
	}
}

func TestServiceControl(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, _ := svc.StartPipeline(ctx, 200, "new", nil, false)
	if err := svc.ControlPipeline(ctx, run.RunID, "pause"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ := svc.GetPipelineState(ctx, 200)
	if got.Status != StatusPaused {
		t.Fatalf("not paused: %s", got.Status)
	}
	if err := svc.ControlPipeline(ctx, run.RunID, "resume"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, _ = svc.GetPipelineState(ctx, 200)
	if got.Status != StatusRunning {
		t.Fatalf("not running: %s", got.Status)
	}
	if err := svc.ControlPipeline(ctx, run.RunID, "stop"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	// aborted 后 GetPipelineState(按 novel) 不再返回该 run（已排除 finished/aborted），
	// 故用按 RunID 的方式核验终态。
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	if gerr != nil {
		t.Fatalf("getByRunID after stop: %v", gerr)
	}
	if got.Status != StatusAborted {
		t.Fatalf("not aborted: %s", got.Status)
	}
}

func TestServiceResumeAbort(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, _ := svc.StartPipeline(ctx, 300, "new", nil, false)
	if err := svc.ResumePipeline(ctx, run.RunID, "abort", ""); err != nil {
		t.Fatalf("abort: %v", err)
	}
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	if gerr != nil || got.Status != StatusAborted {
		t.Fatalf("not aborted: status=%s err=%v", gotStatusSafe(got), gerr)
	}
	// 已中止不可再推进
	if err := svc.ResumePipeline(ctx, run.RunID, "continue", ""); err == nil {
		t.Fatalf("已中止的流水线不应可推进")
	}
}

// waitForStageStatus 轮询流水线状态直到匹配 target，超时则失败。
func waitForStageStatus(t *testing.T, svc *Service, runID, target string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := svc.store.GetByRunID(context.Background(), runID)
		if err == nil && r.Status == target {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待状态 %s 超时 (run=%s)", target, runID)
}

func gotStatusSafe(r *PipelineRun) string {
	if r == nil {
		return "<nil>"
	}
	return r.Status
}

// TestParseAuditExtractsJSON 验证 parseAudit 能从 skill 输出中抽取 ```json 结构化块。
func TestParseAuditExtractsJSON(t *testing.T) {
	text := "分析如下：\n```json\n" +
		`{"verdict":"pass","score":30,"summary":"ok","issues":[{"id":"I1","level":"P1","area":"x","desc":"y"}]}` +
		"\n```"
	data := parseAudit(text)
	require.Equal(t, "pass", data.Verdict)
	require.Equal(t, 30, data.Score)
	require.Len(t, data.Issues, 1)
	require.Equal(t, "P1", data.Issues[0].Level)
}

// TestParseAuditDegradesOnGarbage 验证 parseAudit 在输出无 JSON 块时退化为单条 P0 issue。
func TestParseAuditDegradesOnGarbage(t *testing.T) {
	data := parseAudit("完全不是 JSON 的乱码文本")
	require.Equal(t, "needs_fix", data.Verdict)
	require.Len(t, data.Issues, 1)
	require.Equal(t, "P0", data.Issues[0].Level)
	require.Contains(t, data.Summary, "乱码")
}

// TestServiceGoldenThreeTriggersAudit 验证：写完黄金三章（chapterCount≥3）且尚未体检时，
// S3 的「继续」会自动进入 S4 签约体检，异步体检完成后着陆 G4 闸门且 audit 写入 ContextJSON。
func TestServiceGoldenThreeTriggersAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	// 模拟已写完黄金三章
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 3, nil }
	// stub 体检返回结构化 issue 清单
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"开篇吸引力不足","issues":[{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章钩子弱"}]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			return SkillOutput{Text: "体检结论：\n```json\n" + auditJSON + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}
	ctx := context.Background()
	// 入口判定：启动时尚无章节 → 从 S1 孵化起步（之后再把章数置为 3 模拟写完黄金三章）。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }
	run, err := svc.StartPipeline(ctx, 400, "new", nil, false)
	require.NoError(t, err)
	// 进入 S3 后模拟已写完黄金三章（3 章），供首检触发判定。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 3, nil }
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)              // 等 S2 蓝图落闸
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S2->S3
	got, _ := svc.GetPipelineState(ctx, 400)
	require.Equal(t, StageS3, got.CurrentStage)

	// S3 写完一章后「继续」：黄金三章已达成且未体检 → 自动进入 S4 体检
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))

	// 异步 executeAudit 应把体检结果写回并落 G4 闸门
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage)
	require.Contains(t, got.ContextJSON, "audit", "ContextJSON 应写入 audit 字段")
	require.Contains(t, got.ContextJSON, "needs_fix", "audit 应保留 verdict")

	// 体检完成后 S4Audited 仍应为 false（仅当用户在 G4 选 continue 才置位，避免回 S3 后重复触发）
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.False(t, ctxt.S4Audited, "体检完成但用户尚未出 G4 时不应置 S4Audited")
}

// TestServiceAuditContinueMarksAudited 验证：G4 选 continue（通过/强制继续）后，
// 置 S4Audited=true 并回到 S3 写作，后续 S3 不再自动触发体检。
func TestServiceAuditContinueMarksAudited(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "ok"}, nil
	}
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 401, "new", nil, false)
	require.NoError(t, err)
	// 直接进入 S4（已在 G4 闸门前，模拟已体检），context 含 audit
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning,
		`{"audit":"{\"verdict\":\"needs_fix\",\"score\":20,\"summary\":\"需打磨\",\"issues\":[]}"}`))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS3, got.CurrentStage, "通过/继续后应回到 S3 写作")
	require.Equal(t, StatusRunning, got.Status)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.True(t, ctxt.S4Audited, "continue 后应置 S4Audited，避免 S3 重复触发体检")
}

// TestServiceAuditFixLoopReAudit 验证：G4 选 fix 后进入 S5 修复闭环，
// 修复完成后自动回到 S4 复评，复评结果（pass）重写 ContextJSON.Audit。
// TestServiceRecoverInterruptedAndResume 验证启动恢复闭环：
// 1) status=running 的僵尸运行被 RecoverInterruptedRuns 置为 paused 并打 interrupted 标记（幂等）；
// 2) resume 后按阶段重新派发执行体（S5 → executeFix → emitFixStart），最终回到 S4 闸门。
func TestServiceRecoverInterruptedAndResume(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var fixStarts int
	origFixStart := emitFixStart
	emitFixStart = func(_ context.Context, _ map[string]any) { fixStarts++ }
	t.Cleanup(func() { emitFixStart = origFixStart })

	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			return SkillOutput{Text: "已按 issue 修复：强化开篇钩子"}, nil
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n{\"verdict\":\"needs_fix\",\"score\":10,\"summary\":\"仍有问题\",\"issues\":[]}\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 403, "new", nil, false)
	require.NoError(t, err)
	// 模拟僵尸运行：S5 running（执行体已随旧进程消亡），带 audit（含待修问题）与 audit_stage 供复评回家。
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning,
		`{"audit":"{\"verdict\":\"needs_fix\",\"issues\":[{\"id\":\"I1\",\"level\":\"P0\",\"area\":\"总体\",\"desc\":\"开场灌水\"}]}","audit_stage":"S4"}`))

	// 启动恢复：running → paused + interrupted 标记。
	svc.RecoverInterruptedRuns()
	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StatusPaused, got.Status)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.True(t, ctxt.Interrupted, "应打上中断标记")

	// 幂等：再次恢复不应改变状态。
	svc.RecoverInterruptedRuns()
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StatusPaused, got.Status)

	// 用户点恢复：回 running + 按阶段重新派发（S5 → executeFix）。
	require.NoError(t, svc.ControlPipeline(ctx, run.RunID, "resume"))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage, "复评应回到 audit_stage=S4 闸门")
	require.Equal(t, 1, fixStarts, "恢复应重新派发一次修复（emitFixStart）")

	// 恢复后 interrupted 标记应被清除。
	var ctxt2 GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt2))
	require.False(t, ctxt2.Interrupted, "派发后应清除中断标记")
}

// TestServiceFixBatchingAndResume 验证 S5 分批修复 + 失败恢复：
// 3 个问题（1 P0 + 2 P1）→ P0 独占一批、P1 两条一批；首批失败落地为 paused（断点保留），
// 恢复后从剩余问题继续（跳过已修复项），最终复评通过落 G4 闸门。
func TestServiceFixBatchingAndResume(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"三处待修","issues":[` +
		`{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"五章程序文","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I3","level":"P1","area":"人设爽点","desc":"情感钩子缺失","fix_type":"ledger","target_scope":"active_volume","dimension":"character","direction":"forward_fix"}]}`
	secondAudit := `{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`
	var fixPrompts []string
	var fixSessions []string
	var auditCalls int
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			// 初检结果由测试直接注入 ctxt.Audit，桩里的 audit 调用均为修复后的复评。
			auditCalls++
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			fixPrompts = append(fixPrompts, input.Prompt)
			fixSessions = append(fixSessions, input.SessionID)
			if len(fixPrompts) == 1 {
				// 首批（I1）模拟 MaxTurns 耗尽失败。
				return SkillOutput{}, fmt.Errorf("skill \"signability-fix\" 执行失败: 已达最大轮数，对话结束")
			}
			return SkillOutput{Text: "已修复"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	// 捕获 fix_start 载荷，断言 FixIssuePayload 携带作用域三字段（供前端标签/写保护判路）。
	var firstFixStart map[string]any
	origFixStart := emitFixStart
	emitFixStart = func(_ context.Context, p map[string]any) {
		if firstFixStart == nil {
			firstFixStart = p
		}
	}
	t.Cleanup(func() { emitFixStart = origFixStart })

	run, err := svc.StartPipeline(ctx, 403, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	// fix → S5：首批失败应落地 paused（而非停在 running 无人执行）
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusPaused)
	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS5, got.CurrentStage, "失败后应停在 S5")
	var pausedCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &pausedCtxt))
	require.Empty(t, pausedCtxt.FixStates, "首批失败，尚无已完成问题")
	require.False(t, pausedCtxt.Dispatching, "执行体已退出，标记应清除")

	// 恢复：从剩余问题继续（首批重试 + 第二批），最终复评通过
	require.NoError(t, svc.ControlPipeline(ctx, run.RunID, "resume"))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage, "复评后应在 S4 闸门")
	require.Equal(t, 1, auditCalls, "修复后应跑一次体检做复评（初检由测试直接注入）")
	require.Len(t, fixPrompts, 3, "首批失败重试 + 第二批 = 3 次修复调用")
	require.Contains(t, fixPrompts[0], "I1")
	require.NotContains(t, fixPrompts[0], "I2", "P0 应独占一批")
	require.Contains(t, fixPrompts[1], "I1", "恢复后首批应重试未完成的 I1")
	require.Contains(t, fixPrompts[2], "I2")
	require.Contains(t, fixPrompts[2], "I3")
	require.NotContains(t, fixPrompts[2], "I1", "已修复项不应重复修复")
	for i, sid := range fixSessions {
		for j := i + 1; j < len(fixSessions); j++ {
			require.NotEqual(t, sid, fixSessions[j], "每批应使用独立会话，避免历史挤占轮数")
		}
	}
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	// 复评落地时 executeAudit 会重置 FixStates（新一轮体检 = 全新问题清单，旧断点作废）。
	// 批次断点正确性已由上方调用序列验证：恢复后首批重试 I1、已修项不重复修复。
	require.Empty(t, finalCtxt.FixStates, "复评落地后旧修复断点应被重置")
	require.Equal(t, 1, finalCtxt.FixRounds, "一次完整 sweep 递增一轮")

	// fix_start 载荷应携带作用域三字段（batchIssues/allIssues 两处构造点均拷贝）。
	require.NotNil(t, firstFixStart, "应广播 fix_start")
	all0, ok := firstFixStart["all_issues"].([]FixIssuePayload)
	require.True(t, ok, "all_issues 应为 []FixIssuePayload")
	require.NotEmpty(t, all0)
	require.Equal(t, TargetScopeActiveVolume, all0[0].TargetScope, "FixIssuePayload 应携带 target_scope")
	require.Equal(t, "none", all0[0].Dimension, "FixIssuePayload 应携带 dimension")
	require.Equal(t, DirectionForwardFix, all0[0].Direction, "FixIssuePayload 应携带 direction")
	batch0, ok := firstFixStart["issues"].([]FixIssuePayload)
	require.True(t, ok, "issues 应为 []FixIssuePayload")
	require.NotEmpty(t, batch0)
	require.Equal(t, TargetScopeActiveVolume, batch0[0].TargetScope, "批次清单也应携带 target_scope")
}

// TestServiceBodyIssuesRoutedToRewriteBacklog 验证正文级（fix_type=body）问题被显式路由到
// S3 重写回路：不进修复批次（修复 agent 看不到）、标记 FixStates=rewrite、沉淀进 RewriteBacklog，
// 从而消除 S5 对修不动问题的空转，闭合修复链。
func TestServiceBodyIssuesRoutedToRewriteBacklog(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 初检：I1 正文级（需章节重写），I2 台账级（S5 可修）。
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"五章程序文","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	// 复评：I2 已修（不在清单），I1 正文问题仍在（仍未重写）。
	secondAudit := `{"verdict":"needs_fix","score":20,"summary":"修复后仍有正文问题","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱","fix_type":"body"}]}`

	var fixPrompts []string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			fixPrompts = append(fixPrompts, input.Prompt)
			return SkillOutput{Text: "已修复"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 406, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	// 正文级 I1 必须沉淀进重写清单（复评后仍在该清单，因复评仍报出它）。
	require.Len(t, finalCtxt.RewriteBacklog, 1, "正文级问题应流入 RewriteBacklog")
	require.Equal(t, "I1", finalCtxt.RewriteBacklog[0].ID)
	// 复评阶段 FixStates 被重置——此处只断言重写清单出口正确；修复 agent 是否触及 I1 由下方断言。
	require.Len(t, fixPrompts, 1, "仅台账级 I2 应进入一次修复批次，正文级 I1 不应进批次")
	require.Contains(t, fixPrompts[0], "I2", "修复批次应包含台账级 I2")
	require.NotContains(t, fixPrompts[0], "I1", "修复批次不应包含正文级 I1（已路由到 S3 重写）")
}

// TestServiceFixHandoffRewriteWhenOnlyBodyIssues 验证：当体检问题全部为正文级（fix_type=body）、
// 无可修台账项时，executeFix 必须「交回重写」而非谎报「修复完成」后立刻复评
// （否则复评必复报同一批问题，形成「完成但零修复」死循环）。
func TestServiceFixHandoffRewriteWhenOnlyBodyIssues(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 全部为正文级问题：S5 改不动，必须回流 S3 重写。
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"正文问题","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"中段节奏拖沓","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`

	var fixCalled, auditCalled bool
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-fix" {
			fixCalled = true
		}
		if name == "signability-audit" {
			auditCalled = true
		}
		return SkillOutput{Text: "ok"}, nil
	}

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	run, err := svc.StartPipeline(ctx, 407, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	// 交回重写后阶段应暂停（而非进入复评闸门）。
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.False(t, fixCalled, "无台账级问题，修复 agent 不应被调用")
	require.False(t, auditCalled, "交回重写不应触发复评（避免复报同一批问题）")
	require.Len(t, finalCtxt.RewriteBacklog, 2, "两个正文级问题都应流入 RewriteBacklog")
	require.NotNil(t, fixDonePayload, "应广播 fix_done")
	require.Equal(t, true, fixDonePayload["rewrite_handoff"], "fix_done 应带 rewrite_handoff 标记")
	require.Equal(t, int(2), fixDonePayload["rewrite_count"], "rewrite_count 应为 2")
}

// TestServiceFixRejectsStaleAuditWithoutFixType 验证陈旧 run 守卫：fix_type 特性落地前创建的
// 旧版体检结果其 issue 全部缺省 fix_type 字段，若直接按 ledger 派发会耗尽轮数失败且正文回流
// 永不触发。executeFix 应拒绝派发、明确交回用户重跑体检（stale_audit=true、暂停），而非默默跑空。
func TestServiceFixRejectsStaleAuditWithoutFixType(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 旧版 audit：两条 issue 均无 fix_type（新代码应判定为陈旧、拒绝派发）。
	auditJSON := `{"verdict":"needs_fix","score":20,"summary":"旧版体检","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"中段节奏拖沓"}]}`

	var fixCalled, auditCalled bool
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-fix" {
			fixCalled = true
		}
		if name == "signability-audit" {
			auditCalled = true
		}
		return SkillOutput{Text: "ok"}, nil
	}

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	run, err := svc.StartPipeline(ctx, 408, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.False(t, fixCalled, "陈旧体检不应派发修复 agent（避免耗尽轮数空跑）")
	require.False(t, auditCalled, "陈旧体检不应触发复评")
	require.NotNil(t, fixDonePayload, "应广播 fix_done")
	require.Equal(t, false, fixDonePayload["ok"], "陈旧体检应 ok=false")
	require.Equal(t, true, fixDonePayload["stale_audit"], "fix_done 应带 stale_audit 标记")
	require.Contains(t, fmt.Sprint(fixDonePayload["error"]), "fix_type", "错误提示应说明缺 fix_type 字段")
}

// TestServiceFixRejectsStaleAuditWithoutScopeFields 验证陈旧 run 守卫 2：卷级作用域特性
// 落地前的体检结果缺 target_scope/dimension/direction 字段，无法做冻结卷写保护与前向默认
// 路由。executeFix 应拒绝派发、明确交回用户重跑体检（stale_audit=true、暂停），而非带盲区派发。
// 蓝图体检豁免（不涉卷作用域）由既有蓝图修复测试保障不回归。
func TestServiceFixRejectsStaleAuditWithoutScopeFields(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 旧版 audit：带 fix_type（过守卫 1）但两条 issue 均无作用域三字段（应触发守卫 2）。
	auditJSON := `{"verdict":"needs_fix","score":20,"summary":"旧版体检","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱","fix_type":"body"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"中段节奏拖沓","fix_type":"ledger"}]}`

	var fixCalled, auditCalled bool
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-fix" {
			fixCalled = true
		}
		if name == "signability-audit" {
			auditCalled = true
		}
		return SkillOutput{Text: "ok"}, nil
	}

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	run, err := svc.StartPipeline(ctx, 409, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.False(t, fixCalled, "缺作用域字段的体检不应派发修复 agent（避免带盲区派发）")
	require.False(t, auditCalled, "陈旧体检不应触发复评")
	require.NotNil(t, fixDonePayload, "应广播 fix_done")
	require.Equal(t, false, fixDonePayload["ok"], "陈旧体检应 ok=false")
	require.Equal(t, true, fixDonePayload["stale_audit"], "fix_done 应带 stale_audit 标记")
	require.Contains(t, fmt.Sprint(fixDonePayload["error"]), "target_scope", "错误提示应说明缺作用域字段")
}

// TestServiceFixWriteProtectsFrozenIssues 验证 1.3/1.4 写保护与前向默认路由：
// target_scope=frozen_volume / direction=retcon_candidate 的问题绝不进修复批次（不派发、
// 不出现在修复提示词里），标记 FixStates=retcon 并展示为「转人工裁决」；其余项正常修复。
func TestServiceFixWriteProtectsFrozenIssues(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// I1 正常前向修复；I2 落点在冻结卷（写保护）；I3 硬矛盾 retcon 候选。
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"混合清单","issues":[` +
		`{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"设定自洽","desc":"与前卷第3章冲突","fix_type":"ledger","target_scope":"frozen_volume","dimension":"character","direction":"forward_fix"},` +
		`{"id":"I3","level":"P1","area":"设定自洽","desc":"硬矛盾需回改前卷","fix_type":"ledger","target_scope":"frozen_volume","dimension":"character","direction":"retcon_candidate"}]}`
	secondAudit := `{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`

	var fixPrompts []string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			fixPrompts = append(fixPrompts, input.Prompt)
			return SkillOutput{Text: "已修复"}, nil
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	var firstFixStart map[string]any
	origFixStart := emitFixStart
	emitFixStart = func(_ context.Context, p map[string]any) {
		if firstFixStart == nil {
			firstFixStart = p
		}
	}
	t.Cleanup(func() { emitFixStart = origFixStart })

	run, err := svc.StartPipeline(ctx, 410, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	// 只有 I1 进修复批次；I2/I3 绝不出现在修复提示词里（冻结卷不得被自动回改）。
	require.Len(t, fixPrompts, 1, "应只派发一个修复批次")
	require.Contains(t, fixPrompts[0], "I1")
	require.NotContains(t, fixPrompts[0], "I2", "冻结卷问题不得进修复批次")
	require.NotContains(t, fixPrompts[0], "I3", "retcon 候选不得进修复批次")

	// fix_start 全量清单里写保护问题应标为 retcon 状态。
	require.NotNil(t, firstFixStart)
	all0, ok := firstFixStart["all_issues"].([]FixIssuePayload)
	require.True(t, ok)
	byID := map[string]FixIssuePayload{}
	for _, p := range all0 {
		byID[p.ID] = p
	}
	require.Equal(t, "retcon", byID["I2"].Status, "冻结卷问题应标 retcon")
	require.Equal(t, "retcon", byID["I3"].Status, "retcon 候选应标 retcon")
	require.Equal(t, "fixing", byID["I1"].Status, "正常问题应进批次修复")
}

// TestServiceFixRetconHandoffWhenAllProtected 验证：全部问题命中写保护时，executeFix
// 必须「交回人工裁决」（retcon_handoff）而非复评/谎报完成（否则复评复报死循环）。
func TestServiceFixRetconHandoffWhenAllProtected(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"全部命中写保护","issues":[` +
		`{"id":"I1","level":"P0","area":"设定自洽","desc":"与前卷冲突","fix_type":"ledger","target_scope":"frozen_volume","dimension":"character","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"设定自洽","desc":"硬矛盾","fix_type":"ledger","target_scope":"frozen_volume","dimension":"timeline","direction":"retcon_candidate"}]}`

	var fixCalled, auditCalled bool
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-fix" {
			fixCalled = true
		}
		if name == "signability-audit" {
			auditCalled = true
		}
		return SkillOutput{Text: "ok"}, nil
	}

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	run, err := svc.StartPipeline(ctx, 411, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.False(t, fixCalled, "写保护问题不应派发修复 agent")
	require.False(t, auditCalled, "交回人工裁决不应触发复评（避免复报死循环）")
	require.Len(t, finalCtxt.RetconBacklog, 2, "两项都应沉淀进 RetconBacklog")
	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["retcon_handoff"], "fix_done 应带 retcon_handoff 标记")
	require.Equal(t, int(2), fixDonePayload["retcon_count"], "retcon_count 应为 2")
}

// TestServiceAutoRewriteSkipsProtectedBodyItems 验证写保护覆盖自动章节重写回路：
// 命中写保护的正文级问题绝不派发 chapter-rewrite（冻结卷章节不得被自动重写），
// 走 retcon 交回；且不进 RewriteBacklog（bodyIssuesOf 已分流）。
func TestServiceAutoRewriteSkipsProtectedBodyItems(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 测试库默认只迁移 pipeline_runs，重写回路需要 chapters 表，单独迁移。
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	for i := 1; i <= 3; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: 412, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}

	// 正文级问题指向第 2 章，但落点在冻结卷（写保护）：不得自动重写。
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"冻结卷正文问题","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"第2章未立人设，代入感弱","fix_type":"body","target_scope":"frozen_volume","dimension":"none","direction":"forward_fix"}]}`

	var rewriteCalled bool
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "chapter-rewrite" {
			rewriteCalled = true
		}
		return SkillOutput{Text: "ok"}, nil
	}

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	run, err := svc.StartPipeline(ctx, 412, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.False(t, rewriteCalled, "冻结卷章节不得被自动重写")
	require.Empty(t, finalCtxt.RewriteBacklog, "写保护正文问题不进 RewriteBacklog")
	require.Len(t, finalCtxt.RetconBacklog, 1, "应沉淀进 RetconBacklog 交回人工")
	require.Equal(t, "I1", finalCtxt.RetconBacklog[0].ID)
	require.Equal(t, []int64{2}, finalCtxt.RetconBacklog[0].Chapters, "章节号应解析保留，供人工定位")
	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["retcon_handoff"], "fix_done 应带 retcon_handoff 标记")
}

// TestServiceAutoRewriteDispatch 验证：当重写清单指向可定位章节时，finishFixHandoffRewrite
// 全自动派发 chapter-rewrite agent 重写对应章节（而非仅仅暂停交回），重写后自动回到复评，
// 且 RewriteRounds 递增；复评若仍报出正文问题则停在闸门等人工（不无限循环）。
func TestServiceAutoRewriteDispatch(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 测试库默认只迁移 pipeline_runs，自动重写回路需要 chapters 表（查询会 JOIN volumes），单独迁移。
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))

	// 种入 5 章（无卷，sort_order 即阅读序号 1..5），供重写回路定位 chapter_id。
	for i := 1; i <= 5; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: 421, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}
	// 读取阅读序号→章节，确认第 3 章的 chapter_id。
	seeded, err := svc.chapterStore.ListAllByNovel(ctx, nil, 421)
	require.NoError(t, err)
	readingToID := map[int]int64{}
	for _, ch := range seeded {
		readingToID[ch.ReadingNumber] = ch.ID
	}
	require.Contains(t, readingToID, 3, "应能通过阅读序号定位第 3 章")

	// 正文级问题指向第 3 章：S5 改不动，必须自动整章重写。
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"正文问题","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"第3章未立人设，代入感弱","fix_type":"body"}]}`

	var rewriteCalled bool
	var rewriteChapterID int64
	var auditCalled bool
	var progress []string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "chapter-rewrite":
			rewriteCalled = true
			// 从提示词里抽取被重写的 chapter_id 校验。
			rewriteChapterID = extractChapterIDFromPrompt(t, input.Prompt)
			return SkillOutput{Text: "已整章重写"}, nil
		case "signability-audit":
			auditCalled = true
			// 复评仍报出同一正文问题（模拟未一次解决），停在闸门等人工。
			return SkillOutput{Text: "复评：\n```json\n" +
				`{"verdict":"needs_fix","score":16,"summary":"重写后仍偏弱","issues":[` +
				`{"id":"I1","level":"P0","area":"开篇过签力","desc":"第3章未立人设，代入感弱","fix_type":"body"}]}` +
				"\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	oldProg := emitRewriteProgress
	emitRewriteProgress = func(_ context.Context, p map[string]any) { progress = append(progress, fmt.Sprint(p["phase"])) }
	defer func() { emitRewriteProgress = oldProg }()

	run, err := svc.StartPipeline(ctx, 421, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.True(t, rewriteCalled, "重写清单指向可定位章节，应自动派发 chapter-rewrite")
	require.Equal(t, readingToID[3], rewriteChapterID, "应重写第 3 章（即 chapter_id 对应阅读序号 3）")
	require.True(t, auditCalled, "重写后应自动回到复评")
	require.Equal(t, 1, finalCtxt.RewriteRounds, "重写轮数应 +1")
	require.Contains(t, progress, "starting", "应广播重写开始进度")
	require.Contains(t, progress, "done", "应广播重写完成进度")
	require.Len(t, finalCtxt.RewriteBacklog, 1, "复评仍报正文问题，重写清单应保留 1 项")
}

// TestServiceStyleExemptSkipsAutoRewrite 验证 P2 T2.2 派发豁免（验收②）：
// 混合清单里 V 系文风漂移项进 RewriteBacklog 供作者查看，但自动章节重写只派发
// 非豁免项——I 系正文问题照常整章重写，V 系涉及章节零自动改写（只报告不改写）。
func TestServiceStyleExemptSkipsAutoRewrite(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	for i := 1; i <= 5; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: 431, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}
	seeded, err := svc.chapterStore.ListAllByNovel(ctx, nil, 431)
	require.NoError(t, err)
	readingToID := map[int]int64{}
	for _, ch := range seeded {
		readingToID[ch.ReadingNumber] = ch.ID
	}

	// V1 指向第 3 章（文风漂移，豁免派发），I2 指向第 4 章（常规正文问题，照常派发）。
	issueV := `{"id":"V1","level":"P1","area":"文风漂移·章节#3","desc":"第3章文风指标 avg_sent_len 偏离基线",` +
		`"fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}`
	issueI := `{"id":"I2","level":"P0","area":"开篇过签力","desc":"第4章未立人设，代入感弱","fix_type":"body"}`
	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"混合清单","issues":[` + issueV + `,` + issueI + `]}`

	var rewrittenIDs []int64
	var auditCalled bool
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "chapter-rewrite":
			rewrittenIDs = append(rewrittenIDs, extractChapterIDFromPrompt(t, input.Prompt))
			return SkillOutput{Text: "已整章重写"}, nil
		case "signability-audit":
			auditCalled = true
			// 复评仍报出同一批问题（未一次解决），停在闸门等人工。
			return SkillOutput{Text: "复评：\n```json\n" +
				`{"verdict":"needs_fix","score":16,"summary":"复评仍报","issues":[` + issueV + `,` + issueI + `]}` +
				"\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	oldProg := emitRewriteProgress
	emitRewriteProgress = func(_ context.Context, _ map[string]any) {}
	defer func() { emitRewriteProgress = oldProg }()

	run, err := svc.StartPipeline(ctx, 431, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.Equal(t, []int64{readingToID[4]}, rewrittenIDs,
		"只应重写 I 系指向的第 4 章；V 系第 3 章零自动改写（豁免派发）")
	require.True(t, auditCalled, "I 系重写后应自动回到复评")
	require.Equal(t, 1, finalCtxt.RewriteRounds, "重写轮数应 +1（只计可派发项）")
	require.Len(t, finalCtxt.RewriteBacklog, 2, "复评仍报同批问题，V 系与 I 系都应留在重写清单")
}

// TestServiceStyleExemptHandoffNoLoop 验证 P2 T2.2 派发豁免（验收③）：全 V 系
// RewriteBacklog 不触发自动重写循环——零 chapter-rewrite 派发、零复评，
// 手动交接收口（note「文风漂移项待作者裁决」）且 RewriteRounds 不递增。
func TestServiceStyleExemptHandoffNoLoop(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	for i := 1; i <= 3; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: 432, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}

	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"仅文风漂移","issues":[` +
		`{"id":"V1","level":"P1","area":"文风漂移·章节#2","desc":"第2章文风指标 quote_density 偏离基线",` +
		`"fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`

	var rewriteCalled, auditCalled bool
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "chapter-rewrite":
			rewriteCalled = true
		case "signability-audit":
			auditCalled = true
		}
		return SkillOutput{Text: "ok"}, nil
	}

	var fixDonePayload map[string]any
	oldEmit := emitFixDone
	emitFixDone = func(_ context.Context, p map[string]any) { fixDonePayload = p }
	defer func() { emitFixDone = oldEmit }()

	run, err := svc.StartPipeline(ctx, 432, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusPaused)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))

	require.False(t, rewriteCalled, "全豁免清单不得派发自动章节重写")
	require.False(t, auditCalled, "全豁免不得进复评（防「重写→复评」死循环）")
	require.Equal(t, 0, finalCtxt.RewriteRounds, "重写轮数不得递增")
	require.Len(t, finalCtxt.RewriteBacklog, 1, "V 系应沉淀进重写清单供作者查看")
	require.Equal(t, "V1", finalCtxt.RewriteBacklog[0].ID)
	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["rewrite_handoff"], "fix_done 应带 rewrite_handoff 标记（前端零改动）")
	require.Contains(t, fmt.Sprint(fixDonePayload["error"]), "文风漂移项待作者裁决")
}

// extractChapterIDFromPrompt 从 chapterRewritePrompt 生成的内容中解析 chapter_id=NNN。
func extractChapterIDFromPrompt(t *testing.T, prompt string) int64 {
	t.Helper()
	re := regexp.MustCompile(`chapter_id=(\d+)`)
	m := re.FindStringSubmatch(prompt)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	require.NoError(t, err)
	return n
}

// TestParseAuditFixType 验证 parseAudit 正确解析 fix_type 字段。
func TestParseAuditFixType(t *testing.T) {
	text := "结论\n```json\n{\"verdict\":\"needs_fix\",\"score\":10,\"summary\":\"x\",\"issues\":[" +
		"{\"id\":\"I1\",\"level\":\"P0\",\"area\":\"a\",\"desc\":\"d\",\"fix_type\":\"body\"}," +
		"{\"id\":\"I2\",\"level\":\"P1\",\"area\":\"b\",\"desc\":\"e\"}]}\n```"
	a := parseAudit(text)
	require.Len(t, a.Issues, 2)
	require.Equal(t, FixTypeBody, a.Issues[0].FixType, "显式 fix_type=body 应被解析")
	require.Equal(t, "", a.Issues[1].FixType, "缺省 fix_type 应为空（按 ledger 处理）")
}

// TestParseAuditScopeFields 验证 parseAudit 正确解析作用域三字段
// （target_scope/dimension/direction）。parseAudit 不做归一化：缺省保留为空，
// 由守卫识别旧数据、由使用点按安全缺省（active_volume/forward_fix）处理。
func TestParseAuditScopeFields(t *testing.T) {
	text := "结论\n```json\n{\"verdict\":\"needs_fix\",\"score\":10,\"summary\":\"x\",\"issues\":[" +
		"{\"id\":\"I1\",\"level\":\"P0\",\"area\":\"a\",\"desc\":\"d\",\"fix_type\":\"ledger\",\"target_scope\":\"frozen_volume\",\"dimension\":\"character\",\"direction\":\"retcon_candidate\"}," +
		"{\"id\":\"I2\",\"level\":\"P1\",\"area\":\"b\",\"desc\":\"e\",\"fix_type\":\"ledger\"}]}\n```"
	a := parseAudit(text)
	require.Len(t, a.Issues, 2)
	require.Equal(t, TargetScopeFrozenVolume, a.Issues[0].TargetScope, "显式 target_scope 应被解析")
	require.Equal(t, "character", a.Issues[0].Dimension, "显式 dimension 应被解析")
	require.Equal(t, DirectionRetconCandidate, a.Issues[0].Direction, "显式 direction 应被解析")
	require.Equal(t, "", a.Issues[1].TargetScope, "缺省 target_scope 应保留为空（不归一化）")
	require.Equal(t, "", a.Issues[1].Dimension, "缺省 dimension 应保留为空（不归一化）")
	require.Equal(t, "", a.Issues[1].Direction, "缺省 direction 应保留为空（不归一化）")
}

// TestBodyIssuesOf 验证 bodyIssuesOf 仅析出正文级项，并尽力解析涉及章节号。
func TestBodyIssuesOf(t *testing.T) {
	audit := AuditData{Issues: []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇过签力", Desc: "前三章未立人设", FixType: FixTypeBody,
			TargetScope: TargetScopeFrozenVolume, Dimension: "character", Direction: DirectionRetconCandidate},
		{ID: "I2", Level: "P1", Area: "长线牵引", Desc: "timeline#42 伏笔未回收", FixType: "ledger"},
		{ID: "I3", Level: "P1", Area: "节奏爽点", Desc: "第6-8章爽点断档", FixType: FixTypeBody},
	}}
	items := bodyIssuesOf(audit)
	require.Len(t, items, 1, "只析出可自动重写的 body 项（写保护项 I1 分流进 retconItemsOf）")
	require.Equal(t, "I3", items[0].ID)
	require.Equal(t, []int64{6, 7, 8}, items[0].Chapters, "第6-8章区间应解析为 [6,7,8]")
}

// TestRetconItemsOf 验证 retconItemsOf 析出命中写保护的全部问题（不分 fix_type），
// 并携带三枚举字段；bodyIssuesOf 与 retconItemsOf 对 body 问题互不相交。
func TestRetconItemsOf(t *testing.T) {
	audit := AuditData{Issues: []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇过签力", Desc: "第3章问题", FixType: FixTypeBody,
			TargetScope: TargetScopeFrozenVolume, Dimension: "none", Direction: DirectionForwardFix},
		{ID: "I2", Level: "P1", Area: "设定自洽", Desc: "硬矛盾", FixType: "ledger",
			TargetScope: TargetScopeFrozenVolume, Dimension: "character", Direction: DirectionRetconCandidate},
		{ID: "I3", Level: "P1", Area: "节奏爽点", Desc: "正常问题", FixType: "ledger",
			TargetScope: TargetScopeActiveVolume, Dimension: "none", Direction: DirectionForwardFix},
	}}
	items := retconItemsOf(audit)
	require.Len(t, items, 2, "冻结卷落点与 retcon 候选都应析出")
	require.Equal(t, "I1", items[0].ID)
	require.Equal(t, TargetScopeFrozenVolume, items[0].TargetScope, "应携带源 issue 的 target_scope")
	require.Equal(t, "none", items[0].Dimension)
	require.Equal(t, DirectionForwardFix, items[0].Direction)
	require.Equal(t, []int64{3}, items[0].Chapters, "章节号应解析保留")
	require.Equal(t, "I2", items[1].ID)
	require.Equal(t, DirectionRetconCandidate, items[1].Direction)

	body := bodyIssuesOf(audit)
	require.Empty(t, body, "写保护 body 项不进 bodyIssuesOf（互不相交）")
}

// TestServiceFixAllFixedSkipsToReaudit 验证断点续修边界：全部问题已修复但复评未跑
// （如复评前中断后恢复）时，executeFix 应直接进入复评，不再调用修复 skill。
func TestServiceFixAllFixedSkipsToReaudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var fixCalls int
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"成稿质量","desc":"x"}]}`
	secondAudit := `{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			fixCalls++
			return SkillOutput{Text: "已修复"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 404, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4,
		FixStates: map[string]string{"I1": "fixed"}}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	require.Equal(t, 0, fixCalls, "全部已修复时应跳过修复直接复评")
}

// TestParseFixReceipt 验证修复回执解析：围栏 JSON（含嵌套 skipped）、纯文本包围、缺失回执。
func TestParseFixReceipt(t *testing.T) {
	// 围栏 + 嵌套 skipped 对象（非贪婪正则会截断，必须用流式解码）。
	text := "修复完成。\n```json\n{\"fixed\":[\"I1\",\"I2\"],\"skipped\":[{\"id\":\"I3\",\"reason\":\"正文级问题需章节重写\"}]}\n```\n以上。"
	fixed, skips, found := parseFixReceipt(text)
	require.True(t, found)
	require.True(t, fixed["I1"] && fixed["I2"])
	require.False(t, fixed["I3"])
	require.Equal(t, "正文级问题需章节重写", skips["I3"])

	// 无围栏、多个 JSON 块时应取含 fixed/skipped 的那个。
	fixed2, skips2, found2 := parseFixReceipt(`概况 {"verdict":"pass"} 尾部 {"fixed":["I9"],"skipped":[]}`)
	require.True(t, found2)
	require.True(t, fixed2["I9"])
	require.Empty(t, skips2)

	// 缺失回执：found=false，调用方走整批已修复的兼容回退。
	_, _, found3 := parseFixReceipt("我修好了所有问题，没有 JSON。")
	require.False(t, found3)
}

// TestApplyFixReceipt 验证回执落账：fixed/skipped/未提及三种去向；缺回执时整批兜底。
func TestApplyFixReceipt(t *testing.T) {
	batch := []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏", Desc: "a"},
		{ID: "I2", Level: "P1", Area: "人设", Desc: "b"},
		{ID: "I3", Level: "P1", Area: "合规", Desc: "c"},
	}
	out := "摘要…\n```json\n{\"fixed\":[\"I1\"],\"skipped\":[{\"id\":\"I2\",\"reason\":\"需正文重写\"}]}\n```"

	var ctxt GateContext
	fixedNow := applyFixReceipt(&ctxt, batch, out, slog.Default())
	require.Equal(t, []string{"I1"}, fixedNow, "只有回执确认的才算本批已修复")
	require.Equal(t, "fixed", ctxt.FixStates["I1"])
	require.Equal(t, "skipped", ctxt.FixStates["I2"], "skipped 记录断点，不再重试")
	require.Empty(t, ctxt.FixStates["I3"], "回执未提及的不落断点，后续轮次重试")

	// 缺回执：整批兜底为 fixed（兼容旧行为，不卡死流水线）。
	var ctxt2 GateContext
	fixedNow2 := applyFixReceipt(&ctxt2, batch, "没有 JSON 的输出", slog.Default())
	require.Len(t, fixedNow2, 3)
	for _, it := range batch {
		require.Equal(t, "fixed", ctxt2.FixStates[it.ID])
	}
}

// TestServiceAuditResetsFixStatesOnNewAudit 回归测试：新一轮体检落地时必须重置旧修复
// 断点。否则复评 issue 与旧断点 ID 撞车（LLM 惯性编号 I1..In），再次点修复会被误判为
// 「全部已修复」而静默跳过——表现为提示「修复完成，正在自动复评」却一个问题都没修。
func TestServiceAuditResetsFixStatesOnNewAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	newAudit := `{"verdict":"needs_fix","score":20,"summary":"复评仍有问题","issues":[{"id":"I1","level":"P1","area":"节奏","desc":"复评新发现"}]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			return SkillOutput{Text: "复评：\n```json\n" + newAudit + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, 405, "new", nil, false)
	require.NoError(t, err)
	// 旧断点与复评 issue 同 ID（I1），模拟撞车现场。
	ctxt := GateContext{Audit: `{"verdict":"needs_fix","issues":[]}`, AuditStage: StageS4,
		FixStates: map[string]string{"I1": "fixed"}}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	svc.executeAudit(run.RunID, run.NovelID)

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Empty(t, finalCtxt.FixStates, "新体检落地后旧修复断点必须重置，否则修复闭环空转")
	require.Contains(t, finalCtxt.Audit, "复评仍有问题", "新体检结果应已落库")
}

func TestServiceAuditFixLoopReAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var auditCalls int
	secondAudit := `{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			auditCalls++
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			return SkillOutput{Text: "已按 issue 修复：强化开篇钩子、补人设记忆点"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 402, "new", nil, false)
	require.NoError(t, err)
	// 直接进入 S4（首轮体检已落库，G4 闸门待决策），准备选 fix → S5
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning,
		`{"audit":"{\"verdict\":\"needs_fix\"}"}`))

	// S4 选择 fix → 进入 S5 修复闭环，修复完成后自动回到 S4 复评
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	// 异步链 executeFix → executeAudit 最终应停在 S4 awaiting_gate（复评结果）
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage, "复评后应在 S4 闸门")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Contains(t, ctxt.Audit, "pass", "复评应反映修复后的 pass 结论")
	require.Equal(t, 1, auditCalls, "修复后应再跑一次体检做复评")
}

// withCapturedGates 临时接管 emitGate，收集所有发出的闸门事件，测试结束后还原。
func withCapturedGates(t *testing.T) *[]GatePayload {
	t.Helper()
	var captured []GatePayload
	orig := emitGate
	emitGate = func(_ context.Context, p GatePayload) { captured = append(captured, p) }
	t.Cleanup(func() { emitGate = orig })
	return &captured
}

// startS3Run 直接把流水线置为 S3 写作态（带给定 ContextJSON），跳过 S1/S2 异步路径以便精确控制。
func startS3Run(t *testing.T, svc *Service, novelID int64, contextJSON string) string {
	t.Helper()
	run, err := svc.StartPipeline(context.Background(), novelID, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(context.Background(), run.RunID, StageS3, StatusRunning, contextJSON))
	return run.RunID
}

// TestServiceMilestoneGateOffersAudit 验证：已体检过（last_audit_chapter=3）且当前章数=8
// （距上次 5 章）时，S3「继续」应弹出 G3b 里程碑闸，且提供 audit_now 选项。
func TestServiceMilestoneGateOffersAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 8, nil }
	captured := withCapturedGates(t)
	ctx := context.Background()

	runID := startS3Run(t, svc, 500, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))

	require.NotEmpty(t, *captured, "应弹出 G3b 里程碑闸")
	last := (*captured)[len(*captured)-1]
	require.Equal(t, StageS3, last.Stage)
	require.Equal(t, GateG3b, last.GateID)
	require.Contains(t, last.Options, "audit_now", "里程碑闸应提供进入体检选项")
	require.NotContains(t, last.Options, "modify", "PR4 不再使用 modify 选项")
}

// TestServiceVolumeBoundaryOffersAudit 验证：跨卷边界（volumeBoundary=true）时，
// 即便章数未达 5 章间距，S3「继续」也应弹出 G3b 里程碑闸并提供 audit_now。
func TestServiceVolumeBoundaryOffersAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 3, nil }
	svc.volumeBoundary = func(_ context.Context, _ int64) (bool, error) { return true, nil }
	captured := withCapturedGates(t)
	ctx := context.Background()

	runID := startS3Run(t, svc, 501, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))

	require.NotEmpty(t, *captured)
	last := (*captured)[len(*captured)-1]
	require.Equal(t, StageS3, last.Stage)
	require.Equal(t, GateG3b, last.GateID)
	require.Contains(t, last.Options, "audit_now", "卷边界应触发复检选项")
}

// TestServiceNonMilestoneGateNoAudit 验证：已体检过且章数仅 +1（未达 5 章、未跨卷）时，
// S3「继续」只弹普通逐章闸，不提供 audit_now，避免频繁打断写作。
func TestServiceNonMilestoneGateNoAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 4, nil }
	svc.volumeBoundary = func(_ context.Context, _ int64) (bool, error) { return false, nil }
	captured := withCapturedGates(t)
	ctx := context.Background()

	runID := startS3Run(t, svc, 502, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))

	require.NotEmpty(t, *captured)
	last := (*captured)[len(*captured)-1]
	require.Equal(t, StageS3, last.Stage)
	require.Equal(t, GateG3b, last.GateID)
	require.ElementsMatch(t, []string{"continue", "abort"}, last.Options, "普通逐章闸不应提供 audit_now")
}

// TestServiceAuditNowEntersS4b 验证：G3b 选 audit_now 应进入 S4b 卷边界复评阶段，
// 且 ContextJSON.audit_stage 记录为 S4b（供修复后复评回到同阶段）。
func TestServiceAuditNowEntersS4b(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 8, nil }
	ctx := context.Background()

	runID := startS3Run(t, svc, 503, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "audit_now", ""))

	waitForStageStatus(t, svc, runID, StatusAwaitingGate)
	got, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StageS4b, got.CurrentStage, "audit_now 应进入 S4b 卷边界复评")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Equal(t, StageS4b, ctxt.AuditStage, "audit_stage 应记录为 S4b")
	require.Equal(t, "milestone", ctxt.AuditReason)
}

// TestServiceMilestoneResetsLastAuditChapter 验证：卷边界复评后用户选 continue，
// ContextJSON.last_audit_chapter 应更新为当前章数，避免立刻再次复检。
func TestServiceMilestoneResetsLastAuditChapter(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 8, nil }
	ctx := context.Background()

	runID := startS3Run(t, svc, 504, `{"last_audit_chapter":3,"s4_audited":true}`)
	// 里程碑闸选 audit_now → 进入 S4b
	require.NoError(t, svc.ResumePipeline(ctx, runID, "audit_now", ""))
	waitForStageStatus(t, svc, runID, StatusAwaitingGate)
	got, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StageS4b, got.CurrentStage)

	// G4 选 continue → 回到 S3，且 last_audit_chapter 更新为 8
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))
	got, _ = svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StageS3, got.CurrentStage)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Equal(t, 8, ctxt.LastAuditChapter, "复评后应把 last_audit_chapter 推进到当前章数")
}

// TestServiceBlueprintAuditPass 验证 A 方案蓝图体检闭环（通过支）：
// S1→S2 生成后立即进入蓝图体检（blueprint-audit），通过应落 G1 确认闸（continue/modify/abort），
// ContextJSON 写入 AuditKind=blueprint 与 BlueprintAudit（pass）；G1 选 continue 进入 S3 写作。
func TestServiceBlueprintAuditPass(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	captured := withCapturedGates(t)

	auditJSON := `{"verdict":"pass","score":26,"summary":"蓝图结构清晰","issues":[]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "blueprint-audit" {
			return SkillOutput{Text: "体检：\n```json\n" + auditJSON + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, 600, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2 生成+体检
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS2, got.CurrentStage, "蓝图体检宿主阶段应为 S2")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Equal(t, "blueprint", ctxt.AuditKind, "应为蓝图体检种类")
	require.Contains(t, ctxt.BlueprintAudit, "pass", "蓝图体检结果应写入 BlueprintAudit 字段")

	// 蓝图体检通过应落 G1（continue/modify/abort），而非 G1b 修复闸。
	require.NotEmpty(t, *captured, "应弹出蓝图体检闸门")
	last := (*captured)[len(*captured)-1]
	require.Equal(t, GateG1, last.GateID, "蓝图体检通过应落 G1 确认闸")
	require.ElementsMatch(t, []string{"continue", "modify", "abort"}, last.Options)

	// G1 选 continue → 进入 S3 写作。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS3, got.CurrentStage, "G1 确认后应进入 S3 写作")
}

// TestServiceBlueprintAuditFixLoop 验证 A 方案蓝图体检闭环（修复支）：
// 蓝图体检不通过（needs_fix）落 G1b 修复闸；G1b 选 fix 进入 S5 蓝图修复（blueprint-fix），
// 修复完成后自动回到蓝图复评（blueprint-audit，pass），复评结果重写 BlueprintAudit 并落 G1。
func TestServiceBlueprintAuditFixLoop(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var auditCount, fixCalls int
	firstAudit := `{"verdict":"needs_fix","score":15,"summary":"卷纲待修","issues":[{"id":"B1","level":"P0","area":"卷纲","desc":"第一卷与第三卷冲突"}]}`
	secondAudit := `{"verdict":"pass","score":27,"summary":"修复后通过","issues":[]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "blueprint-audit":
			auditCount++
			if auditCount == 1 {
				return SkillOutput{Text: "体检：\n```json\n" + firstAudit + "\n```"}, nil
			}
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "blueprint-fix":
			fixCalls++
			return SkillOutput{Text: "已修复卷纲冲突"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 601, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2 生成+首检
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS2, got.CurrentStage)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Equal(t, "blueprint", ctxt.AuditKind)
	require.Contains(t, ctxt.BlueprintAudit, "needs_fix", "首检不通过应写入 BlueprintAudit")

	// G1b 选 fix → 进入 S5 蓝图修复闭环，修复完成自动复评（pass）→ 落 G1。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate) // 复评后再次落闸（G1）

	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS2, got.CurrentStage, "复评仍在 S2 宿主阶段内")
	var ctxt2 GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt2))
	require.Contains(t, ctxt2.BlueprintAudit, "pass", "复评应反映修复后的 pass 结论")
	require.Equal(t, 1, fixCalls, "蓝图修复应调用一次 blueprint-fix")
	require.Equal(t, 2, auditCount, "首检 + 复评共两次 blueprint-audit")
}

// TestServiceBlueprintAuditAutoRunSkipped 验证：即便开启 auto_run，蓝图体检也绝不被自动裁决，
// 必须停在 S2 等人工确认（与 G1 原语义一致，避免自动改写卷纲）。不通过应落 G1b 而非被自动推进到 S5。
func TestServiceBlueprintAuditAutoRunSkipped(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	captured := withCapturedGates(t)

	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"卷纲逻辑断裂","issues":[{"id":"B1","level":"P0","area":"卷纲","desc":"第一卷与第三卷冲突"}]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "blueprint-audit" {
			return SkillOutput{Text: "体检：\n```json\n" + auditJSON + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, 602, "new", nil, true) // autoRun=true
	require.NoError(t, err)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS2, got.CurrentStage, "蓝图体检即使用 autoRun 也必须停 S2 等人工确认")
	require.NotEqual(t, StageS5, got.CurrentStage, "蓝图体检不得被 auto_run 自动推进到 S5 修复")

	require.NotEmpty(t, *captured)
	last := (*captured)[len(*captured)-1]
	require.Equal(t, GateG1b, last.GateID, "蓝图体检不通过应落 G1b 修复闸")
	require.ElementsMatch(t, []string{"fix", "continue", "abort"}, last.Options)
}

// TestParseAuditEightDimensions 验证 parseAudit 能正确解析升 8 维/40 后的签约体检结果：
// score=32（满分 40）、9 条 issue（含全台账一致性 issue，如 timeline#42 / reader_perspective#15），
// verdict/score/summary/issues 正确落库，证明 B 方案 8 维量表可被流水线消费。
func TestParseAuditEightDimensions(t *testing.T) {
	text := "结论：勉强能签。\n```json\n" +
		"{\"verdict\":\"needs_fix\",\"score\":32,\"summary\":\"勉强：读者视角分层不清\"," +
		"\"issues\":[" +
		"{\"id\":\"I1\",\"level\":\"P0\",\"area\":\"开篇过签力\",\"desc\":\"前三章代入弱\"}," +
		"{\"id\":\"I2\",\"level\":\"P1\",\"area\":\"读者视角\",\"desc\":\"reveal 提前\"}," +
		"{\"id\":\"I3\",\"level\":\"P1\",\"area\":\"长线牵引\",\"desc\":\"timeline#42 未回收\"}," +
		"{\"id\":\"I4\",\"level\":\"P1\",\"area\":\"节奏爽点\",\"desc\":\"中段拖沓\"}," +
		"{\"id\":\"I5\",\"level\":\"P1\",\"area\":\"题材契合\",\"desc\":\"赛道模糊\"}," +
		"{\"id\":\"I6\",\"level\":\"P1\",\"area\":\"人设卖点\",\"desc\":\"主角无标签\"}," +
		"{\"id\":\"I7\",\"level\":\"P1\",\"area\":\"设定自洽\",\"desc\":\"战力略崩\"}," +
		"{\"id\":\"I8\",\"level\":\"P1\",\"area\":\"平台合规\",\"desc\":\"擦边禁忌\"}," +
		"{\"id\":\"I9\",\"level\":\"P1\",\"area\":\"读者视角\",\"desc\":\"reader_perspective#15 无回收\"}" +
		"]}\n```"
	data := parseAudit(text)
	require.Equal(t, "needs_fix", data.Verdict)
	require.Equal(t, 32, data.Score, "8 维满分 40，score 应正确解析")
	require.Equal(t, "勉强：读者视角分层不清", data.Summary)
	require.Len(t, data.Issues, 9)
	require.Equal(t, "timeline#42 未回收", data.Issues[2].Desc)
}

// TestServiceDriftPatrolAfterFix 验证 G3：S5 修复完成后自动触发一次蓝图一致性巡检
// （blueprint-drift-check，只读非阻断），结论写入 ctxt.DriftReport，并随复评闸门
// summary 带给用户（标出与当前卷纲冲突的章节）。
func TestServiceDriftPatrolAfterFix(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 8, nil } // 有已写章节才巡检
	captured := withCapturedGates(t)

	var driftCalls int
	secondAudit := `{"verdict":"pass","score":33,"summary":"修复后通过","issues":[]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			return SkillOutput{Text: "已按 issue 修复台账"}, nil
		case "blueprint-drift-check":
			driftCalls++
			return SkillOutput{Text: "巡检：\n```json\n{\"verdict\":\"needs_fix\",\"score\":0," +
				"\"summary\":\"发现 2 章与卷纲冲突\",\"issues\":[" +
				"{\"id\":\"D1\",\"level\":\"P1\",\"area\":\"章节#12\",\"desc\":\"卷纲承诺的反转未出现\"}," +
				"{\"id\":\"D2\",\"level\":\"P1\",\"area\":\"章节#15\",\"desc\":\"时间顺序与卷纲错位\"}]}\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 603, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning,
		`{"audit":"{\"verdict\":\"needs_fix\"}","audit_stage":"S4"}`))
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	require.Equal(t, 1, driftCalls, "修复完成后应恰好触发一次蓝图巡检")

	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Contains(t, ctxt.DriftReport, "蓝图巡检", "巡检结论应落库 DriftReport")
	require.Contains(t, ctxt.DriftReport, "章节#12", "巡检结论应包含冲突章节")

	last := (*captured)[len(*captured)-1]
	require.Equal(t, GateG4, last.GateID, "复评仍落 G4 闸")
	require.Contains(t, last.Summary, "蓝图巡检", "巡检结论应随复评闸门 summary 带给用户")
}

// TestServiceDriftPatrolSkippedWhenNoChapters 验证 G3：无已写章节（纯蓝图阶段的蓝图修复）
// 时跳过巡检，不浪费一次 LLM 调用，DriftReport 保持为空。
func TestServiceDriftPatrolSkippedWhenNoChapters(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }

	var driftCalls, auditCount int
	firstAudit := `{"verdict":"needs_fix","score":15,"summary":"卷纲待修","issues":[{"id":"B1","level":"P0","area":"卷纲","desc":"第一卷与第三卷冲突"}]}`
	secondAudit := `{"verdict":"pass","score":27,"summary":"修复后通过","issues":[]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "blueprint-audit":
			auditCount++
			if auditCount == 1 {
				return SkillOutput{Text: "体检：\n```json\n" + firstAudit + "\n```"}, nil
			}
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "blueprint-fix":
			return SkillOutput{Text: "已修复卷纲"}, nil
		case "blueprint-drift-check":
			driftCalls++
			return SkillOutput{Text: "ok"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 604, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2 生成+首检（不通过）
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", "")) // G1b→S5 蓝图修复→复评
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	require.Equal(t, 0, driftCalls, "无章节时不应触发蓝图巡检")
	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Empty(t, ctxt.DriftReport, "无章节时 DriftReport 应为空")
}
