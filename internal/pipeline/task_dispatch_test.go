package pipeline

// R3b 执行体迁持久任务队列测试：队列路径执行/降级直派/活跃去重/启动清扫/
// 执行体注册链路冒烟。外部语义不变是最高验收（降级路径与迁移前逐字一致）。

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/task"
)

// newQueueTestDB 在流水线测试库上加 tasks 表（R3b 队列路径专用）。
func newQueueTestDB(t *testing.T) *gorm.DB {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&task.Task{}))
	return db
}

// newQueueService 装配带快速轮询队列的 Service（测试节奏）。
func newQueueService(t *testing.T, db *gorm.DB) *Service {
	t.Helper()
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.taskRunner = task.NewRunner(svc.taskStore, slog.Default(),
		task.WithPollInterval(10*time.Millisecond), task.WithLeaseDuration(time.Minute))
	svc.registerTaskHandlers()
	return svc
}

// TestDispatchExecutorFallsBackWithoutQueue 队列不可用时降级裸 goroutine（原行为）：
// Runner 未启动 / tasks 表缺失（Enqueue 失败）两种情况都走 legacy。
func TestDispatchExecutorFallsBackWithoutQueue(t *testing.T) {
	db := newTestDB(t) // 无 tasks 表
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())

	called := false
	svc.dispatchExecutor(taskKindAudit, "run-1", 1, func() { called = true })
	require.True(t, called, "Runner 未启动 → 降级直派")

	svc.taskRunner = task.NewRunner(svc.taskStore, slog.Default(), task.WithPollInterval(10*time.Millisecond))
	svc.StartTaskQueue()
	defer svc.StopTaskQueue()
	called = false
	svc.dispatchExecutor(taskKindAudit, "run-2", 1, func() { called = true })
	require.True(t, called, "Enqueue 失败（表缺失）→ 降级直派")
}

// TestDispatchExecutorRunsViaQueue 队列路径：入队 → 消费执行 → done，legacy 不被调用。
func TestDispatchExecutorRunsViaQueue(t *testing.T) {
	db := newQueueTestDB(t)
	svc := newQueueService(t, db)
	var calls atomic.Int32
	svc.taskRunner.Register("probe", func(_ context.Context, tk *task.Task) error {
		calls.Add(1)
		require.Equal(t, "run-1", tk.RunID)
		return nil
	})
	svc.StartTaskQueue()
	defer svc.StopTaskQueue()

	fallback := false
	svc.dispatchExecutor("probe", "run-1", 1, func() { fallback = true })
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.False(t, fallback, "队列可用时不降级")

	require.Eventually(t, func() bool {
		var row task.Task
		if err := db.Where("dedup_key = ?", "probe:run-1").Take(&row).Error; err != nil {
			return false
		}
		return row.Status == task.StatusDone
	}, 5*time.Second, 10*time.Millisecond, "任务未收口 done")
}

// TestDispatchExecutorDedupActive 活跃去重：同 kind+run 在排队/执行中时重复派发被合并；
// 终态后可再派发（新任务）。
func TestDispatchExecutorDedupActive(t *testing.T) {
	db := newQueueTestDB(t)
	svc := newQueueService(t, db)
	ctx := context.Background()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls, fallback atomic.Int32
	svc.taskRunner.Register("probe", func(_ context.Context, tk *task.Task) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	})
	svc.StartTaskQueue()
	defer svc.StopTaskQueue()

	svc.dispatchExecutor("probe", "run-1", 1, func() { fallback.Add(1) })
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler 未启动")
	}
	// 执行中重复派发 → 合并（不新增任务、不降级）。
	svc.dispatchExecutor("probe", "run-1", 1, func() { fallback.Add(1) })
	rows, err := svc.taskStore.ListByDedupKey(ctx, "probe:run-1")
	require.NoError(t, err)
	require.Len(t, rows, 1, "活跃同键任务被合并")
	require.Zero(t, fallback.Load(), "合并不降级")

	close(release)
	require.Eventually(t, func() bool {
		var row task.Task
		if err := db.Take(&row, rows[0].ID).Error; err != nil {
			return false
		}
		return row.Status == task.StatusDone
	}, 5*time.Second, 10*time.Millisecond, "首个任务未收口 done")

	// 终态后同键可再派发（新任务，正常执行）。
	svc.dispatchExecutor("probe", "run-1", 1, func() { fallback.Add(1) })
	require.Eventually(t, func() bool { return calls.Load() == 2 }, 5*time.Second, 10*time.Millisecond)
	rows, err = svc.taskStore.ListByDedupKey(ctx, "probe:run-1")
	require.NoError(t, err)
	require.Len(t, rows, 2, "终态后重新入队为新任务")
	require.Zero(t, fallback.Load())
}

// TestStartTaskQueueAbandonsLeftover 启动清扫：进程退出残留的活跃任务（queued/running）
// 置 dead（跨进程恢复仍走人工恢复链路），清扫后同键可重派新任务。
func TestStartTaskQueueAbandonsLeftover(t *testing.T) {
	db := newQueueTestDB(t)
	svc := newQueueService(t, db)
	ctx := context.Background()

	// 残留任务：queued 一条 + running 一条（模拟进程退出时的中间态）。
	q, _, err := svc.taskStore.Enqueue(ctx, task.Task{Kind: "probe", DedupKey: "probe:run-1"})
	require.NoError(t, err)
	r, _, err := svc.taskStore.Enqueue(ctx, task.Task{Kind: "probe", DedupKey: "probe:run-2"})
	require.NoError(t, err)
	require.NoError(t, db.Model(&task.Task{}).Where("id = ?", r.ID).
		Update("status", task.StatusRunning).Error)

	var calls atomic.Int32
	svc.taskRunner.Register("probe", func(_ context.Context, tk *task.Task) error {
		calls.Add(1)
		return nil
	})
	svc.StartTaskQueue()
	defer svc.StopTaskQueue()

	for _, id := range []int64{q.ID, r.ID} {
		var row task.Task
		require.NoError(t, db.Take(&row, id).Error)
		require.Equal(t, task.StatusDead, row.Status, "残留活跃任务被清扫置 dead")
		require.Contains(t, row.LastError, "进程退出中断")
	}

	// 清扫后同键重派 → 新任务正常执行（人工恢复重派语义）。
	svc.dispatchExecutor("probe", "run-1", 1, func() { t.Fatal("不应降级") })
	require.Eventually(t, func() bool { return calls.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
}

// TestDispatchAuditHandlerWiring 执行体注册链路冒烟：taskKindAudit → executeAudit
// 链路通（不存在的 run：执行体加载失败即返回，任务收口 done，legacy 不触发）。
func TestDispatchAuditHandlerWiring(t *testing.T) {
	db := newQueueTestDB(t)
	svc := newQueueService(t, db)
	svc.StartTaskQueue()
	defer svc.StopTaskQueue()

	fallback := false
	svc.dispatchExecutor(taskKindAudit, "no-such-run", 1, func() { fallback = true })
	require.Eventually(t, func() bool {
		var row task.Task
		if err := db.Where("dedup_key = ?", taskKindAudit+":no-such-run").Take(&row).Error; err != nil {
			return false
		}
		return row.Status == task.StatusDone
	}, 5*time.Second, 10*time.Millisecond, "audit 执行体链路未收口")
	require.False(t, fallback, "队列可用时不降级")
}
