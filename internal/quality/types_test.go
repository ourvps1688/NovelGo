package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChapterQuality{}))
	return db
}

// TestChapterQualitySchema 建表 + 唯一索引 + 契约列（落独立表，勿塞 GateContext）。
func TestChapterQualitySchema(t *testing.T) {
	db := newTestDB(t)

	require.True(t, db.Migrator().HasTable("chapter_quality"))
	for _, col := range []string{
		"novel_id", "chapter_id", "reading_number",
		"l0_metrics", "l0_findings", "l1_findings", "l1_status", "round", "checked_at",
	} {
		require.True(t, db.Migrator().HasColumn("chapter_quality", col), "缺列 %s", col)
	}
	require.False(t, db.Migrator().HasColumn("chapter_quality", "changed_at"),
		"档案不走 temporal/history 履历，不加 changed_at")

	// (novel_id, chapter_id) 唯一：同键第二行拒收。
	first := ChapterQuality{NovelID: 1, ChapterID: 7, L1Status: L1StatusPending}
	require.NoError(t, db.Create(&first).Error)
	dup := ChapterQuality{NovelID: 1, ChapterID: 7, L1Status: L1StatusPending}
	assert.Error(t, db.Create(&dup).Error, "(novel_id, chapter_id) 应唯一")
	// 不同章可共存。
	other := ChapterQuality{NovelID: 1, ChapterID: 8, L1Status: L1StatusPending}
	assert.NoError(t, db.Create(&other).Error)
}

// TestL0MetricsJSON 指标 JSON 往返 + 坏数据宁缺勿错。
func TestL0MetricsJSON(t *testing.T) {
	m := L0Metrics{WordCount: 2100, HasTailHook: true,
		StyleMetrics: map[string]float64{"avg_sent_len": 12.5, "quote_density": 0.31}}
	raw := MarshalL0Metrics(m)
	require.NotEmpty(t, raw)

	got := ParseL0Metrics(raw)
	assert.Equal(t, 2100, got.WordCount)
	assert.True(t, got.HasTailHook)
	assert.Equal(t, 12.5, got.StyleMetrics["avg_sent_len"])
	assert.Equal(t, 0.31, got.StyleMetrics["quote_density"])

	assert.Equal(t, L0Metrics{}, ParseL0Metrics(""), "空串=未查，返回零值")
	assert.Equal(t, L0Metrics{}, ParseL0Metrics("{bad json"), "坏数据宁缺勿错，返回零值不报错")
	assert.NotEmpty(t, MarshalL0Metrics(L0Metrics{StyleMetrics: map[string]float64{"x": 1}}), "正常指标应可序列化")
}

// TestFindingsJSON 发现清单 JSON 往返 + "[]"=已查零发现语义。
func TestFindingsJSON(t *testing.T) {
	list := []Finding{
		{ID: "C1", Check: "word_count", Level: FindingLevelDefault,
			Desc: "本章 820 字，低于门槛", ChapterIDs: []int64{12}, Status: FindingStatusPending},
		{ID: "L1-1", Check: "continuity", Level: FindingLevelDefault,
			Desc: "与前章结尾衔接不一致", ChapterIDs: []int64{11, 12},
			LedgerRefs: []string{"char.hero.state"}, Status: FindingStatusFixed},
	}
	raw := MarshalFindings(list)
	require.NotEmpty(t, raw)

	got := ParseFindings(raw)
	require.Len(t, got, 2)
	assert.Equal(t, "C1", got[0].ID)
	assert.Equal(t, []int64{12}, got[0].ChapterIDs)
	assert.Equal(t, []string{"char.hero.state"}, got[1].LedgerRefs)
	assert.Equal(t, FindingStatusFixed, got[1].Status)

	assert.Equal(t, "[]", MarshalFindings(nil), "空清单序列化为 []（已查零发现 ≠ 未查）")
	assert.Equal(t, "[]", MarshalFindings([]Finding{}))
	assert.Nil(t, ParseFindings(""), "空串=未查")
	assert.Nil(t, ParseFindings("not json"), "坏数据宁缺勿错，返回空清单不报错")
}

// TestL1StatusEnum 枚举值单源：合法三值 + 空规范化 + 非法拒收。
func TestL1StatusEnum(t *testing.T) {
	assert.True(t, validL1Status(L1StatusPending))
	assert.True(t, validL1Status(L1StatusChecked))
	assert.True(t, validL1Status(L1StatusSkipped))
	assert.False(t, validL1Status(""))
	assert.False(t, validL1Status("done"))

	e := &ChapterQuality{NovelID: 1, ChapterID: 2}
	require.NoError(t, normalize(e))
	assert.Equal(t, L1StatusPending, e.L1Status, "空 l1_status 规范化为 pending（行存在即待查）")

	e2 := &ChapterQuality{NovelID: 1, ChapterID: 2, L1Status: "done"}
	assert.Error(t, normalize(e2), "非法 l1_status 拒收（宁缺勿错）")

	assert.Error(t, normalize(nil))
	assert.Error(t, normalize(&ChapterQuality{NovelID: 0, ChapterID: 2}), "novel_id 缺失拒收")
	assert.Error(t, normalize(&ChapterQuality{NovelID: 1, ChapterID: 0}), "chapter_id 缺失拒收")
}
