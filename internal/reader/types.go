package reader

import "time"

// PerspectiveType 定义读者认知条目的类型。
const (
	TypeKnown         = "known"         // 读者已知的信息
	TypeSuspense      = "suspense"      // 读者等待解答的悬念
	TypeMisconception = "misconception" // 读者误以为的情况（用于反转）
)

// ReaderPerspective 是读者认知条目，追踪"读者知道什么、在等什么答案、误以为是什么"。
//
// 三种类型驱动不同的查询和格式化行为：
//   - known：全量返回，不过滤 revealed_chapter
//   - suspense/misconception：只返回 revealed_chapter=0（未回收）的条目
//   - 格式化时分三段输出：已知信息 / 活跃悬念 / 读者误知
//
// 字段精简：Python 9 字段直接平移，无删减。
//
// related_truth 是作者全知视角——不仅 misconception，所有类型都可记录真相。
// "谁杀了村长"是悬念，答案作者心里应该有数。
type ReaderPerspective struct {
	ID                int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID           int64     `gorm:"column:novel_id;not null;index"      json:"novel_id"`
	Type              string    `gorm:"column:type;not null;index"          json:"type"`                    // "known" | "suspense" | "misconception"
	Content           string    `gorm:"column:content;not null"             json:"content"`                 // 条目内容：读者知道/想知道/误以为的事情
	RelatedTruth      string    `gorm:"column:related_truth"                json:"related_truth"`           // 作者视角：真实情况。所有类型可选，不只是 misconception
	PlantedChapterID  *int64    `gorm:"column:planted_chapter_id;index"        json:"planted_chapter_id"`   // 起点事件（埋设/形成/揭示，lifecycle raised_at/formed_at/revealed_at）；nil 表示历史记录无法关联章节
	RevealedChapterID *int64    `gorm:"column:revealed_chapter_id;index"        json:"revealed_chapter_id"` // 终点事件（回收/纠正，lifecycle payoff_at/corrected_at），nil=未回收/持续中；known 单点不设（见 lifecycle.go）
	CreatedAt         time.Time `gorm:"column:created_at;autoCreateTime"    json:"created_at"`
	// VolumeID 归属卷（NULL=未归属/全书级）+ temporal 变革定位（WBS §0.4 清单）。
	VolumeID  *int64     `gorm:"column:volume_id;index" json:"volume_id,omitempty"`
	ChangedAt *time.Time `gorm:"column:changed_at"      json:"changed_at,omitempty"`
}

// TableName 指定 GORM 表名。
func (ReaderPerspective) TableName() string { return "reader_perspectives" }

// POVMode 是 POV 叙事模式的封闭枚举（WBS §0.4 冻结）：limited|omniscient。
const (
	POVModeLimited    = "limited"
	POVModeOmniscient = "omniscient"
)

// PovRoster 是读者视角维的 POV 名册（WBS §0.4 冻结：新表 pov_roster，§0.2 pov_roster[]）：
// 「哪位角色在哪一卷以何种模式担任 POV」。char_id 引用 characters.id。
// 「限知 POV 不泄露」一致性规则以本表 + knowledge_boundaries 为机器校验依据。
type PovRoster struct {
	ID      int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID int64 `gorm:"column:novel_id;not null;index"    json:"novel_id"`
	CharID  int64 `gorm:"column:char_id;not null;index"     json:"char_id"` // characters.id（POV 角色）
	// Mode 封闭枚举（POVMode* 常量）：limited|omniscient。
	Mode string `gorm:"column:mode;not null" json:"mode"`
	// VolumeID 该 POV 生效的卷（volumes.id）；NULL=未定位/全书级。
	VolumeID           *int64     `gorm:"column:volume_id;index"       json:"volume_id,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	ChangedAt          *time.Time `gorm:"column:changed_at"            json:"changed_at,omitempty"`
	ChangedInVolumeID  *int64     `gorm:"column:changed_in_volume_id"  json:"changed_in_volume_id,omitempty"`
	ChangedInChapterID *int64     `gorm:"column:changed_in_chapter_id" json:"changed_in_chapter_id,omitempty"`
}

// TableName 指定 GORM 表名。
func (PovRoster) TableName() string { return "pov_roster" }
