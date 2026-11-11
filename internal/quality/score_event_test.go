package quality

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChapterScoreEventSchema 建表 + 契约列 + 索引（append-only 历史表）。
func TestChapterScoreEventSchema(t *testing.T) {
	db := newTestDB(t)

	require.True(t, db.Migrator().HasTable("chapter_score_events"))
	for _, col := range []string{
		"novel_id", "chapter_id", "reading_number",
		"overall", "dimensions", "verdict", "critique", "finding_keys", "source", "created_at",
	} {
		require.True(t, db.Migrator().HasColumn(&ChapterScoreEvent{}, col), "缺列 %s", col)
	}
}

// TestAppendScoreEventAppendOnly 追加型契约：同章多次评分各占一行，按落库顺序升序返回。
func TestAppendScoreEventAppendOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, overall := range []int{62, 71, 88} {
		require.NoError(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{
			NovelID: 1, ChapterID: 7, ReadingNumber: 3, Overall: overall,
			Dimensions: MarshalScoreDims(map[string]int{"plot_advance": overall}),
			Verdict:    "pass", Critique: "建议", Source: ScoreSourceFullReplace,
		}))
	}

	list, err := s.ListScoreEvents(ctx, 1, 7)
	require.NoError(t, err)
	require.Len(t, list, 3, "append-only：三次评分三行，不得覆盖")
	assert.Equal(t, []int{62, 71, 88}, []int{list[0].Overall, list[1].Overall, list[2].Overall})
	assert.Equal(t, map[string]int{"plot_advance": 88}, ParseScoreDims(list[2].Dimensions))

	// 其他章节/其他书不串账。
	other, err := s.ListScoreEvents(ctx, 1, 8)
	require.NoError(t, err)
	assert.Empty(t, other)
}

// TestListScoreEventsByNovelReturnsAll 全书趋势数据源：跨章全量、稳定顺序。
func TestListScoreEventsByNovelReturnsAll(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{NovelID: 1, ChapterID: 7, Overall: 60, Source: ScoreSourceNew}))
	require.NoError(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{NovelID: 1, ChapterID: 8, Overall: 75, Source: ScoreSourceFullReplace}))
	require.NoError(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{NovelID: 2, ChapterID: 9, Overall: 90, Source: ScoreSourceNew}))

	list, err := s.ListScoreEventsByNovel(ctx, 1)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, int64(7), list[0].ChapterID)
	assert.Equal(t, int64(8), list[1].ChapterID)
}

// TestAppendScoreEventValidation 载荷校验：空行/缺键拒收，空 source 规范化为 full_replace。
func TestAppendScoreEventValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	assert.Error(t, s.AppendScoreEvent(ctx, nil))
	assert.Error(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{NovelID: 0, ChapterID: 1}))
	assert.Error(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{NovelID: 1, ChapterID: 0}))

	require.NoError(t, s.AppendScoreEvent(ctx, &ChapterScoreEvent{NovelID: 1, ChapterID: 1}))
	list, err := s.ListScoreEvents(ctx, 1, 1)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, ScoreSourceFullReplace, list[0].Source, "空 source 规范化")
}

// TestMarshalParseFindingKeys 发现 key 快照往返 + 坏数据宁缺勿错（旧行空串=无快照）。
func TestMarshalParseFindingKeys(t *testing.T) {
	assert.Empty(t, MarshalFindingKeys(nil))
	assert.Nil(t, ParseFindingKeys(""))
	assert.Nil(t, ParseFindingKeys("{bad json"))

	keys := []string{"l0|tail_hook|#0", "l1|reader_state|#0"}
	assert.Equal(t, keys, ParseFindingKeys(MarshalFindingKeys(keys)))
}

// TestMarshalParseScoreDims 分维序列化往返 + 坏数据宁缺勿错。
func TestMarshalParseScoreDims(t *testing.T) {
	assert.Empty(t, MarshalScoreDims(nil))
	assert.Nil(t, ParseScoreDims(""))
	assert.Nil(t, ParseScoreDims("{bad json"))

	dims := map[string]int{"word_count": 80, "taboo": 100}
	got := ParseScoreDims(MarshalScoreDims(dims))
	assert.Equal(t, dims, got)
}
