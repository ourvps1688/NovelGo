package quality

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderLedgerBlockEmpty 空清单返回空串（无档案=不注入，降级零提示）。
func TestRenderLedgerBlockEmpty(t *testing.T) {
	assert.Equal(t, "", RenderLedgerBlock(nil))
	assert.Equal(t, "", RenderLedgerBlock([]ChapterQuality{}))
}

// TestRenderLedgerBlockOrderAndCounts 阅读序号排序 + 汇总计数 + 发现逐条投影。
func TestRenderLedgerBlockOrderAndCounts(t *testing.T) {
	rows := []ChapterQuality{
		{
			NovelID: 1, ChapterID: 21, ReadingNumber: 2,
			L0Metrics:  MarshalL0Metrics(L0Metrics{WordCount: 1800, HasTailHook: false}),
			L0Findings: MarshalFindings([]Finding{{ID: "C1", Check: CheckWordCount, Level: FindingLevelDefault, Desc: "字数不足", Status: FindingStatusPending}}),
			L1Findings: MarshalFindings([]Finding{{ID: "L1-1", Check: L1CheckContinuity, Level: FindingLevelDefault, Desc: "衔接跳跃", Status: FindingStatusFixed}}),
			L1Status:   L1StatusChecked,
		},
		{
			NovelID: 1, ChapterID: 11, ReadingNumber: 1,
			L0Metrics:  MarshalL0Metrics(L0Metrics{WordCount: 2100, HasTailHook: true}),
			L0Findings: MarshalFindings(nil),
			L1Findings: "",
			L1Status:   L1StatusPending,
		},
	}
	got := RenderLedgerBlock(rows)

	// 汇总行：2 章、L1 已查 1、L0 发现 1、L1 发现 1、未修复 1（L1-1 已修不计）。
	require.Contains(t, got, "汇总：档案覆盖 2 章｜L1 已查 1 章｜L0 发现 1 项｜L1 发现 1 项｜未修复 1 项")
	// 阅读序号排序：#1 行在 #2 行前。
	require.Less(t, strings.Index(got, "- #1｜"), strings.Index(got, "- #2｜"))
	// 指标与状态投影。
	require.Contains(t, got, "- #1｜2100 字｜章尾钩子:有｜L0发现:0 项｜L1:待查(0 项)")
	require.Contains(t, got, "- #2｜1800 字｜章尾钩子:无｜L0发现:1 项｜L1:已查(1 项)")
	// 发现逐条投影（含检查项与修复状态）。
	require.Contains(t, got, "  - C1[word_count][pending] 字数不足")
	require.Contains(t, got, "  - L1-1[continuity][fixed] 衔接跳跃")
}

// TestRenderLedgerBlockUncheckedSide 空串=未查（区别于 "[]"=已查零发现）。
func TestRenderLedgerBlockUncheckedSide(t *testing.T) {
	got := RenderLedgerBlock([]ChapterQuality{{
		NovelID: 1, ChapterID: 3, ReadingNumber: 1,
		L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 100}),
		L1Status:  L1StatusSkipped,
	}})
	require.Contains(t, got, "L0发现:未查", "L0Findings 空串应渲染为未查")
	require.Contains(t, got, "L1:跳过(0 项)")
}

// TestRenderLedgerBlockDeterministic 输入乱序/文风指标 map 乱序 → 输出逐字一致（确定性）。
func TestRenderLedgerBlockDeterministic(t *testing.T) {
	row := ChapterQuality{NovelID: 1, ChapterID: 5, ReadingNumber: 3,
		L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 2000, HasTailHook: true,
			StyleMetrics: map[string]float64{"quote_density": 3.2, "avg_sent_len": 12.5}}),
		L0Findings: MarshalFindings(nil), L1Findings: MarshalFindings(nil), L1Status: L1StatusChecked}
	other := ChapterQuality{NovelID: 1, ChapterID: 4, ReadingNumber: 2,
		L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 1500}), L0Findings: MarshalFindings(nil),
		L1Status: L1StatusSkipped}

	a := RenderLedgerBlock([]ChapterQuality{row, other})
	b := RenderLedgerBlock([]ChapterQuality{other, row})
	require.Equal(t, a, b, "输入乱序输出应逐字一致")

	// 文风指标按键名排序：键序无关。
	require.Contains(t, a, "avg_sent_len=12.50, quote_density=3.20")
	require.NotContains(t, a, "quote_density=3.20, avg_sent_len")
}

// TestRenderLedgerBlockFindingDescOneLine 多行描述压成单行（块内每条目一行，不破版式）。
func TestRenderLedgerBlockFindingDescOneLine(t *testing.T) {
	got := RenderLedgerBlock([]ChapterQuality{{
		NovelID: 1, ChapterID: 9, ReadingNumber: 1,
		L0Findings: MarshalFindings([]Finding{{ID: "C1", Check: CheckAIFlavor,
			Level: FindingLevelDefault, Desc: "第一行\n第二行\t第三行", Status: FindingStatusPending}}),
		L1Status: L1StatusPending,
	}})
	require.Contains(t, got, "  - C1[ai_flavor][pending] 第一行 第二行 第三行")
}
