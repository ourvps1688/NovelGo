package writingconfig

// WritingConfig 是每本书的写作评分重写配置。
// 与世界观设定(SettingItem)/创作偏好(PreferenceItem)区分：本表是工具功能开关，
// 不注入 Agent 上下文、不参与世界叙事，仅驱动 edit 管线的评分门禁。
type WritingConfig struct {
	NovelID          int64 `gorm:"column:novel_id;primaryKey"            json:"novel_id"`
	AutoScoreEnabled bool  `gorm:"column:auto_score_enabled;not null"     json:"auto_score_enabled"`
	ScoreThreshold   int   `gorm:"column:score_threshold;not null;default:60"     json:"score_threshold"`
	MaxRetries       int   `gorm:"column:score_max_retries;not null;default:3"    json:"score_max_retries"`
}

// TableName 指定 GORM 表名。
func (WritingConfig) TableName() string { return "novel_writing_configs" }

// DefaultWritingConfig 返回带缺省值的配置（未配置此书时返回）。
func DefaultWritingConfig(novelID int64) *WritingConfig {
	return &WritingConfig{
		NovelID:          novelID,
		AutoScoreEnabled: true,
		ScoreThreshold:   60,
		MaxRetries:       3,
	}
}
