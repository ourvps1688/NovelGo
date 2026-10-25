package quality

// l0.go —— L0 确定性检查器（R2 提案 §4.2 金字塔第一层）。
//
// 零 LLM、毫秒级、纯函数：单章正文 → 指标全集 + C 系发现清单。
// 只报告不改写（P2 纪律）：发现恒 warning 级（P1）落档案，不触发修复路由、不返回错误。
// 台账引用/时间线/冻结事实等需要台账输入的 L0 项属后续扩充（提案 §4.2/§9.2），
// 本文件先落四项确定性检查：字数门槛、章尾钩子、AI 味套话、文风八键漂移。

import (
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/text"
	"github.com/novelgo/novelgo/internal/voice"
)

// L0 检查项标识单源（Summary.ByCheck 键与发现 Check 字段都取自此，禁两处各写一份）。
const (
	CheckEmptyChapter = "empty_chapter" // 空章
	CheckWordCount    = "word_count"    // 字数偏短
	CheckTailHook     = "tail_hook"     // 章尾钩子存在性
	CheckAIFlavor     = "ai_flavor"     // AI 味套话（词表单源 internal/text/deslop.go）
	CheckStyleDrift   = "style_drift"   // 文风八键漂移（P2 口径复用）
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
	if strings.TrimSpace(body) != "" && !metrics.HasTailHook {
		raw = append(raw, Finding{
			Check: CheckTailHook,
			Desc:  "章尾未见钩子信号（悬念问句/转折/突发标记），建议以悬念、未完事件或反转收尾",
		})
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
