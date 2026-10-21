package volume

import "time"

// Volume 是小说分卷的元数据。
// 卷纲正文以文件形式存储在 Git 仓库 volumes/id_{id}.md，DB 仅保存索引。
//
// 设计原则：
//   - sort_order 是内部排序键，仅用于 ORDER BY，对用户/AI 不可见
//   - "第 N 卷" 实时按 (novel_id, sort_order) ASC 排序后位次生成，不存 DB
//   - 卷的增删移动只改 sort_order，不影响 chapter 文件路径（chapter 用 chapter.id 命名）
//
// 约束：
//   - (novel_id, sort_order) 唯一索引：同一小说内卷顺序不可重复
//   - (novel_id, name) 唯一索引：同一小说内卷名不可重复
type Volume struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement"                              json:"id"`
	NovelID         int64     `gorm:"column:novel_id;not null;uniqueIndex:uk_novel_volume_sort;uniqueIndex:uk_novel_volume_name;index" json:"novel_id"`
	Name            string    `gorm:"column:name;not null;uniqueIndex:uk_novel_volume_name"          json:"name"`
	SortOrder       int       `gorm:"column:sort_order;not null;uniqueIndex:uk_novel_volume_sort"   json:"sort_order"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime"                              json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime"                              json:"updated_at"`
	OutlineFilePath string    `gorm:"-" json:"outline_file_path"`

	// Frozen 卷冻结态（G_freeze，卷级作用域流水线）：true=本卷已定稿冻结——
	// 正文与既定设定不再被自动回改，S5 写保护据此拒写（issue.target_scope=frozen_volume
	// 不派自动修复），跨卷问题默认前向适配本卷。
	//
	// 置位规则：默认「本卷过 S4 体检 + S5 修复闭环后」置冻结（自动置位接入
	// Orchestrator 属阶段 2 的 G_freeze 闸门任务；当前经 SetFrozen 显式置位/解冻）。
	Frozen   bool       `gorm:"column:frozen;not null;default:false" json:"frozen"`
	FrozenAt *time.Time `gorm:"column:frozen_at"                    json:"frozen_at,omitempty"`

	// SnapshotVersion 卷快照版本号（阶段 2 卷快照机制用：冻结提交 committed snapshot 时递增）。
	// 阶段 1 只落字段占位，恒为 0。
	SnapshotVersion int `gorm:"column:snapshot_version;not null;default:0" json:"snapshot_version"`
}

// PlaceInput 描述创建或移动卷的目标位置。
// SourceVolumeID 为空时创建新卷，此时 Name 必填；有值时移动已有卷，Name 必须为空。
// BeforeVolumeID 为空表示追加到小说末尾。
type PlaceInput struct {
	NovelID        int64   `json:"novel_id"`
	SourceVolumeID *int64  `json:"source_volume_id"`
	Name           *string `json:"name"`
	BeforeVolumeID *int64  `json:"before_volume_id"`
}

// TableName 指定 GORM 表名。
func (Volume) TableName() string { return "volumes" }
