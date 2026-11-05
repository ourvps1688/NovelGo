package quality

// tension_checks_test.go —— 张力曲线三查（P0-2 情绪账）：拖沓段/爽点断档/开篇坡度
// + beat 配对互斥纪律（beat_density↔beat_gap 绝不双报）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flatSeg 平坦段（tension<25）构造器。
func flatSeg(seg int) TensionPoint {
	return TensionPoint{Seg: seg, StartRune: seg * 400, EndRune: seg*400 + 390, Tension: 10, Emotion: 8, BeatType: BeatTypeFlat}
}

// hotSeg 高张力段构造器。
func hotSeg(seg int) TensionPoint {
	return TensionPoint{Seg: seg, StartRune: seg * 400, EndRune: seg*400 + 390, Tension: 60, Emotion: 40, BeatType: BeatTypeConflict}
}

// TestPacingDragFinding 拖沓段：连续 ≥3 段平坦报最长一处区间、不足 3 段不报、
// 多处同长取最前。
func TestPacingDragFinding(t *testing.T) {
	// 连续 3 段平坦 → 报 seg 0–2。
	f := pacingDragFinding([]TensionPoint{flatSeg(0), flatSeg(1), flatSeg(2), hotSeg(3)})
	require.NotNil(t, f)
	assert.Equal(t, CheckPacingDrag, f.Check)
	assert.Contains(t, f.Desc, "seg 0–2")
	assert.Contains(t, f.Desc, "3 段")

	// 连续 2 段平坦 → 不报。
	assert.Nil(t, pacingDragFinding([]TensionPoint{flatSeg(0), flatSeg(1), hotSeg(2)}))

	// 两处 run（3 与 4 段）→ 报最长的 4 段（seg 3–6）。
	curve := []TensionPoint{flatSeg(0), flatSeg(1), flatSeg(2), hotSeg(3),
		flatSeg(4), flatSeg(5), flatSeg(6), flatSeg(7), hotSeg(8)}
	f2 := pacingDragFinding(curve)
	require.NotNil(t, f2)
	assert.Contains(t, f2.Desc, "seg 4–7")

	// 全程高张力 → 不报。
	assert.Nil(t, pacingDragFinding([]TensionPoint{hotSeg(0), hotSeg(1), hotSeg(2)}))
}

// TestBeatGapFinding 爽点断档：≥3 段无 climax 报、有 climax 不报、<3 段不报。
func TestBeatGapFinding(t *testing.T) {
	noClimax := []TensionPoint{hotSeg(0), hotSeg(1), hotSeg(2)}
	f := beatGapFinding(noClimax)
	require.NotNil(t, f)
	assert.Equal(t, CheckBeatGap, f.Check)
	assert.Contains(t, f.Desc, "无情绪峰")

	withClimax := []TensionPoint{hotSeg(0), hotSeg(1),
		{Seg: 2, Tension: 80, Emotion: 90, BeatType: BeatTypeClimax}}
	assert.Nil(t, beatGapFinding(withClimax))
	assert.Nil(t, beatGapFinding([]TensionPoint{hotSeg(0), hotSeg(1)}), "<3 段不报")
}

// TestOpeningSlopeFinding 开篇坡度：窗口内首末段升幅 <5 报、≥5 不报、
// <2 窗口段不报、窗口外段不参与。
func TestOpeningSlopeFinding(t *testing.T) {
	// 窗口内 3 段、升幅 0 → 报。
	flat := []TensionPoint{flatSeg(0), flatSeg(1), flatSeg(2), hotSeg(3)}
	f := openingSlopeFinding(flat)
	require.NotNil(t, f)
	assert.Equal(t, CheckOpeningSlope, f.Check)
	assert.Contains(t, f.Desc, "开篇坡度不足")

	// 首段 10 → 窗口末段 60，升幅 50 → 不报。
	rise := []TensionPoint{flatSeg(0), hotSeg(1), hotSeg(2)}
	assert.Nil(t, openingSlopeFinding(rise))

	// 窗口内仅 1 段（其余段 StartRune ≥500）→ 不报。
	only := []TensionPoint{
		{Seg: 0, StartRune: 0, EndRune: 450, Tension: 10},
		{Seg: 1, StartRune: 460, EndRune: 900, Tension: 80},
	}
	assert.Nil(t, openingSlopeFinding(only), "<2 窗口段跳过")
}

// TestTensionFindingsBeatPair 互斥对纪律：BeatDensityMin>0 时 beat_gap 绝不出现
// （beat_density 由 platform_checks 消费曲线）；未配置时 beat_gap 兜底。
func TestTensionFindingsBeatPair(t *testing.T) {
	noClimax := []TensionPoint{hotSeg(0), hotSeg(1), hotSeg(2)}

	// min>0：不出 beat_gap（beat_density 走 platformFindings）。
	fs := tensionFindings(noClimax, 1.0)
	assert.Empty(t, findingsByCheck(fs)[CheckBeatGap], "配置密度下限时 beat_gap 不得出现（互斥）")

	// min<=0：beat_gap 兜底。
	fs2 := tensionFindings(noClimax, 0)
	assert.Len(t, findingsByCheck(fs2)[CheckBeatGap], 1)

	// 空曲线：全部自然跳过。
	assert.Empty(t, tensionFindings(nil, 1.0))
	assert.Empty(t, tensionFindings(nil, 0))
}

// TestTensionFindingsOrder 三查在场性与顺序：拖沓段→爽点→开篇坡度（稳定序）。
func TestTensionFindingsOrder(t *testing.T) {
	// 平坦 3 段（无 climax、升幅不足）→ 三项齐报且顺序稳定。
	curve := []TensionPoint{flatSeg(0), flatSeg(1), flatSeg(2)}
	fs := tensionFindings(curve, 0)
	var checks []string
	for _, f := range fs {
		checks = append(checks, f.Check)
	}
	assert.Equal(t, []string{CheckPacingDrag, CheckBeatGap, CheckOpeningSlope}, checks)
}

// TestClimaxBeatCount 情绪峰计数：NormalizeBeatType 口径（L1 自由值归一后计数）。
func TestClimaxBeatCount(t *testing.T) {
	curve := []TensionPoint{
		{Seg: 0, BeatType: BeatTypeClimax},
		{Seg: 1, BeatType: "  climax "}, // L1 自由值 trim 后合法
		{Seg: 2, BeatType: "high"},      // 未知值 → transition
		{Seg: 3},
	}
	assert.Equal(t, 2, climaxBeatCount(curve))
	assert.Equal(t, 0, climaxBeatCount(nil))
}

// TestLongestFlatRun 最长平坦 run：同长取最前、无平坦段返回零值。
func TestLongestFlatRun(t *testing.T) {
	// run 平：3 段（0-2）与 3 段（5-7）→ 取最前。
	curve := []TensionPoint{flatSeg(0), flatSeg(1), flatSeg(2), hotSeg(3), hotSeg(4),
		flatSeg(5), flatSeg(6), flatSeg(7)}
	s, e, n := longestFlatRun(curve)
	assert.Equal(t, 0, s)
	assert.Equal(t, 2, e)
	assert.Equal(t, 3, n)

	_, _, n2 := longestFlatRun([]TensionPoint{hotSeg(0), hotSeg(1)})
	assert.Equal(t, 0, n2)
}
