package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/style"
	"github.com/novelgo/novelgo/internal/voice"
)

// TestVoiceAnchorNovelID 锁死 Voice 台账归属书的推导规则（WBS §0.2 书级锚定）：
// 显式传入优先；否则取选中素材中唯一的非全局素材所属书；无法唯一锚定返回 0（降级）。
func TestVoiceAnchorNovelID(t *testing.T) {
	assert.EqualValues(t, 5, voiceAnchorNovelID(5, nil), "显式传入优先")
	assert.EqualValues(t, 0, voiceAnchorNovelID(0, nil), "无素材无锚点")
	assert.EqualValues(t, 0, voiceAnchorNovelID(0, []style.Sample{{ID: 1}, {ID: 2}}), "全全局素材无锚点")
	assert.EqualValues(t, 3, voiceAnchorNovelID(0, []style.Sample{{ID: 1}, {ID: 2, NovelID: 3}}), "全局+单书 → 该书")
	assert.EqualValues(t, 0, voiceAnchorNovelID(0, []style.Sample{{ID: 1, NovelID: 3}, {ID: 2, NovelID: 4}}), "多书混选无法唯一锚定")
}

// TestUpsertVoiceProfileSource 风格素材双写的落库接线：source=style_sample，
// Stats 确定性 metrics 数值照常落库（WBS §0.6 消费链素材源）。
func TestUpsertVoiceProfileSource(t *testing.T) {
	a := setupTestApp(t)
	items := style.StatsVoiceProfile(
		[]style.Sample{{ID: 11, Content: "他走了。"}, {ID: 22, Content: "风停了！"}},
		style.Stats{AvgSentLen: 8.7, QuoteDensity: 1.5},
	)
	a.upsertVoiceProfileSource(3, voice.SourceStyleSample, items)

	store := voice.NewStore(a.db, a.logger)
	list, err := store.List(t.Context(), 3)
	require.NoError(t, err)
	require.Len(t, list, 3)
	for _, e := range list {
		assert.Equal(t, string(voice.SourceStyleSample), e.Source)
		assert.NotEmpty(t, e.Metrics, "metrics 数值随行落库")
		assert.JSONEq(t, `{"sample_ids":[11,22]}`, e.Evidence, "2 个锚点原样保留")
	}
	fp, err := store.Get(t.Context(), 3, voice.DimSentenceFingerprint)
	require.NoError(t, err)
	require.NotNil(t, fp)
	assert.JSONEq(t, `{"avg_sent_len":8.7,"sent_len_std_dev":0,"short_sent_pct":0}`, fp.Metrics)
}
