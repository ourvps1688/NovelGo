package snapshot

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// deltaSummaryBudgetBytes 是差异摘要文本的预算上限：超限按行截断并标注。
// 结构化差异可随时经 Diff* 重算（diff 运行时算不物化），摘要只为留痕可读。
const deltaSummaryBudgetBytes = 4 * 1024

// maxValRunes 是单个字段值渲染的最大 rune 数（超限截断）。
const maxValRunes = 60

// FormatDelta 把结构化差异渲染为人类可读的留痕摘要文本（retcon Record 用）。
// 无差异返回空串（调用方据此识别「无台账变更」）；超预算按行截断并标注，
// 完整结构化差异以 Diff* 系列为准。
func FormatDelta(d *Delta) string {
	if d == nil || len(d.Tables) == 0 {
		return ""
	}
	var b strings.Builder
	for _, td := range d.Tables {
		fmt.Fprintf(&b, "%s: +%d -%d ~%d\n", td.Table, len(td.Added), len(td.Removed), len(td.Changed))
		for _, row := range td.Added {
			fmt.Fprintf(&b, "  + #%d\n", rowID(row["id"]))
		}
		for _, row := range td.Removed {
			fmt.Fprintf(&b, "  - #%d\n", rowID(row["id"]))
		}
		for _, ch := range td.Changed {
			for _, field := range sortedFieldNames(ch.Fields) {
				fd := ch.Fields[field]
				fmt.Fprintf(&b, "  ~ #%d %s: %s → %s\n", ch.ID, field,
					truncateRunes(formatVal(fd.Old), maxValRunes), truncateRunes(formatVal(fd.New), maxValRunes))
			}
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if len(out) > deltaSummaryBudgetBytes {
		// 按字节预算截断时避开 UTF-8 rune 中间。
		cut := out[:deltaSummaryBudgetBytes]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		out = cut + "\n…（差异摘要超预算截断，完整差异见结构化 diff）"
	}
	return out
}

// sortedFieldNames 稳定输出字段序（与 diffFields 的键排序一致）。
func sortedFieldNames(fields map[string]FieldDelta) []string {
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	// 小映射直接插入排序即可；map 遍历无序，这里做升序排序保输出确定。
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// formatVal 渲染单个字段值：字符串加引号、null 显式、其余按 %v。
func formatVal(v any) string {
	if v == nil {
		return "null"
	}
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%v", v)
}

// truncateRunes 截取前 n 个 rune（超出加省略号）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
