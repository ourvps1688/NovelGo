package app

// P1 T5.1 三联验收（P1 验收标准本体）：端到端一测三联——
// 可查询（GetVoiceLedger 返回落库行）→ 可 diff（两版本快照 diff 出 voice 行变更）
// → 进快照（freeze payload 含 voice_dimensions 行）。

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
)

// TestVoiceLedgerThreeWayAcceptance 三联断言：查询 API、快照 diff、freeze payload
// 全部覆盖 voice 台账——台账落库后可查、可 diff、进快照。
func TestVoiceLedgerThreeWayAcceptance(t *testing.T) {
	a := setupTestApp(t)
	novel := createTestNovel(t, a)

	// 落库两维（人工校正入口）。
	_, err := a.UpsertVoiceDimension(UpsertVoiceDimensionInput{
		NovelID: novel.ID, Dimension: string(voice.DimRhythm), Content: "先结论后铺垫",
	})
	require.NoError(t, err)
	_, err = a.UpsertVoiceDimension(UpsertVoiceDimensionInput{
		NovelID: novel.ID, Dimension: string(voice.DimSentenceFingerprint), Content: "短句为主",
	})
	require.NoError(t, err)

	// ① 可查询：GetVoiceLedger 返回落库行（十维视图，已落库 Present=true）。
	items, err := a.GetVoiceLedger(novel.ID)
	require.NoError(t, err)
	require.Len(t, items, 10)
	byDim := map[string]VoiceDimensionView{}
	for _, it := range items {
		byDim[it.Dimension] = it
	}
	rhythm := byDim[string(voice.DimRhythm)]
	assert.True(t, rhythm.Present, "可查询：落库维度须可查")
	assert.Equal(t, "先结论后铺垫", rhythm.Content)
	assert.Equal(t, string(voice.SourceManual), rhythm.Source)
	assert.True(t, byDim[string(voice.DimSentenceFingerprint)].Present)
	assert.False(t, byDim[string(voice.DimHumor)].Present, "未落库维度不造默认值")

	// ② 进快照：freeze payload 含 voice_dimensions 行。
	name := "第一卷"
	v, err := a.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &name})
	require.NoError(t, err)
	frozen, err := a.SetVolumeFrozen(novel.ID, v.ID, true)
	require.NoError(t, err)
	require.True(t, frozen.Frozen)

	var snap1 volume.VolumeSnapshot
	require.NoError(t, a.db.Where("volume_id = ? AND snapshot_version = 1", v.ID).First(&snap1).Error)
	var p1 struct {
		Tables map[string]json.RawMessage `json:"tables"`
	}
	require.NoError(t, json.Unmarshal([]byte(snap1.Payload), &p1))
	raw1, ok := p1.Tables["voice_dimensions"]
	require.True(t, ok, "进快照：freeze payload 须含 voice_dimensions 表")
	var rows1 []map[string]any
	require.NoError(t, json.Unmarshal(raw1, &rows1))
	assert.Len(t, rows1, 2, "进快照：落库两行随冻结入快照")

	// ③ 可 diff：改节奏 + 增禁忌词 → v2 相对 v1 diff 出 voice 行变更。
	_, err = a.UpsertVoiceDimension(UpsertVoiceDimensionInput{
		NovelID: novel.ID, Dimension: string(voice.DimRhythm), Content: "先铺垫后结论",
	})
	require.NoError(t, err)
	_, err = a.UpsertVoiceDimension(UpsertVoiceDimensionInput{
		NovelID: novel.ID, Dimension: string(voice.DimTabooWords), Content: "禁用突然/忽然",
	})
	require.NoError(t, err)

	snap2, err := a.snapshots.Commit(a.ctx, novel.ID, v.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, snap2.SnapshotVersion)

	d, err := a.snapshots.DiffSnapshots(a.ctx, snap1.ID, snap2.ID)
	require.NoError(t, err)
	require.Len(t, d.Tables, 1, "本流程只有 voice 台账变更")
	vd := d.Tables[0]
	assert.Equal(t, "voice_dimensions", vd.Table)
	require.Len(t, vd.Changed, 1, "可 diff：节奏内容变更")
	cd, ok := vd.Changed[0].Fields["content"]
	assert.True(t, ok)
	assert.Equal(t, "先结论后铺垫", cd.Old)
	assert.Equal(t, "先铺垫后结论", cd.New)
	require.Len(t, vd.Added, 1, "可 diff：禁忌词新增")
	assert.Equal(t, string(voice.DimTabooWords), vd.Added[0]["dimension"])
	assert.Empty(t, vd.Removed)
}
