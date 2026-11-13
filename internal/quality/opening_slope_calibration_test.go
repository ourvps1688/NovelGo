package quality

// #116 开篇加压真实语料回归锁（勿回退）。
//
// 旧判定只认「后段 tension − 首段 ≥ 5.0」，而平台技能、opening_hook 与
// 常驻规范都要求「第一句出事 / 冷开场 / 前 500 字爆点」= 首段信号最高，
// 方向相反；真实落库 31 章 L0 曲线 25 章（80.6%）不达标、rise 中位数 −1.3，
// 单条 l0|opening_slope|#0 连续 21 轮未消除。且 500 rune 窗口按 400 rune
// 分段在真实语料恒 2 段，#114 把「末段−首段」改成「max−首段」差异 0 章。
//
// 现改为峰值/升幅双通路（openingPressureMin 复用 tensionFlatBelow）：
// ① 冷开场首段高压直接达标；② 首段铺垫后继续加压达标；③ 全程平坦才报。
// 本文件锁死：常量同源、真实语料量级可达、平铺仍报、方向冲突反证、去重。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpeningSlopeCalibrationConstants(t *testing.T) {
	assert.Equal(t, tensionFlatBelow, openingPressureMin,
		"峰值下限必须复用「不平坦」同一口径，禁止另写数字")
	assert.Equal(t, 5.0, openingSlopeMinRise)
	// #114 P3 真实语料重标定数值勿回退。
	assert.Equal(t, 1.0, tensionActionCap)
	assert.Equal(t, 0.4, tensionShortRatioCap)
	assert.Equal(t, 0.45, structuralPeakAt)
}

// realParagraphChapter 把真实非武侠语料按句拆成短段落，复现真实章节
// 「累积 400 rune 切一段」的分段形态（seg1 起点落在 500 rune 窗口内）。
func realParagraphChapter() string {
	body := strings.Repeat(realisticActiveBeat, 4)
	var lines []string
	for _, s := range strings.Split(body, "。") {
		if s = strings.TrimSpace(s); s != "" {
			lines = append(lines, s+"。")
		}
	}
	return strings.Join(lines, "\n")
}

func TestRealCorpusOpeningWindowIsTwoSegmentsAndReachable(t *testing.T) {
	curve := BuildTensionCurve(realParagraphChapter())
	require.GreaterOrEqual(t, len(curve), 3)
	rise, peak, segs := openingSlopeMetrics(curve)
	assert.Len(t, segs, 2, "真实短段落章节的 500 rune 窗口恒 2 段（#114 max 口径在此无数值效果）")
	assert.True(t, openingSlopeOK(rise, peak),
		"真实语料量级必须可达开篇加压（peak=%.1f rise=%.1f），否则写几轮都报", peak, rise)
	assert.Nil(t, openingSlopeFinding(curve))

	// 全程平铺仍须报，不能放水成恒真。
	plain := BuildTensionCurve(plainChapter())
	require.NotNil(t, openingSlopeFinding(plain), "全程平坦开篇仍须报")

	// #114 P3 的三常量不得被本次改动顺手回退。
	assert.Equal(t, 400, tensionSegTargetRunes)
	assert.Equal(t, 500, openingSlopeWindowRunes)
}

func TestColdOpeningNoLongerContradictsDetector(t *testing.T) {
	// 旧口径 rise = 10 − 30 = −20 < 5.0 → 必报，与「第一句出事」相反。
	cold := []TensionPoint{
		{Seg: 0, StartRune: 0, Tension: 30},
		{Seg: 1, StartRune: 400, Tension: 10},
	}
	rise, peak, _ := openingSlopeMetrics(cold)
	assert.Less(t, rise, openingSlopeMinRise, "旧升幅口径会报（回归证据）")
	assert.GreaterOrEqual(t, peak, openingPressureMin, "峰值通路必须认可冷开场")
	assert.True(t, openingSlopeOK(rise, peak))
	assert.Nil(t, openingSlopeFinding(cold))
}

func TestDedupeOpeningFindingsOnlyWhenHookReported(t *testing.T) {
	raw := []Finding{
		{Check: CheckOpeningHook, Desc: "开篇爆点不足"},
		{Check: CheckOpeningSlope, Desc: "开篇加压不足"},
		{Check: CheckTailHook, Desc: "章尾无钩"},
	}
	got := dedupeOpeningFindings(raw)
	require.Len(t, got, 2)
	assert.Equal(t, CheckOpeningHook, got[0].Check)
	assert.Equal(t, CheckTailHook, got[1].Check)

	unarmed := []Finding{{Check: CheckOpeningSlope}, {Check: CheckTailHook}}
	assert.Equal(t, unarmed, dedupeOpeningFindings(unarmed),
		"未命中 opening_hook 时清单逐字不变")
}

func TestWritingConstraintsSharesDetectorConstants(t *testing.T) {
	line := WritingConstraints()
	assert.True(t, strings.HasPrefix(line, "【写前硬约束"))
	for _, want := range []string{
		fmt.Sprintf("%d 字窗口", openingSlopeWindowRunes),
		fmt.Sprintf("%d 字切段", tensionSegTargetRunes),
		fmt.Sprintf("%.0f", openingPressureMin),
		fmt.Sprintf("%.1f", openingSlopeMinRise),
		fmt.Sprintf("%.2f 次/百字", tensionActionCap),
		fmt.Sprintf("%d 字短句", tensionShortSentenceMax),
		fmt.Sprintf("%.2f", tensionShortRatioCap),
		fmt.Sprintf("%.2f", structuralPeakAt),
		fmt.Sprintf("%.0f", tensionEmotionClimaxAt),
		fmt.Sprintf("%d 段", pacingDragMinRun),
		fmt.Sprintf("%d 字内", tailHookWindow),
	} {
		assert.Contains(t, line, want, "写前硬约束必须由检测侧常量投影，不得另写数字：%s", want)
	}
	for _, banned := range []string{
		"StartIssueFixTickets", "autoFix", "RunSubAgent", "UpsertL0", "Dispatch",
	} {
		assert.NotContains(t, line, banned, "写前硬约束不得含任何自动修复/派发语义")
	}
}
