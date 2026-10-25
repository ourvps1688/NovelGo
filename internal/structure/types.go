// Package structure 是长程结构索引层的确定性检查器（R2b，提案 §8.2 组件 A）：
// 对台账生命周期投影做规则可判的结构检查（伏笔埋收节奏起步，后续悬念池/人物状态机
// 同层扩充），产出 S 系告警清单供 L2 体检汇总裁决参照。
//
// 契约要点（勿改）：
//   - 纯函数零副作用：输入投影 + 当前进度 → 告警清单，不读库不改写（只报告不改写）；
//   - 告警 ID 用本层命名空间：S 系（S1/S2/…），不与 issue 契约 I/D/T/V 及档案 C/L1 系混排；
//   - 降级零提示：零告警渲染为空串（与无该块逐字一致），坏数据宁缺勿错不猜告警。
package structure

// FindingLevelDefault 是结构告警的严重级：warning 级（P1，与 quality.FindingLevelDefault
// 同值域；结构告警是"需人审的风险提示"而非致命伤，不翻转裁决）。
const FindingLevelDefault = "P1"

// 伏笔 lifecycle 检查项标识（Check 字段取值，封闭枚举）。
const (
	// CheckForeshadowOverdue 超期未收（烂尾风险）：pending 且预期收章已过仍未回收。
	CheckForeshadowOverdue = "foreshadow_overdue"
	// CheckForeshadowNoTarget 无收点规划：pending 且未规划回收位置（target<=0）。
	CheckForeshadowNoTarget = "foreshadow_no_target"
	// CheckForeshadowUnsourced 收未埋=无源：已标记回收但无埋设章记录。
	CheckForeshadowUnsourced = "foreshadow_unsourced"
)

// ForeshadowEntry 是伏笔 lifecycle 检查的输入投影（由 time_entries 伏笔条目投影而来：
// 埋设章/预期收章/回收章/状态）。投影由调用方装配（timeline → structure 适配在流水线侧），
// 本包保持纯函数零依赖。
type ForeshadowEntry struct {
	// ID 是台账条目 ID（time_entries.id），告警定位与排序键。
	ID int64
	// Title 是伏笔简短标题（告警文案引用）。
	Title string
	// Status 是生命周期状态："pending" | "resolved" | "abandoned"。
	Status string
	// TargetReadingNumber 是预期回收的阅读序号；0/负数=未规划收点。
	TargetReadingNumber int
	// SourceChapterID 是埋设章（chapter id）；nil=无埋设记录。
	SourceChapterID *int64
	// ResolvedChapterID 是回收章（chapter id）；nil=未记录回收位置。
	ResolvedChapterID *int64
}

// StructFinding 是结构索引层的一条确定性告警（S 系命名空间，只报告不改写）。
type StructFinding struct {
	// ID 是本层稳定键：S 系（S1/S2/…），按检查产出顺序编号（跨检查器合并时由
	// MergeFindings 重排，保证块内唯一且确定）。
	ID string `json:"id"`
	// Check 是检查项标识（CheckForeshadow* / 不变量 kind）。
	Check string `json:"check"`
	// Level 是严重级：恒 P1（FindingLevelDefault）。
	Level string `json:"level"`
	// Desc 是告警描述（人读文案，只报告）。
	Desc string `json:"desc"`
	// Ref 是涉及的台账条目引用（"表名:id"，如 time_entries:3 / pinned_invariants:2）。
	Ref string `json:"ref"`
}
