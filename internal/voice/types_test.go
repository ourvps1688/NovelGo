package voice

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDimensionEnumMatchesWBS01 锁死 §0.1 十维枚举值与顺序：
// 枚举值是 diff/快照的行键语义，禁改（逐字一致断言）。
func TestDimensionEnumMatchesWBS01(t *testing.T) {
	want := []string{
		"sentence_fingerprint",
		"high_freq_words",
		"taboo_words",
		"rhythm",
		"humor",
		"certainty_tone",
		"pov",
		"tense",
		"imagery_density",
		"dialogue_ratio",
	}
	got := Dimensions()
	require.Len(t, got, len(want))
	seen := map[Dimension]bool{}
	for i, dim := range got {
		assert.Equal(t, want[i], string(dim), "维度 %d 枚举值/顺序须与 §0.1 逐字一致", i)
		assert.False(t, seen[dim], "维度重复: %s", dim)
		seen[dim] = true
		assert.True(t, dim.Valid())
	}
	assert.False(t, Dimension("style_six_dim").Valid(), "旧 style 六维不作为枚举")
	assert.False(t, Dimension("").Valid())
}

func TestSourceEnum(t *testing.T) {
	assert.Equal(t, "style_sample", string(SourceStyleSample))
	assert.Equal(t, "pattern_mental", string(SourcePatternMental))
	assert.Equal(t, "manual", string(SourceManual))
	assert.True(t, SourceStyleSample.Valid())
	assert.True(t, SourcePatternMental.Valid())
	assert.True(t, SourceManual.Valid())
	assert.False(t, Source("other").Valid())
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Entry{}))
	return db
}

// TestEntrySchema 建表 + 唯一索引 + 契约列约束（不加 changed_at）。
func TestEntrySchema(t *testing.T) {
	db := newTestDB(t)

	require.True(t, db.Migrator().HasTable("voice_dimensions"))
	require.True(t, db.Migrator().HasIndex(&Entry{}, "uk_voice_dimension"))
	// 契约：不加 changed_at（风格修改不走 temporal/history 履历）。
	require.False(t, db.Migrator().HasColumn(&Entry{}, "changed_at"))

	// (novel_id, dimension) 唯一：同键第二条写入必须失败。
	first := Entry{NovelID: 1, Dimension: string(DimRhythm), Content: "v1", Source: string(SourceManual)}
	require.NoError(t, db.Create(&first).Error)
	dup := Entry{NovelID: 1, Dimension: string(DimRhythm), Content: "v2", Source: string(SourceManual)}
	require.Error(t, db.Create(&dup).Error)

	// 不同书同维度、同书不同维度均可写。
	require.NoError(t, db.Create(&Entry{NovelID: 2, Dimension: string(DimRhythm), Content: "x", Source: string(SourceStyleSample)}).Error)
	require.NoError(t, db.Create(&Entry{NovelID: 1, Dimension: string(DimPOV), Content: "y", Source: string(SourcePatternMental)}).Error)

	var count int64
	require.NoError(t, db.Model(&Entry{}).Count(&count).Error)
	assert.EqualValues(t, 3, count)
}

// TestProfileItemJSON 锁死双写契约的序列化纪律（WBS §0.3）：
// metrics 可空；evidence「独立证据 <2 降级」——锚点不足置空并标注，不阻断。
func TestProfileItemJSON(t *testing.T) {
	// metrics：有则 JSON，无则空串。
	assert.Empty(t, ProfileItem{Dimension: string(DimRhythm), Content: "x"}.MetricsJSON())
	assert.JSONEq(t, `{"avg_sent_len":8.7}`,
		ProfileItem{Metrics: map[string]float64{"avg_sent_len": 8.7}}.MetricsJSON())

	// 无 evidence → 空串。
	assert.Empty(t, ProfileItem{Dimension: string(DimRhythm), Content: "x"}.EvidenceJSON())

	// 锚点 ≥2 → 原样序列化。
	got := ProfileItem{Evidence: &ProfileEvidence{ChapterIDs: []int64{3, 5}}}.EvidenceJSON()
	assert.JSONEq(t, `{"chapter_ids":[3,5]}`, got)

	// 锚点恰 1 → 置空并标注（维度照常落库，不阻断）。
	got = ProfileItem{Evidence: &ProfileEvidence{ChapterIDs: []int64{3}}}.EvidenceJSON()
	assert.JSONEq(t, `{"note":"独立证据 <2，锚点已置空"}`, got)

	// 既有 note 时保留并追加标注。
	got = ProfileItem{Evidence: &ProfileEvidence{SampleIDs: []int64{7}, Note: "来自素材A"}}.EvidenceJSON()
	assert.JSONEq(t, `{"note":"来自素材A；独立证据 <2，锚点已置空"}`, got)

	// 重复锚点按互异计数：[3,3] 仍算 1 → 置空标注。
	got = ProfileItem{Evidence: &ProfileEvidence{ChapterIDs: []int64{3, 3}}}.EvidenceJSON()
	assert.JSONEq(t, `{"note":"独立证据 <2，锚点已置空"}`, got)

	// 跨列表 2 个互异锚点 → 原样（chapter+sample 各一）。
	got = ProfileItem{Evidence: &ProfileEvidence{ChapterIDs: []int64{3}, SampleIDs: []int64{7}}}.EvidenceJSON()
	assert.JSONEq(t, `{"chapter_ids":[3],"sample_ids":[7]}`, got)

	// 无锚点仅 note → 序列化 note；无锚点无 note → 空串。
	assert.JSONEq(t, `{"note":"人工补记"}`,
		ProfileItem{Evidence: &ProfileEvidence{Note: "人工补记"}}.EvidenceJSON())
	assert.Empty(t, ProfileItem{Evidence: &ProfileEvidence{}}.EvidenceJSON())
}
