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
	// 复评落地时 executeAudit 会重置 FixStates（新一轮体检 = 全新问题清单，旧断点作废）。
	// 批次断点正确性已由上方调用序列验证：恢复后首批重试 I1、已修项不重复修复。
	require.Empty(t, finalCtxt.FixStates, "复评落地后旧修复断点应被重置")
	require.Equal(t, 1, finalCtxt.FixRounds, "一次完整 sweep 递增一轮")
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
		`{"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱","fix_type":"body"},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"五章程序文","fix_type":"ledger"}]}`
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

// TestBodyIssuesOf 验证 bodyIssuesOf 仅析出正文级项，并尽力解析涉及章节号。
func TestBodyIssuesOf(t *testing.T) {
	audit := AuditData{Issues: []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇过签力", Desc: "前三章未立人设", FixType: FixTypeBody},
		{ID: "I2", Level: "P1", Area: "长线牵引", Desc: "timeline#42 伏笔未回收", FixType: "ledger"},
		{ID: "I3", Level: "P1", Area: "节奏爽点", Desc: "第6-8章爽点断档", FixType: FixTypeBody},
	}}
	items := bodyIssuesOf(audit)
	require.Len(t, items, 2, "只析出 body 项")
	require.Equal(t, "I1", items[0].ID)
	require.Empty(t, items[0].Chapters, "I1 未带章节号则章节列表为空")
	require.Equal(t, []int64{6, 7, 8}, items[1].Chapters, "第6-8章区间应解析为 [6,7,8]")
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
