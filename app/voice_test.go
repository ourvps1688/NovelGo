package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/voice"
)

// TestGetVoiceLedger_Empty 空台账：十维全为占位（Present=false），按 §0.1 顺序。
func TestGetVoiceLedger_Empty(t *testing.T) {
	a := setupTestApp(t)

	items, err := a.GetVoiceLedger(1)
	require.NoError(t, err)
	require.Len(t, items, 10, "占位视图恒为十维")
	for i, d := range voice.Dimensions() {
		assert.Equal(t, string(d), items[i].Dimension, "顺序须与 §0.1 一致")
		assert.False(t, items[i].Present, "未落库维度不造默认值")
		assert.Empty(t, items[i].Content)
	}
}

// TestGetVoiceLedger_Populated 有行：落库维度带完整字段，未落库维度仍占位。
func TestGetVoiceLedger_Populated(t *testing.T) {
	a := setupTestApp(t)
	a.upsertVoiceProfileSource(1, voice.SourcePatternMental, []voice.ProfileItem{
		{Dimension: string(voice.DimRhythm), Content: "先结论后铺垫",
			Metrics: map[string]float64{voice.MetricKeyAvgSentLen: 8.7}},
		{Dimension: string(voice.DimPOV), Content: "第三人称限知"},
	})

	items, err := a.GetVoiceLedger(1)
	require.NoError(t, err)
	require.Len(t, items, 10)

	byDim := map[string]VoiceDimensionView{}
	for _, it := range items {
		byDim[it.Dimension] = it
	}
	rhythm := byDim[string(voice.DimRhythm)]
	assert.True(t, rhythm.Present)
	assert.Equal(t, "先结论后铺垫", rhythm.Content)
	assert.Equal(t, string(voice.SourcePatternMental), rhythm.Source)
	assert.JSONEq(t, `{"avg_sent_len":8.7}`, rhythm.Metrics)
	assert.False(t, rhythm.UpdatedAt.IsZero())

	assert.True(t, byDim[string(voice.DimPOV)].Present)
	assert.False(t, byDim[string(voice.DimHumor)].Present, "未落库维度仍占位")

	// 书隔离。
	other, err := a.GetVoiceLedger(2)
	require.NoError(t, err)
	for _, it := range other {
		assert.False(t, it.Present)
	}
}

// TestUpsertVoiceDimension_ManualOverride 人工覆盖：双写落库行可被 source=manual
// 幂等覆盖校正，同维度不新增行；非法输入拒绝落库。
func TestUpsertVoiceDimension_ManualOverride(t *testing.T) {
	a := setupTestApp(t)
	a.upsertVoiceProfileSource(1, voice.SourceStyleSample, []voice.ProfileItem{
		{Dimension: string(voice.DimRhythm), Content: "机器初稿"},
	})

	got, err := a.UpsertVoiceDimension(UpsertVoiceDimensionInput{
		NovelID:   1,
		Dimension: string(voice.DimRhythm),
		Content:   "人工校正后的节奏结论",
		Metrics:   `{"avg_sent_len": 9.1}`,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, string(voice.SourceManual), got.Source, "人工校正标记 manual")
	assert.Equal(t, "人工校正后的节奏结论", got.Content)
	assert.JSONEq(t, `{"avg_sent_len":9.1}`, got.Metrics)

	store := voice.NewStore(a.db, a.logger)
	list, err := store.List(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, list, 1, "同维度覆盖不新增行")

	// 非法维度 / 非法 JSON / 缺 novel 拒绝。
	_, err = a.UpsertVoiceDimension(UpsertVoiceDimensionInput{NovelID: 1, Dimension: "nope", Content: "x"})
	assert.Error(t, err)
	_, err = a.UpsertVoiceDimension(UpsertVoiceDimensionInput{
		NovelID: 1, Dimension: string(voice.DimRhythm), Content: "x", Metrics: `{bad json`,
	})
	assert.Error(t, err)
	_, err = a.UpsertVoiceDimension(UpsertVoiceDimensionInput{Dimension: string(voice.DimRhythm), Content: "x"})
	assert.Error(t, err)

	list, err = store.List(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, list, 1, "非法输入不得落库")
	assert.Equal(t, "人工校正后的节奏结论", list[0].Content)
}
