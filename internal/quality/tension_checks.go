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
		Desc: fmt.Sprintf("拖沓段：seg %d–%d 连续 %d 段张力平坦（tension<%.0f）——中段平缓易致读者弃书，建议压缩冗余或插入对抗/反转",
			start, end, run, tensionFlatBelow),
	}
}

// beatGapFinding 爽点断档：段数 ≥3 且章内无情绪峰（climax）→ 发现。
// 仅在未配置爽点密度下限时兜底（与 beat_density 配对互斥）。
func beatGapFinding(curve []TensionPoint) *Finding {
	if len(curve) < pacingDragMinRun || climaxBeatCount(curve) > 0 {
		return nil
	}
	return &Finding{
		Check: CheckBeatGap,
		Desc: fmt.Sprintf("爽点断档：全章 %d 段无情绪峰（climax 为 0）——缺少高光/爽点易被判平淡，建议安排至少一处情绪高潮（未配置爽点密度下限时的兜底口径）",
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
	rise := segs[len(segs)-1].Tension - segs[0].Tension
	if rise >= openingSlopeMinRise {
		return nil
	}
	return &Finding{
		Check: CheckOpeningSlope,
		Desc: fmt.Sprintf("开篇坡度不足：前 %d 字 %d 段张力仅上扬 %.1f（<%.1f）——开篇平缓缺抓力，建议前置对抗/异常事件抬升开篇张力",
			openingSlopeWindowRunes, len(segs), rise, openingSlopeMinRise),
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
