package task

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// Store 是 task 表的读写口（R3 §4.4）。全部操作失败即报错由上层降级；
// 领取/续租是并发敏感点，靠条件 UPDATE 的 RowsAffected 原子判定。
type Store struct {
	db  *gorm.DB
	now func() time.Time
}

// NewStore 装配 Store（now 可注入用于测试租约过期；nil=系统时钟）。
func NewStore(db *gorm.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// Enqueue 幂等入队：同 dedup_key 存在活跃任务（queued/running）时返回既有任务
// （existing=true，不重复入队）；否则插入新任务（queued）。事务内查+插，
// 并发同键入队由 sqlite 写事务串行化保证唯一。
func (s *Store) Enqueue(ctx context.Context, t Task) (*Task, bool, error) {
	if t.MaxAttempts <= 0 {
		t.MaxAttempts = DefaultMaxAttempts
	}
	t.Status = StatusQueued
	out := &Task{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing Task
		err := tx.Where("dedup_key = ? AND status IN ?", t.DedupKey,
			[]string{StatusQueued, StatusRunning}).Order("id").Limit(1).Take(&existing).Error
		if err == nil {
			*out = existing
			return errAlreadyEnqueued
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		row := t
		row.Attempt = 0
		if cerr := tx.Create(&row).Error; cerr != nil {
			return cerr
		}
		*out = row
		return nil
	})
	if err == errAlreadyEnqueued {
		return out, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, false, nil
}

// errAlreadyEnqueued 是事务内的去重信号（非错误路径）。
var errAlreadyEnqueued = errors.New("task: dedup key already active")

// Lease 领取一个可执行任务（queued，或 running 且租约过期=执行体消亡可续跑）：
// 条件 UPDATE 原子置 running + 续租 + attempt+1；竞争失败自动重试下一个候选。
// 无任务返回 nil。leaseDur<=0 按 DefaultLeaseDuration。
func (s *Store) Lease(ctx context.Context, leaseDur time.Duration) (*Task, error) {
	if leaseDur <= 0 {
		leaseDur = DefaultLeaseDuration
	}
	for {
		var candidates []Task
		err := s.db.WithContext(ctx).
			Where("status = ? OR (status = ? AND lease_until < ?)",
				StatusQueued, StatusRunning, s.now()).
			Order("id").Limit(8).Find(&candidates).Error
		if err != nil {
			return nil, err
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		for _, c := range candidates {
			res := s.db.WithContext(ctx).Model(&Task{}).
				Where("id = ? AND (status = ? OR (status = ? AND lease_until < ?))",
					c.ID, StatusQueued, StatusRunning, s.now()).
				Updates(map[string]any{
					"status":       StatusRunning,
					"lease_until":  s.now().Add(leaseDur),
					"heartbeat_at": s.now(),
					"attempt":      gorm.Expr("attempt + 1"),
				})
			if res.Error != nil {
				return nil, res.Error
			}
			if res.RowsAffected == 1 {
				leased := c
				leased.Status = StatusRunning
				leased.Attempt++
				leased.LeaseUntil = s.now().Add(leaseDur)
				leased.HeartbeatAt = s.now()
				return &leased, nil
			}
			// 竞争失败（已被他人领取/状态已变）：试下一个候选。
		}
	}
}

// Heartbeat 续租并刷新心跳时间。attempt 是领取时的围栏令牌（reclaim 会 +1）：
// 仅当任务仍为 running 且 attempt 未被他人推进时生效；返回 false=租约已被回收
// （或任务完成/推进），执行体应尽快收口，不得再写回状态。
func (s *Store) Heartbeat(ctx context.Context, id int64, attempt int, leaseDur time.Duration) (bool, error) {
	if leaseDur <= 0 {
		leaseDur = DefaultLeaseDuration
	}
	res := s.db.WithContext(ctx).Model(&Task{}).
		Where("id = ? AND status = ? AND attempt = ?", id, StatusRunning, attempt).
		Updates(map[string]any{
			"lease_until":  s.now().Add(leaseDur),
			"heartbeat_at": s.now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Complete 标记任务完成（done）。attempt 是围栏令牌：非 running 或已被重领
// （attempt 被推进）的任务再完成返回 false 不改写（防旧执行体覆写新执行体状态）。
func (s *Store) Complete(ctx context.Context, id int64, attempt int) (bool, error) {
	res := s.db.WithContext(ctx).Model(&Task{}).
		Where("id = ? AND status = ? AND attempt = ?", id, StatusRunning, attempt).
		Update("status", StatusDone)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Fail 记录一次执行失败：attempt 耗尽 → dead（放弃，交上层转人工/告警）；
// 否则回 queued 排队重试。attempt 参数是围栏令牌：已被重领的任务返回 false 不改写
// （防旧执行体把新执行体拉回 queued 造成双跑）。errText 仅存错误摘要（不含内容文本）。
func (s *Store) Fail(ctx context.Context, id int64, attempt int, errText string) (bool, error) {
	var t Task
	if err := s.db.WithContext(ctx).Take(&t, id).Error; err != nil {
		return false, err
	}
	if t.Status != StatusRunning || t.Attempt != attempt {
		return false, nil
	}
	status := StatusQueued
	if t.Attempt >= t.MaxAttemptsEffective() {
		status = StatusDead
	}
	res := s.db.WithContext(ctx).Model(&Task{}).
		Where("id = ? AND status = ? AND attempt = ?", id, StatusRunning, attempt).
		Updates(map[string]any{"status": status, "last_error": errText})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Kill 置 dead（立即放弃，不重试）：用于不可重试的失败（如未注册的执行体种类）。
// attempt 是围栏令牌：已被重领的任务返回 false 不改写。
func (s *Store) Kill(ctx context.Context, id int64, attempt int, errText string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&Task{}).
		Where("id = ? AND status = ? AND attempt = ?", id, StatusRunning, attempt).
		Updates(map[string]any{"status": StatusDead, "last_error": errText})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ListByDedupKey 返回同幂等键的全部任务（升序，含历史，供观察/测试）。
func (s *Store) ListByDedupKey(ctx context.Context, dedupKey string) ([]Task, error) {
	var rows []Task
	err := s.db.WithContext(ctx).Where("dedup_key = ?", dedupKey).Order("id").Find(&rows).Error
	return rows, err
}

// ListActive 返回活跃任务清单（queued/running，升序；前端队列可见性取数口）。
func (s *Store) ListActive(ctx context.Context) ([]Task, error) {
	var rows []Task
	err := s.db.WithContext(ctx).Where("status IN ?",
		[]string{StatusQueued, StatusRunning}).Order("id").Find(&rows).Error
	return rows, err
}
