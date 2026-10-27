package pipeline

// detect_status_test.go — R5-3 T1.3 检测任务结果消费面验收（WBS §1 T1.3）：
// ① 检测容器按 kind 分化（开放设计点①）：book/structure 全书容器互不干扰
//    （按 kind 的最近一次粒度 + detect/patrol 并发写同一容器的竞态根治）；
// ② GetPatrolReport 双源合并（开放设计点②）：活跃流水线 run + structure 检测容器
//    按落库时间戳最新者胜；旧数据无时间戳回退 run.UpdatedAt；无报告返回 nil；
// ③ 检测状态查询：四 kind「进行态 + 最近一次结果摘要」，概览 ListDetectionTaskStatuses；
// ④ 降级零提示：无历史/坏数据一律零结果，不报错。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/quality"
)

// detectStatusAuditJSON 是手工装配的结构化体检结果（AuditData 形状）。
func detectStatusAuditJSON(verdict string, issueCount int) string {
	issues := ""
	for i := 0; i < issueCount; i++ {
		if i > 0 {
			issues += ","
		}
		issues += fmt.Sprintf(`{"id":"I%d","level":"P1","area":"测试","desc":"问题%d"}`, i+1, i+1)
	}
	return fmt.Sprintf(`{"verdict":%q,"score":30,"summary":"测试结论","issues":[%s]}`, verdict, issues)
}

// detectStatusGateCtx 装配 GateContext 胖 JSON 夹具：key=audit|patrol_report 的值是
// AuditData 形状 JSON 文本——GateContext 对应字段是 string，落库为 JSON 串字面量
// （与 json.Marshal(*GateContext) 的真实产物同形，夹具不得塞裸对象）。
// stamp 非空时附 patrol_report_at 落库时间戳（T1.3 双源合并判据）。
func detectStatusGateCtx(key, auditJSON, stamp string) string {
	lit, _ := json.Marshal(auditJSON)
	if stamp == "" {
		return fmt.Sprintf(`{%q:%s}`, key, lit)
	}
	return fmt.Sprintf(`{%q:%s,"patrol_report_at":%q}`, key, lit, stamp)
}

