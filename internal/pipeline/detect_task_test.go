package pipeline

// detect_task_test.go — R5-3 T1.2 统一检测任务入口 RunDetectionTask 验收（WBS §1 T1.2）：
// ① 四种 kind 各能独立启动、不依赖流水线阶段（无活跃流水线也能跑）；
// ② 结果落正确容器（chapter→质量档案；volume/book→发现项工单+检测 run 上下文；
//    structure→PatrolReport 张力报告）；
// ③ 队列不可用时降级 legacy 直跑（本文件 volume/book/structure 用例均跑在无 tasks 表
//    的测试库上，即降级路径；queue 用例覆盖队列路径）；
// ④ 幂等重入（同 kind+run 由队列 dedup 合并，容器复用同 RunID）。

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/volume"
)

// detectTaskL1Fixture 是章节检测（chapter-ci）的 L1 回执（契约块齐全）。
const detectTaskL1Fixture = "小结\n```json\n{\"findings\":[{\"id\":\"L1-1\",\"check\":\"continuity\",\"desc\":\"与前章矛盾\",\"chapter_ids\":[4,5]}],\"summary\":\"1 项\"}\n```\n"

// waitForDetectDone 等待一次检测完成事件（超时判失败）。
func waitForDetectDone(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("检测完成事件未到达")
		return nil
	}
}

