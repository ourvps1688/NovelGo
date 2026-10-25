package quality

// l0.go —— L0 确定性检查器（R2 提案 §4.2 金字塔第一层）。
//
// 零 LLM、毫秒级、纯函数：单章正文 → 指标全集 + C 系发现清单。
// 只报告不改写（P2 纪律）：发现恒 warning 级（P1）落档案，不触发修复路由、不返回错误。
// 本文件落六项确定性检查：字数门槛、章尾钩子、AI 味套话、文风八键漂移、
// 题材卡禁忌（R2b-3）、角色台账称呼混用/死人出镜（R2b-5，§9.2 表）。
// 时间线/冻结事实等需要更多台账输入的 L0 项属后续扩充（提案 §4.2/§9.2）。

import (
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/text"
	"github.com/novelgo/novelgo/internal/voice"
)

// L0 检查项标识单源（Summary.ByCheck 键与发现 Check 字段都取自此，禁两处各写一份）。
const (
	CheckEmptyChapter   = "empty_chapter"   // 空章
	CheckWordCount      = "word_count"      // 字数偏短
	CheckTailHook       = "tail_hook"       // 章尾钩子存在性
	CheckAIFlavor       = "ai_flavor"       // AI 味套话（词表单源 internal/text/deslop.go）
	CheckStyleDrift     = "style_drift"     // 文风八键漂移（P2 口径复用）
	CheckTaboo          = "taboo"           // 题材卡禁忌清单命中（R2b-3，novel 级配置）
	CheckNameDrift      = "name_drift"      // 称呼混用：同一角色 ≥2 种称呼形式同章出现（R2b-5）
	CheckDeadAppearance = "dead_appearance" // 死人出镜：已死角色名/别名在正文出现（R2b-5）
)

// DefaultTargetWords 是单章默认目标字数（与 identity 写作纪律同一口径：
// 章纲/签约约束未给目标时默认 2000 字）。
const DefaultTargetWords = 2000

// shortWordRatio 是字数不达标红线：实际字数低于目标该比例视为偏短
// （与 identity 写作纪律「低于目标 70% 视为不达标」同一口径）。
const shortWordRatio = 0.7

// tailHookWindow 是章尾钩子扫描窗口（按 rune 计，取正文末尾一段）。
const tailHookWindow = 120

// tailHookSignals 是章尾悬念/转折信号词（保守高精度表：宁缺勿错，
// 只列几乎只承担悬念转折的标记，不做宽泛匹配）。
var tailHookSignals = []string{
	"忽然", "骤然", "蓦地", "就在这时", "正在这时",
	"没想到", "不料", "然而", "竟是", "竟然", "居然",
	"不对劲", "糟了", "不好了",
}

// l0MinNameRunes 是角色称呼最短匹配长度（rune）：单字名/单字别名噪声大，
// 宁可漏拦不误报（只报告场景，漏一条无害、错一条误导）。
const l0MinNameRunes = 2

// L0Character 是角色台账投影（正名/别名/生死态），供称呼混用/死人出镜检查（R2b-5）。
// 由 characters 表投影而来；各包自持投影，不跨包导出。
type L0Character struct {
	// Name 是角色正名。
	Name string
	// Aliases 是别名列表（调用方投影时已容错，坏数据按无别名）。
	Aliases []string
	// Alive 是生死态三态：nil=未标注，true=在世，false=已死
	// （只把显式 false 当已死——未标注不算死，宁缺勿错）。
	Alive *bool
}

// L0Input 是 L0 确定性检查的输入（全部确定性来源，零 LLM）。
type L0Input struct {
	// Text 是本章正文（调用方读不到正文时不应调用 RunL0Checks——降级=不跑）。
	Text string
	// ReadingNumber 是本章全局阅读序号（>0 时写入发现作用域 ChapterIDs）。
	ReadingNumber int
	// TargetWords 是本章目标字数（0=DefaultTargetWords；有章纲/签约约束时由调用方带入）。
	TargetWords int
	// Baseline 是文风八键基线（voice 台账 metrics）；空=无基线零漂移发现（不造默认基线）。
	Baseline map[string]float64
	// Genre 是活动题材（AI 味题材豁免，见 text 的 exemptInWebNovel）；空=保守不豁免。
	Genre string
	// TailHookRequired 章尾钩子是否必需（题材卡参数，CheckerConfig.HookRequired 口径）：
	// nil=缺省 true=现行行为（缺钩告警）；显式 false 时跳过章尾钩子检查。
	TailHookRequired *bool
	// TabooList 是题材卡禁忌清单（CheckerConfig.Taboos）：逐词确定性扫描正文，
	// 空清单=不检查=现行行为（缺省逐字一致）。
	TabooList []string
	// Characters 是角色台账投影（称呼混用/死人出镜检查，R2b-5）：
	// 空=无台账=两项检查自然跳过零发现（缺省=现行行为逐字一致）。
	Characters []L0Character
}

