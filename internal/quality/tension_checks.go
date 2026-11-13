package quality

// tension_checks.go —— 张力曲线三查 + 爽点配对（P0-2 情绪账）。
//
// 基于 BuildTensionCurve 的段级曲线数据（L0 确定性基线口径）的确定性检查：
//   - pacing_drag 拖沓段：连续 ≥N 段 tension 平坦（tensionFlatBelow），报最长一处段序区间；
//   - opening_slope 开篇坡度：前 openingSlopeWindowRunes 字内首末段 tension 升幅不足；
//   - beat 配对（配置互斥对，绝不双报）：BeatDensityMin>0 走 beat_density（曲线口径
//     情绪峰计数，platform_checks.beatDensityFinding）；未配置（<=0）走 beat_gap 兜底
//     （章内无情绪峰）。qimao BeatDensityMin=1.0 校准语义 = 每章 ≥1 情绪峰。
//
// 缺省纪律：曲线为空（空正文/过短正文）= 全部自然跳过零发现。
// 误报以粗粒度复验回落兜底（pipeline.coarseHeuristicChecks 含三查）。

import "fmt"

// 曲线三查阈值（常量口径，实证调参前不动）。
const (
	pacingDragMinRun        = 3   // 拖沓段判定：连续平坦段最少段数
	openingSlopeWindowRunes = 500 // 开篇坡度窗口（rune）
	openingSlopeMinRise     = 5.0 // 开篇坡度最小升幅（窗口首→后续段 tension）
	// #116：开篇窗口张力峰值下限，直接复用「不平坦」同一口径（不在别处另写数字）。
	// 与写作规范同向：「第一句出事 / 冷开场 / 前 500 字爆点」让首段高压，
	// 由峰值通路直接达标；旧口径只认「首段低、后段更高」与之相反，
	// 真实 31 章曲线 25 章（80.6%）不达标、单条发现连续 21 轮未消除。
	openingPressureMin = tensionFlatBelow
)

// tensionFindings 是曲线检查统一入口：拖沓段/爽点断档兜底/开篇坡度。
// beat 配对纪律（配置互斥对，绝不双报）：beatDensityMin>0 时密度下限由
// platform_checks.beatDensityFinding 消费曲线，此处 beat_gap 仅未配置时兜底。
func tensionFindings(curve []TensionPoint, beatDensityMin float64) []Finding {
	if len(curve) == 0 {
		return nil
	}
	var out []Finding
	if f := pacingDragFinding(curve); f != nil {
		out = append(out, *f)
	}
	if beatDensityMin <= 0 {
		if f := beatGapFinding(curve); f != nil {
			out = append(out, *f)
		}
	}
	if f := openingSlopeFinding(curve); f != nil {
		out = append(out, *f)
	}
	return out
}

// pacingDragFinding 拖沓段：连续 ≥pacingDragMinRun 段 tension < tensionFlatBelow
// → 报最长一处段序区间（多处同长取最前）。
func pacingDragFinding(curve []TensionPoint) *Finding {
	start, end, run := longestFlatRun(curve)
	if run < pacingDragMinRun {
		return nil
	}
	return &Finding{
		Check: CheckPacingDrag,
		Desc: fmt.Sprintf("拖沓段：seg %d–%d 连续 %d 段张力平坦（tension<%.0f）——中段平缓易致读者弃书；请在 seg %d–%d 用具体动作细节、对抗升级或短句节奏替换冗余描述，使这些段中至少一段 tension≥%.0f",
			start, end, run, tensionFlatBelow, start, end, tensionFlatBelow),
	}
}

// beatGapFinding 爽点断档：段数 ≥3 且章内无情绪峰（climax）→ 发现。
// 仅在未配置爽点密度下限时兜底（与 beat_density 配对互斥）。
func beatGapFinding(curve []TensionPoint) *Finding {
	if len(curve) < pacingDragMinRun || beatPeakCount(curve) > 0 {
		return nil
	}
	return &Finding{
		Check: CheckBeatGap,
		Desc: fmt.Sprintf("爽点断档：全章 %d 段无情绪峰/结构峰（爽点 0 处）——缺少高光易被判平淡，请安排至少一处对抗升级、反转或情绪高潮；用具体动作与短句节奏呈现即可，不必堆直白情绪词（未配置爽点密度下限时的兜底口径）",
			len(curve)),
	}
}

// openingSlopeMetrics 是开篇窗口的「升幅 / 峰值 / 窗口段」单源：判定、
// 验收处方、图表摘要、写前硬约束都引用同一份口径，杜绝两套数字。
func openingSlopeMetrics(curve []TensionPoint) (rise, peak float64, segs []TensionPoint) {
	segs = openingSlopeSegs(curve)
	if len(segs) == 0 {
		return 0, 0, segs
	}
	peak = segs[0].Tension
	for _, pt := range segs {
		if pt.Tension > peak {
			peak = pt.Tension
		}
		if d := pt.Tension - segs[0].Tension; d > rise {
			rise = d
		}
	}
	return rise, peak, segs
}

// openingSlopeOK 是达标判定单源：峰值通路（冷开场/第一句出事）或
// 升幅通路（首段铺垫、后续继续加压），任一达标即不报。
func openingSlopeOK(rise, peak float64) bool {
	return peak >= openingPressureMin || rise >= openingSlopeMinRise
}

