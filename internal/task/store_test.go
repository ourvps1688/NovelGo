package task

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Task{}))
	return db
}

// newTestStore 装配带可控时钟的 Store（测试租约过期/重领用 advance 推进时钟；
// 时钟读写加锁——Runner goroutine 会并发读 now）。
func newTestStore(t *testing.T) (*Store, func(time.Duration)) {
	t.Helper()
	s := NewStore(newTestDB(t), nil)
	var mu sync.Mutex
	clock := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	advance := func(d time.Duration) {
		mu.Lock()
		clock = clock.Add(d)
		mu.Unlock()
	}
	return s, advance
}

func TestEnqueueIdempotentAndReopenAfterTerminal(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	t1, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)
	require.False(t, existing)
	require.Equal(t, StatusQueued, t1.Status)
	require.Equal(t, 0, t1.Attempt)

	// 同键活跃 → 去重返回既有任务，不新增行。
	t2, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)
	require.True(t, existing)
	require.Equal(t, t1.ID, t2.ID)
	rows, err := s.ListByDedupKey(ctx, "audit:run-1")
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// running 也占键。
	leased, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, leased)
	t3, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)
	require.True(t, existing)
	require.Equal(t, t1.ID, t3.ID)

	// 终态（done）后不占键 → 可再入队（新行）。
	ok, err := s.Complete(ctx, leased.ID, leased.Attempt)
	require.NoError(t, err)
	require.True(t, ok)
	t4, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "audit:run-1"})
	require.NoError(t, err)
	require.False(t, existing)
	require.NotEqual(t, t1.ID, t4.ID)
	rows, err = s.ListByDedupKey(ctx, "audit:run-1")
	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestLeaseBasicAndEmpty(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Nil(t, got, "空队列领取应返回 nil")

	a, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:a"})
	require.NoError(t, err)
	b, _, err := s.Enqueue(ctx, Task{Kind: "fix", DedupKey: "k:b"})
	require.NoError(t, err)

	// 按 id 升序领取，每次领取 attempt+1、置 running。
	first, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, a.ID, first.ID)
	require.Equal(t, StatusRunning, first.Status)
	require.Equal(t, 1, first.Attempt)
	require.False(t, first.LeaseUntil.IsZero())

	second, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, b.ID, second.ID)
	require.Equal(t, 1, second.Attempt)

	third, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Nil(t, third, "全部在租约期内无可领取任务")
}

func TestLeaseReclaimsExpiredRunning(t *testing.T) {
	s, advance := newTestStore(t)
	ctx := context.Background()

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:1"})
	require.NoError(t, err)
	first, err := s.Lease(ctx, 2*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, first.Attempt)

	// 租约未过期 → 不可重领。
	got, err := s.Lease(ctx, 2*time.Minute)
	require.NoError(t, err)
	require.Nil(t, got)

	// 租约过期（执行体消亡）→ 可重领，attempt 继续推进（围栏令牌换代）。
	advance(3 * time.Minute)
	again, err := s.Lease(ctx, 2*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, again)
	require.Equal(t, first.ID, again.ID)
	require.Equal(t, 2, again.Attempt)
}

func TestHeartbeatFencing(t *testing.T) {
	s, advance := newTestStore(t)
	ctx := context.Background()

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:1"})
	require.NoError(t, err)
	leased, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)

	// 有效围栏 → 续租成功。
	ok, err := s.Heartbeat(ctx, leased.ID, leased.Attempt, time.Minute)
	require.NoError(t, err)
	require.True(t, ok)

	// 错误围栏令牌 → 不生效（不替他人续租）。
	ok, err = s.Heartbeat(ctx, leased.ID, leased.Attempt-1, time.Minute)
	require.NoError(t, err)
	require.False(t, ok)

	// 被重领后，旧执行体心跳必须失效（否则双执行体互相续租）。
	advance(2 * time.Minute)
	reclaimed, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, reclaimed.Attempt)
	ok, err = s.Heartbeat(ctx, leased.ID, leased.Attempt, time.Minute)
	require.NoError(t, err)
	require.False(t, ok, "旧执行体围栏令牌已过期")
	ok, err = s.Heartbeat(ctx, reclaimed.ID, reclaimed.Attempt, time.Minute)
	require.NoError(t, err)
	require.True(t, ok, "新执行体可正常续租")
}

func TestCompleteFencing(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:1"})
	require.NoError(t, err)
	leased, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)

	ok, err := s.Complete(ctx, leased.ID, leased.Attempt-1)
	require.NoError(t, err)
	require.False(t, ok, "错误围栏令牌不得改写")

	ok, err = s.Complete(ctx, leased.ID, leased.Attempt)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = s.Complete(ctx, leased.ID, leased.Attempt)
	require.NoError(t, err)
	require.False(t, ok, "done 后再完成不改写（幂等）")

	var t2 Task
	require.NoError(t, ctx.Err())
	require.NoError(t, s.db.Take(&t2, leased.ID).Error)
	require.Equal(t, StatusDone, t2.Status)
}

