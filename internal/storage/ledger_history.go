package storage

import "time"

// HistorySource 是台账变更履历来源的封闭枚举（WBS §0.4 冻结）：auto|author|retcon。
// auto=AI/流水线自动写入；author=作者手动编辑；retcon=卷冻结后对冻结卷条目的回改
// （写路径自动标注，供一致性巡检回溯张力、供作者审查「吃书」）。
const (
	HistorySourceAuto   = "auto"
	HistorySourceAuthor = "author"
	HistorySourceRetcon = "retcon"
)

// LedgerChangeHistory 是 series bible 四维台账条目的变更履历（WBS §0.3 temporal.history[]
// 的结构化落点，§0.4 冻结：新表 ledger_change_history，参考 operation_log）。
//
// 语义要点：
//   - append-only：只增不删，按 (entry_table, entry_id) 查询即为单条目的完整演变史；
//   - 回答「什么时候改变了什么」：field + old → new + at + source；
//   - 卷冻结后对冻结卷条目的改动，source 自动标 retcon（由 2.6 写路径判定落值）。
//
// 写入由阶段 2 的 temporal/history 写路径（2.6）接入台账 store 写方法维护。
type LedgerChangeHistory struct {
	ID      int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID int64 `gorm:"column:novel_id;not null;index"  json:"novel_id"`
	// EntryTable 台账表名（如 characters / time_entries），与 EntryId 联合定位被改条目。
	EntryTable string `gorm:"column:entry_table;not null;uniqueIndex:uk_ledger_entry_at" json:"entry_table"`
	EntryID    int64  `gorm:"column:entry_id;not null;uniqueIndex:uk_ledger_entry_at"    json:"entry_id"`
	// Field 变更字段名（列名/JSON 路径）。
	Field string `gorm:"column:field" json:"field"`
	// Old / New 变更前后的值（文本序列化，JSON 字段存 JSON 文本）。
	Old string `gorm:"column:old_value" json:"old_value"`
	New string `gorm:"column:new_value" json:"new_value"`
	// Reason 变更理由（自由文本，LLM/作者填写，可空）。
	Reason string `gorm:"column:reason" json:"reason"`
	// Source 封闭枚举（HistorySource* 常量）：auto|author|retcon。
	Source string `gorm:"column:source;not null;index" json:"source"`
	// VolumeID / ChapterID 变更定位：发生在哪卷哪章（§0.3 changed_in）。
	VolumeID  *int64 `gorm:"column:volume_id;index"  json:"volume_id,omitempty"`
	ChapterID *int64 `gorm:"column:chapter_id;index" json:"chapter_id,omitempty"`
	// At 变更发生时刻（墙钟审计时间）。
	At time.Time `gorm:"column:at;not null;uniqueIndex:uk_ledger_entry_at" json:"at"`
}

// TableName 指定 GORM 表名。
func (LedgerChangeHistory) TableName() string { return "ledger_change_history" }
