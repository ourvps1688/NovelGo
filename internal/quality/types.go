// Package quality 是章节质量档案（Quality Ledger，R2 提案 §4.1）的真理源：
// 每章落盘后沉淀一份结构化质检档案——L0 指标全集、L0/L1 发现清单、修复状态、
// 轮次与检查时间。它是章节的"体检病历"，落独立表（勿塞 GateContext），
// 供 L2 深审"档案汇总 + 关键章深读"消费（提案 §4.1/§4.2）。
//
// 契约要点（勿改）：
//   - 档案按 (novel_id, chapter_id) 唯一，写入幂等（Upsert 覆盖同一行）；
//   - 发现 ID 用档案内命名空间：L0 发现 C 系（C1/C2/…）、L1 发现 L1 系（L1-1/…），
//     不与 issue 契约 I/D/T/V 混排；
//   - 只报告不改写（P2 纪律）：warning 级（P1）起步，发现落档不触发修复路由；
//   - 降级零发现零提示：检查失败/坏数据静默降级，不落错误发现（宁缺勿错）。
package quality

import (
	"encoding/json"
	"strings"
	"time"
)

// FindingLevelDefault 是档案发现的起步严重级：warning 级（P1，与 issue 契约 Level 同值域）。
const FindingLevelDefault = "P1"

// 发现的修复状态（档案内自持，供"修复状态"汇总；与 pipeline.IssueStatus 同值域但独立定义，
// 避免档案层反向依赖流水线层）。
const (
	FindingStatusPending = "pending" // 待修（未修复即 open）
	FindingStatusFixed   = "fixed"   // 已修复
	FindingStatusSkipped = "skipped" // 已跳过（工程级/误报，转人工或豁免）
)

// L1Status 是档案行的 L1 增量检查状态。
const (
	L1StatusPending = "pending" // 档案行存在但 L1 未查
	L1StatusChecked = "checked" // L1 已查（发现清单为准，空清单=零发现）
	L1StatusSkipped = "skipped" // L1 降级跳过（派发失败静默降级，零提示零报错）
)

// validL1Status 判断 L1 状态枚举值是否合法。
func validL1Status(s string) bool {
	switch s {
	case L1StatusPending, L1StatusChecked, L1StatusSkipped:
		return true
	}
	return false
}

// L0Metrics 是 L0 确定性检查的指标全集（提案 §4.2「L0 指标全集」）。
// 键名单源：L0 检查器写、档案汇总快审读，两处不得各写一份字符串；
// 后续 L0 扩充（提案 §9.2 网文崩点）只允许加字段（additive），禁改既有键语义。
type L0Metrics struct {
	// WordCount 是本章正文字数（字数/结构门槛检查的原始指标）。
	WordCount int `json:"word_count"`
	// HasTailHook 是章尾钩子存在性（§4.2「章尾钩子存在性」）。
	HasTailHook bool `json:"has_tail_hook"`
	// StyleMetrics 是文风八键当前值（键=voice.MetricKey*，P2 漂移检测口径）。
	StyleMetrics map[string]float64 `json:"style_metrics,omitempty"`
}

// Finding 是章节质量档案内的一条质检发现（提案 §4.1）。
// 只报告不改写：发现落档供修复路由/汇总裁决消费，档案本身不触发任何写操作。
type Finding struct {
	// ID 是档案内稳定键：L0 发现 C 系（C1/C2/…），L1 发现 L1 系（L1-1/…）。
	ID string `json:"id"`
	// Check 是检查项标识（word_count/tail_hook/ai_flavor/style_drift/…）。
	Check string `json:"check"`
	// Level 是严重级：warning 级（P1）起步（FindingLevelDefault）。
	Level string `json:"level"`
	// Desc 是发现描述（人读文案，只报告）。
	Desc string `json:"desc"`
	// ChapterIDs 是涉及章节全局阅读序号（L0 即本章；L1 可跨章）。
	ChapterIDs []int64 `json:"chapter_ids,omitempty"`
	// LedgerRefs 是影响/涉及的台账条目（提案 §4.1「影响的台账条目」）。
	LedgerRefs []string `json:"ledger_refs,omitempty"`
	// Status 是修复状态（FindingStatus*）。
	Status string `json:"status"`
}