// RunL0Checks 对单章跑 L0 全套确定性检查，返回指标全集与 C 系发现清单。
// 纯函数、从不返回错误：任何无输入的检查自然跳过，整体最多零发现（降级纪律）。
// 发现 ID 在一次运行内 C1…Cn 顺编（档案内命名空间，不与 I/D/T/V/L1 混排）。
func RunL0Checks(in L0Input) (L0Metrics, []Finding) {
	metrics := L0Metrics{}
	body := in.Text

	// 字数口径单源：与 chapter.word_count 落库同一计算（text.ComputeStats）。
	st := text.ComputeStats(body)
	metrics.WordCount = st.WordCount

	// 文风八键当前值（与 P2 chapterCurrentMetrics 同口径）。
	metrics.StyleMetrics = style.StatsMetrics(style.ComputeStats([]style.Sample{{Content: body}}))

	target := in.TargetWords
	if target <= 0 {
		target = DefaultTargetWords
	}

	var raw []Finding
	if strings.TrimSpace(body) == "" {
		raw = append(raw, Finding{
			Check: CheckEmptyChapter,
			Desc:  "本章正文为空（0 字），请补写正文内容",
		})
	} else if float64(st.WordCount) < float64(target)*shortWordRatio {
		raw = append(raw, Finding{
			Check: CheckWordCount,
			Desc: fmt.Sprintf("本章 %d 字，低于目标 %d 字的 %.0f%%（偏短），建议扩写至目标量",
				st.WordCount, target, shortWordRatio*100),
		})
	}

	metrics.HasTailHook = hasTailHook(body)
	hookRequired := in.TailHookRequired == nil || *in.TailHookRequired
	if hookRequired && strings.TrimSpace(body) != "" && !metrics.HasTailHook {
		raw = append(raw, Finding{
			Check: CheckTailHook,
			Desc:  "章尾未见钩子信号（悬念问句/转折/突发标记），建议以悬念、未完事件或反转收尾",
		})
	}

	// 题材卡禁忌清单（R2b-3）：逐词确定性扫描，命中即一条发现；空清单=不检查（现行行为）。
	for _, term := range dedupeTerms(in.TabooList) {
		if strings.TrimSpace(body) == "" {
			break
		}
		if strings.Contains(body, term) {
			raw = append(raw, Finding{
				Check: CheckTaboo,
				Desc:  fmt.Sprintf("正文命中题材卡禁忌词「%s」（题材卡优先级最高，请改写规避）", term),
			})
		}
	}

	// 角色台账检查（R2b-5，正向命中判定、宁缺勿错）：称呼混用先于死人出镜（固定顺序）。
	// 无台账输入=两项自然跳过零发现（缺省=现行行为逐字一致）。
	if strings.TrimSpace(body) != "" {
		for _, c := range in.Characters {
			matched := dropSubsumedForms(matchedForms(body, l0CharForms(c)))
			if len(matched) < 2 {
				continue
			}
			raw = append(raw, Finding{
				Check: CheckNameDrift,
				Desc: fmt.Sprintf("正文混用「%s」的多种称呼：%s（同一章内建议统一称呼，或确认别名表是否该更新）",
					strings.TrimSpace(c.Name), strings.Join(matched, "、")),
			})
		}
		for _, c := range in.Characters {
			if c.Alive == nil || *c.Alive {
				continue // 未标注/在世不判（只把显式已死当已死）
			}
			hit := firstMatchedForm(body, l0CharForms(c))
			if hit == "" {
				continue
			}
			raw = append(raw, Finding{
				Check: CheckDeadAppearance,
				Desc: fmt.Sprintf("已死角色「%s」在正文出镜（命中「%s」，台账生死态=已死；回忆/闪回请显式标注，若复活请先更新台账）",
					strings.TrimSpace(c.Name), hit),
			})
		}
	}

	// AI 味套话：词表单源 internal/text/deslop.go（不另立词表）。
	// blocking 命中通常是写入时已拒的历史遗留/导入章节，advisory 是密度启发式；统一 warning 落档。
	for _, it := range text.BlockingIssuesForGenre(body, in.Genre) {
		raw = append(raw, aiFlavorFinding(it))
	}
	for _, it := range text.AdvisoryIssues(body) {
		raw = append(raw, aiFlavorFinding(it))
	}

	// 文风八键漂移：P2 口径复用（无基线零发现，不造默认基线）。
	for _, f := range voice.DetectDrift(in.Baseline, metrics.StyleMetrics) {
		raw = append(raw, Finding{
			Check: CheckStyleDrift,
			Desc: fmt.Sprintf("文风指标 %s 偏离基线：基线 %.2f，当前 %.2f，容差 %.2f",
				f.Metric, f.Baseline, f.Current, f.Tol),
		})
	}

	// C 系顺编 + 作用域回填（一次运行内不撞号）。
	out := make([]Finding, 0, len(raw))
	for i, f := range raw {
		f.ID = fmt.Sprintf("C%d", i+1)
		f.Level = FindingLevelDefault
		f.Status = FindingStatusPending
		if in.ReadingNumber > 0 {
			f.ChapterIDs = []int64{int64(in.ReadingNumber)}
		}
		out = append(out, f)
	}
	return metrics, out
}

