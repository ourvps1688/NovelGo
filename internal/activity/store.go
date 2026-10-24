package activity

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/novelgo/novelgo/internal/config"
)

// Store 负责每日统计的尽力记录；写入失败只告警，不向业务调用方返回错误。
type Store struct {
	db     *gorm.DB
	logger *slog.Logger
	now    func() time.Time
}

func NewStore(db *gorm.DB, logger *slog.Logger) *Store {
	return &Store{db: db, logger: logger, now: time.Now}
}

// InitTracking 在表就绪后、加载运行时 AppSettings 前调用，保留已有统计起点。
// 起点写入 app_config；先初始化再加载设置，避免后续 SaveSettings 写回旧快照。
func (s *Store) InitTracking(ctx context.Context) {
	now := s.now()
	startedAt := now.UTC()
	settings := config.AppSettings{
		ID:                1,
		TrackingStartedAt: &startedAt,
		TrackingStartDate: now.Format(time.DateOnly),
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"tracking_started_at": startedAt,
			"tracking_start_date": settings.TrackingStartDate,
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "tracking_started_at"}, Value: nil},
		}},
	}).Create(&settings).Error
	if err != nil {
		s.logger.Warn("初始化创作统计起点失败", "err", err)
	}
}

// AddActivity 将非负增量原子累加到当天；全零增量不创建空行。
// 调用位置应在业务事务外，统计失败无需补偿或重试。
func (s *Store) AddActivity(ctx context.Context, delta ActivityDelta) {
	if delta == (ActivityDelta{}) {
		return
	}
	if !nonNegative(delta.NovelsCreated, delta.ChaptersCreated, delta.CharactersCreated,
		delta.LocationsCreated, delta.CreativeSeconds, delta.ConversationTurns, delta.ToolCalls) {
		s.logger.Warn("忽略负数创作统计增量")
		return
	}
	record := DailyActivity{Date: s.now().Format(time.DateOnly), ActivityDelta: delta}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}},
		DoUpdates: clause.Assignments(map[string]any{
			"novels_created":     gorm.Expr("novels_created + excluded.novels_created"),
			"chapters_created":   gorm.Expr("chapters_created + excluded.chapters_created"),
			"characters_created": gorm.Expr("characters_created + excluded.characters_created"),
			"locations_created":  gorm.Expr("locations_created + excluded.locations_created"),
			"creative_seconds":   gorm.Expr("creative_seconds + excluded.creative_seconds"),
			"conversation_turns": gorm.Expr("conversation_turns + excluded.conversation_turns"),
			"tool_calls":         gorm.Expr("tool_calls + excluded.tool_calls"),
		}),
	}).Create(&record).Error
	if err != nil {
		s.logger.Warn("记录每日创作统计失败", "date", record.Date, "err", err)
	}
}

// AddLLMUsage 累计一次已取得的最终 usage，包括服务商明确返回全零的用量。
// 调用方跳过缺失 usage 的请求，并负责从原始响应中提取单次用量。
func (s *Store) AddLLMUsage(ctx context.Context, provider, model, purpose string, usage TokenUsage) {
	if !nonNegative(usage.InputTokens, usage.OutputTokens, usage.TotalTokens,
		usage.CacheHitTokens, usage.CacheMissTokens, usage.ReasoningTokens) {
		s.logger.Warn("忽略负数 Token 统计增量", "provider", provider, "model", model)
		return
	}
	record := DailyLLMUsage{
		Date:       s.now().Format(time.DateOnly),
		Provider:   provider,
		Model:      model,
		Purpose:    purpose,
		UsageCount: 1,
		TokenUsage: usage,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}, {Name: "provider"}, {Name: "model"}, {Name: "purpose"}},
		DoUpdates: clause.Assignments(map[string]any{
			"usage_count":       gorm.Expr("usage_count + excluded.usage_count"),
			"input_tokens":      gorm.Expr("input_tokens + excluded.input_tokens"),
			"output_tokens":     gorm.Expr("output_tokens + excluded.output_tokens"),
			"total_tokens":      gorm.Expr("total_tokens + excluded.total_tokens"),
			"cache_hit_tokens":  gorm.Expr("cache_hit_tokens + excluded.cache_hit_tokens"),
			"cache_miss_tokens": gorm.Expr("cache_miss_tokens + excluded.cache_miss_tokens"),
			"reasoning_tokens":  gorm.Expr("reasoning_tokens + excluded.reasoning_tokens"),
		}),
	}).Create(&record).Error
	if err != nil {
		s.logger.Warn("记录每日 Token 用量失败", "date", record.Date, "provider", provider, "model", model, "err", err)
	}
}

