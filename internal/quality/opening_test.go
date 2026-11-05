package quality

// opening_test.go —— 开篇体检专项（P0-0）：位次三查/章长区间的确定性判定
// （七猫量化口径：主角前 200 字/看点前 500 字/金手指前 800 字/黄金三章 1000–1500 字）。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// openingIn 构造开篇体检输入（wordCount 由调用方显式给，绕开 ComputeStats 口径）。
func openingIn(reading int, body string, opening *L0Opening) L0Input {
	return L0Input{Text: body, ReadingNumber: reading, Opening: opening}
}

// TestOpeningFindingsDisabledByDefault 未配置=整专项不跑（缺省=现行行为逐字一致）。
func TestOpeningFindingsDisabledByDefault(t *testing.T) {
	body := strings.Repeat("旁观。", 600)
	assert.Empty(t, openingFindings(openingIn(1, body, nil), 2400))
	assert.Empty(t, openingFindings(openingIn(1, body, &L0Opening{}), 2400),
		"三组关键词全空=不检查")
	assert.Empty(t, openingFindings(openingIn(0, body, &L0Opening{PremiseKeywords: []string{"灾星"}}), 2400),
		"阅读序号缺失（0）不跑（定位不了开篇）")
	assert.Empty(t, openingFindings(openingIn(1, "", &L0Opening{PremiseKeywords: []string{"灾星"}}), 0),
		"空正文不跑（空章另由 empty_chapter 报）")
}

// TestOpeningPositionAllInWindow 全部在窗内=零发现（含边界：字位=窗口值-1 通过）。
func TestOpeningPositionAllInWindow(t *testing.T) {
	// 字位：主角 0、看点 499（第 500 字，窗内）、金手指 700。
	body := "林澈" + strings.Repeat("。", 497) + "灾星" + strings.Repeat("。", 199) + "吞星炼命" + strings.Repeat("。", 200)
	in := openingIn(1, body, &L0Opening{
		PremiseKeywords:  []string{"灾星"},
		CheatKeywords:    []string{"吞星炼命"},
		ProtagonistNames: []string{"林澈"},
	})
	assert.Empty(t, openingFindings(in, 1300))
}

// TestOpeningPositionLateAndMissing 迟到/缺失两种违例形态。
func TestOpeningPositionLateAndMissing(t *testing.T) {
	// 看点字位 500（第 501 字=迟到边界）、金手指缺失、主角字位 300。
	body := strings.Repeat("。", 500) + "灾星" + strings.Repeat("。", 1000)
	in := openingIn(1, body, &L0Opening{
		PremiseKeywords:  []string{"灾星", "深地"},
		CheatKeywords:    []string{"吞星炼命"},
		ProtagonistNames: []string{"林澈"},
	})
	by := map[string][]Finding{}
	for _, f := range openingFindings(in, 1600) {
		by[f.Check] = append(by[f.Check], f)
	}
	assert.Len(t, by[CheckPremiseLate], 1)
	assert.Contains(t, by[CheckPremiseLate][0].Desc, "「灾星」首现于约第 501 字", "报最早命中词与字位")
	assert.Len(t, by[CheckCheatLate], 1)
	assert.Contains(t, by[CheckCheatLate][0].Desc, "未见金手指/核心机制关键词", "整表未命中报缺失")
	assert.Len(t, by[CheckProtagonistLate], 1)
	assert.Contains(t, by[CheckProtagonistLate][0].Desc, "未见主角关键词")

	// 主角迟到（字位 200=第 201 字）：另一形态。
	body2 := strings.Repeat("。", 200) + "林澈来了"
	by2 := map[string]bool{}
	for _, f := range openingFindings(openingIn(1, body2, &L0Opening{ProtagonistNames: []string{"林澈"}}), 2100) {
		by2[f.Check] = true
	}
	assert.True(t, by2[CheckProtagonistLate])
	assert.False(t, by2[CheckPremiseLate], "未配置看点词不报看点")
	assert.False(t, by2[CheckCheatLate])
}

// TestOpeningPositionOnlyChapter1 位次三查只作用于第 1 章（第 2 章起迟到不算违例）。
func TestOpeningPositionOnlyChapter1(t *testing.T) {
	body := strings.Repeat("。", 2000) + "灾星吞星炼命林澈"
	in := openingIn(2, body, &L0Opening{
		PremiseKeywords:  []string{"灾星"},
		CheatKeywords:    []string{"吞星炼命"},
		ProtagonistNames: []string{"林澈"},
	})
	for _, f := range openingFindings(in, 2020) {
		assert.NotEqual(t, CheckPremiseLate, f.Check)
		assert.NotEqual(t, CheckCheatLate, f.Check)
		assert.NotEqual(t, CheckProtagonistLate, f.Check)
	}
}

// TestOpeningLengthWindow 黄金三章章长区间：1..3 章查、边界 1000/1500、第 4 章不查。
func TestOpeningLengthWindow(t *testing.T) {
	opening := &L0Opening{PremiseKeywords: []string{"灾星"}}
	assert.Len(t, openingFindings(openingIn(1, "灾星", opening), 999), 1)
	assert.Empty(t, openingFindings(openingIn(1, "灾星", opening), 1000))
	assert.Empty(t, openingFindings(openingIn(3, "灾星", opening), 1500))
	long := openingFindings(openingIn(3, "灾星", opening), 1501)
	assert.Len(t, long, 1)
	assert.Contains(t, long[0].Desc, "超出七猫区间 1000–1500 字")
	assert.Empty(t, openingFindings(openingIn(4, "灾星", opening), 900), "第 4 章起不查章长区间")
}

// TestRunL0ChecksOpeningIntegration 开篇发现经 RunL0Checks 汇入 C 系（编号/级别/作用域）。
func TestRunL0ChecksOpeningIntegration(t *testing.T) {
	body := strings.Repeat("。", 600) + "灾星升起"
	_, findings := RunL0Checks(L0Input{
		Text:          body,
		ReadingNumber: 1,
		Opening:       &L0Opening{PremiseKeywords: []string{"灾星"}},
	})
	by := findingsByCheck(findings)
	assert.Len(t, by[CheckPremiseLate], 1)
	got := by[CheckPremiseLate][0]
	assert.True(t, strings.HasPrefix(got.ID, "C"), "发现走 C 系顺编：%s", got.ID)
	assert.Equal(t, FindingLevelDefault, got.Level)
	assert.Equal(t, FindingStatusPending, got.Status)
	assert.Equal(t, []int64{1}, got.ChapterIDs)
}
