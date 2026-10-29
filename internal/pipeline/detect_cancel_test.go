package pipeline

// detect_cancel_test.go — B5 验收：检测任务取消/超时。
// ① CancelDetectionTask 触发在飞执行体的派生 ctx 立即 Done（agent loop / dispatchSkill
//    感知安全退出），并即时注销检测进行态；
// ② CancelDetectionTask 对「无在飞执行体」返回 cancelled=false（排队尚未起执行体的已知限制）；
// ③ detectTaskTimeout 兜底：超时后派生 ctx 自动取消，避免大书检测永久卡死无退出。

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
)

// TestCancelDetectionTaskChapter 验收①：章节检测执行体持有带取消的派生 ctx；
// CancelDetectionTask 触发后执行体 ctx 立即 Done，且检测进行态即时注销（不依赖 unwind）。
func TestCancelDetectionTaskChapter(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &quality.ChapterQuality{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())

	cid := int64(7)
	started := make(chan struct{})
	captured := make(chan context.Context, 1)
	svc.SetChapterCIRunner(func(c context.Context, novelID, chapterID int64) error {
		captured <- c
		close(started)
		<-c.Done() // 阻塞至被取消
		return c.Err()
	})

	res, err := svc.RunDetectionTask(context.Background(), 900, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, res.Dispatched)

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("执行体未启动")
	}
	require.True(t, svc.isDetectRunning(DetectionKindChapter, 900, &cid), "在飞执行体应处进行态")

	// 取消在飞检测。
	cancelled, err := svc.CancelDetectionTask(900, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, cancelled, "在飞执行体应被取消")
	// 进行态即时注销（前端轮询/事件无需等待 unwind）。
	require.False(t, svc.isDetectRunning(DetectionKindChapter, 900, &cid), "取消后应立即注销进行态")

	// 执行体 ctx 立即 Done（取消信号送达）。
	rc := <-captured
	select {
	case <-rc.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("执行体 ctx 取消信号未送达")
	}
}

// TestCancelDetectionTaskNotRunning 验收②：无在飞执行体时返回 cancelled=false（best-effort），
// 不报错。
func TestCancelDetectionTaskNotRunning(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())

	cancelled, err := svc.CancelDetectionTask(900, DetectionKindBook, nil)
	require.NoError(t, err)
	require.False(t, cancelled, "无在飞执行体应返回 cancelled=false")

	// 未知 kind 回错（API 误用，同 RunDetectionTask）。
	_, err = svc.CancelDetectionTask(900, "nope", nil)
	require.Error(t, err)
}

// TestDetectTaskTimeout 验收③：detectTaskTimeout 兜底——超时后派生 ctx 自动取消，
// 防止大书/慢供应商下检测任务永久卡死无退出。临时收窄超时避免真等 60min。
func TestDetectTaskTimeout(t *testing.T) {
	old := detectTaskTimeout
	detectTaskTimeout = 50 * time.Millisecond
	defer func() { detectTaskTimeout = old }()

	svc := NewService(&gorm.DB{}, nil, nil, nil, "", "", context.Background(), slog.Default())
	// 仅验证派生 ctx 超时自动取消；不依赖真实 DB（beginDetectCtx 不触库）。
	dctx := svc.beginDetectCtx(DetectionKindBook, 900, nil)
	defer svc.endDetectCtx(DetectionKindBook, 900, nil)
	select {
	case <-dctx.Done():
		assert.Equal(t, context.DeadlineExceeded, dctx.Err())
	case <-time.After(2 * time.Second):
		t.Fatal("超时未触发 ctx 自动取消")
	}
}
