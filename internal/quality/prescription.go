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
// 判官评分门禁走这条零锚点路径，输出与锚点改造前逐字一致。
func BuildPrescriptions(findings []Finding, curve []TensionPoint, wordCount int, words ChapterWordsTarget) []Prescription {
	out := make([]Prescription, 0, len(findings))
	for _, f := range findings {
		p := prescriptionFor(f, curve, wordCount, words)
		p.Check = f.Check
		out = append(out, p)
	}
	return out
}

// BuildPrescriptionsAnchored 是给「去修复」消息用的定向版：额外传入当前落盘正文，
// 为每条 L1 语义发现算出可在正文逐字命中的原句锚点，处方里的位置/动作/避免项
// 因此按条不同、可 search_replace 直改。L0 处方与旧 L1 模板逐字不变。
//
// 只有档案详情视图调用本函数；判官评分门禁仍走 BuildPrescriptions（零锚点），
// 保证评分 prompt 与历史 golden 不受影响。纯函数、零 LLM、零写操作。
func BuildPrescriptionsAnchored(findings []Finding, curve []TensionPoint, wordCount int, words ChapterWordsTarget, content string) []Prescription {
	out := make([]Prescription, 0, len(findings))
	for _, f := range findings {
		var p Prescription
		if L1CheckValid(f.Check) {
			// 定向路径下 L1 一律走定向处方：抽不到锚点也如实标「未定位」，
			// 绝不退回会把纯正文数字/措辞问题导向「去更新台账」的旧通用模板。
			p = l1AnchoredPrescription(f, anchoredForFinding(f, content, curve))
		} else {
			p = prescriptionFor(f, curve, wordCount, words)
		}
		p.Check = f.Check
		out = append(out, p)
	}
	return out
}

// anchoredForFinding 只给 L1 语义发现算锚点；L0 发现一律零值（其处方本就带段级
// 实测，不走锚点分支）。档案里已有 Anchor 且仍命中正文时直接定位，不重抽 Desc。
func anchoredForFinding(f Finding, content string, curve []TensionPoint) Anchor {
	if !L1CheckValid(f.Check) || strings.TrimSpace(content) == "" {
		return Anchor{Seg: -1}
	}
	if a, ok := LocateAnchor(content, f.Anchor, curve); ok {
		return a
	}
	if a, ok := ExtractAnchor(f.Desc, content, curve); ok {
		return a
	}
	return Anchor{Seg: -1}
}