// AddFeatureUsage 将一次功能使用记到当天；只记 key+次数，不记用户内容（隐私纪律）。
// 空 key 拒收（宁缺勿错，不写脏数据）；写入失败只告警，与 AddActivity 同纪律。
func (s *Store) AddFeatureUsage(ctx context.Context, featureKey string) {
	if strings.TrimSpace(featureKey) == "" {
		s.logger.Warn("忽略空功能键的功能使用统计")
		return
	}
	record := FeatureUsage{Date: s.now().Format(time.DateOnly), FeatureKey: featureKey, Count: 1}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "date"}, {Name: "feature_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"count": gorm.Expr("count + excluded.count"),
		}),
	}).Create(&record).Error
	if err != nil {
		s.logger.Warn("记录功能使用统计失败", "date", record.Date, "feature_key", featureKey, "err", err)
	}
}

// ListFeatureUsage 返回近 days 天（含今天）按功能键聚合的使用次数，按次数降序。
// days 非正时按 30 天；查询失败返回错误，由调用方静默降级为空表。
func (s *Store) ListFeatureUsage(ctx context.Context, days int) ([]FeatureUsageStat, error) {
	if days <= 0 {
		days = 30
	}
	since := s.now().AddDate(0, 0, -(days - 1)).Format(time.DateOnly)
	var stats []FeatureUsageStat
	err := s.db.WithContext(ctx).Model(&FeatureUsage{}).
		Select("feature_key, SUM(count) AS count").
		Where("date >= ?", since).
		Group("feature_key").
		Order("count DESC, feature_key ASC").
		Scan(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

// AddExecStat 记录一次执行体派发的观测样本（R0 观测先行）：只记数字与状态，
// 不记任何内容文本（隐私纪律：度量纯本地不上传）。写入失败只告警不抛错，
// 与 AddFeatureUsage 同纪律——观测自身绝不影响业务行为（零行为变化）。
func (s *Store) AddExecStat(ctx context.Context, stat ExecStat) {
	if strings.TrimSpace(stat.Skill) == "" {
		s.logger.Warn("忽略无 skill 名的执行体观测样本")
		return
	}
	record := stat
	// 与本包其余统计同口径：时间戳用 store 时钟（测试可钉住；autoCreateTime 只补零值）。
	if record.CreatedAt.IsZero() {
		record.CreatedAt = s.now()
	}
	err := s.db.WithContext(ctx).Create(&record).Error
	if err != nil {
		s.logger.Warn("记录执行体观测样本失败", "skill", stat.Skill, "run_id", stat.RunID, "err", err)
	}
}

// SummarizeExecStats 返回近 days 天（含今天）按 skill 聚合的执行体观测汇总
// （R0 瓶颈分布取数口），按总耗时降序。days 非正时按 30 天；
// 查询失败返回错误，由调用方静默降级为空表。
func (s *Store) SummarizeExecStats(ctx context.Context, days int) ([]ExecStatSummary, error) {
	if days <= 0 {
		days = 30
	}
	// 窗口起点=近 days 天前的本地零点（含今天），时间值绑定避免驱动格式差异。
	dayStart := s.now().AddDate(0, 0, -(days - 1))
	since := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, dayStart.Location())
	var stats []ExecStatSummary
	err := s.db.WithContext(ctx).Model(&ExecStat{}).
		Select("skill, COUNT(*) AS runs, "+
			"SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) AS error_runs, "+
			"SUM(CASE WHEN status = 'tripped' THEN 1 ELSE 0 END) AS tripped_runs, "+
			"SUM(duration_ms) AS total_duration_ms, "+
			"CAST(AVG(duration_ms) AS INTEGER) AS avg_duration_ms, "+
			"MAX(duration_ms) AS max_duration_ms, "+
			"SUM(turns) AS total_turns, "+
			"CAST(AVG(turns) AS INTEGER) AS avg_turns, "+
			"MAX(turns) AS max_turns_used").
		Where("created_at >= ?", since).
		Group("skill").
		Order("total_duration_ms DESC, skill ASC").
		Scan(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func nonNegative(values ...int64) bool {
	for _, value := range values {
		if value < 0 {
			return false
		}
	}
	return true
}
