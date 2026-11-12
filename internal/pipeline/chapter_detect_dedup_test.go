package pipeline

// #114 P3 章节检测交互行为契约：底部「启动检测」以前需要点三次——后端每次点击
// 都真的派发，chapter 既无 run 容器也无队列 dedup，连点会并发跑多个执行体、
// 共用同一个 detect:chapter:<novel>:<chapter> 进行态键互相覆盖；且收口没有
// 完成事件，前端只能等 3s 轮询观测 running 边沿才刷新档案。
// 本文件锁死：同章在飞合并点击 + 收口发 chapter_detect_done（带 ok）+ 取消不误报完成。

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func waitForChapterDetectDone(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(5 * time.Second):
		t.Fatal("章节检测完成事件未到达")
		return nil
	}
}

func TestChapterDetectDedupsInFlightClicks(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var runCount atomic.Int32
	svc.SetChapterCIRunner(func(ctx context.Context, novelID, chapterID int64) error {
		runCount.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	done := make(chan map[string]any, 1)
	t.Cleanup(swapEmitChapterDetectDone(func(_ context.Context, p map[string]any) { done <- p }))

	cid := int64(42)
	first, err := svc.RunDetectionTask(context.Background(), 7, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, first.Dispatched)
	require.False(t, first.AlreadyRunning)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("章节检测执行体未启动")
	}

	second, err := svc.RunDetectionTask(context.Background(), 7, DetectionKindChapter, &cid)
	require.NoError(t, err)
	assert.True(t, second.AlreadyRunning, "在飞的重复点击必须合并并如实回执")
	assert.False(t, second.Dispatched, "合并的点击不得再派发执行体")
	assert.True(t, svc.isDetectRunning(DetectionKindChapter, 7, &cid), "进行态仍属于首个执行体")
	assert.Equal(t, int32(1), runCount.Load(), "连点只能跑一次执行体")

	close(release)
	p := waitForChapterDetectDone(t, done)
	assert.Equal(t, int64(7), p["novel_id"])
	assert.Equal(t, cid, p["chapter_id"])
	assert.Equal(t, true, p["ok"])
	assert.False(t, svc.isDetectRunning(DetectionKindChapter, 7, &cid), "收口必须注销进行态")

	// 收口后可再次正常启动（防重不是永久锁）。
	third, err := svc.RunDetectionTask(context.Background(), 7, DetectionKindChapter, &cid)
	require.NoError(t, err)
	assert.True(t, third.Dispatched)
	assert.False(t, third.AlreadyRunning)
	waitForChapterDetectDone(t, done)
	assert.Equal(t, int32(2), runCount.Load())
}

func TestChapterDetectDoneReportsFailure(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.SetChapterCIRunner(func(_ context.Context, _, _ int64) error {
		return errors.New("L1 回执解析失败")
	})
	done := make(chan map[string]any, 1)
	t.Cleanup(swapEmitChapterDetectDone(func(_ context.Context, p map[string]any) { done <- p }))
	cid := int64(9)
	res, err := svc.RunDetectionTask(context.Background(), 3, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, res.Dispatched)
	p := waitForChapterDetectDone(t, done)
	assert.Equal(t, false, p["ok"], "执行体失败必须如实回执 ok=false")
	assert.False(t, svc.isDetectRunning(DetectionKindChapter, 3, &cid), "失败也要注销进行态")
}

func TestChapterDetectCancelSkipsDoneEvent(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	started := make(chan struct{}, 1)
	svc.SetChapterCIRunner(func(ctx context.Context, _, _ int64) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	done := make(chan map[string]any, 1)
	t.Cleanup(swapEmitChapterDetectDone(func(_ context.Context, p map[string]any) { done <- p }))
	cid := int64(11)
	_, err := svc.RunDetectionTask(context.Background(), 5, DetectionKindChapter, &cid)
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("章节检测执行体未启动")
	}
	cancelled, err := svc.CancelDetectionTask(5, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, cancelled)
	select {
	case p := <-done:
		t.Fatalf("取消不应发完成事件，却收到 %v", p)
	case <-time.After(300 * time.Millisecond):
	}
}
