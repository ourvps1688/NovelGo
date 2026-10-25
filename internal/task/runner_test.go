package task

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitTaskStatus 轮询等待任务进入目标状态（runner 异步执行的观察口）。
func waitTaskStatus(t *testing.T, db *gorm.DB, id int64, want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		var row Task
		if err := db.Take(&row, id).Error; err != nil {
			return false
		}
		return row.Status == want
	}, 5*time.Second, 10*time.Millisecond, "任务 %d 未进入状态 %s", id, want)
}

func TestRunnerExecutesAndCompletes(t *testing.T) {
	s, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	r := NewRunner(s, testLogger(), WithPollInterval(10*time.Millisecond))
	r.Register("audit", func(ctx context.Context, tk *Task) error {
		calls.Add(1)
		require.Equal(t, "run-1", tk.RunID)
		return nil
	})
	r.Start(ctx)
	defer r.Stop()

	tk, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1", RunID: "run-1"})
	require.NoError(t, err)
	require.False(t, existing)

	waitTaskStatus(t, s.db, tk.ID, StatusDone)
	require.Equal(t, int32(1), calls.Load(), "handler 恰执行一次")
}

func TestRunnerRetriesThenDead(t *testing.T) {
	s, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	r := NewRunner(s, testLogger(), WithPollInterval(10*time.Millisecond))
	r.Register("audit", func(ctx context.Context, tk *Task) error {
		calls.Add(1)
		return errors.New("boom")
	})
	r.Start(ctx)
	defer r.Stop()

	tk, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1", MaxAttempts: 2})
	require.NoError(t, err)

	waitTaskStatus(t, s.db, tk.ID, StatusDead)
	require.Equal(t, int32(2), calls.Load(), "MaxAttempts=2 恰尝试 2 次")
	var row Task
	require.NoError(t, s.db.Take(&row, tk.ID).Error)
	require.Equal(t, "boom", row.LastError)
}

func TestRunnerUnregisteredKindGoesDead(t *testing.T) {
	s, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := NewRunner(s, testLogger(), WithPollInterval(10*time.Millisecond))
	r.Start(ctx)
	defer r.Stop()

	tk, _, err := s.Enqueue(ctx, Task{Kind: "no-such-kind", DedupKey: "no-such-kind:run-1"})
	require.NoError(t, err)

	waitTaskStatus(t, s.db, tk.ID, StatusDead)
	var row Task
	require.NoError(t, s.db.Take(&row, tk.ID).Error)
	require.Contains(t, row.LastError, "unregistered kind")
}

func TestRunnerStopCancelsActiveTask(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	started := make(chan struct{})
	cancelled := make(chan struct{})
	r := NewRunner(s, testLogger(), WithPollInterval(10*time.Millisecond))
	r.Register("audit", func(ctx context.Context, tk *Task) error {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return ctx.Err()
	})
	r.Start(ctx)

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler 未启动")
	}
	// Stop 取消活跃执行并等待收口（不返回=死锁）。
	done := make(chan struct{})
	go func() { r.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 未收口")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("handler 未收到取消信号")
	}
}

func TestRunnerStartIdempotentAndStarted(t *testing.T) {
	s, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	r := NewRunner(s, testLogger(), WithPollInterval(10*time.Millisecond))
	r.Register("audit", func(ctx context.Context, tk *Task) error {
		calls.Add(1)
		return nil
	})
	require.False(t, r.Started())
	r.Start(ctx)
	r.Start(ctx) // 幂等：重复启动不起第二个循环
	require.True(t, r.Started())

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"重复 Start 起了多个消费循环（任务被消费超过一次）")

	r.Stop()
	require.False(t, r.Started(), "Stop 后 Started=false（派发口降级依据）")
	r.Start(ctx) // Stop 后不再启动
	require.False(t, r.Started())
}

func TestRunnerPanicRecoveredAsFailure(t *testing.T) {
	s, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	r := NewRunner(s, testLogger(), WithPollInterval(10*time.Millisecond))
	r.Register("audit", func(ctx context.Context, tk *Task) error {
		calls.Add(1)
		panic("boom-panic")
	})
	r.Start(ctx)
	defer r.Stop()

	tk, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1", MaxAttempts: 2})
	require.NoError(t, err)

	// panic 收口为失败：按 MaxAttempts 重试后 dead（进程不陪葬）。
	waitTaskStatus(t, s.db, tk.ID, StatusDead)
	require.Equal(t, int32(2), calls.Load())
	var row Task
	require.NoError(t, s.db.Take(&row, tk.ID).Error)
	require.Contains(t, row.LastError, "handler panic")
}

func TestRunnerHeartbeatReclaimCancelsExecution(t *testing.T) {
	s, advance := newTestStore(t)
	ctx := context.Background()

	var startedOnce, cancelledOnce sync.Once
	started := make(chan struct{})
	cancelled := make(chan struct{})
	r := NewRunner(s, testLogger(),
		WithPollInterval(time.Hour), WithLeaseDuration(300*time.Millisecond))
	// 租约 300ms，心跳周期下限 1s：重领后约 1s 内旧执行体发现围栏失效并自行取消。
	r.Register("audit", func(ctx context.Context, tk *Task) error {
		startedOnce.Do(func() { close(started) })
		<-ctx.Done()
		cancelledOnce.Do(func() { close(cancelled) })
		return ctx.Err()
	})
	r.Start(ctx)
	defer r.Stop()

	tk, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler 未启动")
	}

	// 推进时钟越过租约，模拟执行体消亡后的重领（attempt+1 换围栏令牌；
	// 赢家可能是本 Runner 轮询或显式 Lease，测试对两者均成立）。
	advance(time.Second)
	if fresh, lerr := s.Lease(ctx, time.Minute); lerr == nil && fresh != nil {
		require.Equal(t, 2, fresh.Attempt)
	}

	// 旧执行体心跳发现围栏失效 → 自行取消（防双执行体）。
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("旧执行体未因围栏失效被取消")
	}

	// 旧执行体退出后不得改写任务状态（Fail 围栏拒绝），任务归属新围栏令牌。
	require.Eventually(t, func() bool {
		var row Task
		if err := s.db.Take(&row, tk.ID).Error; err != nil {
			return false
		}
		return row.Status == StatusRunning && row.Attempt >= 2
	}, 5*time.Second, 10*time.Millisecond, "旧执行体覆写了新执行体的任务状态")
}
