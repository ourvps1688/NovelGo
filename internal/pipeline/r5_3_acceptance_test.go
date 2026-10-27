package pipeline

// r5_3_acceptance_test.go — R5-3 T4.1 双验收（WBS §4 验收标准本体）的端到端载体：
//
//	验收 A（检测独立性）：无活跃流水线时四类检测任务皆可单独启动并落正确容器
//	  （章节→质量档案、卷/书→发现项工单、巡检→张力报告）；检测-only 零
//	  auto-advance/fix/gate（零闸门/阶段/修复广播、run 不推进、零活跃 run 污染）。
//	验收 B（编排预设 + 勿回退）：流水线作为预设仍一键跑「写→检测」——黄金三章
//	  首检自动进 S4 落工单且裁决（G4 闸门）保留；自动检测节奏不改纯手动（源码
//	  grep 锁定黄金三章首检/完本终检/卷边界与里程碑复检/写后章节 CI 钩子四点 +
//	  检测核心零裁决调用锁）。PanelId 零删除由前端 PipelineWizard.test（22 项）/
//	  p3-acceptance 承载；P0–P2 工件零触碰由门禁 git diff grep 断言承载（结论记 WBS）。

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/volume"
)

// TestR53Acceptance_DetectIndependence 验收 A：无活跃流水线（既无 run 也全程不建
// run）时四类检测逐一单独启动——章节落质量档案、卷/书落发现项工单、巡检落张力
// 报告；检测-only 零 auto-advance/fix/gate：零闸门/阶段/修复/重写广播、零修复与
// 重写技能派发、检测容器出生即终态且阶段恒 detect、检测不产生活跃流水线。
func TestR53Acceptance_DetectIndependence(t *testing.T) {
	db := newTestDB(t) // 无 tasks 表：队列不可用 → legacy 直跑（降级逐字一致）
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &quality.ChapterQuality{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 技能派发记录：断言检测-only 零修复/重写技能派发。
	var mu sync.Mutex
	var skills []string
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		mu.Lock()
		skills = append(skills, name)
		mu.Unlock()
		if name == "consistency-patrol" {
			return SkillOutput{Text: "```json\n" + detectAuditFixture + "\n```"}, nil
		}
		return SkillOutput{Text: "结论\n```json\n" + detectAuditFixture + "\n```"}, nil
	}
	// 章节检测走真实 chapter-ci 链路（stub 子代理回执，契约块齐全）。
	svc.SetChapterCIRunner(func(c context.Context, novelID, chapterID int64) error {
		return mcp_tools.RunChapterCIForChapter(c, func(_ context.Context, _ mcp_tools.SubAgentRequest) (string, error) {
			return detectTaskL1Fixture, nil
		}, nil, db, slog.Default(), "r53-accept", novelID, chapterID)
	})

	// 广播捕获：检测-only 零裁决广播（与 TestR53Acceptance_PresetKeepsGate 对照）。
	var gateN, stageN, fixStartN, fixDoneN, rewriteN atomic.Int32
	t.Cleanup(swapEmitGate(func(context.Context, GatePayload) { gateN.Add(1) }))
	t.Cleanup(swapEmitStage(func(context.Context, StagePayload) { stageN.Add(1) }))
	t.Cleanup(swapEmitFixStart(func(context.Context, map[string]any) { fixStartN.Add(1) }))
	t.Cleanup(swapEmitFixDone(func(context.Context, map[string]any) { fixDoneN.Add(1) }))
	t.Cleanup(swapEmitRewriteBacklog(func(context.Context, map[string]any) { rewriteN.Add(1) }))
	detectCh := make(chan map[string]any, 2)
	t.Cleanup(swapEmitDetectDone(func(_ context.Context, p map[string]any) { detectCh <- p }))
	patrolCh := make(chan map[string]any, 1)
	t.Cleanup(swapEmitConsistencyPatrol(func(_ context.Context, p map[string]any) { patrolCh <- p }))

	// 前提：无活跃流水线（「检测可脱离流水线任意时刻单独启动」的基座断言）。
	_, gerr := svc.store.GetByNovel(ctx, 905)
	require.ErrorIs(t, gerr, gorm.ErrRecordNotFound, "验收前提：无活跃流水线")

	vol := &volume.Volume{NovelID: 905, Name: "第一卷", SortOrder: 1}
	require.NoError(t, db.Create(vol).Error)

	// ── 章节检测 → 质量档案（chapter_quality，无 run 依赖）。
	cid := int64(11)
	resCh, err := svc.RunDetectionTask(ctx, 905, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, resCh.Dispatched)
	require.Empty(t, resCh.RunID, "章节检测无 run 依赖")
	require.Eventually(t, func() bool {
		got, qerr := quality.NewStore(db, slog.Default()).GetByChapter(ctx, 905, cid)
		return qerr == nil && got != nil && got.L1Status == quality.L1StatusChecked
	}, 5*time.Second, 10*time.Millisecond, "章节检测应落质量档案")

	// ── 卷体检 → 发现项工单（卷级检测容器）。
	resV, err := svc.RunDetectionTask(ctx, 905, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	pV := waitForDetectDone(t, detectCh)
	require.Equal(t, DetectionKindVolume, pV["kind"])
	rowsV, lerr := svc.store.ListIssues(ctx, 905, resV.RunID)
	require.NoError(t, lerr)
	require.Len(t, rowsV, 2, "卷体检发现项应落工单")

	// ── 书体检 → 发现项工单（全书检测容器，与卷分容器）。
	resB, err := svc.RunDetectionTask(ctx, 905, DetectionKindBook, nil)
	require.NoError(t, err)
	require.NotEqual(t, resV.RunID, resB.RunID, "卷/书检测各自独立容器")
	pB := waitForDetectDone(t, detectCh)
	require.Equal(t, DetectionKindBook, pB["kind"])
	rowsB, lerr := svc.store.ListIssues(ctx, 905, resB.RunID)
	require.NoError(t, lerr)
	require.Len(t, rowsB, 2, "书体检发现项应落工单")

	// ── 巡检·结构 → 张力报告（只报告不改写：零工单）。
	resS, err := svc.RunDetectionTask(ctx, 905, DetectionKindStructure, nil)
	require.NoError(t, err)
	select {
	case p := <-patrolCh:
		require.Equal(t, resS.RunID, p["run_id"])
	case <-time.After(5 * time.Second):
		t.Fatal("巡检完成事件未到达")
	}
	runS, gerr := svc.store.GetByRunID(ctx, resS.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(runS.ContextJSON), &ctxt))
	require.Contains(t, ctxt.PatrolReport, "检测出2条问题", "巡检张力报告应落检测容器")
	rowsS, lerr := svc.store.ListIssues(ctx, 905, resS.RunID)
	require.NoError(t, lerr)
	require.Empty(t, rowsS, "巡检只报告不改写：零发现项工单")

	// ── 检测-only 零 auto-advance/fix/gate。
	require.Zero(t, gateN.Load(), "检测-only 零闸门广播")
	require.Zero(t, stageN.Load(), "检测-only 零阶段推进广播")
	require.Zero(t, fixStartN.Load(), "检测-only 零修复派发")
	require.Zero(t, fixDoneN.Load(), "检测-only 零修复收口")
	require.Zero(t, rewriteN.Load(), "检测-only 零重写清单广播")
	mu.Lock()
	defer mu.Unlock()
	for _, name := range skills {
		require.NotContains(t, []string{"chapter-rewrite", "signability-fix", "blueprint-fix"},
			name, "检测-only 不得派发修复/重写技能：%s", name)
	}
	// run 不被推进：三容器出生即终态、阶段恒 detect，全程不产生活跃流水线。
	for _, rid := range []string{resV.RunID, resB.RunID, resS.RunID} {
		r, rerr := svc.store.GetByRunID(ctx, rid)
		require.NoError(t, rerr)
		require.Equal(t, detectContainerStage, r.CurrentStage, "检测容器阶段不被推进")
		require.Equal(t, StatusFinished, r.Status, "检测容器出生即终态")
	}
	_, gerr = svc.store.GetByNovel(ctx, 905)
	require.ErrorIs(t, gerr, gorm.ErrRecordNotFound, "四类检测全程零活跃流水线（零 auto-advance）")
}

