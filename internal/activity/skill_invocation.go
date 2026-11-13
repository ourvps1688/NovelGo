package activity

import (
	"context"
	"strings"
	"sync"
	"time"
)

// SkillInvocation 是技能装配契约（#111 P0）的统一观测样本：一次技能正文注入或
// 一次流水线技能派发记一条。只记标识与字符数，不记正文（隐私纪律：度量纯本地不上传）。
//
// Skill 为空表示「通道级事件」：该挂载点已运行但当前没有技能正文挂载（例如 P0 的
// state_sync 仍用硬编码指令，P2 接入 collect 后改记具体技能名）。汇总查询与前端
// 用量面板过滤空 skill，避免把通道事件误算成技能用量。
type SkillInvocation struct {
	ID        int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Skill     string `gorm:"column:skill;not null;index"         json:"skill"`
	Channel   string `gorm:"column:channel;not null;index"       json:"channel"`
	Mount     string `gorm:"column:mount;index"                  json:"mount"`
	NovelID   int64  `gorm:"column:novel_id;not null;default:0;index" json:"novel_id"`
	RunID     string `gorm:"column:run_id;index"                 json:"run_id"`
	ChapterID int64  `gorm:"column:chapter_id;not null;default:0" json:"chapter_id"`
	SessionID string `gorm:"column:session_id;index"             json:"session_id"`
	// #114 P2：本轮身份。TurnID 复用 chat 链路 session.NextTurn 的原子自增 int
	// （不是新 ID）；FindingKeys 是 quality.FindingKeys 单源生成的 JSON 快照
	// （[]string），只在带验收条件的改稿轮非空。pipeline/子任务埋点恒零值。
	TurnID      int       `gorm:"column:turn_id;not null;default:0;index" json:"turn_id"`
	FindingKeys string    `gorm:"column:finding_keys;type:text"           json:"finding_keys"`
	Runes       int       `gorm:"column:runes;not null;default:0"          json:"runes"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"      json:"created_at"`
}

// TableName 指定 GORM 表名（exec_run_stats / feature_usage_daily 同族：AutoMigrate 注册、纯本地）。
func (SkillInvocation) TableName() string { return "skill_invocations" }

// SkillInvocationStat 是近若干天按 skill + channel 聚合的只读消费面数据。
// LastSeen 为字符串，避免 wails 绑定的时间类型漂移（与 ExecStatSummary 同口径）。
// #114 P2 追加最近一次 turn 与本技能最近关联的目标发现项快照，GROUP BY 仍是
// skill + channel（行数不变），不改变既有「谁被注入、走哪条通道、用了几次」口径。
type SkillInvocationStat struct {
	Skill       string `gorm:"column:skill"        json:"skill"`
	Channel     string `gorm:"column:channel"      json:"channel"`
	Count       int64  `gorm:"column:count"        json:"count"`
	LastSeen    string `gorm:"column:last_seen"    json:"last_seen"`
	LastTurnID  int64  `gorm:"column:last_turn_id" json:"last_turn_id"`
	FindingKeys string `gorm:"column:finding_keys" json:"finding_keys"`
}

// 六条注入通道分散在 app / pipeline / mcp_tools / agentcfg，用可注入的包级 sink
// 避免给每个装配器加构造参数。未注入时完全 no-op —— P0 零行为变更的基石。
// 并发安全：dev 运行中子 Agent 与 chat 会同时写账。
var (
	skillInvMu   sync.RWMutex
	skillInvSink func(context.Context, SkillInvocation)
)

// SetSkillInvocationSink 注入/替换统一账本落库口。生产由 app.OnStartup 注入
// (*activity.Store).AddSkillInvocation；测试可注入捕获桩；传 nil 关闭观测。
func SetSkillInvocationSink(fn func(context.Context, SkillInvocation)) {
	skillInvMu.Lock()
	skillInvSink = fn
	skillInvMu.Unlock()
}

// RecordSkillInvocation 落一条统一账本。观测自身绝不影响业务行为：
// 未注入 sink 直接返回；sink panic 吞掉；任务取消后仍照记（context.WithoutCancel）。
func RecordSkillInvocation(ctx context.Context, inv SkillInvocation) {
	skillInvMu.RLock()
	sink := skillInvSink
	skillInvMu.RUnlock()
	if sink == nil {
		return
	}
	defer func() { _ = recover() }()
	// #114 P2：本轮身份由 app/chat.go 一次性绑进 ctx，13 处埋点零改签名。
	// 未绑定（pipeline / 子任务 / 旧链路）时为零值，天然兼容。
	inv.TurnID, inv.FindingKeys = turnFindingFromContext(ctx)
	sink(context.WithoutCancel(ctx), inv)
}

// AddSkillInvocation 写一条统一账本样本。写入失败只告警不抛错，与 AddExecStat
// 同纪律——观测自身不影响业务行为。Channel 必填；Skill 可为通道级事件的空串。
func (s *Store) AddSkillInvocation(ctx context.Context, inv SkillInvocation) {
	if strings.TrimSpace(inv.Channel) == "" {
		s.logger.Warn("忽略无通道标识的技能观测样本")
		return
	}
	record := inv
	if record.CreatedAt.IsZero() {
		record.CreatedAt = s.now()
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		s.logger.Warn("记录技能观测样本失败", "skill", inv.Skill, "channel", inv.Channel, "err", err)
	}
}

// SummarizeSkillInvocations 返回近 days 天（含今天）按 skill + channel 聚合的用量，
// 按次数降序。days 非正时按 30 天；查询失败返回错误，由调用方静默降级为空表。
// 空 skill 的通道级事件不参与技能用量统计。
func (s *Store) SummarizeSkillInvocations(ctx context.Context, days int) ([]SkillInvocationStat, error) {
	if days <= 0 {
		days = DefaultStatDays
	}
	dayStart := s.now().AddDate(0, 0, -(days - 1))
	since := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 0, 0, 0, 0, dayStart.Location())
	var stats []SkillInvocationStat
	err := s.db.WithContext(ctx).Model(&SkillInvocation{}).
		Select(`skill, channel, COUNT(*) AS count, MAX(created_at) AS last_seen,
			MAX(turn_id) AS last_turn_id,
			COALESCE((SELECT t2.finding_keys FROM skill_invocations t2
				WHERE t2.skill = skill_invocations.skill
				  AND t2.channel = skill_invocations.channel
				  AND t2.finding_keys <> ''
				ORDER BY t2.id DESC LIMIT 1), '') AS finding_keys`).
		Where("created_at >= ? AND skill <> ''", since).
		Group("skill, channel").
		Order("count DESC, skill ASC, channel ASC").
		Scan(&stats).Error
	if err != nil {
		return nil, err
	}
	return stats, nil
}
