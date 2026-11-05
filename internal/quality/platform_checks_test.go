package quality

// platform_checks_test.go —— 平台三查（P0-1）：opening_hook / chapter_length_fit /
// beat_density 判定 + 七猫 profile 验收口径（前 500 字爆点/章长/爽点密度三项 L0 可检，
// 不达标出 finding）+ 缺省回归（无 profile/密度下限=零新发现，逐字一致）。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOpeningHookFinding 开篇爆点密度：双窗口下限判定、合并单条发现、未设窗口不查。
func TestOpeningHookFinding(t *testing.T) {
	p := QimaoProfile()

	// 前 200 字 ≥1 且前 500 字 ≥2 → 通过。
	hit := "忽然，一具尸体从井口浮了上来。他没想到昨夜的失踪案竟和自己有关。" + cleanBody(3)
	assert.Nil(t, openingHookFinding(hit, p))

	// 无信号 → 合并单条发现（双窗口都报）。
	f := openingHookFinding(cleanBody(20), p)
	require.NotNil(t, f)
	assert.Equal(t, CheckOpeningHook, f.Check)
	assert.Contains(t, f.Desc, "前 200 字", "合并发现须报前置窗口口径")
	assert.Contains(t, f.Desc, "前 500 字", "合并发现须报主窗口口径")

	// 仅前 200 字达标、前 500 字不足（信号只在开头 1 处）→ 仍报（主窗口下限 2）。
	partial := "忽然，巷口传来一声巨响。" + cleanBody(20)
	f2 := openingHookFinding(partial, p)
	require.NotNil(t, f2, "前 500 字仅 1 处信号 < 下限 2 应报")

	// 窗口/下限未设（<=0）的侧不查；双侧未设 → nil。
	none := &PlatformProfile{Name: "x"}
	assert.Nil(t, openingHookFinding(cleanBody(5), none))
}

// TestChapterLengthFitFinding 章长贴合平台区间：区间外报、区间内不报、未设区间不查。
func TestChapterLengthFitFinding(t *testing.T) {
	p := QimaoProfile()
	assert.Nil(t, chapterLengthFitFinding(1200, p), "区间内不报")
	f := chapterLengthFitFinding(300, p)
	require.NotNil(t, f)
	assert.Equal(t, CheckChapterLengthFit, f.Check)
	assert.Contains(t, f.Desc, "低于")
	f2 := chapterLengthFitFinding(2600, p)
	require.NotNil(t, f2)
	assert.Contains(t, f2.Desc, "超出")
	assert.Nil(t, chapterLengthFitFinding(9999, &PlatformProfile{Title: "x"}), "未设区间不查")
}

// TestBeatDensityFinding 爽点密度：情绪峰段数（张力曲线口径）< 下限报、达标不报。
func TestBeatDensityFinding(t *testing.T) {
	noClimax := []TensionPoint{flatSeg(0), flatSeg(1), flatSeg(2)}
	f := beatDensityFinding(noClimax, 1.0)
	require.NotNil(t, f)
	assert.Equal(t, CheckBeatDensity, f.Check)
	assert.Contains(t, f.Desc, "情绪峰")
	assert.Contains(t, f.Desc, "张力曲线口径")
	withClimax := []TensionPoint{flatSeg(0),
		{Seg: 1, Tension: 80, Emotion: 90, BeatType: BeatTypeClimax}}
	assert.Nil(t, beatDensityFinding(withClimax, 1.0))
	assert.Nil(t, beatDensityFinding(noClimax, 0), "下限 0=不查")
}

// TestPlatformFindingsGating 平台三查武装口径：profile/密度下限未配置=零发现；
// chapter_length_fit 只作用黄金三章外（与 opening_length 零重叠）。
func TestPlatformFindingsGating(t *testing.T) {
	flat := cleanBody(3) // 短平章：三查都不达标
	flatCurve := BuildTensionCurve(flat)

	// 缺省（无 profile、密度 0）→ 零发现（逐字一致）。
	assert.Empty(t, platformFindings(L0Input{Text: flat, ReadingNumber: 5}, 100, flatCurve))

	// profile 武装 opening_hook + chapter_length_fit（第 5 章）。
	p := QimaoProfile()
	fs := platformFindings(L0Input{Text: flat, ReadingNumber: 5, Profile: p}, 100, flatCurve)
	byCheck := findingsByCheck(fs)
	assert.Len(t, byCheck[CheckOpeningHook], 1)
	assert.Len(t, byCheck[CheckChapterLengthFit], 1)

	// 黄金三章内（阅读序号 2）：chapter_length_fit 不跑（opening_length 覆盖）。
	fs2 := platformFindings(L0Input{Text: flat, ReadingNumber: 2, Profile: p}, 100, flatCurve)
	assert.Empty(t, findingsByCheck(fs2)[CheckChapterLengthFit], "黄金三章由 opening_length 覆盖，零重叠")

	// beat_density 由生效密度下限武装（无 profile 也可单独武装），曲线口径情绪峰计数。
	fs3 := platformFindings(L0Input{Text: flat, ReadingNumber: 5, BeatDensityMin: 1.0}, 100, flatCurve)
	assert.Len(t, findingsByCheck(fs3)[CheckBeatDensity], 1)

	// 空正文/序号缺失 → 零发现。
	assert.Empty(t, platformFindings(L0Input{Text: "  ", ReadingNumber: 1, Profile: p, BeatDensityMin: 1}, 100, flatCurve))
	assert.Empty(t, platformFindings(L0Input{Text: flat, ReadingNumber: 0, Profile: p, BeatDensityMin: 1}, 100, flatCurve))
}

