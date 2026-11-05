package pipeline

// P1-2 真实数据对账：平台数据服务层测试——粘贴解析预览（章号归属/跳过行）、
// 落库幂等（同章最新粘贴胜）、越界章号回报、列表/清空。

import (
	"context"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newPlatformDataTest 装配平台数据测试：迁移章节/卷/平台数据表并构造 Service。
func newPlatformDataTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&chapter.Chapter{}, &volume.Volume{}, &quality.PlatformMetric{},
	))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	return svc, db
}

// seedPlatformChapters 建三章，返回各章 ID（按阅读序号）。
func seedPlatformChapters(t *testing.T, db *gorm.DB, novelID int64) (int64, int64, int64) {
	t.Helper()
	var ids []int64
	for i, title := range []string{"第一章", "第二章", "第三章"} {
		ch := chapter.Chapter{NovelID: novelID, SortOrder: i + 1, Title: title}
		require.NoError(t, db.Create(&ch).Error)
		ids = append(ids, ch.ID)
	}
	return ids[0], ids[1], ids[2]
}

func TestPreviewPlatformMetricsResolveAndSkip(t *testing.T) {
	svc, db := newPlatformDataTest(t)
	ctx := context.Background()
	const novelID int64 = 801
	seedPlatformChapters(t, db, novelID)

	got, err := svc.PreviewPlatformMetrics(ctx, novelID,
		"章节\t完读率\t追读率\n第1章\t65%\t40%\n2\t58%\t35%\n9\t50%\t30%\n垃圾行")
	require.NoError(t, err)
	require.Len(t, got.Rows, 3)

	assert.True(t, got.Rows[0].Matched)
	assert.Equal(t, "第一章", got.Rows[0].Title)
	assert.True(t, got.Rows[1].Matched)
	assert.False(t, got.Rows[2].Matched, "第9章越界")
	assert.Equal(t, []int{9}, got.UnmatchedNumbers)
	assert.Len(t, got.SkippedLines, 2, "表头 + 垃圾行")
}

func TestSavePlatformMetricsTextUpsertAndReport(t *testing.T) {
	svc, db := newPlatformDataTest(t)
	ctx := context.Background()
	const novelID int64 = 802
	ch1, ch2, _ := seedPlatformChapters(t, db, novelID)

	res, err := svc.SavePlatformMetricsText(ctx, novelID,
		"1\t65%\t40%\n2\t58%\t35%\n9\t50%\t30%\n垃圾行", "batch-1")
	require.NoError(t, err)
	assert.Len(t, res.Saved, 2)
	assert.Equal(t, []int{9}, res.UnmatchedNumbers)
	assert.Len(t, res.SkippedLines, 1)

	// 同章重贴覆盖（最新胜）。
	res, err = svc.SavePlatformMetricsText(ctx, novelID, "1\t70%\t45%", "batch-2")
	require.NoError(t, err)
	require.Len(t, res.Saved, 2, "回的是全书当前行集")
	byID := map[int64]PlatformMetricView{}
	for _, v := range res.Saved {
		byID[v.ChapterID] = v
	}
	assert.InDelta(t, 70.0, *byID[ch1].CompletionRate, 1e-9)
	assert.Equal(t, "batch-2", byID[ch1].Batch)
	assert.InDelta(t, 58.0, *byID[ch2].CompletionRate, 1e-9)

	// 列表与清空。
	list, err := svc.ListPlatformMetricsView(ctx, novelID)
	require.NoError(t, err)
	assert.Len(t, list, 2)
	assert.Equal(t, "第二章", list[1].Title)

	require.NoError(t, svc.DeletePlatformMetrics(ctx, novelID))
	list, err = svc.ListPlatformMetricsView(ctx, novelID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestSavePlatformMetricsTextRejectsEmpty(t *testing.T) {
	svc, db := newPlatformDataTest(t)
	ctx := context.Background()
	const novelID int64 = 803
	seedPlatformChapters(t, db, novelID)

	_, err := svc.SavePlatformMetricsText(ctx, novelID, "垃圾行", "")
	assert.Error(t, err, "无可入库行拒写")

	_, err = svc.SavePlatformMetricsText(ctx, novelID, "9\t50%\t30%", "")
	assert.Error(t, err, "全越界拒写")

	_, err = svc.SavePlatformMetricsText(ctx, 0, "1\t50%\t30%", "")
	assert.Error(t, err)
}
