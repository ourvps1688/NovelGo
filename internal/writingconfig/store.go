package writingconfig

import (
	"context"
	"errors"
	"log/slog"

	"gorm.io/gorm"
)

// Store 管理 WritingConfig 持久化。
type Store struct {
	DB     *gorm.DB
	logger *slog.Logger
}

// NewStore 创建写作配置存储。
func NewStore(db *gorm.DB, logger *slog.Logger) *Store {
	return &Store{DB: db, logger: logger}
}

// Get 返回本书配置，未配置时返回带缺省值的 WritingConfig（不落库，待 Save 才持久化）。
// 对缺字段（threshold/retries 为 0）做缺省兜底，避免脏数据导致门禁失效。
func (s *Store) Get(ctx context.Context, novelID int64) (*WritingConfig, error) {
	var c WritingConfig
	err := s.DB.WithContext(ctx).First(&c, "novel_id = ?", novelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultWritingConfig(novelID), nil
	}
	if err != nil {
		return nil, err
	}
	if c.ScoreThreshold <= 0 {
		c.ScoreThreshold = 60
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	return &c, nil
}

// Save 幂等 upsert：按 novel_id 定位，不存在则创建、存在则覆盖各字段。
func (s *Store) Save(ctx context.Context, cfg *WritingConfig) error {
	th := cfg.ScoreThreshold
	if th <= 0 {
		th = 60
	}
	mr := cfg.MaxRetries
	if mr <= 0 {
		mr = 3
	}
	return s.DB.WithContext(ctx).
		Where(WritingConfig{NovelID: cfg.NovelID}).
		Assign(WritingConfig{
			AutoScoreEnabled: cfg.AutoScoreEnabled,
			ScoreThreshold:   th,
			MaxRetries:       mr,
		}).
		FirstOrCreate(cfg).Error
}
