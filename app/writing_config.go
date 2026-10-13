package app

import (
	"github.com/novelgo/novelgo/internal/writingconfig"
)

// GetWritingConfig 返回本书的写作评分配置；未配置时返回带缺省值的配置
//（默认开启、阈值 60、最大重试 3）。
func (a *App) GetWritingConfig(novelID int64) (*writingconfig.WritingConfig, error) {
	return writingconfig.NewStore(a.db, a.logger).Get(a.ctx, novelID)
}

// SaveWritingConfigInput 是保存写作评分配置的入参。
type SaveWritingConfigInput struct {
	AutoScoreEnabled bool `json:"auto_score_enabled"`
	ScoreThreshold   int  `json:"score_threshold"`
	MaxRetries       int  `json:"score_max_retries"`
}

// SaveWritingConfig 保存本书的写作评分配置（按 novel_id 幂等 upsert）。
func (a *App) SaveWritingConfig(novelID int64, input SaveWritingConfigInput) error {
	cfg := &writingconfig.WritingConfig{
		NovelID:          novelID,
		AutoScoreEnabled: input.AutoScoreEnabled,
		ScoreThreshold:   input.ScoreThreshold,
		MaxRetries:       input.MaxRetries,
	}
	return writingconfig.NewStore(a.db, a.logger).Save(a.ctx, cfg)
}
