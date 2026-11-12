package quality

// prescription.go —— #114「验收处方」唯一真相源。
//
// 旧链路只把 [check] desc 这句人读文案发给 chat，LLM 拿不到阈值、命中位置、
// 实测值和「改成什么才算不再命中」，于是每轮凭语感重写整章，判定器又从头报一遍。
// 本文件把发现项投影成结构化处方：判定式 / 当前实测值 / 目标值 / 位置 /
// 可执行动作 / 避免动作。所有数值直接引用检测侧同一份常量，禁止再写一份数字。
//
// 纯函数、零 LLM、零写操作：不落表、不改 Finding.Status、不触发任何修复派发。
// 「所有修复都走 chat」纪律不变——处方只是聊天消息与判官提示词的输入。

import (
	"fmt"
	"strings"
)

// AcceptanceMarker 是 chat 改稿消息里验收条件块的标题标记。
// 前端 i18n 值必须逐字包含该串，app 侧据此确定性注入验收硬约束（不经技能路由）。
const AcceptanceMarker = "【本章验收条件】"

// Prescription 是一条发现的可执行验收条件。
type Prescription struct {
	Check     string // 检查项标识（与 Finding.Check 同源，供前端对齐）
	Criterion string // 机器判定式（"什么条件会报"）
	Current   string // 当前实测值
	Target    string // 达标目标（"改到什么才不报"）
	Where     string // 命中位置（段序 / 字区间 / 章尾窗口）
	Do        string // 可执行动作
	Avoid     string // 明确禁止的动作（互斥项预检结论）
}

// Render 渲染为一行文本（chat 消息 / 判官 prompt 共用的同一份表述）。
func (p Prescription) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] 判定：%s ｜ 当前：%s ｜ 目标：%s ｜ 位置：%s ｜ 动作：%s",
		p.Check, p.Criterion, p.Current, p.Target, p.Where, p.Do)
	if p.Avoid != "" {
		fmt.Fprintf(&b, " ｜ 避免：%s", p.Avoid)
	}
	return b.String()
}

// RenderPrescriptions 批量渲染（保序、不去重：一条发现对应一条处方）。
func RenderPrescriptions(list []Prescription) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Render())
	}
	return out
}

// BuildPrescriptions 把当前档案发现清单投影为处方清单。
// curve 必须是与检测同源的 L0 基线曲线（BuildTensionCurve 或 chapter_tension
// 的 source=l0 行）；wordCount 是 text.ComputeStats 口径；words 是本书生效字数口径。
// 任何输入缺失都降级为通用处方，绝不报错、绝不臆造数值。
func BuildPrescriptions(findings []Finding, curve []TensionPoint, wordCount int, words ChapterWordsTarget) []Prescription {
	out := make([]Prescription, 0, len(findings))
	for _, f := range findings {
		p := prescriptionFor(f, curve, wordCount, words)
		p.Check = f.Check
		out = append(out, p)
	}
	return out
}

