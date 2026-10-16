package novel

import "time"

// 小说类型：区分「写作的小说」与「参考小说」。
const (
	// KindWriting 是用户正在创作的写作目标书（默认）。
	KindWriting = "writing"
	// KindReference 是导入/标记为参考的学习样本，只读、仅可分析。
	KindReference = "reference"
)

// Novel 是小说索引，记录每部小说的基本信息。
// 正文存储在 Git 仓库中，路径由 config.NovelDirPath(ID) 实时计算。
type Novel struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Title       string    `gorm:"column:title;not null;index"        json:"title"`
	Genre       string    `gorm:"column:genre;index"                 json:"genre"`
	Description string    `gorm:"column:description"                 json:"description"`
	Kind        string    `gorm:"column:kind;type:varchar(16);not null;default:writing" json:"kind"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime"   json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime"   json:"updated_at"`
}

// IsReference 判断是否为参考小说。
func (n *Novel) IsReference() bool { return n.Kind == KindReference }

// TableName 指定 GORM 表名。
func (Novel) TableName() string { return "novels" }
