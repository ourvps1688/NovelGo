package reader

import "fmt"

// ── 读者认知条目的戏剧生命周期（WBS §0.2 状态机 + R2 决策）──
//
// lifecycle = 故事时间（章节引用），映射既有 planted_chapter_id/revealed_chapter_id 两列，不加新列；
// temporal 的 created_at/changed_at 为墙钟审计时间，二者并存不混（R2）。
//
// 每种类型的生命周期事件与列映射：
//
//	known         揭示 revealed_at → planted_chapter_id（单点，无终点事件）
//	suspense      埋设 raised_at → planted_chapter_id；回收 payoff_at → revealed_chapter_id（nil=未回收）
//	misconception 形成 formed_at → planted_chapter_id；纠正 corrected_at → revealed_chapter_id（nil=持续中）
//
// 时间戳采集双通道（WBS 2.7，闭 G4）：
//   - S4 检测通道：signability-audit 体检检测「埋设未回收 / 误解未纠正 / 该揭示未揭示」等
//     lifecycle 缺口 → 产 fix_type=ledger + dimension=reader_pov 的 issue
//     → S5 经 create/update_reader_perspective_entry 补写时间戳。
//   - 作者确认通道：作者在前端「标记回收」（quick reveal）或编辑表单确认事件
//     → app API 写入时间戳。
//
// 两个通道的写入口（app/reader.go 与 mcp_tools）都必须经 ValidateLifecycle/ValidateLifecycleUpdate 落库。

// LifecycleEvent 是生命周期事件（故事时间点，落在章节上）。
type LifecycleEvent string

const (
	// 起点事件：写入 planted_chapter_id。
	EventReveal LifecycleEvent = "reveal" // 揭示（known）：读者自该章起知道
	EventRaise  LifecycleEvent = "raise"  // 埋设（suspense）：悬念种下
	EventForm   LifecycleEvent = "form"   // 形成（misconception）：误知形成
	// 终点事件：写入 revealed_chapter_id。
	EventPayoff  LifecycleEvent = "payoff"  // 回收（suspense）
	EventCorrect LifecycleEvent = "correct" // 纠正（misconception）
)

// StartEvent 返回某类型的起点事件（写 planted_chapter_id）。类型非法时 ok=false。
func StartEvent(typ string) (LifecycleEvent, bool) {
	switch typ {
	case TypeKnown:
		return EventReveal, true
	case TypeSuspense:
		return EventRaise, true
	case TypeMisconception:
		return EventForm, true
	}
	return "", false
}

// EndEvent 返回某类型的终点事件（写 revealed_chapter_id）。
// known 是单点生命周期（仅揭示），无终点事件，ok=false。
func EndEvent(typ string) (LifecycleEvent, bool) {
	switch typ {
	case TypeSuspense:
		return EventPayoff, true
	case TypeMisconception:
		return EventCorrect, true
	}
	return "", false
}

// EventLabel 返回事件的中文名（错误消息与文档用）。
func EventLabel(ev LifecycleEvent) string {
	switch ev {
	case EventReveal:
		return "揭示"
	case EventRaise:
		return "埋设"
	case EventForm:
		return "形成"
	case EventPayoff:
		return "回收"
	case EventCorrect:
		return "纠正"
	}
	return string(ev)
}

// ValidateLifecycle 校验一条读者认知条目的 lifecycle 终态自洽（创建与更新共用）。
// orderOf 返回章节的阅读序（0=无法定位则跳过时序比较）；可传 nil 表示不做时序校验。
//
// 规则（WBS §0.2 状态机）：
//   - type 必须是 known|suspense|misconception；
//   - content 非空；
//   - planted_chapter_id 必填（起点事件：埋设/形成/揭示）；
//   - known 是单点生命周期，不得设置 revealed_chapter_id（无终点事件）；
//   - 回收/纠正章的阅读序不得早于埋设/形成章（先埋后收）。
func ValidateLifecycle(e ReaderPerspective, orderOf func(chapterID int64) int) error {
	switch e.Type {
	case TypeKnown, TypeSuspense, TypeMisconception:
	default:
		return fmt.Errorf("读者认知条目类型必须是 %s/%s/%s，当前 %q",
			TypeKnown, TypeSuspense, TypeMisconception, e.Type)
	}
	if e.Content == "" {
		return fmt.Errorf("读者认知条目内容不能为空")
	}
	if e.PlantedChapterID == nil {
		start, _ := StartEvent(e.Type)
		return fmt.Errorf("种下章节不能为空（%s事件的时间戳）", EventLabel(start))
	}
	if e.Type == TypeKnown && e.RevealedChapterID != nil {
		return fmt.Errorf("known 是单点生命周期（揭示事件），不应设置回收章")
	}
	if e.RevealedChapterID != nil && orderOf != nil {
		plantedOrder := orderOf(*e.PlantedChapterID)
		revealedOrder := orderOf(*e.RevealedChapterID)
		if plantedOrder > 0 && revealedOrder > 0 && revealedOrder < plantedOrder {
			start, _ := StartEvent(e.Type)
			end, _ := EndEvent(e.Type)
			return fmt.Errorf("%s章不能早于%s章（第%d章 < 第%d章）",
				EventLabel(end), EventLabel(start), revealedOrder, plantedOrder)
		}
	}
	return nil
}

// ValidateLifecycleUpdate 在更新时校验：lifecycle 字段（type/planted/revealed）未变则跳过
// （存量条目允许只改文案，不被历史数据卡住），任一变化则按终态全量校验。
func ValidateLifecycleUpdate(old, new ReaderPerspective, orderOf func(chapterID int64) int) error {
	if sameLifecycle(old, new) {
		return nil
	}
	return ValidateLifecycle(new, orderOf)
}

// sameLifecycle 比较两条目的 lifecycle 三字段（type/planted/revealed）是否一致。
func sameLifecycle(a, b ReaderPerspective) bool {
	return a.Type == b.Type &&
		sameChapter(a.PlantedChapterID, b.PlantedChapterID) &&
		sameChapter(a.RevealedChapterID, b.RevealedChapterID)
}

func sameChapter(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