// prescriptionFor 单条投影：所有阈值/区间均直接引用检测侧常量。
func prescriptionFor(f Finding, curve []TensionPoint, wordCount int, words ChapterWordsTarget) Prescription {
	switch f.Check {
	case CheckTailHook:
		return Prescription{
			Criterion: fmt.Sprintf("正文末尾 %d rune 内命中悬念/转折信号词，或问句/省略号收束", tailHookWindow),
			Current:   "未命中（has_tail_hook=false）",
			Target:    "命中 ≥1 个信号，或以问句/省略收束",
			Where:     fmt.Sprintf("章末 %d 字", tailHookWindow),
			Do:        "在章末窗口内补一个未完事件、反转或悬念问句",
			Avoid:     "不要用感叹号或直白情绪词充钩子（会触发去 AI 味）",
		}

	case CheckWordCount, CheckOpeningLength, CheckChapterLengthFit:
		lo, hi, ok := words.EffectiveLengthWindow()
		target := fmt.Sprintf("≥%d 字（目标 %.0f%% 红线）", int(float64(words.EffectiveTarget())*shortWordRatio), shortWordRatio*100)
		if ok {
			target = fmt.Sprintf("落在 %d–%d 字区间内", lo, hi)
		}
		return Prescription{
			Criterion: fmt.Sprintf("word_count 低于目标 %d 字的 %.0f%%", words.EffectiveTarget(), shortWordRatio*100),
			Current:   fmt.Sprintf("%d 字", wordCount),
			Target:    target,
			Where:     "全章",
			Do:        "用推进情节的实质内容调整篇幅",
			Avoid:     "不要注水凑字，也不要为补爽点单纯扩写超区间",
		}

	case CheckPacingDrag:
		start, end, run := longestFlatRun(curve)
		return Prescription{
			Criterion: fmt.Sprintf("连续 ≥%d 段 tension < %.0f", pacingDragMinRun, tensionFlatBelow),
			Current:   fmt.Sprintf("seg %d–%d 连续 %d 段 tension < %.0f", start, end, run, tensionFlatBelow),
			Target:    fmt.Sprintf("连续平坦段 < %d 段（至少把其中一段抬到 tension ≥ %.0f）", pacingDragMinRun, tensionFlatBelow),
			Where:     curveRange(curve, start, end),
			Do:        "在该区间用具体动作细节、对抗升级或短句节奏替换冗余描述",
			Avoid:     "不要堆直白情绪词或感叹号（与去 AI 味互斥）",
		}

	case CheckBeatGap, CheckBeatDensity:
		// #114 P3：只给「安排一处对抗升级」这种语感级指令，LLM 不知道改哪一段、
		// 差多少、判定式长什么样，改完判定器仍看不见 → 反复重写整章。
		// 现在把段级实测信号、离达标最近的段位、最弱段和可执行数字一并交出去。
		diagnosis, where := beatSegmentDiagnostics(curve)
		return Prescription{
			Criterion: fmt.Sprintf(
				"全章爽点段数 ≥1（该段 emotion ≥ %.0f 判 climax，或 structure ≥ %.2f 判结构峰）；structure = 0.40×min(冲突/%.1f,1) + 0.35×min(动作/%.2f,1) + 0.25×min(短句/%.2f,1)",
				tensionEmotionClimaxAt, structuralPeakAt, tensionConflictCap, tensionActionCap, tensionShortRatioCap),
			Current: fmt.Sprintf("爽点 %d 处（climax %d / 结构峰 %d）；%s；阈值 %.2f",
				beatPeakCount(curve), climaxBeatCount(curve), structuralPeakCount(curve),
				diagnosis, structuralPeakAt),
			Target: fmt.Sprintf("该段 structure ≥ %.2f 或 emotion ≥ %.0f（全章 ≥1 处爽点）",
				structuralPeakAt, tensionEmotionClimaxAt),
			Where: where,
			Do: fmt.Sprintf(
				"定点只改目标段（不要整章重写）：写一处明确的对抗升级/反转，段落里给足 ≥%d 处具体动作或身体细节（动作密度 ≥ %.2f 次/百字），并把 ≤%d 字短句占比提到 ≥ %.2f；改完重新检测看 structure 数值是否过线",
				beatActionHitsNeeded(), tensionActionCap,
				tensionShortSentenceMax, tensionShortRatioCap),
			Avoid: "不要靠堆直白情绪词或感叹号凑情绪峰（会触发去 AI 味）；不要靠堆武侠冲突词，非武侠题材该词表恒 0，只能靠动作细节 + 短句节奏抬 structure",
		}

	case CheckOpeningSlope:
		segs := openingSlopeSegs(curve)
		rise := 0.0
		if len(segs) >= 2 {
			for _, pt := range segs[1:] {
				if pt.Tension-segs[0].Tension > rise {
					rise = pt.Tension - segs[0].Tension
				}
			}
		}
		return Prescription{
			Criterion: fmt.Sprintf("前 %d 字内「后续段最高 tension − 首段 tension」≥ %.1f", openingSlopeWindowRunes, openingSlopeMinRise),
			Current:   fmt.Sprintf("最高升幅 %.1f（窗口 %d 段）", rise, len(segs)),
			Target:    fmt.Sprintf("升幅 ≥ %.1f", openingSlopeMinRise),
			Where:     fmt.Sprintf("前 %d 字（seg 0 之后）", openingSlopeWindowRunes),
			Do:        "在首段之后安排对抗升级/反转/关键信息揭露抬升后段张力",
			Avoid:     "不要只在首段堆冲突（抬高首段反而缩小升幅）",
		}

	case CheckAIFlavor:
		return Prescription{
			Criterion: "命中 internal/text/deslop.go 的 blocking/advisory 规则",
			Current:   "见发现描述中的命中片段",
			Target:    "该片段不再命中任一 deslop 规则",
			Where:     "发现描述标注的命中处",
			Do:        "改写为具体动作或场景细节（show-not-tell）",
			Avoid:     "不要用另一句套话替换套话，也不要新增直白情绪词/感叹号",
		}

	case "reader_state", "continuity", "foreshadow", "pacing", "immersion_anchor", "task_card":
		return Prescription{
			Criterion: "台账/前章事实与本章正文一致（L1 子 Agent 判定）",
			Current:   f.Desc,
			Target:    "台账与正文互相印证，无互相矛盾条目",
			Where:     "本章正文 + " + strings.Join(f.LedgerRefs, " · "),
			Do:        "二选一：把正文改成与台账一致，或更新/作废过时台账条目",
			Avoid:     "不要只在正文里反复猜措辞——台账未更新时矛盾会一直复发",
		}

	default:
		return Prescription{
			Criterion: "该检查项不再命中其确定性判定式",
			Current:   f.Desc,
			Target:    "该发现消失（重新检测后不再出现）",
			Where:     "本章",
			Do:        "按发现描述定位并改写命中处",
			Avoid:     "不要整体推翻重写无关段落",
		}
	}
}

