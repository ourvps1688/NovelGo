package voice

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db := newTestDB(t)
	logger := newTestLogger()
	return NewStore(db, logger)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestUpsertIdempotent 同维度重复 Upsert 只留最新（幂等断言）。
func TestUpsertIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := &Entry{NovelID: 1, Dimension: string(DimRhythm), Content: "先结论后铺垫",
		Metrics: `{"avg_sent_len":8.7}`, Source: string(SourceStyleSample)}
	require.NoError(t, s.Upsert(ctx, first))
	require.NotZero(t, first.ID)

	// 同键再写：同一行覆盖，不新增行。
	second := &Entry{NovelID: 1, Dimension: string(DimRhythm), Content: "先铺垫后结论",
		Evidence: `{"sample_ids":[3]}`, Source: string(SourceManual)}
	require.NoError(t, s.Upsert(ctx, second))

	list, err := s.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, list, 1, "同维度重复 Upsert 不得新增行")
	assert.Equal(t, first.ID, list[0].ID, "应更新同一行")
	assert.Equal(t, "先铺垫后结论", list[0].Content, "只留最新")
	assert.Equal(t, `{"sample_ids":[3]}`, list[0].Evidence, "字段整体覆盖")
	assert.Empty(t, list[0].Metrics, "未带 metrics 即覆盖为空（宁缺勿错）")
	assert.Equal(t, string(SourceManual), list[0].Source)

	got, err := s.Get(ctx, 1, DimRhythm)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "先铺垫后结论", got.Content)
}

func TestUpsertValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		entry *Entry
	}{
		{"nil", nil},
		{"novel 缺失", &Entry{NovelID: 0, Dimension: string(DimRhythm), Content: "x", Source: string(SourceManual)}},
		{"非法维度", &Entry{NovelID: 1, Dimension: "style_six_dim", Content: "x", Source: string(SourceManual)}},
		{"空维度", &Entry{NovelID: 1, Dimension: "", Content: "x", Source: string(SourceManual)}},
		{"非法来源", &Entry{NovelID: 1, Dimension: string(DimRhythm), Content: "x", Source: "other"}},
		{"无内容", &Entry{NovelID: 1, Dimension: string(DimRhythm), Content: "   ", Source: string(SourceManual)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, s.Upsert(ctx, tc.entry))
		})
	}

	list, err := s.List(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, list, "非法输入不得落库")
}

func TestListFixedDimensionOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 乱序写入，List 须按 §0.1 固定维度顺序返回。
	for _, dim := range []Dimension{DimDialogueRatio, DimRhythm, DimPOV, DimSentenceFingerprint} {
		require.NoError(t, s.Upsert(ctx, &Entry{NovelID: 1, Dimension: string(dim), Content: "x", Source: string(SourceManual)}))
	}
	list, err := s.List(ctx, 1)
	require.NoError(t, err)
	require.Len(t, list, 4)
	want := []string{string(DimSentenceFingerprint), string(DimRhythm), string(DimPOV), string(DimDialogueRatio)}
	for i, e := range list {
		assert.Equal(t, want[i], e.Dimension)
	}

	// 书隔离：另一本不受影响。
	other, err := s.List(ctx, 2)
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestGetMissingReturnsNil(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Get(context.Background(), 42, DimHumor)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDeleteByNovel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, novelID := range []int64{1, 2} {
		require.NoError(t, s.Upsert(ctx, &Entry{NovelID: novelID, Dimension: string(DimHumor), Content: "x", Source: string(SourceManual)}))
	}
	require.NoError(t, s.DeleteByNovel(ctx, 1))

	list, err := s.List(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, list)

	list, err = s.List(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, list, 1, "不得误删其他书")
}