// TestR53Acceptance_PresetKeepsGate 验收 B（行为面）：流水线作为编排预设仍一键跑
// 「写→检测」——从零起步写到黄金三章，首检自动进 S4 落发现项工单（全程零手动检测
// 入口介入）；流水线内体检仍接裁决（G4 闸门广播）——检测-only 零闸门与预设内
// 闸门裁决对照，契约 0.3「Orchestrator.Advance 裁决不动」成立。
func TestR53Acceptance_PresetKeepsGate(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	var mu sync.Mutex
	var skills []string
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		mu.Lock()
		skills = append(skills, name)
		mu.Unlock()
		if name == "signability-audit" {
			return SkillOutput{Text: "体检结论：\n```json\n" + detectAuditFixture + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}
	var gates []GatePayload
	t.Cleanup(swapEmitGate(func(_ context.Context, p GatePayload) {
		mu.Lock()
		gates = append(gates, p)
		mu.Unlock()
	}))

	// 编排预设一键「写→检测」：S1 孵化 → S2 蓝图 → S3 写作 → 黄金三章首检自动 S4。
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 0, nil }
	run, err := svc.StartPipeline(ctx, 490, "new", nil, false)
	require.NoError(t, err)
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 3, nil } // 模拟写完黄金三章
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))             // S1→S2
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)                          // S2 蓝图落闸
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", ""))             // S2→S3
	got, _ := svc.GetPipelineState(ctx, 490)
	require.Equal(t, StageS3, got.CurrentStage)
	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "continue", "")) // 黄金三章首检自动进 S4

	// 自动节奏保留：体检由预设自动串起并落发现项工单（检测结果一键可达）。
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)
	got, _ = svc.store.GetByRunID(ctx, run.RunID)
	require.Equal(t, StageS4, got.CurrentStage, "黄金三章首检应自动进 S4（自动检测节奏保留，不改纯手动）")
	require.Contains(t, got.ContextJSON, "needs_fix", "体检结果应落上下文")
	rows, lerr := svc.store.ListIssues(ctx, 490, run.RunID)
	require.NoError(t, lerr)
	require.Len(t, rows, 2, "预设内体检发现项应落工单（写→检测一键串起）")

	// 流水线内体检仍接裁决：G4 闸门广播（与验收 A 零闸门对照）；
	// 技能路由断言（T4.1 抓获的 AuditKind 泄漏回归锁）：黄金三章首检必须走
	// signability-audit——蓝图体检置 AuditKind=blueprint 后不复位会静默改走
	// blueprint-audit，签约体检被吞。
	mu.Lock()
	defer mu.Unlock()
	var hasG4 bool
	for _, g := range gates {
		if g.GateID == GateG4 && g.Stage == StageS4 {
			hasG4 = true
		}
	}
	require.True(t, hasG4, "流水线内体检应接 G4 闸门裁决（编排预设自动节奏，契约 0.3 裁决不动）")
	var signabilityN, blueprintAuditN int
	for _, name := range skills {
		switch name {
		case "signability-audit":
			signabilityN++
		case "blueprint-audit":
			blueprintAuditN++
		}
	}
	require.Equal(t, 1, signabilityN, "黄金三章首检应跑 signability-audit（签约体检不得被蓝图路由吞掉）")
	require.Equal(t, 1, blueprintAuditN, "蓝图复体检只应跑一次（S2 蓝图轮）")
}