// openingSlopeFinding 开篇加压不足：窗口内段数 ≥2 且「全程平坦（峰值 <
// openingPressureMin）+ 没有继续加压（升幅 < openingSlopeMinRise）」才报。
// #116 与写作规范同向：旧公式只认「后段 − 首段 ≥5.0」，而平台技能与
// opening_hook 都要求首段最炸，写作越合规越被报；且 500 rune 窗口按 400 rune
// 分段在真实语料恒 2 段，#114 把「末段」改「max」在真实语料上差异 0 章。
func openingSlopeFinding(curve []TensionPoint) *Finding {
	rise, peak, segs := openingSlopeMetrics(curve)
	if len(segs) < 2 || openingSlopeOK(rise, peak) {
		return nil
	}
	return &Finding{
		Check: CheckOpeningSlope,
		Desc: fmt.Sprintf("开篇坡度不足：前 %d 字 %d 段峰值 tension %.1f（<%.0f）、最高升幅 %.1f（<%.1f）——开篇窗口全程平坦。请在 seg 0 之后定点写一处对抗升级/反转，给足 ≥%d 处具体动作（动作密度 ≥ %.2f 次/百字）或把 ≤%d 字短句占比提到 ≥ %.2f，或让后段 tension 比首段高 ≥ %.1f；「第一句出事 / 冷开场」已满足峰值口径，不要只在首段堆冲突词（既不能解决全程平坦，又会触发去 AI 味）",
			openingSlopeWindowRunes, len(segs), peak, openingPressureMin, rise, openingSlopeMinRise,
			beatActionHitsNeeded(), tensionActionCap, tensionShortSentenceMax, tensionShortRatioCap,
			openingSlopeMinRise),
	}
}

// climaxBeatCount 情绪峰段数（BeatType==climax，消费侧口径：NormalizeBeatType 归一）。
func climaxBeatCount(curve []TensionPoint) int {
	n := 0
	for _, pt := range curve {
		if NormalizeBeatType(pt.BeatType) == BeatTypeClimax {
			n++
		}
	}
	return n
}

// longestFlatRun 最长连续平坦段（tension < tensionFlatBelow），
// 返回该 run 的首末段 seg 与段数（多处同长取最前；无平坦段返回零值）。
func longestFlatRun(curve []TensionPoint) (int, int, int) {
	bestStart, bestEnd, best := 0, 0, 0
	curStart, curLen := 0, 0
	for i, pt := range curve {
		if pt.Tension >= tensionFlatBelow {
			curLen = 0
			continue
		}
		if curLen == 0 {
			curStart = i
		}
		curLen++
		if curLen > best {
			best, bestStart, bestEnd = curLen, curve[curStart].Seg, pt.Seg
		}
	}
	return bestStart, bestEnd, best
}

// openingSlopeSegs 开篇坡度窗口内段（StartRune < openingSlopeWindowRunes，段序）。
func openingSlopeSegs(curve []TensionPoint) []TensionPoint {
	var out []TensionPoint
	for _, pt := range curve {
		if pt.StartRune < openingSlopeWindowRunes {
			out = append(out, pt)
		}
	}
	return out
}

// TensionMarkers 是张力曲线的图表标记位（前端可视化消费）：与曲线三查判定
// 同源（复用 longestFlatRun/climaxBeatCount 口径/openingSlopeSegs），阈值一致，
// 保证图上标记与工单发现不漂移。仅携带位置语义，不携带文案。
type TensionMarkers struct {
	// DragStartSeg/DragEndSeg 是拖沓段段序区间（仅当命中 pacing_drag 判定
	// （最长平坦 run ≥ pacingDragMinRun）时有效；未命中 = -1/-1）。
	DragStartSeg int `json:"drag_start_seg"`
	DragEndSeg   int `json:"drag_end_seg"`
	// ClimaxSegs 是情绪峰段序（BeatType==climax，NormalizeBeatType 归一口径）。
	ClimaxSegs []int `json:"climax_segs"`
	// OpeningSegs 是开篇坡度窗口内段序（StartRune < openingSlopeWindowRunes）。
	OpeningSegs []int `json:"opening_segs"`
}

// MarkTensionCurve 计算曲线图表标记位（与 tensionFindings 判定同源复用私有助手）。
// 空曲线返回零发现标记（拖沓 -1/-1）。
func MarkTensionCurve(curve []TensionPoint) TensionMarkers {
	m := TensionMarkers{DragStartSeg: -1, DragEndSeg: -1}
	if len(curve) == 0 {
		return m
	}
	if start, end, run := longestFlatRun(curve); run >= pacingDragMinRun {
		m.DragStartSeg, m.DragEndSeg = start, end
	}
	for _, pt := range curve {
		// #114：图表标记与爽点判定同源（climax 或结构峰），避免「图上没标、判定却算」。
		if NormalizeBeatType(pt.BeatType) == BeatTypeClimax ||
			pt.Metrics[tensionMetricStructural] >= structuralPeakAt {
			m.ClimaxSegs = append(m.ClimaxSegs, pt.Seg)
		}
	}
	for _, pt := range openingSlopeSegs(curve) {
		m.OpeningSegs = append(m.OpeningSegs, pt.Seg)
	}
	return m
}

// dedupeOpeningFindings 是 #116 开篇检查去重（纯清单后处理，不改任何判定式）：
// opening_hook 命中时开篇吸引力已有可执行处方，抑制同轮 opening_slope。
// 两者同管开篇，旧链路一个要求首段最炸、一个要求首段低，互相拆台正是
// 「按最佳实践写却反复被报」的结构性来源。未命中时清单逐字不变。
func dedupeOpeningFindings(raw []Finding) []Finding {
	hasHook := false
	for _, f := range raw {
		if f.Check == CheckOpeningHook {
			hasHook = true
			break
		}
	}
	if !hasHook {
		return raw
	}
	out := make([]Finding, 0, len(raw))
	for _, f := range raw {
		if f.Check != CheckOpeningSlope {
			out = append(out, f)
		}
	}
	return out
}
