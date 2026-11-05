package quality

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlatformMetricsTable(t *testing.T) {
	rate := func(v float64) *float64 { return &v }
	tests := []struct {
		name    string
		text    string
		want    []PlatformMetricRow
		skipped int
	}{
		{
			name: "百分号与第N章形态",
			text: "第1章\t65.3%\t42.1%\n第2章 58% 39%",
			want: []PlatformMetricRow{
				{ReadingNumber: 1, CompletionRate: rate(65.3), RetentionRate: rate(42.1)},
				{ReadingNumber: 2, CompletionRate: rate(58), RetentionRate: rate(39)},
			},
		},
		{
			name: "纯数字章号与逗号分隔",
			text: "3,65.3,42.1\n4，60.5，33.0",
			want: []PlatformMetricRow{
				{ReadingNumber: 3, CompletionRate: rate(65.3), RetentionRate: rate(42.1)},
				{ReadingNumber: 4, CompletionRate: rate(60.5), RetentionRate: rate(33.0)},
			},
		},
		{
			name: "小数率归一为百分比",
			text: "5\t0.653\t0.421",
			want: []PlatformMetricRow{
				{ReadingNumber: 5, CompletionRate: rate(65.3), RetentionRate: rate(42.1)},
			},
		},
		{
			name: "全角百分号与缺追读率列",
			text: "6\t50％\n7\t40%",
			want: []PlatformMetricRow{
				{ReadingNumber: 6, CompletionRate: rate(50), RetentionRate: nil},
				{ReadingNumber: 7, CompletionRate: rate(40), RetentionRate: nil},
			},
		},
		{
			name:    "表头与噪声行跳过",
			text:    "章节\t完读率\t追读率\n8\t50%\t30%\n备注：无",
			want:    []PlatformMetricRow{{ReadingNumber: 8, CompletionRate: rate(50), RetentionRate: rate(30)}},
			skipped: 2,
		},
		{
			name:    "越界率与缺率列跳过",
			text:    "9\t150%\t30%\n10\n11\tabc\t30%",
			skipped: 3,
		},
		{
			name: "同章重复后行覆盖",
			text: "12\t50%\t30%\n12\t55%\t35%",
			want: []PlatformMetricRow{{ReadingNumber: 12, CompletionRate: rate(55), RetentionRate: rate(35)}},
		},
		{
			name: "N章与章N形态",
			text: "13章\t50%\t30%\n章14\t51%\t31%",
			want: []PlatformMetricRow{
				{ReadingNumber: 13, CompletionRate: rate(50), RetentionRate: rate(30)},
				{ReadingNumber: 14, CompletionRate: rate(51), RetentionRate: rate(31)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, skipped := ParsePlatformMetrics(tt.text)
			assert.Equal(t, tt.want, rows)
			assert.Len(t, skipped, tt.skipped)
		})
	}
}

func TestParsePlatformMetricsEmpty(t *testing.T) {
	rows, skipped := ParsePlatformMetrics("  \n\n")
	assert.Empty(t, rows)
	assert.Empty(t, skipped)
}

func TestPlatformMetricSchema(t *testing.T) {
	db := newTestDB(t)
	require.True(t, db.Migrator().HasTable("platform_metrics"))
	for _, col := range []string{
		"novel_id", "chapter_id", "reading_number",
		"completion_rate", "retention_rate", "batch",
	} {
		assert.True(t, db.Migrator().HasColumn("platform_metrics", col), col)
	}
}

func TestReplacePlatformMetricsUpsertIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	rate := func(v float64) *float64 { return &v }

	rows := []PlatformMetric{
		{ChapterID: 101, ReadingNumber: 1, CompletionRate: rate(65), RetentionRate: rate(40)},
		{ChapterID: 102, ReadingNumber: 2, CompletionRate: rate(58)},
	}
	require.NoError(t, s.ReplacePlatformMetrics(ctx, 7, "batch-1", rows))

	// 同章重贴覆盖（最新胜），批次标签更新。
	require.NoError(t, s.ReplacePlatformMetrics(ctx, 7, "batch-2", []PlatformMetric{
		{ChapterID: 101, ReadingNumber: 1, CompletionRate: rate(70), RetentionRate: rate(45)},
	}))
	got, err := s.ListPlatformMetrics(ctx, 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].ReadingNumber)
	assert.InDelta(t, 70.0, *got[0].CompletionRate, 1e-9)
	assert.InDelta(t, 45.0, *got[0].RetentionRate, 1e-9)
	assert.Equal(t, "batch-2", got[0].Batch)
	assert.Equal(t, "batch-1", got[1].Batch)
	assert.Nil(t, got[1].RetentionRate)

	// 清空重录。
	require.NoError(t, s.DeletePlatformMetrics(ctx, 7))
	got, err = s.ListPlatformMetrics(ctx, 7)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestReplacePlatformMetricsValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	rate := func(v float64) *float64 { return &v }

	assert.Error(t, s.ReplacePlatformMetrics(ctx, 0, "", nil))
	assert.Error(t, s.ReplacePlatformMetrics(ctx, 7, "", nil))
	assert.Error(t, s.ReplacePlatformMetrics(ctx, 7, "", []PlatformMetric{{ReadingNumber: 1}}))
	assert.Error(t, s.ReplacePlatformMetrics(ctx, 7, "", []PlatformMetric{
		{NovelID: 8, ChapterID: 1, ReadingNumber: 1},
	}))
	assert.Error(t, s.ReplacePlatformMetrics(ctx, 7, "", []PlatformMetric{
		{ChapterID: 1, ReadingNumber: 1, CompletionRate: rate(150)},
	}))
	assert.Error(t, s.ReplacePlatformMetrics(ctx, 7, "", []PlatformMetric{
		{ChapterID: 1, ReadingNumber: 1, RetentionRate: rate(-1)},
	}))
}
