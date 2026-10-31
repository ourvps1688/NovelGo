package storyarc

import "time"

// StoryArc 是跨越多章节的叙事弧线容器，处于 ChapterPlan 和整体大纲之间的战略层。
// 一个小说通常 3-5 条弧线，弧线之间靠自然语言关联，不需要结构化边表。
//
// arc_type 驱动过滤和注入策略，status 驱动活跃窗口筛选，均保持约束枚举。
type StoryArc struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID      int64     `gorm:"column:novel_id;not null;uniqueIndex:uk_novel_story_arc_name,priority:1" json:"novel_id"`
	Name         string    `gorm:"column:name;not null;uniqueIndex:uk_novel_story_arc_name,priority:2" json:"name"` // 弧线名称，如"复仇之路"
	Description  string    `gorm:"column:description"                json:"description"`                            // 弧线整体描述
	ArcType      string    `gorm:"column:arc_type;not null;index"    json:"arc_type"`                               // "main" | "sub" | "character" | "background"
	Importance   int       `gorm:"column:importance;default:1"       json:"importance"`                             // 1-5，同类型内的排序优先度
	Status       string    `gorm:"column:status;not null;index"      json:"status"`                                 // "active" | "paused" | "completed" | "abandoned"
	ReactivateAt string    `gorm:"column:reactivate_at"              json:"reactivate_at"`                          // 自然语言，暂停弧线的恢复条件。LLM 填写，MCP 工具格式化后呈现给 LLM 自行判断
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"  json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime"  json:"updated_at"`

	// ── 阶段 2（WBS §0.4 冻结清单）：角色弧/主题弧归属 + 卷归属 + temporal ──
	// CharID 角色弧归属（characters.id，arc_type=character 时填）；nil=非角色弧。
	CharID *int64 `gorm:"column:char_id;index" json:"char_id,omitempty"`
	// Motif 主题弧母题（theme_arcs.motif，自由文本）；空=非主题弧。
	Motif string `gorm:"column:motif" json:"motif"`
	// VolumeID 归属卷：NULL=未归属/全书级（存量迁移前为空，见 2.8 backfill）。
	VolumeID  *int64     `gorm:"column:volume_id;index" json:"volume_id,omitempty"`
	ChangedAt *time.Time `gorm:"column:changed_at"      json:"changed_at,omitempty"`
}

func (StoryArc) TableName() string { return "story_arcs" }

// ArcNode 是弧线内的有序链节，承接 Python plot_node 的设计意图。
//
// 排序由 target_chapter ASC, id ASC 替代序列号——创建顺序天然打破同章平局。
// target_chapter 是 LLM 对不确定未来的估算，只排不滤——不准确时写后由 statesync 校准。
// actual_chapter 记录实际发生在哪章，0=未发生。
type ArcNode struct {
	ID                  int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID             int64     `gorm:"column:novel_id;not null;index"         json:"novel_id"`
	StoryArcID          int64     `gorm:"column:story_arc_id;not null;index"     json:"story_arc_id"`
	Title               string    `gorm:"column:title;not null"                  json:"title"`                 // "发现仇人身份"
	Description         string    `gorm:"column:description"                     json:"description"`           // 节点详情
	TargetReadingNumber int       `gorm:"column:target_reading_number;default:0" json:"target_reading_number"` // 预计发生的阅读位置，0=未定；未来章节尚无 ID
	ActualChapterID     *int64    `gorm:"column:actual_chapter_id;index"          json:"actual_chapter_id"`    // 实际发生章节的稳定引用，nil=未发生
	Status              string    `gorm:"column:status;not null;default:pending" json:"status"`                // "pending" | "completed" | "abandoned"
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime"       json:"created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at;autoUpdateTime"       json:"updated_at"`
	// VolumeID 归属卷（NULL=未归属/全书级）+ temporal 变革定位（WBS §0.4 清单）。
	VolumeID  *int64     `gorm:"column:volume_id;index" json:"volume_id,omitempty"`
	ChangedAt *time.Time `gorm:"column:changed_at"      json:"changed_at,omitempty"`
}

func (ArcNode) TableName() string { return "arc_nodes" }
