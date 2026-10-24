package activity

import "time"

// ActivityDelta 是一次操作对全局每日统计的非负增量。
type ActivityDelta struct {
	NovelsCreated     int64 `gorm:"column:novels_created;not null;default:0"`
	ChaptersCreated   int64 `gorm:"column:chapters_created;not null;default:0"`
	CharactersCreated int64 `gorm:"column:characters_created;not null;default:0"`
	LocationsCreated  int64 `gorm:"column:locations_created;not null;default:0"`
	CreativeSeconds   int64 `gorm:"column:creative_seconds;not null;default:0"`
	ConversationTurns int64 `gorm:"column:conversation_turns;not null;default:0"`
	ToolCalls         int64 `gorm:"column:tool_calls;not null;default:0"`
}

// DailyActivity 按记录时的本地日期累计，不与小说或会话建立级联关系。
type DailyActivity struct {
	Date          string `gorm:"column:date;primaryKey;size:10"`
	ActivityDelta `gorm:"embedded"`
}

func (DailyActivity) TableName() string { return "activity_daily" }

// TokenUsage 是调用方已取得的单次用量；缺失字段不贡献增量。
// TotalTokens 由调用方按服务商口径确定，缓存和推理明细不再加进总量。
type TokenUsage struct {
	InputTokens     int64 `gorm:"column:input_tokens;not null;default:0"`
	OutputTokens    int64 `gorm:"column:output_tokens;not null;default:0"`
	TotalTokens     int64 `gorm:"column:total_tokens;not null;default:0"`
	CacheHitTokens  int64 `gorm:"column:cache_hit_tokens;not null;default:0"`
	CacheMissTokens int64 `gorm:"column:cache_miss_tokens;not null;default:0"`
	ReasoningTokens int64 `gorm:"column:reasoning_tokens;not null;default:0"`
}

// DailyLLMUsage 按日期、服务商、模型和用途累计已取得的用量。
type DailyLLMUsage struct {
	Date       string `gorm:"column:date;primaryKey;size:10"`
	Provider   string `gorm:"column:provider;primaryKey"`
	Model      string `gorm:"column:model;primaryKey"`
	Purpose    string `gorm:"column:purpose;primaryKey"`
	UsageCount int64  `gorm:"column:usage_count;not null;default:0"`
	TokenUsage `gorm:"embedded"`
}

func (DailyLLMUsage) TableName() string { return "llm_usage_daily" }

// FeatureUsage 按日期、功能键累计使用次数；只记 key+次数，不记用户内容（隐私纪律）。
type FeatureUsage struct {
	Date       string `gorm:"column:date;primaryKey;size:10"`
	FeatureKey string `gorm:"column:feature_key;primaryKey"`
	Count      int64  `gorm:"column:count;not null;default:0"`
}

func (FeatureUsage) TableName() string { return "feature_usage_daily" }

// FeatureUsageStat 是近若干天按功能键聚合的只读消费面数据，按 Count 降序。
type FeatureUsageStat struct {
	FeatureKey string `gorm:"column:feature_key" json:"feature_key"`
	Count      int64  `gorm:"column:count" json:"count"`
}

// 执行体观测状态值（ExecStat.Status）：单次派发的收口口径。
const (
	ExecStatusOK      = "ok"      // 正常完成
	ExecStatusError   = "error"   // 执行失败（含部分产物）
	ExecStatusTripped = "tripped" // 供应商熔断跳过派发（未跑 agent）
)

// ExecStat 是流水线执行体（S4 体检 / S5 修复 / 章节重写等 skill 派发）的单次观测样本
// （R0 观测先行，pipeline-core-redesign-proposal.md §5）：只记数字与状态，不记任何
// 内容文本（隐私纪律：度量纯本地不上传）。耗时 + turn 数 + 归属 run 足以确认瓶颈分布
// （S5「轮数」= 同 run 同 skill 的样本行数）。
type ExecStat struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID    int64     `gorm:"column:novel_id;not null;index"      json:"novel_id"`
	RunID      string    `gorm:"column:run_id;index"                 json:"run_id"` // 归属流水线 run（非 run 任务为空）
	Skill      string    `gorm:"column:skill;not null;index"         json:"skill"`  // 派发的 builtin skill 名（执行体标识）
	DurationMS int64     `gorm:"column:duration_ms;not null"         json:"duration_ms"`
	Turns      int       `gorm:"column:turns;not null;default:0"     json:"turns"`     // agent loop 实耗轮数
	MaxTurns   int       `gorm:"column:max_turns;not null;default:0" json:"max_turns"` // 本轮轮数预算（判断是否顶格）
	Status     string    `gorm:"column:status;not null;index"        json:"status"`    // ok | error | tripped
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime"    json:"created_at"`
}

// TableName 指定 GORM 表名（feature_usage_daily 同族：AutoMigrate 注册、纯本地）。
func (ExecStat) TableName() string { return "exec_run_stats" }

// ExecStatSummary 是近若干天按 skill 聚合的只读消费面数据（R0 瓶颈分布取数口），
// 按总耗时降序。
type ExecStatSummary struct {
	Skill         string `gorm:"column:skill" json:"skill"`
	Runs          int64  `gorm:"column:runs" json:"runs"`
	ErrorRuns     int64  `gorm:"column:error_runs" json:"error_runs"`
	TrippedRuns   int64  `gorm:"column:tripped_runs" json:"tripped_runs"`
	TotalDuration int64  `gorm:"column:total_duration_ms" json:"total_duration_ms"`
	AvgDuration   int64  `gorm:"column:avg_duration_ms" json:"avg_duration_ms"`
	MaxDuration   int64  `gorm:"column:max_duration_ms" json:"max_duration_ms"`
	TotalTurns    int64  `gorm:"column:total_turns" json:"total_turns"`
	AvgTurns      int64  `gorm:"column:avg_turns" json:"avg_turns"`
	MaxTurnsUsed  int64  `gorm:"column:max_turns_used" json:"max_turns_used"`
}