// TestQimaoProfileL0Acceptance 验收口径（P0-1）：七猫 profile 下前 500 字爆点/章长/
// 爽点密度三项 L0 可检，不达标出 finding；达标章三项零发现。
// 曲线三查（拖沓段/开篇坡度）不在本验收范围（tension_checks_test.go 覆盖）。
func TestQimaoProfileL0Acceptance(t *testing.T) {
	p := QimaoProfile()
	in := L0Input{Text: cleanBody(2), ReadingNumber: 5, Profile: p, BeatDensityMin: p.BeatDensityMin}
	_, fs := RunL0Checks(in)
	byCheck := findingsByCheck(fs)
	assert.NotEmpty(t, byCheck[CheckOpeningHook], "前 500 字爆点不足须出 finding")
	assert.NotEmpty(t, byCheck[CheckChapterLengthFit], "章长不适配须出 finding")
	assert.NotEmpty(t, byCheck[CheckBeatDensity], "爽点密度不足须出 finding")

	// 达标章（爆点双窗口达标 + 章内含情绪峰 + 章长 1000–1500）三项零发现。
	// 情绪峰构造：末段独立成段（段落累积 400 rune 切段，hook+7×54 与后续 8×54 均触发
	// flush），高唤醒情绪词+感叹密段独立成段 → emotion 满刻度判 climax。
	hookPara := "忽然，一具尸体从井口浮了上来。他没想到失踪案竟和自己有关。"
	burst := "「不可能！！」全场震惊，众人难以置信，纷纷倒吸凉气，瞳孔猛缩！！他大笑：「还有谁！！」"
	good := hookPara + "\n" + cleanBody(23) + burst
	require.NotZero(t, climaxBeatCount(BuildTensionCurve(good)), "good 章须含情绪峰段（beat_density 曲线口径）")
	m, fsGood := RunL0Checks(L0Input{Text: good, ReadingNumber: 5, Profile: p, BeatDensityMin: p.BeatDensityMin})
	require.GreaterOrEqual(t, m.WordCount, 1000)
	require.LessOrEqual(t, m.WordCount, 1500)
	byGood := findingsByCheck(fsGood)
	assert.Empty(t, byGood[CheckOpeningHook])
	assert.Empty(t, byGood[CheckChapterLengthFit])
	assert.Empty(t, byGood[CheckBeatDensity])
}

// TestQimaoProfileArmsOpeningLength profile 单独武装黄金三章硬查（无需关键词，
// P0-1 章长门控「任一关键词已配 || profile!=nil」）。
func TestQimaoProfileArmsOpeningLength(t *testing.T) {
	_, fs := RunL0Checks(L0Input{Text: cleanBody(2), ReadingNumber: 1, Profile: QimaoProfile()})
	byCheck := findingsByCheck(fs)
	assert.NotEmpty(t, byCheck[CheckOpeningLength], "profile 单独武装黄金三章章长查")

	// 缺省（无 profile 无关键词）→ opening_length 不跑（逐字一致）。
	_, fs2 := RunL0Checks(L0Input{Text: cleanBody(2), ReadingNumber: 1})
	assert.Empty(t, findingsByCheck(fs2)[CheckOpeningLength])
}

// TestRunL0ChecksDefaultNoPlatformFindings 缺省回归：无 profile/密度下限时，
// 平台三查恒零发现（缺省=现行行为逐字一致）。
func TestRunL0ChecksDefaultNoPlatformFindings(t *testing.T) {
	bodies := []string{cleanBody(20), "忽然，一具尸体浮了上来。他打脸逆袭全场震惊。", ""}
	for i, body := range bodies {
		_, fs := RunL0Checks(L0Input{Text: body, ReadingNumber: i + 1, TargetWords: 1})
		for _, f := range fs {
			assert.NotContains(t, []string{CheckOpeningHook, CheckChapterLengthFit, CheckBeatDensity}, f.Check,
				"缺省路径不得出平台三查发现（case %d）", i)
		}
	}
}

// TestCountSignalsAndFirstRunes 近似口径工具函数：子串计数求和、前 n rune 截取。
func TestCountSignalsAndFirstRunes(t *testing.T) {
	assert.Equal(t, 3, countSignals("忽然尸体忽然", openingHookSignals))
	assert.Equal(t, 0, countSignals("风平浪静", openingHookSignals))
	assert.Equal(t, "", firstRunes("abc", 0))
	assert.Equal(t, "abc", firstRunes("abc", 10))
	assert.Equal(t, "忽然", firstRunes("忽然，尸体", 2))
	require.True(t, strings.HasPrefix(firstRunes("忽然，尸体", 2), "忽然"))
}
