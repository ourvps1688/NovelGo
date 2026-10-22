package volume

import "time"

// VolumeSnapshot 是卷冻结提交的已提交快照（WBS §0.4 D3 冻结：新表 volume_snapshots）。
// 每卷每次冻结提交一行，snapshot_version 从 1 递增（与 volumes.snapshot_version 对齐）；
// payload 为该卷 series bible 条目 + 正文清单的 JSON 快照，后续卷以它为「既定事实」。
//
// diff_ref 指向快照间 diff 的落盘位置（阶段 3 retcon 受控编辑的留痕/回滚依据）；
// 阶段 2 落字段占位，2.2 快照机制接写入、阶段 3 接 diff。
//
// 唯一约束 (volume_id, snapshot_version)：同卷同版本只允许一份已提交快照，
// 「冻结即串行提交」（WBS 阶段 2 风险对策）由此在存储层兜底。
type VolumeSnapshot struct {
	ID              int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID         int64     `gorm:"column:novel_id;not null;index"    json:"novel_id"`
	VolumeID        int64     `gorm:"column:volume_id;not null;uniqueIndex:uk_volume_snapshot" json:"volume_id"`
	SnapshotVersion int       `gorm:"column:snapshot_version;not null;uniqueIndex:uk_volume_snapshot" json:"snapshot_version"`
	CommittedAt     time.Time `gorm:"column:committed_at;not null"      json:"committed_at"`
	// Payload 快照正文：卷内 series bible 条目 + 章节清单的 JSON 序列化。
	Payload string `gorm:"column:payload" json:"payload"`
	// DiffRef 相对上一版本 diff 的引用（路径/对象键），首版为空。
	DiffRef   string    `gorm:"column:diff_ref" json:"diff_ref"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 指定 GORM 表名。
func (VolumeSnapshot) TableName() string { return "volume_snapshots" }