// TestDetectContainersPerKindIsolation 验收①（开放设计点①）：book 与 structure 共享
// 全书作用域但各自独立容器（mode='detect:<kind>'）；同 kind+scope 复用同 RunID。
func TestDetectContainersPerKindIsolation(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	book, err := svc.ensureDetectRun(ctx, 401, DetectionKindBook, nil)
	require.NoError(t, err)
	structRun, err := svc.ensureDetectRun(ctx, 401, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.NotEqual(t, book.RunID, structRun.RunID, "book/structure 全书检测不得共享容器")
	require.Equal(t, detectModeOf(DetectionKindBook), book.Mode)
	require.Equal(t, detectModeOf(DetectionKindStructure), structRun.Mode)

	// 同 kind+scope 复用同 RunID（新检测=同一容器换新一轮结果）。
	book2, err := svc.ensureDetectRun(ctx, 401, DetectionKindBook, nil)
	require.NoError(t, err)
	require.Equal(t, book.RunID, book2.RunID)
	struct2, err := svc.ensureDetectRun(ctx, 401, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.Equal(t, structRun.RunID, struct2.RunID)

	// 卷检测独立第三容器；GetDetectRun 按 kind+scope 各取各的，互不串门。
	volID := int64(9)
	volRun, err := svc.ensureDetectRun(ctx, 401, DetectionKindVolume, &volID)
	require.NoError(t, err)
	require.Equal(t, detectModeOf(DetectionKindVolume), volRun.Mode)
	got, err := svc.store.GetDetectRun(ctx, 401, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.Equal(t, structRun.RunID, got.RunID, "structure 查询不得被 book 容器遮蔽")
	got, err = svc.store.GetDetectRun(ctx, 401, DetectionKindVolume, &volID)
	require.NoError(t, err)
	require.Equal(t, volRun.RunID, got.RunID)
}

// TestGetPatrolReportMergesSources 验收②（开放设计点②）：双源合并按落库时间戳
// 最新者胜（两个方向都验证）；旧数据无时间戳回退 run.UpdatedAt；无报告 nil。
func TestGetPatrolReportMergesSources(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	patrolCtx := func(stamp string) string {
		return detectStatusGateCtx("patrol_report", detectStatusAuditJSON("needs_fix", 1), stamp)
	}

	// ── 只有 structure 检测容器有报告 → Source=detect。
	container, err := svc.ensureDetectRun(ctx, 402, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, container.RunID, detectContainerStage, StatusFinished,
		patrolCtx("2026-01-01T00:00:00Z")))

	view, err := svc.GetPatrolReport(ctx, 402)
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Equal(t, PatrolSourceDetect, view.Source)
	require.Equal(t, container.RunID, view.RunID)
	var report AuditData
	require.NoError(t, json.Unmarshal([]byte(view.Report), &report))
	require.Equal(t, "needs_fix", report.Verdict)

	// ── 活跃流水线 run 报告更新（时间戳更新）→ Source=pipeline。
	active := &PipelineRun{NovelID: 402, RunID: "active-402", Mode: "new",
		CurrentStage: "S3", Status: StatusRunning, ContextJSON: patrolCtx("2026-02-01T00:00:00Z")}
	require.NoError(t, svc.store.Create(ctx, active))
	view, err = svc.GetPatrolReport(ctx, 402)
	require.NoError(t, err)
	require.Equal(t, PatrolSourcePipeline, view.Source, "活跃 run 报告更新应胜出")
	require.Equal(t, "active-402", view.RunID)

	// ── 检测容器报告再更新（最新时间戳）→ Source=detect 反向胜出。
	require.NoError(t, svc.store.SetStageStatus(ctx, container.RunID, detectContainerStage, StatusFinished,
		patrolCtx("2026-03-01T00:00:00Z")))
	view, err = svc.GetPatrolReport(ctx, 402)
	require.NoError(t, err)
	require.Equal(t, PatrolSourceDetect, view.Source, "检测容器报告更新应反向胜出")

	// ── 旧数据无时间戳：回退 run.UpdatedAt（≈now，最新）→ pipeline 胜出。
	require.NoError(t, svc.store.SetStageStatus(ctx, "active-402", "S3", StatusRunning, patrolCtx("")))
	view, err = svc.GetPatrolReport(ctx, 402)
	require.NoError(t, err)
	require.Equal(t, PatrolSourcePipeline, view.Source, "无时间戳回退 run.UpdatedAt 参与比较")

	// ── 两源皆无报告 → nil（降级零提示）。
	_, err = svc.ensureDetectRun(ctx, 403, DetectionKindStructure, nil)
	require.NoError(t, err)
	view, err = svc.GetPatrolReport(ctx, 403)
	require.NoError(t, err)
	require.Nil(t, view)
	view, err = svc.GetPatrolReport(ctx, 999)
	require.NoError(t, err)
	require.Nil(t, view, "查询失败/无历史一律降级零结果")
}

// TestDetectionTaskStatusQueries 验收③：四 kind 状态查询（进行态 + 最近一次结果摘要）
// 与概览 ListDetectionTaskStatuses；未知 kind 回错。
func TestDetectionTaskStatusQueries(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&quality.ChapterQuality{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 落四类结果容器/档案（手工装配，落法本身已由 detect_task_test 覆盖）。
	require.NoError(t, db.Create(&quality.ChapterQuality{
		NovelID: 404, ChapterID: 5, ReadingNumber: 5, L1Status: quality.L1StatusChecked,
		L0Findings: "[]", L1Findings: `[{"id":"L1-1","check":"continuity","level":"warning","desc":"矛盾","status":""}]`,
		CheckedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
	}).Error)
	volID := int64(11)
	volRun, err := svc.ensureDetectRun(ctx, 404, DetectionKindVolume, &volID)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, volRun.RunID, detectContainerStage, StatusFinished,
		detectStatusGateCtx("audit", detectStatusAuditJSON("needs_fix", 2), "")))
	bookRun, err := svc.ensureDetectRun(ctx, 404, DetectionKindBook, nil)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, bookRun.RunID, detectContainerStage, StatusFinished,
		detectStatusGateCtx("audit", detectStatusAuditJSON("pass", 0), "")))
	structRun, err := svc.ensureDetectRun(ctx, 404, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, structRun.RunID, detectContainerStage, StatusFinished,
		detectStatusGateCtx("patrol_report", detectStatusAuditJSON("needs_fix", 3), "2026-04-01T00:00:00Z")))

	// ── chapter：结果=质量档案（L1Status + L0/L1 发现数）。
	cid := int64(5)
	st, err := svc.GetDetectionTaskStatus(ctx, 404, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.Equal(t, DetectionKindChapter, st.Kind)
	require.Equal(t, cid, *st.ScopeID)
	require.Equal(t, quality.L1StatusChecked, st.LastVerdict)
	require.Equal(t, 1, st.LastIssueCount)
	require.Empty(t, st.LastRunID, "章节检测无结果容器")
	require.NotNil(t, st.LastRunAt)
	// 概览口径（scopeID 空）取最近一次档案行。
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindChapter, nil)
	require.NoError(t, err)
	require.Equal(t, quality.L1StatusChecked, st.LastVerdict)

	// ── volume：结果=容器 Audit（scope 取本卷容器）。
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindVolume, &volID)
	require.NoError(t, err)
	require.Equal(t, volRun.RunID, st.LastRunID)
	require.Equal(t, "needs_fix", st.LastVerdict)
	require.Equal(t, 2, st.LastIssueCount)
	require.NotNil(t, st.LastRunAt)

	// ── book：结果=容器 Audit。
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindBook, nil)
	require.NoError(t, err)
	require.Equal(t, bookRun.RunID, st.LastRunID)
	require.Equal(t, "pass", st.LastVerdict)
	require.Zero(t, st.LastIssueCount)

	// ── structure：结果=容器 PatrolReport，落库时间=patrol_report_at。
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindStructure, nil)
	require.NoError(t, err)
	require.Equal(t, structRun.RunID, st.LastRunID)
	require.Equal(t, "needs_fix", st.LastVerdict)
	require.Equal(t, 3, st.LastIssueCount)
	require.NotNil(t, st.LastRunAt)
	require.Equal(t, 2026, st.LastRunAt.Year())

	// ── 进行态：派发即 true，收口回 false（runID 注销与 key 注销两口皆验）。
	svc.beginDetect(DetectionKindVolume, 404, &volID, volRun.RunID)
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindVolume, &volID)
	require.NoError(t, err)
	require.True(t, st.Running)
	svc.endDetectByRunID(volRun.RunID)
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindVolume, &volID)
	require.NoError(t, err)
	require.False(t, st.Running)

	svc.beginDetect(DetectionKindChapter, 404, &cid, "")
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, st.Running)
	svc.endDetect(DetectionKindChapter, 404, &cid)
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.False(t, st.Running)

	// ── 概览 ListDetectionTaskStatuses：固定四 kind 顺序，卷级进行态对概览可见。
	otherVol := int64(12)
	svc.beginDetect(DetectionKindVolume, 404, &otherVol, "run-x")
	list, err := svc.ListDetectionTaskStatuses(ctx, 404)
	require.NoError(t, err)
	require.Len(t, list, 4)
	require.Equal(t, DetectionKindChapter, list[0].Kind)
	require.Equal(t, DetectionKindVolume, list[1].Kind)
	require.Equal(t, DetectionKindBook, list[2].Kind)
	require.Equal(t, DetectionKindStructure, list[3].Kind)
	require.True(t, list[1].Running, "任一卷检测进行中即 kind 级概览 Running")
	require.Equal(t, "needs_fix", list[1].LastVerdict, "概览取最近一次卷检测结果")
	svc.endDetectByRunID("run-x")

	// ── 未知 kind 回错（API 误用，不属降级红线）；坏数据降级零结果。
	_, err = svc.GetDetectionTaskStatus(ctx, 404, "nope", nil)
	require.Error(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, bookRun.RunID, detectContainerStage, StatusFinished,
		`{"audit":"not-json"}`))
	st, err = svc.GetDetectionTaskStatus(ctx, 404, DetectionKindBook, nil)
	require.NoError(t, err)
	require.Empty(t, st.LastVerdict, "坏数据宁缺勿错：零结果不报错")
}