// TestRunDetectionTaskChapterLandsQualityArchive 验收①②（chapter）：无活跃流水线
// 直接启动章节检测——经真实 RunChapterCIForChapter 落质量档案（chapter_quality），
// 回执解析成功记 checked；无 run 依赖（结果容器 RunID 为空）。
func TestRunDetectionTaskChapterLandsQualityArchive(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &quality.ChapterQuality{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var runSubCalls atomic.Int32
	svc.SetChapterCIRunner(func(c context.Context, novelID, chapterID int64) error {
		return mcp_tools.RunChapterCIForChapter(c, func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
			runSubCalls.Add(1)
			assert.Equal(t, "chapterci", req.AgentType)
			assert.Equal(t, int64(900), req.NovelID)
			return detectTaskL1Fixture, nil
		}, nil, db, slog.Default(), "detect-test", novelID, chapterID)
	})

	cid := int64(7)
	res, err := svc.RunDetectionTask(ctx, 900, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, res.Dispatched)
	require.Empty(t, res.RunID, "章节检测无 run 依赖（直写质量档案）")
	require.Equal(t, DetectionKindChapter, res.Kind)
	require.Equal(t, cid, *res.ScopeID)

	require.Eventually(t, func() bool {
		got, gerr := quality.NewStore(db, slog.Default()).GetByChapter(ctx, 900, cid)
		return gerr == nil && got != nil && got.L1Status == quality.L1StatusChecked
	}, 5*time.Second, 10*time.Millisecond, "L1 回执应落质量档案")
	require.Equal(t, int32(1), runSubCalls.Load())

	// 降级零提示零报错：未注入执行器时回执如实标记未派发，不报错。
	svc2 := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	res2, err := svc2.RunDetectionTask(ctx, 900, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.False(t, res2.Dispatched, "未注入执行器应降级跳过")

	// 参数校验（API 误用回错，不属降级红线）。
	_, err = svc.RunDetectionTask(ctx, 900, DetectionKindChapter, nil)
	require.Error(t, err)
	_, err = svc.RunDetectionTask(ctx, 900, "nope", nil)
	require.Error(t, err)
}

// TestRunDetectionTaskVolumeAndBookLandTickets 验收①②③（volume/book）：无活跃流水线
// 直接启动卷/书体检（库无 tasks 表=队列不可用→legacy 直跑）——发现项工单落检测
// 容器、结果写回容器上下文，detect_done 广播收口；检测容器出生即终态，不进任何
// 活跃 run 查询（GetByNovel/GetActiveVolumeRun 零污染）。
func TestRunDetectionTaskVolumeAndBookLandTickets(t *testing.T) {
	db := newTestDB(t) // 无 tasks 表：dispatchExecutor 降级 legacy 直跑（验收③）
	require.NoError(t, db.AutoMigrate(&volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		assert.Equal(t, "signability-audit", name)
		return SkillOutput{Text: "结论\n```json\n" + detectAuditFixture + "\n```"}, nil
	}
	vol := &volume.Volume{NovelID: 901, Name: "第一卷", SortOrder: 1}
	require.NoError(t, db.Create(vol).Error)

	doneCh := make(chan map[string]any, 4)
	t.Cleanup(swapEmitDetectDone(func(_ context.Context, p map[string]any) { doneCh <- p }))
	var gateEmits atomic.Int32
	t.Cleanup(swapEmitGate(func(_ context.Context, _ GatePayload) { gateEmits.Add(1) }))

	// ── 卷体检：scope=volumeID。
	res, err := svc.RunDetectionTask(ctx, 901, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	require.True(t, res.Dispatched)
	require.Equal(t, vol.ID, *res.ScopeID)

	p := waitForDetectDone(t, doneCh)
	require.Equal(t, DetectionKindVolume, p["kind"])
	require.Equal(t, "needs_fix", p["verdict"])
	require.Equal(t, 2, p["issue_count"])
	require.Equal(t, res.RunID, p["run_id"])

	run, gerr := svc.store.GetByRunID(ctx, res.RunID)
	require.NoError(t, gerr)
	require.Equal(t, detectModeOf(DetectionKindVolume), run.Mode, "卷检测容器按 kind 分容器（T1.3）")
	require.Equal(t, StatusFinished, run.Status, "检测容器出生即终态")
	require.Equal(t, detectContainerStage, run.CurrentStage)
	require.NotNil(t, run.VolumeID)
	require.Equal(t, vol.ID, *run.VolumeID)

	// 结果落正确容器②：工单 keyed by 检测容器 runID + 结果写回容器上下文。
	rows, lerr := svc.store.ListIssues(ctx, 901, res.RunID)
	require.NoError(t, lerr)
	require.Len(t, rows, 2, "卷体检发现项应落检测容器工单")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(run.ContextJSON), &ctxt))
	require.Contains(t, ctxt.Audit, "检测出2条问题")
	require.Len(t, ctxt.RewriteBacklog, 1)

	// 验收①「不依赖流水线阶段」+ 容器零污染：无活跃 run，检测容器不进活跃查询。
	_, gerr = svc.store.GetByNovel(ctx, 901)
	require.ErrorIs(t, gerr, gorm.ErrRecordNotFound, "检测容器不得成为活跃流水线")
	_, gerr = svc.store.GetActiveVolumeRun(ctx, 901, vol.ID)
	require.ErrorIs(t, gerr, gorm.ErrRecordNotFound, "检测容器不得被卷级 scoped 复用口捡走")

	// ── 书体检：scope 全书。
	res2, err := svc.RunDetectionTask(ctx, 901, DetectionKindBook, nil)
	require.NoError(t, err)
	require.NotEqual(t, res.RunID, res2.RunID, "书/卷检测各自独立容器")
	p2 := waitForDetectDone(t, doneCh)
	require.Equal(t, DetectionKindBook, p2["kind"])
	run2, gerr := svc.store.GetByRunID(ctx, res2.RunID)
	require.NoError(t, gerr)
	require.Nil(t, run2.VolumeID)
	rows2, lerr := svc.store.ListIssues(ctx, 901, res2.RunID)
	require.NoError(t, lerr)
	require.Len(t, rows2, 2)

	// 检测-only：全程零闸门广播、零阶段事件。
	require.Zero(t, gateEmits.Load(), "检测任务零闸门广播")

	// 卷不存在（API 误用）回错，不落野容器。
	bogus := int64(99999)
	_, err = svc.RunDetectionTask(ctx, 901, DetectionKindVolume, &bogus)
	require.Error(t, err)
}

// TestRunDetectionTaskStructureLandsPatrolReport 验收①②（structure）：无活跃流水线
// 直接启动巡检·结构——张力报告落检测容器 PatrolReport（只报告：零发现项工单），
// 完成沿用既有 consistency_patrol 事件通道。
func TestRunDetectionTaskStructureLandsPatrolReport(t *testing.T) {
	db := newTestDB(t) // 队列不可用→legacy 直跑（验收③）
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		assert.Equal(t, "consistency-patrol", name)
		return SkillOutput{Text: "```json\n" + detectAuditFixture + "\n```"}, nil
	}
	patrolCh := make(chan map[string]any, 1)
	t.Cleanup(swapEmitConsistencyPatrol(func(_ context.Context, p map[string]any) { patrolCh <- p }))

	res, err := svc.RunDetectionTask(ctx, 902, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.True(t, res.Dispatched)

	select {
	case p := <-patrolCh:
		require.Equal(t, res.RunID, p["run_id"])
		require.Equal(t, int64(902), p["novel_id"])
	case <-time.After(5 * time.Second):
		t.Fatal("巡检完成事件未到达")
	}

	run, gerr := svc.store.GetByRunID(ctx, res.RunID)
	require.NoError(t, gerr)
	require.Equal(t, detectModeOf(DetectionKindStructure), run.Mode, "巡检·结构检测容器按 kind 分容器（T1.3）")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(run.ContextJSON), &ctxt))
	require.Contains(t, ctxt.PatrolReport, "检测出2条问题", "张力报告应落检测容器")
	require.NotNil(t, ctxt.PatrolReportAt, "张力报告落库应打时间戳（T1.3 双源合并判据）")
	rows, lerr := svc.store.ListIssues(ctx, 902, res.RunID)
	require.NoError(t, lerr)
	require.Empty(t, rows, "巡检只报告不改写：零发现项工单")
}