// ChapterQuality 是一章的质量档案（提案 §4.1）。(novel_id, chapter_id) 唯一。
// L0 侧与 L1 侧字段可分阶段写入（UpsertL0/UpsertL1 互不覆盖）。
type ChapterQuality struct {
	ID        int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID   int64 `gorm:"column:novel_id;not null;uniqueIndex:uk_quality_chapter,priority:1;index" json:"novel_id"`
	ChapterID int64 `gorm:"column:chapter_id;not null;uniqueIndex:uk_quality_chapter,priority:2" json:"chapter_id"`
	// ReadingNumber 是章节全局阅读序号（1-based，随章节移动变化，写入时取当前值）。
	ReadingNumber int `gorm:"column:reading_number;not null;index" json:"reading_number"`
	// L0Metrics 是 JSON：L0 指标全集（L0Metrics 形）。
	L0Metrics string `gorm:"column:l0_metrics;type:text" json:"l0_metrics"`
	// L0Findings 是 JSON：L0 发现清单（[]Finding，C 系命名空间）。"[]"=已查零发现。
	L0Findings string `gorm:"column:l0_findings;type:text" json:"l0_findings"`
	// L1Findings 是 JSON：L1 发现清单（[]Finding，L1 系命名空间）。"[]"=已查零发现。
	L1Findings string `gorm:"column:l1_findings;type:text" json:"l1_findings"`
	// L1Status 是 L1 增量检查状态（L1Status*），空值规范化为 pending。
	L1Status string `gorm:"column:l1_status;not null" json:"l1_status"`
	// Round 是记录/流转时的修复轮数（与 GateContext.FixRounds 同语义）。
	Round int `gorm:"column:round;not null;default:0" json:"round"`
	// CheckedAt 是最近一次检查落档时间（L0/L1 谁后写谁更新）。
	CheckedAt time.Time `gorm:"column:checked_at" json:"checked_at"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (ChapterQuality) TableName() string { return "chapter_quality" }

// Summary 是某书质量档案的汇总统计（提案 §4.1「档案汇总快审」的输入之一）。
type Summary struct {
	// Chapters 是档案覆盖章数。
	Chapters int `json:"chapters"`
	// L1Checked 是 L1 已查（status=checked）章数。
	L1Checked int `json:"l1_checked"`
	// L0Findings/L1Findings 是发现总数。
	L0Findings int `json:"l0_findings"`
	L1Findings int `json:"l1_findings"`
	// OpenFindings 是未修复发现数（status 空或 pending）。
	OpenFindings int `json:"open_findings"`
	// ByCheck 是按检查项计数（L0+L1 合并计数）。
	ByCheck map[string]int `json:"by_check"`
	// ByStatus 是按修复状态计数（空状态归入 pending）。
	ByStatus map[string]int `json:"by_status"`
}

// MarshalL0Metrics 序列化 L0 指标为 JSON 文本；失败返回空串（坏数据宁缺勿错）。
func MarshalL0Metrics(m L0Metrics) string {
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseL0Metrics 解析 L0 指标 JSON；空串/坏数据返回零值（宁缺勿错，不报错）。
func ParseL0Metrics(s string) L0Metrics {
	var m L0Metrics
	if strings.TrimSpace(s) == "" {
		return m
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return L0Metrics{}
	}
	return m
}

// MarshalFindings 序列化发现清单为 JSON 文本；空清单序列化为 "[]"
// （"[]"=已查零发现，区别于空串=未查）。失败返回空串（坏数据宁缺勿错）。
func MarshalFindings(list []Finding) string {
	if list == nil {
		list = []Finding{}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParseFindings 解析发现清单 JSON；空串/坏数据返回空清单（宁缺勿错，不报错）。
func ParseFindings(s string) []Finding {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var list []Finding
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		return nil
	}
	return list
}
