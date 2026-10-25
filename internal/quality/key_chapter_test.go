package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPickKeyChaptersEmpty 零信号=空清单（零提示契约）；goldenN 非正=不挑黄金三章。
func TestPickKeyChaptersEmpty(t *testing.T) {
	assert.Nil(t, PickKeyChapters(nil, 0))
	// 全绿档案（已查零发现）+ 不挑黄金三章 → 无信号。
	rows := []ChapterQuality{
		{ReadingNumber: 4, L0Findings: "[]", L1Findings: "[]"},
		{ReadingNumber: 5, L0Findings: "[]", L1Findings: "[]"},
	}
	assert.Nil(t, PickKeyChapters(rows, 0))
}

// TestPickKeyChaptersGoldenThree 黄金三章恒入选（原因=黄金三章）。
func TestPickKeyChaptersGoldenThree(t *testing.T) {
	got := PickKeyChapters(nil, 3)
	require.Len(t, got, 3)
	assert.Equal(t, []KeyChapter{
		{ReadingNumber: 1, Reasons: []string{keyChapterReasonGoldenThree}},
		{ReadingNumber: 2, Reasons: []string{keyChapterReasonGoldenThree}},
		{ReadingNumber: 3, Reasons: []string{keyChapterReasonGoldenThree}},
	}, got)
}

// TestPickKeyChaptersOpenFindings 未修复发现入选：pending 与空状态都算待修，
// fixed/skipped 不算；L0/L1 任一侧有待修即入选。
func TestPickKeyChaptersOpenFindings(t *testing.T) {
	rows := []ChapterQuality{
		// L0 有待修发现 → 入选。
		{ReadingNumber: 7, L0Findings: `[{"id":"C1","check":"word_count","status":"pending"}]`, L1Findings: "[]"},
		// 空状态=待修（契约：空归入 pending）→ 入选。
		{ReadingNumber: 8, L0Findings: "[]", L1Findings: `[{"id":"L1-1","check":"tail_hook","status":""}]`},
		// 全部已修/已跳过 → 不入选。
		{ReadingNumber: 9, L0Findings: `[{"id":"C1","check":"word_count","status":"fixed"}]`,
			L1Findings: `[{"id":"L1-1","check":"tail_hook","status":"skipped"}]`},
	}
	// goldenN=3：黄金三章与未修复发现章合并、按阅读序排序去重。
	got := PickKeyChapters(rows, 3)
	require.Len(t, got, 5)
	assert.Equal(t, []int{1, 2, 3, 7, 8}, []int{
		got[0].ReadingNumber, got[1].ReadingNumber, got[2].ReadingNumber,
		got[3].ReadingNumber, got[4].ReadingNumber})
	assert.Equal(t, []string{keyChapterReasonGoldenThree}, got[0].Reasons)
	assert.Equal(t, []string{keyChapterReasonOpenFindings}, got[3].Reasons)
	// goldenN=0：只挑未修复发现章。
	only := PickKeyChapters(rows, 0)
	require.Len(t, only, 2)
	assert.Equal(t, 7, only[0].ReadingNumber)
	assert.Equal(t, 8, only[1].ReadingNumber)
}

// TestPickKeyChaptersReasonMerge 同一章双重信号：原因定序合并（未修复发现在前）。
func TestPickKeyChaptersReasonMerge(t *testing.T) {
	rows := []ChapterQuality{
		{ReadingNumber: 2, L0Findings: `[{"id":"C1","check":"word_count","status":"pending"}]`, L1Findings: "[]"},
	}
	got := PickKeyChapters(rows, 3)
	require.Len(t, got, 3)
	// 第 2 章：未修复发现 + 黄金三章，定序合并且去重。
	assert.Equal(t, 2, got[1].ReadingNumber)
	assert.Equal(t, []string{keyChapterReasonOpenFindings, keyChapterReasonGoldenThree}, got[1].Reasons)
}

// TestRenderKeyChapterBlockEmpty 空清单返回空串（零提示：与无该块逐字一致）。
func TestRenderKeyChapterBlockEmpty(t *testing.T) {
	assert.Equal(t, "", RenderKeyChapterBlock(nil))
	assert.Equal(t, "", RenderKeyChapterBlock([]KeyChapter{}))
}

// TestRenderKeyChapterBlockFormat 渲染版式：标题 + 逐章行（原因顿号连接）。
func TestRenderKeyChapterBlockFormat(t *testing.T) {
	got := RenderKeyChapterBlock([]KeyChapter{
		{ReadingNumber: 2, Reasons: []string{keyChapterReasonOpenFindings, keyChapterReasonGoldenThree}},
		{ReadingNumber: 7, Reasons: []string{keyChapterReasonOpenFindings}},
	})
	assert.Contains(t, got, "【关键章定向深读】")
	assert.Contains(t, got, "只负责验证")
	assert.Contains(t, got, "不承担全书发现职责")
	assert.Contains(t, got, "  - 第 2 章（未修复发现、黄金三章）")
	assert.Contains(t, got, "  - 第 7 章（未修复发现）")
}