// TestRunDetectionTaskAuditRejectionLandsNoTickets 验收回归：签约体检（S4）被模型拒绝/
// 安全审核拦截（err==nil，content 为拒绝语）时，不得被 parseFreshAudit 兜底成 needs_fix
// 的假体检结果并生成假工单；应落地 verdict=error 失败态、零发现项工单，并广播完成事件。
func TestRunDetectionTaskAuditRejectionLandsNoTickets(t *testing.T) {
	db := newTestDB(t) // 无 tasks 表→legacy 直跑（验收③）
	require.NoError(t, db.AutoMigrate(&volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		assert.Equal(t, "signability-audit", name)
		return SkillOutput{Text: "The request was rejected because it was considered high risk"}, nil
	}
	vol := &volume.Volume{NovelID: 904, Name: "第一卷", SortOrder: 1}
	require.NoError(t, db.Create(vol).Error)

	doneCh := make(chan map[string]any, 4)
	t.Cleanup(swapEmitDetectDone(func(_ context.Context, p map[string]any) { doneCh <- p }))

	res, err := svc.RunDetectionTask(ctx, 904, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	require.True(t, res.Dispatched)

	p := waitForDetectDone(t, doneCh)
	require.Equal(t, DetectionKindVolume, p["kind"])
	require.Equal(t, "error", p["verdict"], "拒绝应标记为 error 而非 needs_fix")
	require.Equal(t, 0, p["issue_count"], "拒绝不应产生任何发现项")

	run, gerr := svc.store.GetByRunID(ctx, res.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(run.ContextJSON), &ctxt))
	require.NotEmpty(t, ctxt.Audit, "失败态应落地（verdict=error）")
	var audit AuditData
	require.NoError(t, json.Unmarshal([]byte(ctxt.Audit), &audit))
	require.Equal(t, "error", audit.Verdict)
	require.Empty(t, audit.Issues, "拒绝不应产生任何张力/问题项")
	require.Contains(t, audit.Summary, "模型拒绝", "失败态应给出可读原因")

	rows, lerr := svc.store.ListIssues(ctx, 904, res.RunID)
	require.NoError(t, lerr)
	require.Empty(t, rows, "拒绝绝不应生成假工单")
}

// TestRunDetectionTaskQueueDedup 验收④（幂等重入）：队列可用时同 kind+run 在执行中
// 重复派发被 dedup 合并（只跑一次），检测容器同 scope 复用同 RunID。
func TestRunDetectionTaskQueueDedup(t *testing.T) {
	db := newQueueTestDB(t)
	require.NoError(t, db.AutoMigrate(&volume.Volume{}))
	svc := newQueueService(t, db)
	ctx := context.Background()
	vol := &volume.Volume{NovelID: 903, Name: "第一卷", SortOrder: 1}
	require.NoError(t, db.Create(vol).Error)

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return SkillOutput{Text: "```json\n" + detectAuditFixture + "\n```"}, nil
	}
	doneCh := make(chan map[string]any, 2)
	t.Cleanup(swapEmitDetectDone(func(_ context.Context, p map[string]any) { doneCh <- p }))

	svc.StartTaskQueue()
	defer svc.StopTaskQueue()

	res1, err := svc.RunDetectionTask(ctx, 903, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("检测执行体未启动")
	}
	// 执行中重复派发（同 scope）：容器复用同 RunID，队列 dedup 合并不二次执行。
	res2, err := svc.RunDetectionTask(ctx, 903, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	require.Equal(t, res1.RunID, res2.RunID, "同 scope 检测容器应复用")
	rows, lerr := svc.taskStore.ListByDedupKey(ctx, taskKindDetect+":"+res1.RunID)
	require.NoError(t, lerr)
	require.Len(t, rows, 1, "活跃同键检测任务被合并")

	close(release)
	waitForDetectDone(t, doneCh)
	require.Equal(t, int32(1), calls.Load(), "dedup 合并后检测只执行一次")
}
