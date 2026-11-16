package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 真实第 12 章语料形状：本章原句 + 前章引用（不在本章正文里，必须被自动过滤）。
const anchorTestContent = "开头一句无关。智脑说「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」，陈烬没接话。后面又写「多的两百全在北沿那截绕上」。结尾。"

func TestExtractAnchorPicksQuotePresentInChapter(t *testing.T) {
	desc := `智脑「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」与前章「五公里。路线我重算过，避开第二公里那段坡」的分工表述不一致`
	a, ok := ExtractAnchor(desc, anchorTestContent, nil)
	require.True(t, ok, "必须能从 Desc 里定位到本章原句")
	assert.Equal(t, "比推算多两百步。六千七百步按你的步幅折，是五公里零三十", a.Quote)
	assert.GreaterOrEqual(t, a.StartRune, 0)
	assert.Greater(t, a.EndRune, a.StartRune)
	assert.Equal(t, -1, a.Seg, "curve 为 nil 时不得臆造段号")
	assert.True(t, AnchorPresent(anchorTestContent, a.Quote))
	assert.Contains(t, a.Where(), "原句「比推算多两百步")
	assert.Contains(t, a.Where(), "字 ")
}

func TestExtractAnchorRejectsForeignAndShortQuotes(t *testing.T) {
	_, ok := ExtractAnchor(`前章「台阶一共七级，他数着下」与本章不一致`, anchorTestContent, nil)
	assert.False(t, ok, "前章引用不在本章正文里，绝不能拿来当锚点")

	_, ok = ExtractAnchor(`「五公里」这个数字反复出现`, anchorTestContent, nil)
	assert.False(t, ok, "短于 8 rune 的片段定位价值低，不收")

	_, ok = ExtractAnchor(`没有任何引号的描述`, anchorTestContent, nil)
	assert.False(t, ok)

	_, ok = ExtractAnchor(`「开头一句无关。」`, "", nil)
	assert.False(t, ok, "正文为空时不得臆造位置")
}

func TestLocateAnchorComputesSeg(t *testing.T) {
	curve := []TensionPoint{
		{Seg: 0, StartRune: 0, EndRune: 20},
		{Seg: 1, StartRune: 20, EndRune: 200},
	}
	a, ok := LocateAnchor(anchorTestContent, "比推算多两百步", curve)
	require.True(t, ok)
	assert.Equal(t, 0, a.Seg)
	assert.Equal(t, 11, a.StartRune)

	_, ok = LocateAnchor(anchorTestContent, "这句正文里根本没有", curve)
	assert.False(t, ok)
}

func TestAnchorHashIsStableAndShort(t *testing.T) {
	assert.Empty(t, AnchorHash(""))
	assert.Len(t, AnchorHash("六千七百步"), 8)
	assert.Equal(t, AnchorHash("六千七百步"), AnchorHash("六千七百步"))
	assert.NotEqual(t, AnchorHash("六千七百步"), AnchorHash("六千七百一世"))
}

func TestWithAnchorsOnlyFillsL1AndDoesNotMutateInput(t *testing.T) {
	findings := []Finding{
		{ID: "C1", Check: "word_count", Desc: "「开头一句无关。」字数不足"},
		{ID: "L1-1", Check: "continuity", Desc: "「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」数字自相矛盾"},
		{ID: "L1-2", Check: "continuity", Desc: "纯描述没有任何引号"},
	}
	out := WithAnchors(findings, anchorTestContent, nil)
	assert.Empty(t, out[0].Anchor, "L0 发现本就有段级实测，不打锚点")
	assert.NotEmpty(t, out[1].Anchor)
	assert.Empty(t, out[2].Anchor, "抽不到就如实留空，绝不臆造")
	assert.Empty(t, findings[1].Anchor, "不得改动入参")
	assert.Equal(t, "比推算多两百步。六千七百步按你的步幅折，是五公里零三十", out[1].Anchor)
}
