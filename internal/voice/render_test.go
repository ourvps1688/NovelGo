package voice

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderLedger_Empty 空台账/无内容行 → 空串（降级：provider 输出与 P0 逐字一致）。
func TestRenderLedger_Empty(t *testing.T) {
	assert.Equal(t, "", RenderLedger(nil))
	assert.Equal(t, "", RenderLedger([]Entry{}))
	// 全部无内容 → 不渲染任何行，块头也不出现。
	assert.Equal(t, "", RenderLedger([]Entry{
		{Dimension: string(DimRhythm), Content: "   "},
		{Dimension: "bogus_dim", Content: ""},
	}))
}

// TestRenderLedger_FixedOrder 输入乱序 → 输出按 §0.1 固定维度顺序。
func TestRenderLedger_FixedOrder(t *testing.T) {
	// 故意乱序（且含 §0.1 尾部维度在前）。
	got := RenderLedger([]Entry{
		{Dimension: string(DimDialogueRatio), Content: "D"},
		{Dimension: string(DimPOV), Content: "P"},
		{Dimension: string(DimSentenceFingerprint), Content: "S"},
		{Dimension: string(DimRhythm), Content: "R"},
	})
	want := strings.Join([]string{
		LedgerHeader,
		"- 句式指纹：S",
		"- 节奏：R",
		"- POV 与视角处理：P",
		"- 对话-叙述比：D",
	}, "\n")
	require.Equal(t, want, got, "渲染顺序须为 §0.1 固定序，与落库顺序无关")
}

// TestRenderLedger_GoldenAllTen 十维全量 golden（§0.1 顺序 + 中文标签单源锁定）。
func TestRenderLedger_GoldenAllTen(t *testing.T) {
	entries := []Entry{
		{Dimension: string(DimHumor), Content: "冷幽默"},
		{Dimension: string(DimTabooWords), Content: "禁词表"},
		{Dimension: string(DimImageryDensity), Content: "意象浓"},
		{Dimension: string(DimSentenceFingerprint), Content: "短句多"},
		{Dimension: string(DimCertaintyTone), Content: "留余地"},
		{Dimension: string(DimHighFreqWords), Content: "高频词表"},
		{Dimension: string(DimTense), Content: "过去时"},
		{Dimension: string(DimRhythm), Content: "先结论"},
		{Dimension: string(DimPOV), Content: "限知视角"},
		{Dimension: string(DimDialogueRatio), Content: "对话多"},
	}
	want := strings.Join([]string{
		LedgerHeader,
		"- 句式指纹：短句多",
		"- 高频词：高频词表",
		"- 禁忌词：禁词表",
		"- 节奏：先结论",
		"- 幽默：冷幽默",
		"- 确定性语气：留余地",
		"- POV 与视角处理：限知视角",
		"- 时态：过去时",
		"- 意象密度：意象浓",
		"- 对话-叙述比：对话多",
	}, "\n")
	require.Equal(t, want, RenderLedger(entries))
}

// TestRenderLedger_BadDataRules 坏数据宁缺勿错 + 不丢行：
// 无内容行跳过；同维度重复取首现；未知维度排最后按输入序；metrics/evidence 不进渲染。
func TestRenderLedger_BadDataRules(t *testing.T) {
	got := RenderLedger([]Entry{
		{Dimension: string(DimRhythm), Content: "首现", Metrics: `{"avg_sent_len":8.7}`, Evidence: `{"chapter_ids":[1,2]}`},
		{Dimension: string(DimRhythm), Content: "后现（应被忽略）"},
		{Dimension: string(DimPOV), Content: "   "}, // 无内容不渲染
		{Dimension: "zz_unknown", Content: "未知乙"},
		{Dimension: "aa_unknown", Content: "未知甲"},
		{Dimension: "zz_unknown", Content: "未知乙重复（应被忽略）"},
	})
	want := strings.Join([]string{
		LedgerHeader,
		"- 节奏：首现",
		"- zz_unknown：未知乙", // 未知维度按输入序（zz 先入参）
		"- aa_unknown：未知甲",
	}, "\n")
	require.Equal(t, want, got)
	assert.NotContains(t, got, "avg_sent_len", "metrics 是溯源元数据，不进提示词")
	assert.NotContains(t, got, "chapter_ids", "evidence 是溯源元数据，不进提示词")
	assert.NotContains(t, got, "后现", "同维度取首现")
}
