package contextasm

import "unicode/utf8"

// 预算定案（WBS §0.4）：purpose 不改变预算（简化，避免组合爆炸）。
const (
	// VoiceBudgetBytes Voice 块正文上限：头部保留、尾部截断 + 标注「（已截断）」。
	VoiceBudgetBytes = 8 * 1024
	// IntentBudgetBytes Intent 块正文上限：同 Voice。
	IntentBudgetBytes = 8 * 1024
	// FactsBudgetBytes Facts 上限（现状不变）：由 snapshot.BibleText 既有表粒度截断执行，
	// 装配器不再截断 facts（避免与既有截断标注叠加）。
	FactsBudgetBytes = 24 * 1024
)

// truncatedMark 预算截断标注（WBS §0.2 纪律 3）。
const truncatedMark = "（已截断）"

// fitBudget 把正文压进 budget 字节：头部保留、尾部截断并追加「（已截断）」标注。
// 截断点回退到 rune 边界（绝不切出半个字符，坏数据宁缺勿错）；budget<=0 或未超限则原样返回。
// 返回值为截断后的正文（标注计入结果尾部）。
func fitBudget(text string, budget int) string {
	if budget <= 0 || len(text) <= budget {
		return text
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + truncatedMark
}
