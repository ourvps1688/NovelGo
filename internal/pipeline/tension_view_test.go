package pipeline

// tension_view_test.go —— 单章张力曲线消费视图（P0-2 情绪账前端可视化）。

import (
	"context"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newTensionViewTest 装配张力视图测试：迁移 chapter_tension 表并构造 Service。
func newTensionViewTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&quality.ChapterTension{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	return svc, db
}

func tensionPt(seg int, tension float64, beat string) quality.TensionPoint {
	return quality.TensionPoint{
		Seg: seg, StartRune: seg * 400, EndRune: seg*400 + 390,
		Emotion: 20, Tension: tension, BeatType: beat,
	}
}

// TestGetChapterTensionCurve 视图组装：生效曲线（同 seg L1 优先）+ 标记位 +
// 阅读序号；无曲线章返回 Exists=false 空视图不报错。
func TestGetChapterTensionCurve(t *testing.T) {
	svc, db := newTensionViewTest(t)
	ctx := context.Background()
	qs := quality.NewStore(db, slog.Default())
	const novelID, chapterID int64 = 980, 9801

	// L0 基线：平坦 0–2 + 高张力 seg 3；L1 覆盖 seg 3（climax）。
	require.NoError(t, qs.ReplaceTensionCurve(ctx, novelID, chapterID, 5, quality.TensionSourceL0,
		[]quality.TensionPoint{
			tensionPt(0, 10, quality.BeatTypeFlat), tensionPt(1, 10, quality.BeatTypeFlat),
			tensionPt(2, 10, quality.BeatTypeFlat), tensionPt(3, 60, quality.BeatTypeConflict),
		}))
	require.NoError(t, qs.ReplaceTensionCurve(ctx, novelID, chapterID, 5, quality.TensionSourceL1,
		[]quality.TensionPoint{tensionPt(3, 90, quality.BeatTypeClimax)}))

	view, err := svc.GetChapterTensionCurve(ctx, novelID, chapterID)
	require.NoError(t, err)
	require.True(t, view.Exists)
	assert.Equal(t, chapterID, view.ChapterID)
	assert.Equal(t, 5, view.ReadingNumber)
	require.Len(t, view.Points, 4)
	assert.Equal(t, 3, view.Points[3].Seg)
	assert.Equal(t, 90.0, view.Points[3].Tension) // L1 优先于 L0
	assert.Equal(t, 0, view.Markers.DragStartSeg)
	assert.Equal(t, 2, view.Markers.DragEndSeg)
	assert.Equal(t, []int{3}, view.Markers.ClimaxSegs)

	// 无曲线章：Exists=false 空视图不报错。
	empty, err := svc.GetChapterTensionCurve(ctx, novelID, 404404)
	require.NoError(t, err)
	assert.False(t, empty.Exists)
	assert.Empty(t, empty.Points)
}
