package quality

// opening.go —— 开篇体检专项（P0-0）：签约驳回高频根因的机器化检查。
//
// 真实编辑驳回实证（两本七猫男频悬疑，docs/quality-signability-upgrade-plan-2026-11-05.md §7）：
// 「代入感较弱、开篇切入点缺乏吸引力」「核心看点不够明确和突出」的共同签名 =
// 卖点迟到（金手指 >800 字才出场、标题卖点压到章末）+ 个人利害缺位（主角旁观者无绑定）。
// 本文件把可确定性量化的部分机器化（七猫量化口径硬节点）：
//   - 核心看点关键词前 500 字实体化（premise_late）；
//   - 金手指/核心机制关键词前 800 字出场（cheat_late）；
//   - 主角前 200 字首现（protagonist_late）；
//   - 黄金三章章长 1000–1500 字区间（opening_length）。
// 语义级部分（个人利害绑定/POV 聚焦/标题卖点实体化判读）由 L1 immersion_anchor
// 检查项承接（quality/contract.go + chapter-ci）。
//
// 缺省纪律：三组关键词未配置=整专项不跑（现行行为逐字一致，CheckerConfig.Opening 投影）；
// 阈值取七猫签约量化口径常量（平台 profile 机制化后可迁配置，P0-1）。

import (
	"fmt"
	"strings"
)

const (
	// premiseWindowRunes 是核心看点实体化窗口（字位）：标题/简介卖点须在前 500 字落地。
	premiseWindowRunes = 500
	// cheatWindowRunes 是金手指出场窗口（字位）：前 800 字内须亮出金手指/核心机制。
	cheatWindowRunes = 800
	// protagonistWindowRunes 是主角首现窗口（字位）：前 200 字主角须入戏。
	protagonistWindowRunes = 200
	// openingChapterWindow 是黄金三章窗口（全局阅读序号 1..N）：章长区间检查作用域。
	openingChapterWindow = 3
	// openingLenMin/openingLenMax 是黄金三章章长适配区间（七猫 1000–1500 字）。
	openingLenMin = 1000
	openingLenMax = 1500
)

// L0Opening 是开篇体检规则投影（CheckerConfig 三组关键词，P0-0）。
type L0Opening struct {
	// PremiseKeywords 是核心看点（标题/简介卖点）关键词。
	PremiseKeywords []string
	// CheatKeywords 是金手指/核心机制关键词。
	CheatKeywords []string
	// ProtagonistNames 是主角称呼（正名+常用别名）。
	ProtagonistNames []string
}

// openingFindings 开篇体检（P0-0）：位次三查仅作用于第 1 章（开篇章），
// 章长区间查作用于黄金三章（阅读序号 1..openingChapterWindow）。
// 未配置/序号缺失（ReadingNumber<=0）/空正文自然跳过（宁缺勿错）。
func openingFindings(in L0Input, wordCount int) []Finding {
	if in.Opening == nil || in.ReadingNumber <= 0 {
		return nil
	}
	o := in.Opening
	premises := dedupeTerms(o.PremiseKeywords)
	cheats := dedupeTerms(o.CheatKeywords)
	protagonists := dedupeTerms(o.ProtagonistNames)
	if len(premises)+len(cheats)+len(protagonists) == 0 {
		return nil
	}
	body := in.Text
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var out []Finding
	if in.ReadingNumber == 1 {
		if f := windowPositionFinding(CheckPremiseLate, body, premises, premiseWindowRunes,
			"核心看点", "七猫口径：标题/简介卖点须在前 500 字内实体化，卖点迟到是签约驳回高频签名"); f != nil {
			out = append(out, *f)
		}
		if f := windowPositionFinding(CheckCheatLate, body, cheats, cheatWindowRunes,
			"金手指/核心机制", "七猫口径：前 800 字内须亮出金手指/核心机制"); f != nil {
			out = append(out, *f)
		}
		if f := windowPositionFinding(CheckProtagonistLate, body, protagonists, protagonistWindowRunes,
			"主角", "七猫口径：前 200 字主角须与事件产生利害绑定入戏"); f != nil {
			out = append(out, *f)
		}
	}
	if in.ReadingNumber <= openingChapterWindow {
		switch {
		case wordCount < openingLenMin:
			out = append(out, Finding{
				Check: CheckOpeningLength,
				Desc: fmt.Sprintf("黄金三章章长不适配：本章 %d 字，低于七猫区间 %d–%d 字（开篇过短易被判信息量不足/代入感弱）",
					wordCount, openingLenMin, openingLenMax),
			})
		case wordCount > openingLenMax:
			out = append(out, Finding{
				Check: CheckOpeningLength,
				Desc: fmt.Sprintf("黄金三章章长不适配：本章 %d 字，超出七猫区间 %d–%d 字（开篇过长易被判节奏拖、卖点迟到）",
					wordCount, openingLenMin, openingLenMax),
			})
		}
	}
	return out
}

// windowPositionFinding 位次检查通用判定（P0-0）：取关键词表中最早字位；
// 全表未命中 → 「未见」发现；最早字位在窗外 → 「迟到」发现（含命中词与字位）；
// 窗内命中 → nil。窗口<=0 或词表空 → nil（宁缺勿错）。
func windowPositionFinding(check, body string, terms []string, windowRunes int, label, rule string) *Finding {
	if windowRunes <= 0 || len(terms) == 0 {
		return nil
	}
	best := -1
	bestTerm := ""
	for _, term := range terms {
		if pos := firstRunePos(body, term); pos >= 0 && (best < 0 || pos < best) {
			best, bestTerm = pos, term
		}
	}
	if best < 0 {
		return &Finding{Check: check,
			Desc: fmt.Sprintf("开篇章未见%s关键词（%s）（%s）", label, strings.Join(terms, "、"), rule)}
	}
	if best < windowRunes {
		return nil
	}
	return &Finding{Check: check,
		Desc: fmt.Sprintf("%s迟到：「%s」首现于约第 %d 字，超出前 %d 字窗口（%s）",
			label, bestTerm, best+1, windowRunes, rule)}
}

// firstRunePos 返回 term 在正文中的首个字位（该词前的 rune 数），未命中 -1。
func firstRunePos(body, term string) int {
	i := strings.Index(body, term)
	if i < 0 {
		return -1
	}
	return len([]rune(body[:i]))
}
