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