// beatActionHitsNeeded 把动作密度帽换算成「一个 400 字 beat 段要写几处具体动作」。
// 数字直接由检测侧常量派生，禁止再写一份口径。
func beatActionHitsNeeded() int {
	n := int(float64(tensionSegTargetRunes) / 100.0 * tensionActionCap)
	if n < 1 {
		return 1
	}
	return n
}

// beatSegmentDiagnostics 给出爽点类处方的段级实测口径：
//   - 「离达标最近」= structure 最高的段（改这一段最省字，避免整章重写）；
//   - 「最弱」= tension 最低的段（拖沓/平坦的直观位置）；
//   - 同时列出动作/短句/冲突密度，让 LLM 看得见「差在哪一项、差多少」。
//
// 曲线为空时不臆造段位，返回可降级文案。
func beatSegmentDiagnostics(curve []TensionPoint) (string, string) {
	if len(curve) == 0 {
		return "曲线为空（重新检测本章后可见段级信号）", "全章（任一 400 字 beat 段）"
	}
	best, weakest := curve[0], curve[0]
	for _, pt := range curve[1:] {
		if pt.Metrics[tensionMetricStructural] > best.Metrics[tensionMetricStructural] {
			best = pt
		}
		if pt.Tension < weakest.Tension {
			weakest = pt
		}
	}
	diagnosis := fmt.Sprintf(
		"离达标最近 seg %d：structure %.2f（动作 %.2f/百字、短句占比 %.2f、冲突 %.2f/百字）；张力最弱 seg %d：tension %.1f",
		best.Seg, best.Metrics[tensionMetricStructural], best.Metrics[tensionMetricAction],
		best.Metrics[tensionMetricShortRatio], best.Metrics[tensionMetricConflict],
		weakest.Seg, weakest.Tension)
	where := fmt.Sprintf("seg %d（字 %d–%d，约 %.0f 字）", best.Seg, best.StartRune, best.EndRune,
		best.Metrics[tensionMetricRunes])
	return diagnosis, where
}

// curveRange 把段序区间映射成「seg a–b（字 x–y）」的可读位置。
func curveRange(curve []TensionPoint, start, end int) string {
	lo, hi := -1, -1
	for _, pt := range curve {
		if pt.Seg == start {
			lo = pt.StartRune
		}
		if pt.Seg == end {
			hi = pt.EndRune
		}
	}
	if lo < 0 || hi < 0 {
		if len(curve) == 0 {
			return fmt.Sprintf("seg %d–%d（曲线为空，重检后可见字区间）", start, end)
		}
		return fmt.Sprintf("seg %d–%d", start, end)
	}
	return fmt.Sprintf("seg %d–%d（字 %d–%d）", start, end, lo, hi)
}