func TestFailRetryThenDeadWithFencing(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:1", MaxAttempts: 2})
	require.NoError(t, err)

	// 第 1 次失败：未到上限 → 回 queued 重试。
	first, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	ok, err := s.Fail(ctx, first.ID, first.Attempt, "boom-1")
	require.NoError(t, err)
	require.True(t, ok)
	var t1 Task
	require.NoError(t, s.db.Take(&t1, first.ID).Error)
	require.Equal(t, StatusQueued, t1.Status)
	require.Equal(t, "boom-1", t1.LastError)

	// 第 2 次失败：attempt 耗尽 → dead。
	second, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, second.Attempt)
	ok, err = s.Fail(ctx, second.ID, second.Attempt, "boom-2")
	require.NoError(t, err)
	require.True(t, ok)
	var t2 Task
	require.NoError(t, s.db.Take(&t2, second.ID).Error)
	require.Equal(t, StatusDead, t2.Status)

	// dead 不占键，可重开新任务；stale Fail 不得改写 dead 状态。
	_, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:1", MaxAttempts: 2})
	require.NoError(t, err)
	require.False(t, existing)
	ok, err = s.Fail(ctx, second.ID, second.Attempt, "stale")
	require.NoError(t, err)
	require.False(t, ok, "已终态任务的失败落库被围栏拒绝")
}

func TestFailFencedOutAfterReclaim(t *testing.T) {
	s, advance := newTestStore(t)
	ctx := context.Background()

	_, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:1"})
	require.NoError(t, err)
	old, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)

	advance(2 * time.Minute)
	fresh, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, 2, fresh.Attempt)

	// 旧执行体失败落库 → 围栏拒绝，不得把新执行体的 running 拉回 queued。
	ok, err := s.Fail(ctx, old.ID, old.Attempt, "stale fail")
	require.NoError(t, err)
	require.False(t, ok)
	var t2 Task
	require.NoError(t, s.db.Take(&t2, old.ID).Error)
	require.Equal(t, StatusRunning, t2.Status)
	require.Equal(t, 2, t2.Attempt)
}

func TestLeaseConcurrentSingleWinner(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	target, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:race"})
	require.NoError(t, err)

	const workers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, lerr := s.Lease(ctx, time.Minute)
			if lerr != nil || got == nil {
				return
			}
			mu.Lock()
			winners++
			mu.Unlock()
		}()
	}
	wg.Wait()
	require.Equal(t, 1, winners, "并发领取同一任务只允许一个赢家")

	var t2 Task
	require.NoError(t, s.db.Take(&t2, target.ID).Error)
	require.Equal(t, StatusRunning, t2.Status)
	require.Equal(t, 1, t2.Attempt)
}

func TestListActive(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	a, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:a"})
	require.NoError(t, err)
	b, _, err := s.Enqueue(ctx, Task{Kind: "fix", DedupKey: "k:b"})
	require.NoError(t, err)
	leased, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, a.ID, leased.ID, "按 id 升序先领 a")
	_, err = s.Complete(ctx, leased.ID, leased.Attempt)
	require.NoError(t, err)

	active, err := s.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1, "done 不在活跃清单")
	require.Equal(t, b.ID, active[0].ID)
	require.Equal(t, StatusQueued, active[0].Status)
}

func TestAbandonActive(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	q, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:q"})
	require.NoError(t, err)
	r, _, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:r"})
	require.NoError(t, err)
	leased, err := s.Lease(ctx, time.Minute)
	require.NoError(t, err)
	require.Equal(t, q.ID, leased.ID)

	n, err := s.AbandonActive(ctx, "interrupted: 进程退出中断")
	require.NoError(t, err)
	require.Equal(t, int64(2), n, "queued+running 两条活跃任务被放弃")

	var row Task
	require.NoError(t, s.db.Take(&row, q.ID).Error)
	require.Equal(t, StatusDead, row.Status)
	require.Contains(t, row.LastError, "进程退出中断")
	row = Task{}
	require.NoError(t, s.db.Take(&row, r.ID).Error)
	require.Equal(t, StatusDead, row.Status)

	// 清扫后同键可重开（人工恢复重派语义）。
	_, existing, err := s.Enqueue(ctx, Task{Kind: "audit", DedupKey: "k:q"})
	require.NoError(t, err)
	require.False(t, existing)
}

func TestMaxAttemptsEffectiveAndIsActive(t *testing.T) {
	require.Equal(t, DefaultMaxAttempts, (&Task{}).MaxAttemptsEffective())
	require.Equal(t, DefaultMaxAttempts, (&Task{MaxAttempts: -1}).MaxAttemptsEffective())
	require.Equal(t, 5, (&Task{MaxAttempts: 5}).MaxAttemptsEffective())

	require.True(t, (&Task{Status: StatusQueued}).IsActive())
	require.True(t, (&Task{Status: StatusRunning}).IsActive())
	require.False(t, (&Task{Status: StatusDone}).IsActive())
	require.False(t, (&Task{Status: StatusDead}).IsActive())
}
