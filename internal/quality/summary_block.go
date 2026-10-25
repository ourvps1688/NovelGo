package quality

import (
	"fmt"
	"sort"
	"strings"
)

// RenderLedgerBlock 把一书的章节质量档案序列渲染为「质量档案汇总」文本块
// （R2-4：milestone 口径注入 L2 体检输入 + 档案汇总快审的唯一输入，提案 §4.1/§4.2）。
//
// 渲染纪律：
//   - 确定性：按阅读序号升序（同序按 chapter_id），文风指标按键名排序，同输入同输出；
//   - 空清单返回空串——无档案=不注入（降级零提示，提示词与无该块逐字一致）；
//   - 只读汇编不改写：发现原样投影（含修复状态），不做任何聚合判断（裁决留给快审/L2）。
func RenderLedgerBlock(rows []ChapterQuality) string {
	if len(rows) == 0 {
		return ""
	}
	sorted := append([]ChapterQuality(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ReadingNumber != sorted[j].ReadingNumber {
			return sorted[i].ReadingNumber < sorted[j].ReadingNumber
		}
		return sorted[i].ChapterID < sorted[j].ChapterID
	})

	var b strings.Builder
	b.WriteString("【质量档案汇总】各章落盘后的 L0 确定性检查 + L1 增量检查记录" +
		"（供汇总裁决参照；不替代逐章通读举证）：\n")
	fmt.Fprintf(&b, "汇总：档案覆盖 %d 章｜L1 已查 %d 章｜L0 发现 %d 项｜L1 发现 %d 项｜未修复 %d 项\n",
		len(sorted), l1CheckedCount(sorted), l0FindingCount(sorted), l1FindingCount(sorted), openFindingCount(sorted))
	for _, row := range sorted {
		m := ParseL0Metrics(row.L0Metrics)
		hook := "无"
		if m.HasTailHook {
			hook = "有"
		}
		fmt.Fprintf(&b, "- #%d｜%d 字｜章尾钩子:%s｜%s｜%s\n",
			row.ReadingNumber, m.WordCount, hook, l0SideText(row), l1SideText(row))
		if line := styleMetricsLine(m.StyleMetrics); line != "" {
			b.WriteString(line + "\n")
		}
		for _, f := range ParseFindings(row.L0Findings) {
			b.WriteString(findingLine(f) + "\n")
		}
		for _, f := range ParseFindings(row.L1Findings) {
			b.WriteString(findingLine(f) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// l0SideText 渲染 L0 侧发现概况：空清单=未查（"[]"=已查零发现，语义不同）。
func l0SideText(row ChapterQuality) string {
	if strings.TrimSpace(row.L0Findings) == "" {
		return "L0发现:未查"
	}
	return fmt.Sprintf("L0发现:%d 项", len(ParseFindings(row.L0Findings)))
}

// l1SideText 渲染 L1 侧状态与发现数（状态枚举见 L1Status*，空值按 pending）。
func l1SideText(row ChapterQuality) string {
	label := map[string]string{
		L1StatusChecked: "已查",
		L1StatusPending: "待查",
		L1StatusSkipped: "跳过",
	}[row.L1Status]
	if label == "" {
		label = "待查"
	}
	return fmt.Sprintf("L1:%s(%d 项)", label, len(ParseFindings(row.L1Findings)))
}

// styleMetricsLine 渲染文风指标子行（键名排序保证确定性）；空指标省略该行。
func styleMetricsLine(m map[string]float64) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.2f", k, m[k]))
	}
	return "  · 文风指标: " + strings.Join(parts, ", ")
}

// findingLine 渲染单条发现（ID[检查项][修复状态] 描述）；描述压成单行防破版式。
func findingLine(f Finding) string {
	status := f.Status
	if status == "" {
		status = FindingStatusPending
	}
	return fmt.Sprintf("  - %s[%s][%s] %s", f.ID, f.Check, status, oneLine(f.Desc))
}

// oneLine 把多行文本压成单行（块内每条目一行，描述不破版式）。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// l1CheckedCount 统计 L1 已查章数。
func l1CheckedCount(rows []ChapterQuality) int {
	n := 0
	for _, r := range rows {
		if r.L1Status == L1StatusChecked {
			n++
		}
	}
	return n
}

// l0FindingCount / l1FindingCount 统计两侧发现总数。
func l0FindingCount(rows []ChapterQuality) int {
	n := 0
	for _, r := range rows {
		n += len(ParseFindings(r.L0Findings))
	}
	return n
}

func l1FindingCount(rows []ChapterQuality) int {
	n := 0
	for _, r := range rows {
		n += len(ParseFindings(r.L1Findings))
	}
	return n
}

// openFindingCount 统计未修复发现数（状态空或 pending，与 Store.Summary 同口径）。
func openFindingCount(rows []ChapterQuality) int {
	n := 0
	for _, r := range rows {
		for _, f := range append(ParseFindings(r.L0Findings), ParseFindings(r.L1Findings)...) {
			if f.Status == "" || f.Status == FindingStatusPending {
				n++
			}
		}
	}
	return n
}
