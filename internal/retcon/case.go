// Package retcon 实现 retcon 审批闸门（WBS 阶段 3.1）：把「回改已冻结前卷」收敛为
// 作者显式决策的状态机——候选 → 审批（通过/驳回）→ 受控编辑 → 留痕 → 回滚点（可回滚）。
//
// 设计原则（勿回退）：
//   - retcon 必须显式审批：状态迁移只能由显式 API 调用（Approve/Reject/BeginEdit/
//     Record/Rollback）驱动，不存在任何自动流转路径（非循环自动）。
//   - 冻结卷写保护（1.3）不变：流水线自动修复/自动重写绝不触碰冻结卷；retcon 候选
//     只沉淀不执行，批准后才进入受控编辑（3.2 基于快照 diff 留痕、可回滚）。
//   - 每次裁决必须留痕理由（note 必填）；受控编辑必须先固化回滚点（快照 id）才能开改。
package retcon

import "time"

// retcon 案件的来源（Origin 枚举）：
// pipeline=S4 体检检出的硬矛盾候选（RetconBacklog 沉淀）；author=作者主动发起。
const (
	OriginPipeline = "pipeline"
	OriginAuthor   = "author"
)

// retcon 案件的类型（Kind 枚举，P2 T4.2，单源禁改）：
// fact=回改既定事实（缺省，Rollback 走全量 Restore）；
// style=文风回改（Rollback 走 RestoreStyle，只还原 voice_dimensions，事实层不动）。
const (
	KindFact  = "fact"
	KindStyle = "style"
)

// RetconCase 是一条 retcon 案件：对某已冻结前卷发起的「回改既定事实」申请，
// 经作者显式审批后受控执行。状态机见 state.go，写入由 store.go 承担。
type RetconCase struct {
	ID      int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID int64 `gorm:"column:novel_id;not null;index"     json:"novel_id"`
	// VolumeID 目标冻结卷（可空=候选时未定位，由作者审批时在界面确认）。
	VolumeID *int64 `gorm:"column:volume_id;index" json:"volume_id,omitempty"`
	// Origin 来源枚举（OriginPipeline/OriginAuthor）。
	Origin string `gorm:"column:origin;not null;default:author" json:"origin"`
	// Kind 类型枚举（KindFact/KindStyle，P2 T4.2）：决定 Rollback 回滚执行体路由——
	// fact=全量 Restore（缺省，存量案回填 fact 行为逐字不变）；style=RestoreStyle
	// 只还原 voice_dimensions。状态机不感知本列（kind 迁移零触碰状态机）。
	Kind string `gorm:"column:kind;not null;default:fact" json:"kind"`
	// IssueID 源体检 issue 编号（pipeline 候选，供去重与回溯；author 候选可空）。
	IssueID string `gorm:"column:issue_id" json:"issue_id,omitempty"`
	// 三枚举字段（与 issue 契约同名同义，见 pipeline/store.go）。
	TargetScope string `gorm:"column:target_scope" json:"target_scope,omitempty"`
	Dimension   string `gorm:"column:dimension"   json:"dimension,omitempty"`
	Direction   string `gorm:"column:direction"   json:"direction,omitempty"`

	Title       string `gorm:"column:title;not null" json:"title"`
	Description string `gorm:"column:description"    json:"description,omitempty"`

	// Status 状态机当前状态（状态常量见 state.go）。
	Status string `gorm:"column:status;not null;default:candidate;index" json:"status"`
	// DecisionNote 裁决理由（Approve/Reject 必填留痕）。
	DecisionNote string     `gorm:"column:decision_note" json:"decision_note,omitempty"`
	DecidedBy    string     `gorm:"column:decided_by"    json:"decided_by,omitempty"`
	DecidedAt    *time.Time `gorm:"column:decided_at"    json:"decided_at,omitempty"`

	// RollbackSnapshotID 回滚点：受控编辑开始前固化的卷快照 id（BeginEdit 必填），
	// 回滚（Rollback）以它为准整体还原台账。留痕与回滚机制见 3.2（快照 diff/Restore）。
	RollbackSnapshotID *int64 `gorm:"column:rollback_snapshot_id" json:"rollback_snapshot_id,omitempty"`
	// DiffSummary 受控编辑的差异摘要（Record 必填留痕）。
	DiffSummary string     `gorm:"column:diff_summary" json:"diff_summary,omitempty"`
	AppliedBy   string     `gorm:"column:applied_by"   json:"applied_by,omitempty"`
	AppliedAt   *time.Time `gorm:"column:applied_at"   json:"applied_at,omitempty"`

	RolledBackBy string     `gorm:"column:rolled_back_by" json:"rolled_back_by,omitempty"`
	RolledBackAt *time.Time `gorm:"column:rolled_back_at" json:"rolled_back_at,omitempty"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定 GORM 表名。
func (RetconCase) TableName() string { return "retcon_cases" }

// CreateInput 是发起一条 retcon 候选的输入（作者主动 / 流水线沉淀共用）。
type CreateInput struct {
	NovelID  int64  `json:"novel_id"`
	VolumeID *int64 `json:"volume_id,omitempty"`
	// Origin 缺省按 OriginAuthor；流水线沉淀显式传 OriginPipeline。
	Origin string `json:"origin,omitempty"`
	// Kind 缺省按 KindFact；文风漂移（V 系）立案显式传 KindStyle。
	Kind        string `json:"kind,omitempty"`
	IssueID     string `json:"issue_id,omitempty"`
	TargetScope string `json:"target_scope,omitempty"`
	Dimension   string `json:"dimension,omitempty"`
	Direction   string `json:"direction,omitempty"`
	// Title 空时取 Description 前 80 个 rune。
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}