// prescriptionFor 单条投影：所有阈值/区间均直接引用检测侧常量。
// 判官评分侧与 L0 处方共用；L1 语义发现的定向版见 l1AnchoredPrescription。
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
		// #120：#114 P3 给 beat_density/beat_gap 与 opening_slope 都补了段级实测 +
		// 可执行数字，唯独拖沓段还是语感级指令，模型拿不到「改哪一段、差多少」，
		// 真实第5章 seg 3–5 连挂 9 轮。现与那两处同源，复用同一批检测侧常量。
		start, end, run := longestFlatRun(curve)
		diagnosis, targetWhere := dragSegmentDiagnostics(curve, start, end)
		return Prescription{
			Criterion: fmt.Sprintf("连续 ≥%d 段 tension < %.0f；tension = 基线 %.0f + 四指标加权 + 结构信号（structure ≥ %.2f 判结构峰）",
				pacingDragMinRun, tensionFlatBelow, tensionBase, structuralPeakAt),
			Current: fmt.Sprintf("seg %d–%d 连续 %d 段 tension < %.0f；%s",
				start, end, run, tensionFlatBelow, diagnosis),
			Target: fmt.Sprintf("连续平坦段 < %d 段（只需把其中一段抬到 tension ≥ %.0f）", pacingDragMinRun, tensionFlatBelow),
			Where:  fmt.Sprintf("%s；优先改 %s", curveRange(curve, start, end), targetWhere),
			Do: fmt.Sprintf(
				"定点只改目标段（不要整章重写）：写一处对抗升级/反转，段落里给足 ≥%d 处具体动作或身体细节（动作密度 ≥ %.2f 次/百字），并把 ≤%d 字短句占比提到 ≥ %.2f；改完重新检测看该段 tension 是否过 %.0f",
				beatActionHitsNeeded(), tensionActionCap,
				tensionShortSentenceMax, tensionShortRatioCap, tensionFlatBelow),
			Avoid: "不要堆直白情绪词或感叹号（与去 AI 味互斥）；不要靠堆武侠冲突词，非武侠题材该词表恒 0，只能靠动作细节 + 短句节奏抬 structure",
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
		// #116：与 openingSlopeFinding 同函数同源（峰值/升幅双通路），并补段位
		// 与可执行数字。旧处方只给「安排对抗升级」这种语感动词，LLM 不知道改
		// 哪一段、差多少，判定器也看不见 → 开篇坡度单条连续 21 轮未消除。
		rise, peak, segs := openingSlopeMetrics(curve)
		where := fmt.Sprintf("前 %d 字（窗口 %d 段）", openingSlopeWindowRunes, len(segs))
		if len(segs) >= 2 {
			where = fmt.Sprintf("%s；目标段 %s", where,
				curveRange(curve, segs[0].Seg, segs[len(segs)-1].Seg))
		}
		return Prescription{
			Criterion: fmt.Sprintf("前 %d 字内峰值 tension ≥ %.0f 或后段比首段升幅 ≥ %.1f（任一达标即不报）",
				openingSlopeWindowRunes, openingPressureMin, openingSlopeMinRise),
			Current: fmt.Sprintf("峰值 %.1f（目标 %.0f）、最高升幅 %.1f（目标 %.1f）；窗口 %d 段",
				peak, openingPressureMin, rise, openingSlopeMinRise, len(segs)),
			Target: fmt.Sprintf("峰值 tension ≥ %.0f 或 升幅 ≥ %.1f", openingPressureMin, openingSlopeMinRise),
			Where:  where,
			Do: fmt.Sprintf(
				"定点只改开篇窗口（不要整章重写）：在 seg 0 之后写一处对抗升级/反转，给足 ≥%d 处具体动作（动作密度 ≥ %.2f 次/百字）或把 ≤%d 字短句占比提到 ≥ %.2f；「第一句出事 / 冷开场」已满足峰值口径，不必再往首段堆冲突词；改完重新检测看峰值/升幅数值是否过线",
				beatActionHitsNeeded(), tensionActionCap, tensionShortSentenceMax, tensionShortRatioCap),
			Avoid: "不要只在首段堆冲突（不能解决全程平坦，且会触发去 AI 味）；不要靠堆直白情绪词或感叹号抬张力",
		}

	case CheckAIFlavor:
		// #120：判定是「全章存在性/密度」而非「单处」，旧处方却让 LLM 以为改掉
		// 一个片段就达标，且 Where 指向当时根本不存在的命中标注（advisory 旧实现
		// Matched/Context 全空）。现与 deslop.go 的逐处命中透出同源，明确清零口径。
		return Prescription{
			Criterion: "全章正文任一处命中 internal/text/deslop.go 的 blocking/advisory 规则即报（同一规则多处命中仍算该规则命中）",
			Current:   f.Desc,
			Target:    "该规则在全章清零（不是只改这一处：别处还有命中，重新检测仍会报）",
			Where:     "发现描述里的「（命中：…）」与「（所在句：…）」逐处定位",
			Do:        "逐处只改命中句：把套话换成具体动作、物件或台词，其余正文原样保留；改完重新检测确认该规则整章清零，不要整章重写",
			Avoid:     "不要用另一句套话替换套话，不要在别处改用同义表达（会重新命中）；不要新增直白情绪词/感叹号",
		}

	case "reader_state", "continuity", "foreshadow", "pacing", "immersion_anchor", "task_card":
		// 判官评分门禁（零锚点路径）专用：输出与锚点改造前逐字一致，历史 golden
		// 不受影响。「去修复」消息走 BuildPrescriptionsAnchored 的定向处方，不经过这里。
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

// l1AnchoredPrescription 是 L1 语义发现的定向处方：位置给到 seg/字区间/原文，
// 动作给到「用 search_replace 只改这一句」，避免项按检查项分流。
//
// 旧模板把 6 类 L1 发现压成同一句「二选一：改正文或更新台账」，位置恒为空，
// 于是「六千七百步折算差」「首句分工措辞」这类纯正文问题也被导向改台账；用户
// 勾 5 条拿到 5 行几乎相同的模板，模型只能凭语感整章重写（第 12 章 9 轮实证）。
// 这里不引入任何新数值、不调用 LLM、不改判定式，只把已存在的原句交出去。
func l1AnchoredPrescription(f Finding, a Anchor) Prescription {
	p := Prescription{
		Criterion: "L1 语义判定：本章正文与计划卡/前章/台账的事实、数字或表述互相印证",
		Current:   f.Desc,
		Target:    "锚点原句被改写后该检查项不再命中；其余正文逐字不动",
		Where:     a.Where(),
	}
	switch f.Check {
	case "continuity":
		p.Criterion = "L1 语义判定：本章正文内部或与前章/台账的数字、称谓、动作主体、分工表述互相矛盾"
		p.Do = "用 search_replace 只把上列原句改成自洽写法（数字/称谓/动作主体与前后文一致）：只改这一句，不补解释段、不动其它段落"
		p.Avoid = "不要去改台账、不要整章 full_replace、不要反复换措辞碰运气；与该句无关的段落一律不动"
	case "task_card":
		p.Criterion = "L1 语义判定：计划卡点名的爽点/动作细节未在正文兑现"
		p.Do = "在锚点所在段定点补写计划卡指定的具体动作或短句节奏（search_replace 该句或其紧邻句），补齐点名细节即可"
		p.Avoid = "不要扩写整章、不要把计划卡条目当成新情节重写、不要堆直白情绪词或感叹号（会触发去 AI 味）"
	case "foreshadow":
		p.Criterion = "L1 语义判定：伏笔/计划卡口径（精简、专属信号、生命周期）与正文落点不一致"
		p.Do = "用 search_replace 只把上列原句对齐计划卡口径（删多余项或补专属信号），保持该段原有功能"
		p.Avoid = "不要为对齐口径扩写整段、不要改动该伏笔在别处的回收点、不要整章 full_replace"
	case "pacing":
		p.Criterion = "L1 语义判定：同一动作/句式在邻近段落重复出现，稀释了唯一性"
		p.Do = "用 search_replace 删掉或改写上列重复原句中的一处（保留首次出现），其余正文原样"
		p.Avoid = "不要整段重写、不要把两处都删掉、不要顺手调整无关节奏"
	default: // reader_state / immersion_anchor
		p.Do = "用 search_replace 只改上列原句，使读者认知/代入感锚点与前后文一致"
		p.Avoid = "不要整章 full_replace、不要改动无关段落、不要靠加解释性旁白绕过"
	}
	return p
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

// dragSegmentDiagnostics 给拖沓段处方的段级实测口径：列出平坦 run 内每段
// tension/structure/动作/短句实测，并在其中挑出「离达标最近」的段（structure
// 最高）作为优先改写目标。LLM 因此看得见差在哪一项、改哪一段最省字，而不是
// 只知道「seg 3–5 太平」然后整章加散点动作。
func dragSegmentDiagnostics(curve []TensionPoint, start, end int) (string, string) {
	var segs []TensionPoint
	for _, pt := range curve {
		if pt.Seg >= start && pt.Seg <= end {
			segs = append(segs, pt)
		}
	}
	if len(segs) == 0 {
		return "曲线为空（重新检测本章后可见段级信号）", fmt.Sprintf("seg %d–%d", start, end)
	}
	best := segs[0]
	parts := make([]string, 0, len(segs))
	for _, pt := range segs {
		parts = append(parts, fmt.Sprintf(
			"seg %d tension %.1f / structure %.2f（动作 %.2f/百字、短句占比 %.2f）",
			pt.Seg, pt.Tension, pt.Metrics[tensionMetricStructural],
			pt.Metrics[tensionMetricAction], pt.Metrics[tensionMetricShortRatio]))
		if pt.Metrics[tensionMetricStructural] > best.Metrics[tensionMetricStructural] {
			best = pt
		}
	}
	where := fmt.Sprintf("seg %d（字 %d–%d，约 %.0f 字）", best.Seg, best.StartRune, best.EndRune,
		best.Metrics[tensionMetricRunes])
	return strings.Join(parts, "；"), where
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
