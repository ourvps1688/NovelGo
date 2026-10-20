package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 测试中无 Wails runtime 上下文，将事件发射器替换为无操作，避免
// "invalid context" 报错；真实运行仍走 wails 事件总线。
func init() {
	emitStage = func(_ context.Context, _ StagePayload) {}
	emitGate = func(_ context.Context, _ GatePayload) {}
	emitControlAck = func(_ context.Context, _ ControlAckPayload) {}
	emitStockAudit = func(_ context.Context, _ map[string]any) {}
	emitFixStart = func(_ context.Context, _ map[string]any) {}
	emitFixDone = func(_ context.Context, _ map[string]any) {}
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
		`{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"五章程序文"},` +
		`{"id":"I3","level":"P1","area":"人设爽点","desc":"情感钩子缺失"}]}`
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
	require.Equal(t, "fixed", finalCtxt.FixStates["I1"])
	require.Equal(t, "fixed", finalCtxt.FixStates["I2"])
	require.Equal(t, "fixed", finalCtxt.FixStates["I3"])
	require.Equal(t, 1, finalCtxt.FixRounds, "一次完整 sweep 递增一轮")
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