// TestR53Acceptance_AutoCadenceSourceLock 验收 B（grep 断言）：自动检测节奏保留
// （不改纯手动）的源码锁——黄金三章首检/完本终检/卷边界与里程碑复检/写后章节 CI
// 钩子四个自动触发点不得退役；检测-only 核心零裁决调用锁（零 auto-advance/fix/gate
// 的源码面判据，与 TestR53Acceptance_DetectIndependence 行为面互证）。
func TestR53Acceptance_AutoCadenceSourceLock(t *testing.T) {
	orch, err := os.ReadFile("orchestrator.go")
	require.NoError(t, err)
	orchSrc := string(orch)
	require.Contains(t, orchSrc, `enterAudit(ctx, run, "golden3")`, "黄金三章首检自动触发不得退役")
	require.Contains(t, orchSrc, `o.svc.enterFinalAudit(ctx, run)`, "完本终检入口不得退役")
	require.Contains(t, orchSrc, `reason = "volume"`, "卷边界复检触发不得退役")
	require.Contains(t, orchSrc, `GateG3b`, "每 5 章里程碑复检闸不得退役")

	rw, err := os.ReadFile("../mcp_tools/rw_tools.go")
	require.NoError(t, err)
	require.Contains(t, string(rw), `ScheduleChapterCI(ctx, tc, ch, proposed)`,
		"写后章节 CI 自动钩子不得退役（自动节奏不改纯手动）")

	// 检测-only 核心零裁决调用：detect.go 不得出现闸门/修复/重写发射或 Advance 裁决。
	det, err := os.ReadFile("detect.go")
	require.NoError(t, err)
	detSrc := string(det)
	for _, forbidden := range []string{"emitGate(", "emitFixStart(", "emitFixDone(", "emitRewriteBacklog(", ".Advance("} {
		require.NotContains(t, detSrc, forbidden, "检测-only 核心不得调用裁决/修复发射器：%s", forbidden)
	}
}