// dedupeTerms 归一化词表：TrimSpace 去空去重，保持首次出现顺序（确定性）。
// 输入侧（CheckerConfig.Validate）已归一，此处再兜一层——L0Input 是公开入参，
// 调用方可能直接喂原始清单（宁缺勿错：空词跳过不产生空命中）。
func dedupeTerms(terms []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// aiFlavorFinding 把一条去 AI 味命中投影为 C 系发现（rule 入 desc 供 Summary 归因）。
func aiFlavorFinding(it text.DeslopIssue) Finding {
	desc := "AI 味套话 [" + it.Rule + "]：" + it.Message
	if it.Matched != "" {
		desc += "（命中：" + it.Matched + "）"
	}
	return Finding{Check: CheckAIFlavor, Desc: desc}
}

// hasTailHook 判定章尾钩子存在性：正文末尾窗口内出现悬念/转折信号词，
// 或以问句/省略号收束，均视为有钩。纯确定性启发式（warning 级，宁缺勿错）。
func hasTailHook(body string) bool {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return false
	}
	runes := []rune(trimmed)
	if len(runes) > tailHookWindow {
		runes = runes[len(runes)-tailHookWindow:]
	}
	tail := string(runes)
	if strings.ContainsAny(tail, "？?…") {
		return true
	}
	for _, sig := range tailHookSignals {
		if strings.Contains(tail, sig) {
			return true
		}
	}
	return false
}

// l0CharForms 投影角色的称呼形式（正名+别名，TrimSpace 去空去重保序，过短跳过）。
func l0CharForms(c L0Character) []string {
	terms := append([]string{c.Name}, c.Aliases...)
	out := make([]string, 0, len(terms))
	seen := map[string]bool{}
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] || len([]rune(t)) < l0MinNameRunes {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// matchedForms 返回正文中命中的称呼形式（保形式序：正名先于别名）。
func matchedForms(body string, forms []string) []string {
	var out []string
	for _, f := range forms {
		if strings.Contains(body, f) {
			out = append(out, f)
		}
	}
	return out
}

// firstMatchedForm 返回正文中第一个命中的称呼形式（正名先于别名），未命中空串。
func firstMatchedForm(body string, forms []string) string {
	for _, f := range forms {
		if strings.Contains(body, f) {
			return f
		}
	}
	return ""
}

// dropSubsumedForms 剔除被更长命中形式包含的短形式（「林澈」⊂「林澈哥哥」只算一种称呼）：
// 长短并用的真实混用会漏报，但误报误导、漏报无害（宁缺勿错）。
func dropSubsumedForms(matched []string) []string {
	out := make([]string, 0, len(matched))
	for i, f := range matched {
		subsumed := false
		for j, g := range matched {
			if i != j && strings.Contains(g, f) {
				subsumed = true
				break
			}
		}
		if !subsumed {
			out = append(out, f)
		}
	}
	return out
}
