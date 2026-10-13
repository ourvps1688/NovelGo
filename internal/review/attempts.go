package review

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// QualityAttempt 记录某次"成章写"的评分重试状态，用于有界重写循环。
// key 稳定标识一次章节写作尝试：已有章节用 "chapter:<id>"，新建章节用 "new:<标题>"。
// 评分通过或超重试上限放行后调用 Clear 清理，避免脏计数。
type QualityAttempt struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID    int64     `gorm:"column:novel_id;not null;index"     json:"novel_id"`
	AttemptKey string    `gorm:"column:attempt_key;not null;index"   json:"attempt_key"`
	Retries    int       `gorm:"column:retries;not null;default:0"   json:"retries"`
	LastScore  *int      `gorm:"column:last_score"                  json:"last_score"`
	LastReport string    `gorm:"column:last_report"                 json:"last_report"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime"    json:"updated_at"`
}

// TableName 指定 GORM 表名。
func (QualityAttempt) TableName() string { return "quality_attempts" }

// AttemptStore 管理 QualityAttempt 持久化。
type AttemptStore struct {
	DB     *gorm.DB
	logger *slog.Logger
}

// NewAttemptStore 创建重试计数存储。
func NewAttemptStore(db *gorm.DB, logger *slog.Logger) *AttemptStore {
	return &AttemptStore{DB: db, logger: logger}
}

// Increment 递增某 key 的重试次数并记录最近一次分数/报告，返回递增后的重试次数。
// 记录不存在时创建（retries 从 0 起步，本次调用后变为 1）。
func (s *AttemptStore) Increment(ctx context.Context, novelID int64, key string, score int, report string) (int, error) {
	var a QualityAttempt
	err := s.DB.WithContext(ctx).Where("novel_id = ? AND attempt_key = ?", novelID, key).First(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		a = QualityAttempt{NovelID: novelID, AttemptKey: key}
	} else if err != nil {
		return 0, err
	}
	a.Retries++
	sc := score
	a.LastScore = &sc
	a.LastReport = report
	if err := s.DB.WithContext(ctx).Save(&a).Error; err != nil {
		return 0, err
	}
	return a.Retries, nil
}

// Clear 清理某 key 的重试记录（评分通过或超重试上限放行后调用）。
func (s *AttemptStore) Clear(ctx context.Context, novelID int64, key string) error {
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND attempt_key = ?", novelID, key).
		Delete(&QualityAttempt{}).Error; err != nil {
		return err
	}
	return nil
}
