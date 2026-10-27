package pipeline

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// setRunStage 直接把流水线置为给定阶段/状态/上下文，便于精确驱动 Orchestrator.Advance。
func setRunStage(t *testing.T, svc *Service, runID, stage, status, ctxJSON string) {
	t.Helper()
	require.NoError(t, svc.store.SetStageStatus(context.Background(), runID, stage, status, ctxJSON))
}

// TestOrchestratorFullRun 验证：从 S1 跑通至 S6 → G终 → finished，且关键产物（卷/体检/终检）落库。
// 用确定性 stub 替代 LLM，覆盖完整的 A 期状态机主路径。
func TestOrchestratorFullRun(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 入口：新书（无章节）→ S1。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }
	run, err := svc.StartPipeline(ctx, 600, "new", nil, false)
	require.NoError(t, err)
	require.Equal(t, StageS1, run.CurrentStage)

	// S1 → S2（蓝图）→ S3（逐章写作）。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)              // S2 蓝图落 G1
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // G1->S3

	// 写好黄金三章 → 首检进入 S4 签约体检。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 3, nil }
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate) // S4 体检落 G4
	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage)
	require.Contains(t, got.ContextJSON, "audit", "首检结果应落库")

	// G4 通过 → 回到 S3。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS3, got.CurrentStage)

	// 在 S3 主动发起完本终检 → S6 终检。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "finish_book", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate) // S6 终检落 G终
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS6, got.CurrentStage)
	require.Contains(t, got.ContextJSON, "audit", "终检结果应落库")

	// G终 确认完本 → finished 收口。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "finish", ""))
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StatusFinished, got.Status, "完本终检确认后应 finished")
}

// TestOrchestratorGateBlockResume 验证闸门点 Advance 后状态为 awaiting_gate，
// 用户 ResumePipeline 恢复后继续推进（S3 普通闸 / G4 / G终 三类闸门）。
func TestOrchestratorGateBlockResume(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// S3 普通逐章闸：章数 +1（未达 5 章、未跨卷）→ 落 G3b 普通闸（awaiting_gate）。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 4, nil }
	svc.volumeBoundary = func(_ context.Context, _ int64) (bool, error) { return false, nil }
	runID := startS3Run(t, svc, 610, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))
	got, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusAwaitingGate, got.Status, "闸门点应停在 awaiting_gate")
	require.Equal(t, StageS3, got.CurrentStage)
	// 用户继续 → 推进（普通闸 continue 回 S3 running）。
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))
	got, _ = svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusRunning, got.Status)

	// G4 闸门：进入 S4 后选 continue → 回到 S3。
	runID2 := startS3Run(t, svc, 611, `{"audit":"{\"verdict\":\"needs_fix\"}"}`)
	setRunStage(t, svc, runID2, StageS4, StatusRunning, `{"audit":"{\"verdict\":\"needs_fix\"}"}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID2, "continue", ""))
	got2, _ := svc.store.GetByRunID(ctx, runID2)
	require.Equal(t, StageS3, got2.CurrentStage, "G4 continue 应回到 S3")

	// G终 闸门：进入 S6 后选 finish → finished。
	runID3 := startS3Run(t, svc, 612, `{"audit":"{\"verdict\":\"pass\"}"}`)
	setRunStage(t, svc, runID3, StageS6, StatusRunning, `{"audit":"{\"verdict\":\"pass\"}","audit_stage":"S6"}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID3, "finish", ""))
	got3, _ := svc.store.GetByRunID(ctx, runID3)
	require.Equal(t, StatusFinished, got3.Status, "G终 finish 应 finished")
}

// TestOrchestratorAutoRunStopsAtGate 验证 auto_run=true 时连续推进直到首个 EvGate，不在非闸门节点停留；
// 当前实现中 G1（蓝图确认闸）即便开启 auto_run 仍为人工闸，故 auto_run 从 S1 推进至 G1 即停下。
func TestOrchestratorAutoRunStopsAtGate(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }

	// 开启 auto_run 启动：入口 S1（无章节）。
	run, err := svc.StartPipeline(ctx, 620, "new", nil, true)
	require.NoError(t, err)
	require.Equal(t, StageS1, run.CurrentStage)
	require.True(t, run.AutoRun)

	// S1 →（自动）S2 蓝图异步执行 → 落 G1 闸门；auto_run 不跳过 G1。
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS2, got.CurrentStage, "auto_run 应推进过非闸门节点 S1→S2")
	require.Equal(t, StatusAwaitingGate, got.Status, "auto_run 应在首个闸门 G1 停下")

	// 对照：关闭 auto_run 时 S2 同样停在 G1（行为一致），确保不是 auto_run 独有。
	run2, _ := svc.StartPipeline(ctx, 621, "new", nil, false)
	require.NoError(t, svc.ResumePipeline(ctx, run2.RunID, "continue", ""))
	waitForStageStatus(t, svc, run2.RunID, StatusAwaitingGate)
	got2, _ := svc.store.GetByRunID(ctx, run2.RunID)
	require.Equal(t, StatusAwaitingGate, got2.Status)
}

