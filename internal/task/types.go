// Package task 是持久任务队列（R3，pipeline-core-redesign-proposal.md §4.4）：
// 流水线执行体（体检/修复/复验/蓝图/对齐/巡检）从裸 goroutine 迁到 task 表——
// kind/payload/status/attempt/lease/heartbeat，幂等消费、崩溃续跑、可并行、可观察。
//
// 契约要点（勿改）：
//   - 状态机四态：queued → running → done | dead（失败重试=回 queued，attempt 耗尽=dead）；
//   - 幂等入队：同 dedup_key 存在活跃任务（queued/running）时不重复入队；
//   - 租约领取原子：竞争失败自动重试领取；lease 过期的任务可被重新领取（崩溃续跑），
//     每次领取 attempt+1（崩溃/重试都算一次尝试）；
//   - attempt 上限：执行失败且 attempt 耗尽 → dead（不再自动重试，交上层转人工/告警）；
//   - 失败分类沿用 P4 重试矩阵/熔断（本层只管重试次数，不判失败原因）；
//   - 纯本地表（AutoMigrate 注册），不上传（隐私纪律）。
package task

import "time"

// 任务状态（封闭枚举）。
const (
	// StatusQueued 排队中（含失败待重试）。
	StatusQueued = "queued"
	// StatusRunning 已被 worker 领取执行（租约有效期内为活跃执行）。
	StatusRunning = "running"
	// StatusDone 正常完成。
	StatusDone = "done"
	// StatusDead 尝试耗尽放弃（attempt 达上限仍失败；不再自动重试）。
	StatusDead = "dead"
)

// 缺省参数（Store/Runner 可覆盖）。
const (
	// DefaultMaxAttempts 是执行尝试上限（含崩溃续跑的重领）。
	DefaultMaxAttempts = 3
	// DefaultLeaseDuration 是租约时长：领取后心跳续租，租约过期即视为执行体消亡可重领。
	DefaultLeaseDuration = 2 * time.Minute
	// DefaultPollInterval 是空转轮询间隔（无任务时的领取节奏）。
	DefaultPollInterval = 3 * time.Second
)

// Task 是一条持久化执行任务（task 表，R3 §4.4）。
type Task struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	// Kind 是执行体种类（如 audit/fix/verify/final_audit/blueprint/s0_align/full_align/patrol），
	// 派发到对应 Handler。
	Kind string `gorm:"column:kind;not null;index" json:"kind"`
	// DedupKey 是幂等键（约定 kind:run_id）：同键存在活跃任务时不重复入队
	// （活跃唯一性由 Store.Enqueue 事务保证——历史 done/dead 行不占键，不做 DB 唯一索引）。
	DedupKey string `gorm:"column:dedup_key;not null;index" json:"dedup_key"`
	// Payload 是执行参数 JSON（run_id/novel_id 等），由 Handler 自行解析。
	Payload string `gorm:"column:payload;type:text" json:"payload"`
	// Status 是任务状态（Status* 四态）。
	Status string `gorm:"column:status;not null;index" json:"status"`
	// Attempt 是已执行尝试次数（每次领取 +1；上限见 MaxAttempts）。
	Attempt int `gorm:"column:attempt;not null;default:0" json:"attempt"`
	// MaxAttempts 是尝试上限（<=0 按 DefaultMaxAttempts）。
	MaxAttempts int `gorm:"column:max_attempts;not null;default:0" json:"max_attempts"`
	// LeaseUntil 是租约到期时间：running 任务到期可被重领（执行体消亡续跑）。
	LeaseUntil time.Time `gorm:"column:lease_until" json:"lease_until"`
	// HeartbeatAt 是最近心跳时间（执行体存活观测）。
	HeartbeatAt time.Time `gorm:"column:heartbeat_at" json:"heartbeat_at"`
	// LastError 是最近一次执行失败的错误摘要（仅定位用，不含内容文本）。
	LastError string `gorm:"column:last_error;type:text" json:"last_error"`
	// RunID/NovelID 是归属流水线 run 与书（冗余自 Payload，供观察/查询）。
	RunID   string `gorm:"column:run_id;index" json:"run_id"`
	NovelID int64  `gorm:"column:novel_id;index" json:"novel_id"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (Task) TableName() string { return "tasks" }

// IsActive 判断任务状态是否活跃（占住 dedup 键）。
func (t *Task) IsActive() bool {
	return t.Status == StatusQueued || t.Status == StatusRunning
}

// MaxAttemptsEffective 返回生效的尝试上限（<=0 视为缺省）。
func (t *Task) MaxAttemptsEffective() int {
	if t.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return t.MaxAttempts
}
