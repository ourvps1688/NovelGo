package activity

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// #114 P2 供应商可观测：把「截断 / 工具参数解析失败 / 空流 / 熔断跳过」从纯
// logger.Warn 升级为按天×服务商×模型×任务类型×信号累加的本地指标。
//
// 纪律（与 skill-wiring-contract §2.4 同族）：只记标识与计数，绝不记正文、
// 错误原文或用户内容；纯本地不上传；写入失败只告警；不改重试/熔断语义。
// 旧版本无此表、旧行无此信号时查询自然返回空表，前端静默降级。
type LLMSignalDaily struct {
	Date     string `gorm:"column:date;primaryKey;size:10"`
	Provider string `gorm:"column:provider;primaryKey"`
	Model    string `gorm:"column:model;primaryKey"`
	TaskKind string `gorm:"column:task_kind;primaryKey"`
	Signal   string `gorm:"column:signal;primaryKey"`
	Count    int64  `gorm:"column:count;not null;default:0"`
}

// TableName 指定 GORM 表名（AutoMigrate 注册，不写版本化迁移）。
func (LLMSignalDaily) TableName() string { return "llm_signal_daily" }

// LLMSignalStat 是近若干天按 provider×model×task_kind×signal 聚合的只读消费面。
// LastDate 用字符串，避免 wails 绑定的时间类型漂移（同 SkillInvocationStat）。
type LLMSignalStat struct {
	Provider string `gorm:"column:provider" json:"provider"`
	Model    string `gorm:"column:model" json:"model"`
	TaskKind string `gorm:"column:task_kind" json:"task_kind"`
	Signal   string `gorm:"column:signal" json:"signal"`
	Count    int64  `gorm:"column:count" json:"count"`
	LastDate string `gorm:"column:last_date" json:"last_date"`
}

// AddLLMSignal 原子累加一次失败信号。空信号拒收；provider/model/task_kind
// 为空时归一为 "unknown"，保证聚合键稳定（宁缺勿错，不写脏数据）。
func (s *Store) AddLLMSignal(ctx context.Context, provider, model, taskKind, signal string) {
	if strings.TrimSpace(signal) == "" {
		s.logger.Warn("忽略空供应商失败信号")
		return
	}
	record := LLMSignalDaily{
		Date:     s.now().Format(time.DateOnly),
		Provider: normalizeLLMSignalKey(provider),
		Model:    normalizeLLMSignalKey(model),
		TaskKind: normalizeLLMSignalKey(taskKind),
		Signal:   strings.TrimSpace(signal),
		Count:    1,
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "date"}, {Name: "provider"}, {Name: "model"},
			{Name: "task_kind"}, {Name: "signal"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"count": gorm.Expr("count + excluded.count"),
		}),
	}).Create(&record).Error
	if err != nil {
		s.logger.Warn("记录供应商失败信号失败", "signal", record.Signal, "err", err)
	}
}

func normalizeLLMSignalKey(s string) string {
	if v := strings.TrimSpace(s); v != "" {
		return v
	}
	return "unknown"
}

// SummarizeLLMSignals 返回近 days 天（含今天）按 provider×model×task_kind×signal
// 聚合的失败信号次数，按次数降序。days 非正时按 30 天；查询失败返回错误，
// 由调用方静默降级为空表。
func (s *Store) SummarizeLLMSignals(ctx context.Context, days int) ([]LLMSignalStat, error) {
	if days <= 0 {
		days = DefaultStatDays
	}
	dayStart := s.now().AddDate(0, 0, -(days - 1))
	since := dayStart.Format(time.DateOnly)
	var stats []LLMSignalStat
	err := s.db.WithContext(ctx).Model(&LLMSignalDaily{}).
		Select("provider, model, task_kind, signal, SUM(count) AS count, MAX(date) AS last_date").
		Where("date >= ?", since).
		Group("provider, model, task_kind, signal").
		Order("count DESC, provider ASC, model ASC, task_kind ASC, signal ASC").
		Scan(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}