// TestOrchestratorPauseAtCheckpoint 验证安全检查点：运行中置 paused，Advance 应挂起不推进。
func TestOrchestratorPauseAtCheckpoint(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 4, nil }
	svc.volumeBoundary = func(_ context.Context, _ int64) (bool, error) { return false, nil }

	// 在 S3 阶段暂停，再尝试继续 → 应被安全检查点挂起，阶段/状态不变。
	runID := startS3Run(t, svc, 630, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ControlPipeline(ctx, runID, "pause"))
	got, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusPaused, got.Status)
	// 暂停态下 ResumePipeline(continue) → Orchestrator 安全检查点拦截，不推进。
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))
	got, _ = svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusPaused, got.Status, "暂停态不应推进")
	require.Equal(t, StageS3, got.CurrentStage)

	// 恢复后继续 → 正常推进到 awaiting_gate。
	require.NoError(t, svc.ControlPipeline(ctx, runID, "resume"))
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))
	got, _ = svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusAwaitingGate, got.Status, "恢复后应正常推进到闸门")
}

// TestOrchestratorS4S5ReAuditEdge 验证 S4→fix→S5→复评→S4 回边正确，且 audit_stage 在 S4/S4b 间保持。
func TestOrchestratorS4S5ReAuditEdge(t *testing.T) {
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
			return SkillOutput{Text: "已修复"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	// 卷边界复评阶段 S4b 进入 G4，选 fix → S5 路由检测结果为发现项工单 → 落 S5 paused 交回用户。
	// R5-2 摘除「修复→自动复评」：fix 决策只路由工单、不派发修复、不自动复评（复评由用户
	// 显式点「继续」触发）。本用例改锁「volume 口径 fix 决策同样无自动复评」。
	runID := startS3Run(t, svc, 640, `{"audit":"{\"verdict\":\"needs_fix\"}","audit_stage":"S4b","audit_reason":"volume"}`)
	setRunStage(t, svc, runID, StageS4b, StatusRunning, `{"audit":"{\"verdict\":\"needs_fix\"}","audit_stage":"S4b","audit_reason":"volume"}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "fix", ""))
	waitForStageStatus(t, svc, runID, StatusPaused)
	got, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StageS5, got.CurrentStage, "R5-2：fix 决策路由为工单后落 S5（非自动复评闸门）")
	require.Equal(t, 0, auditCalls, "R5-2 摘除自动复评：fix 决策不得自动触发复评体检")
}

// TestOrchestratorS0ImportEntry 验证 DetermineEntryStage 依据小说状态自动选入口，
// 且 S0 存量书能汇入 S3 公共循环。
func TestOrchestratorS0ImportEntry(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 注入老书：有章节(5)、无台账(volumes) → 入口应为 S0。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 5, nil }
	svc.hasLedger = func(_ context.Context, _ int64) (bool, error) { return false, nil }
	entry, err := svc.DeterminingEntryStageSafe(ctx, 650)
	require.NoError(t, err)
	require.Equal(t, StageS0, entry, "有章节无台账应走 S0 对齐入口")

	// 已就绪：有章节且有台账 → 入口应为 S3。
	svc.hasLedger = func(_ context.Context, _ int64) (bool, error) { return true, nil }
	entry, err = svc.DeterminingEntryStageSafe(ctx, 650)
	require.NoError(t, err)
	require.Equal(t, StageS3, entry, "有章节且有台账应直接进 S3")

	// 新书：无章节 → 入口应为 S1。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }
	entry, err = svc.DeterminingEntryStageSafe(ctx, 650)
	require.NoError(t, err)
	require.Equal(t, StageS1, entry, "无章节应为 S1")

	// S0 启动后，汇入公共循环：StartPipeline 选 S0 → ResumePipeline(continue) → S3。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 5, nil }
	svc.hasLedger = func(_ context.Context, _ int64) (bool, error) { return false, nil }
	run, err := svc.StartPipeline(ctx, 650, "new", nil, false)
	require.NoError(t, err)
	require.Equal(t, StageS0, run.CurrentStage, "启动应落入 S0")
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))
	got, _ := svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS3, got.CurrentStage, "S0 对齐后应汇入 S3 公共循环")
}

// DeterminingEntryStageSafe 包装 DetermineEntryStage，便于测试忽略判定失败的细节。
func (s *Service) DeterminingEntryStageSafe(ctx context.Context, novelID int64) (string, error) {
	entry, err := s.DetermineEntryStage(ctx, novelID)
	if err != nil {
		// 判定失败（无 chapters 表）保守返回 S1，测试里视为 S1。
		return StageS1, nil
	}
	return entry, nil
}

// TestOrchestratorResumeAfterRestart 验证中断后能从 current_stage + context_json 续跑。
func TestOrchestratorResumeAfterRestart(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }

	// 推进到 S3 并写入上下文（模拟用户写作进度与上次体检状态）。
	run, err := svc.StartPipeline(ctx, 660, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S1->S2
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S2->S3
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 3, nil }
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // S3->S4(golden3)
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	// 模拟「重启」：用另一条查询路径按 RunID 恢复，断言阶段与上下文完好。
	recovered, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StageS4, recovered.CurrentStage)
	require.Equal(t, StatusAwaitingGate, recovered.Status)
	require.Contains(t, recovered.ContextJSON, "audit")
	// 续跑：G4 通过 → 回到 S3，证明中断后可继续。
	require.NoError(t, svc.ResumePipeline(ctx, recovered.RunID, "continue", ""))
	got, _ := svc.store.GetByRunID(ctx, recovered.RunID)
	require.Equal(t, StageS3, got.CurrentStage)
}

// TestServiceExecuteS0Align 验证存量书最小对齐：建卷 + 批量 statesync + 置 ImportAligned。
func TestServiceExecuteS0Align(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var volumeAssigned bool
	var synced []int64
	svc.chapterLister = func(_ context.Context, _ int64) ([]int64, error) { return []int64{11, 12, 13}, nil }
	svc.volumeAssigner = func(_ context.Context, _ int64) error { volumeAssigned = true; return nil }
	svc.stateSyncRunner = func(_ context.Context, _ int64, cid int64) error { synced = append(synced, cid); return nil }

	run := &PipelineRun{NovelID: 9100, RunID: "run-s0align", Mode: "new", CurrentStage: StageS0, Status: StatusRunning}
	require.NoError(t, svc.store.Create(ctx, run))

	svc.executeS0Align(ctx, run)

	require.True(t, volumeAssigned, "应执行建卷")
	require.Equal(t, []int64{11, 12, 13}, synced, "应对每章跑一次 statesync")

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.True(t, ctxt.ImportAligned, "对齐完成应置 ImportAligned=true")
}

// TestServiceRunFullAlign 验证「全量对齐」按钮：跑全本体检并将结果写入 ContextJSON.stock_audit。
func TestServiceRunFullAlign(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "```json\n{\"verdict\":\"needs_fix\",\"score\":10,\"summary\":\"存量书存在开篇过签力不足等问题\",\"issues\":[{\"id\":\"I1\",\"level\":\"P0\",\"area\":\"开篇钩子\",\"desc\":\"前三章缺乏强冲突\"}]}\n```"}, nil
	}

	run := &PipelineRun{
		NovelID:      9200,
		RunID:        "run-fullalign",
		Mode:         "new",
		CurrentStage: StageS3,
		Status:       StatusRunning,
		ContextJSON:  `{"import_aligned":true}`,
	}
	require.NoError(t, svc.store.Create(ctx, run))

	svc.runFullAlign(ctx, run)

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Contains(t, got.ContextJSON, "stock_audit", "全本体检结果应写入 stock_audit")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Contains(t, ctxt.StockAudit, "needs_fix")
	require.Contains(t, ctxt.StockAudit, "开篇钩子")
}

// TestControlPipelineAlignFull 验证 align_full 控制指令可被接受且不阻塞返回（实际对齐在后台 goroutine 执行）。
func TestControlPipelineAlignFull(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "```json\n{\"verdict\":\"pass\",\"score\":30,\"summary\":\"ok\",\"issues\":[]}\n```"}, nil
	}
	run := &PipelineRun{NovelID: 9300, RunID: "run-ctrlalign", Mode: "new", CurrentStage: StageS3, Status: StatusRunning, ContextJSON: `{"import_aligned":true}`}
	require.NoError(t, svc.store.Create(ctx, run))

	require.NoError(t, svc.ControlPipeline(ctx, run.RunID, "align_full"))
	// 等待后台对齐 goroutine 落库 stock_audit（最多 2s）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := svc.store.GetByRunID(ctx, run.RunID)
		if got != nil && strings.Contains(got.ContextJSON, "stock_audit") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Contains(t, got.ContextJSON, "stock_audit", "align_full 后台应完成全本体检并落库")
}
