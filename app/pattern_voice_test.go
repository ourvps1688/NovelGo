package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/voice"
)

// TestUpsertVoiceProfile 双写落库接线（WBS §0.3）：结构化维度落 voice_dimensions
// （source=pattern_mental）；空清单不落库（降级）；坏维度跳过不阻断其余维度。
func TestUpsertVoiceProfile(t *testing.T) {
	a := setupTestApp(t)

	// 空清单：降级不落库。
	a.upsertVoiceProfile(1, nil)
	store := voice.NewStore(a.db, a.logger)
	list, err := store.List(t.Context(), 1)
	require.NoError(t, err)
	assert.Empty(t, list)

	// 正常双写：含 metrics 与 evidence 纪律（锚点 1 个 → 置空标注）。
	a.upsertVoiceProfile(1, []voice.ProfileItem{
		{Dimension: string(voice.DimSentenceFingerprint), Content: "均句长 8.7 字，短句为主",
			Metrics:  map[string]float64{"avg_sent_len": 8.7},
			Evidence: &voice.ProfileEvidence{ChapterIDs: []int64{3, 5}}},
		{Dimension: string(voice.DimDialogueRatio), Content: "对话稀疏",
			Evidence: &voice.ProfileEvidence{ChapterIDs: []int64{3}}}, // <2 → 置空标注
		{Dimension: "not_a_dimension", Content: "坏数据"}, // 非法维度 → 跳过
	})

	list, err = store.List(t.Context(), 1)
	require.NoError(t, err)
	require.Len(t, list, 2, "坏维度跳过，其余照常落库")

	byDim := map[string]voice.Entry{}
	for _, e := range list {
		byDim[e.Dimension] = e
	}
	fp := byDim[string(voice.DimSentenceFingerprint)]
	assert.Equal(t, string(voice.SourcePatternMental), fp.Source)
	assert.JSONEq(t, `{"avg_sent_len":8.7}`, fp.Metrics)
	assert.JSONEq(t, `{"chapter_ids":[3,5]}`, fp.Evidence)

	dr := byDim[string(voice.DimDialogueRatio)]
	assert.JSONEq(t, `{"note":"独立证据 <2，锚点已置空"}`, dr.Evidence)

	// 幂等：同维度再写只留最新。
	a.upsertVoiceProfile(1, []voice.ProfileItem{
		{Dimension: string(voice.DimDialogueRatio), Content: "对话略增"},
	})
	list, err = store.List(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, list, 2)
	got, err := store.Get(t.Context(), 1, voice.DimDialogueRatio)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "对话略增", got.Content)
}
