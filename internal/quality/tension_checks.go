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
	openingSlopeMinRise     = 5.0 // 开篇坡度最小升幅（窗口首→末段 tension）
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

// openingSlopeFinding 开篇坡度：前 openingSlopeWindowRunes 字内段数 ≥2 且
// 首→末段 tension 升幅 < openingSlopeMinRise → 发现。
func openingSlopeFinding(curve []TensionPoint) *Finding {
	segs := openingSlopeSegs(curve)
	if len(segs) < 2 {
		return nil
	}
	// #114：取窗口内「后续段最高张力 - 首段」，与下文可执行指令方向一致。
	// 旧口径取末段（可能恰好回落）会误报；旧建议「前置异常抬升开篇张力」只抬首段，
	// 反而缩小升幅，与公式方向相反——已改为「首段之后继续加压」。
	rise := segs[1].Tension - segs[0].Tension
	for _, pt := range segs[1:] {
		if pt.Tension-segs[0].Tension > rise {
			rise = pt.Tension - segs[0].Tension
		}
	}
	if rise >= openingSlopeMinRise {
		return nil
	}
	return &Finding{
		Check: CheckOpeningSlope,
		Desc: fmt.Sprintf("开篇坡度不足：前 %d 字 %d 段最高仅上扬 %.1f（<%.1f）——请在首段之后安排对抗升级/反转/关键信息揭露，让后续段 tension 比首段高至少 %.1f；不要只在首段堆冲突（抬高首段反而缩小升幅）",
			openingSlopeWindowRunes, len(segs), rise, openingSlopeMinRise, openingSlopeMinRise),
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
