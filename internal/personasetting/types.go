package personasetting

import "time"

// PersonaSetting 是每本书的写作人格（persona）开关与引用，持久化于 novel_persona_settings 表。
//
// persona 与 .enabled.json 的常驻技能启用正交：persona 是会话级 / 每轮注入的"写作模式"开关，
// 关闭即回普通模式，不影响技能常驻生效。落地位置参考 V3 设计决策 #5——不写入 .enabled.json。
type PersonaSetting struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	NovelID   int64     `gorm:"uniqueIndex;not null" json:"novel_id"`
	Enabled   bool      `json:"enabled"` // 是否启用写作人格（默认 false = 普通模式）
	Kind      string    `json:"kind"`    // "skill" | "author"
	Ref       string    `json:"ref"`     // 技能名 或 规范 source_author
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 显式指定表名，避免 GORM 默认复数推断。
func (PersonaSetting) TableName() string { return "novel_persona_settings" }
