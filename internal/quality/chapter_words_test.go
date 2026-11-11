package quality

// chapter_words_test.go —— 单章字数口径唯一真相源的契约测试（#113）。
//
// 用户事故：章节体检 L0 说「超出七猫区间 1000–1500 字」，修复后的前后对比
// 又说「2000 字」——检测侧与写作侧各持一套数字。本测试锁死：
//   1) 解析优先级（profile > 平台卡 > 题材卡 > 默认）；
//   2) 检测侧 L0 的 word_count / opening_length / chapter_length_fit 用同一区间；
//   3) 缺省零值逐字保持既有行为（不破坏 P0-0/P0-1 验收）。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveChapterWordsPriority(t *testing.T) {
	const fanqie = "【平台约束·番茄】\n- 单章目标字数 = 1200–2000 字\n"
	const genre = "【创作约束·爽文】\n- 单章正文不少于 3500 字\n"

	// 无配置 → 默认 2000（缺省逐字一致）。
	def := ResolveChapterWords(nil, nil)
	assert.Equal(t, "default", def.Source)
	assert.Equal(t, DefaultTargetWords, def.EffectiveTarget())
	_, _, ok := def.EffectiveLengthWindow()
	assert.False(t, ok, "默认口径不设区间")

	// 平台 profile 优先级最高（即便偏好卡写别的区间）。
	p := ResolveChapterWords(QimaoProfile(), []string{fanqie, genre})
	assert.Equal(t, "platform_profile", p.Source)
	assert.Equal(t, 1000, p.Min)
	assert.Equal(t, 1500, p.Max)
	assert.Equal(t, 1500, p.EffectiveTarget(), "目标取区间上限，不再用 2000")

	// 仅平台卡 → 用卡内区间。
	c := ResolveChapterWords(nil, []string{fanqie, genre})
	assert.Equal(t, "platform_card", c.Source)
	assert.Equal(t, 1200, c.Min)
	assert.Equal(t, 2000, c.Max)

	// 仅题材卡下限 → 目标=下限，不设上限。
	g := ResolveChapterWords(nil, []string{genre})
	assert.Equal(t, "genre_card", g.Source)
	assert.Equal(t, 3500, g.EffectiveTarget())

	// 手写自由文本（无 marker）不得误识别成卡片。
	free := ResolveChapterWords(nil, []string{"用户手写：每章写 2500 字左右"})
	assert.Equal(t, "default", free.Source)
}

func TestChapterWordsShareOneWindowInL0(t *testing.T) {
	// 1562 字：体检说「超出 1000–1500」，同时 word_count 不得再说「目标 2000 字」。
	body := strings.Repeat("文", 1562)
	words := ResolveChapterWords(QimaoProfile(), nil)
	_, findings := RunL0Checks(L0Input{
		Text:          body,
		ReadingNumber: 1,
		ChapterWords:  words,
	})
	var desc strings.Builder
	for _, f := range findings {
		desc.WriteString(f.Desc)
		desc.WriteString("\n")
	}
	assert.NotContains(t, desc.String(), "2000 字", "检测侧不得再出现旧的默认目标")
	assert.Contains(t, desc.String(), "1000–1500", "章长区间来自单源")

	// 偏短文案必须带上口径来源，让修复处方与前后对比能对齐同一数字。
	short := strings.Repeat("文", 500)
	_, shortFindings := RunL0Checks(L0Input{
		Text:          short,
		ReadingNumber: 1,
		ChapterWords:  words,
	})
	var shortDesc strings.Builder
	for _, f := range shortFindings {
		shortDesc.WriteString(f.Desc)
		shortDesc.WriteString("\n")
	}
	assert.Contains(t, shortDesc.String(), "七猫平台预设", "偏短发现标注口径来源")
	assert.Contains(t, shortDesc.String(), "500 字，低于目标 1500 字", "目标取单源上限")
}

func TestChapterWordsZeroValueKeepsLegacy(t *testing.T) {
	// 未注入（零值）时逐字保持既有缺省：目标 2000、区间文案仍为「七猫区间」。
	body := strings.Repeat("文", 1562)
	_, findings := RunL0Checks(L0Input{Text: body, ReadingNumber: 1, Profile: QimaoProfile()})
	var found bool
	for _, f := range findings {
		if f.Check == CheckOpeningLength {
			found = true
			require.Contains(t, f.Desc, "超出七猫区间 1000–1500 字")
		}
	}
	assert.True(t, found, "profile 单独武装黄金三章章长检查（逐字一致）")
}

func TestChapterWordsSystemLineSingleSource(t *testing.T) {
	w := ResolveChapterWords(QimaoProfile(), nil)
	line := w.SystemLine()
	assert.Contains(t, line, "1000–1500")
	assert.NotContains(t, line, "2000")
	assert.Contains(t, line, "检测 L0 / 修复处方 / 前后对比一律以此为准")
}
