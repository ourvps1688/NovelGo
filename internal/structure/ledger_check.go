package structure

// ledger_check.go —— 正文↔台账双向对账（R2b-7，提案 §8.3 组件 B）。
//
// 台账是全书比对的基准，基准错了下游全错。本检查对「带 provenance 来源标注」的
// 台账条目做确定性来源支撑核验：
//   - 来源章已不存在（正文删改/重排）→ 台账来源失效告警；
//   - 来源章正文已不支撑条目（改写后剥离/台账记错）→ 来源失支撑告警。
//
// 冲突不预设谁对：告警只报「台账↔正文失配」这个事实——可能台账过时/记错该修台账
// （fix_type=ledger 出口已有），也可能正文改坏该修正文（fix_type=body）；方向判断
// 由 L1 契约承载（issue fix_type 字段），本层只报告不改写、不翻转裁决。
//
// 宁缺勿错：无 provenance 来源标注/来源章正文不可得/条目无可靠短标签 → 该条目
// 不参与对账（跳过，绝不造告警）；标签 <2 rune 不匹配。

import (
	"fmt"
	"sort"
	"strings"
)

// 台账对账检查项标识（Check 字段取值，封闭枚举）。
const (
	// CheckLedgerSourceMissing 来源失效：条目标注的来源章已不存在（正文删改/重排）。
	CheckLedgerSourceMissing = "ledger_source_missing"
	// CheckLedgerUnsupported 来源失支撑：来源章存在但正文已不出现条目标签
	// （正文改写后台账可能过时，或台账记错——不预设谁对）。
	CheckLedgerUnsupported = "ledger_unsupported"
)

// 对账匹配的最小标签长度（rune）：过短标签误报率高，跳过（宁缺勿错）。
const reconcileMinLabelRunes = 2

// ReconcileEntry 是双向对账的输入投影（由台账条目 + provenance + 来源章正文装配；
// 投影由调用方完成，本包保持纯函数零依赖）。
type ReconcileEntry struct {
	// Ref 是条目引用（"表名:id"），告警归因与排序键。
	Ref string
	// Labels 是条目标签集合（名称/标题/别名）：任一在来源章正文出现即算被支撑。
	Labels []string
	// Sourced 为 true 表示有 provenance 来源章标注；false=未标注（不参与对账）。
	Sourced bool
	// SourceGone 为 true 表示来源章已不存在（调用方已核实）。
	SourceGone bool
	// SourceText 是来源章正文（来源章存在且可读时装配；空=不可得，不参与支撑判定）。
	SourceText string
}

// CheckLedgerReconcile 对带来源标注的台账条目做确定性来源支撑核验，返回 S 系告警。
// 确定性：按 Ref 升序遍历、每条按固定检查顺序产出，同输入同输出。
func CheckLedgerReconcile(entries []ReconcileEntry) []StructFinding {
	sorted := append([]ReconcileEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Ref < sorted[j].Ref })

	var findings []StructFinding
	add := func(check, desc, ref string) {
		findings = append(findings, StructFinding{
			ID:    fmt.Sprintf("S%d", len(findings)+1),
			Check: check,
			Level: FindingLevelDefault,
			Desc:  desc,
			Ref:   ref,
		})
	}
	for _, e := range sorted {
		if !e.Sourced {
			continue // 无来源标注：不参与对账（宁缺勿错）
		}
		if e.SourceGone {
			add(CheckLedgerSourceMissing,
				"台账条目的来源章已不存在（正文可能已删改/重排）——需双向对账："+
					"可能台账过时该修台账，也可能正文丢失该修正文，不预设谁对", e.Ref)
			continue
		}
		if strings.TrimSpace(e.SourceText) == "" {
			continue // 来源章正文不可得：不判支撑（宁缺勿错）
		}
		if !hasEffectiveLabel(e.Labels) {
			continue // 无可靠短标签：无法核验支撑（宁缺勿错）
		}
		if matchedAnyLabel(e.SourceText, e.Labels) {
			continue // 来源章仍支撑该条目
		}
		add(CheckLedgerUnsupported,
			"来源章正文已不支撑该条目（"+labelsPreview(e.Labels)+"）——可能正文改写后台账过时，"+
				"也可能台账记错，不预设谁对；需对账（方向判断进 L1 契约：正文错→body、台账错→ledger）", e.Ref)
	}
	return findings
}

// matchedAnyLabel 任一标签（>=reconcileMinLabelRunes）在正文中出现即算被支撑。
func matchedAnyLabel(text string, labels []string) bool {
	for _, lb := range labels {
		lb = strings.TrimSpace(lb)
		if len([]rune(lb)) < reconcileMinLabelRunes {
			continue
		}
		if strings.Contains(text, lb) {
			return true
		}
	}
	return false
}

// hasEffectiveLabel 是否存在可靠短标签（>=reconcileMinLabelRunes）。
func hasEffectiveLabel(labels []string) bool {
	for _, lb := range labels {
		if len([]rune(strings.TrimSpace(lb))) >= reconcileMinLabelRunes {
			return true
		}
	}
	return false
}

// labelsPreview 生成告警文案里的标签预览（首个有效标签，单行截断）。
func labelsPreview(labels []string) string {
	for _, lb := range labels {
		lb = strings.TrimSpace(lb)
		if len([]rune(lb)) < reconcileMinLabelRunes {
			continue
		}
		return oneLine(truncateRunes(lb, 30))
	}
	return "无标签"
}

// truncateRunes 按 rune 截断（超长加省略号）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
