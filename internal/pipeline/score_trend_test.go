package pipeline

// P0-3 评分打通：趋势视图数据包测试——逐章分数/发现数曲线（主轨评分明细历史 +
// 旧快照回填）与单章修复前后对比，6↔8 映射契约快照直通 review 单源。

import (
	"context"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/review"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newScoreTrendTest 装配趋势测试：迁移章节/卷/质量档案/评分明细历史表并构造 Service。
func newScoreTrendTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&chapter.Chapter{}, &volume.Volume{},
		&quality.ChapterQuality{}, &quality.ChapterScoreEvent{},
	))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	return svc, db
}

// seedScoreTrend 建两章：ch1（两次评分事件=修复前后对比）、ch2（仅旧快照）。
// 返回 (ch1ID, ch2ID)。ch1 带质量档案发现（L0 1 条未修 + L1 1 条已修）。
func seedScoreTrend(t *testing.T, svc *Service, db *gorm.DB, novelID int64) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	qs := quality.NewStore(db, slog.Default())

	ch1 := chapter.Chapter{NovelID: novelID, SortOrder: 1, Title: "第一章"}
	ch2 := chapter.Chapter{NovelID: novelID, SortOrder: 2, Title: "第二章"}
	require.NoError(t, db.Create(&ch1).Error)
	require.NoError(t, db.Create(&ch2).Error)

	for _, ev := range []*quality.ChapterScoreEvent{
		{NovelID: novelID, ChapterID: ch1.ID, ReadingNumber: 1, Overall: 60,
			Dimensions: quality.MarshalScoreDims(map[string]int{"emotion_hook": 40, "taboo": 90}),
			Verdict:    "fail", Critique: "先改", Source: quality.ScoreSourceNew},
		{NovelID: novelID, ChapterID: ch1.ID, ReadingNumber: 1, Overall: 75,
			Dimensions: quality.MarshalScoreDims(map[string]int{"emotion_hook": 90, "taboo": 95}),
			Verdict:    "pass", Critique: "后改", Source: quality.ScoreSourceFullReplace},
	} {
		require.NoError(t, qs.AppendScoreEvent(ctx, ev))
	}
	require.NoError(t, qs.UpsertL0(ctx, &quality.ChapterQuality{
		NovelID: novelID, ChapterID: ch1.ID, ReadingNumber: 1,
		L0Metrics:  quality.MarshalL0Metrics(quality.L0Metrics{WordCount: 2100}),
		L0Findings: quality.MarshalFindings([]quality.Finding{{ID: "C1", Check: "tail_hook", Level: quality.FindingLevelDefault, Status: quality.FindingStatusPending}}),
		L1Findings: quality.MarshalFindings([]quality.Finding{{ID: "L1-1", Check: "continuity", Level: quality.FindingLevelDefault, Status: quality.FindingStatusFixed}}),
		L1Status:   quality.L1StatusChecked,
	}))

	// ch2 旧快照回填：只有 chapters.quality_score，无明细历史。
	score := 88
	require.NoError(t, db.Model(&chapter.Chapter{}).Where("id = ?", ch2.ID).
		Updates(map[string]any{"quality_score": score, "quality_report": "旧报告"}).Error)
	return ch1.ID, ch2.ID
}

// TestGetScoreTrendPointsAndDeltas 逐章曲线：最新/上一次分、修复前后变化、尝试次数、
// 发现数，旧快照章节回填 attempts=1；映射契约快照直通 review 单源。
func TestGetScoreTrendPointsAndDeltas(t *testing.T) {
	svc, db := newScoreTrendTest(t)
	ctx := context.Background()
	const novelID int64 = 701
	ch1ID, ch2ID := seedScoreTrend(t, svc, db, novelID)

	trend, err := svc.GetScoreTrend(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, trend.Points, 2)

	p1, p2 := trend.Points[0], trend.Points[1]
	require.Equal(t, ch1ID, p1.ChapterID)
	require.Equal(t, ch2ID, p2.ChapterID)
	assert.Equal(t, 1, p1.ReadingNumber)
	assert.Equal(t, 2, p2.ReadingNumber)

	assert.True(t, p1.Scored)
	assert.Equal(t, 75, p1.Score, "最新分")
	assert.Equal(t, 60, p1.PrevScore, "上一次分")
	assert.Equal(t, 15, p1.Delta, "修复前后变化")
	assert.Equal(t, 2, p1.Attempts)
	assert.Equal(t, 1, p1.L0Findings)
	assert.Equal(t, 1, p1.L1Findings)
	assert.Equal(t, 1, p1.OpenFindings, "未修复发现只计 pending")

	assert.True(t, p2.Scored, "旧快照回填也算已评分")
	assert.Equal(t, 88, p2.Score)
	assert.Equal(t, 0, p2.PrevScore)
	assert.Equal(t, 0, p2.Delta)
	assert.Equal(t, 1, p2.Attempts)

	// 映射契约快照 = review 单源（前端图例不另行硬编码）。
	assert.Equal(t, review.ScoreDimInfos(), trend.JudgeDims)
	assert.Equal(t, review.AuditDimOrder, trend.AuditDimOrder)
}

// TestGetChapterScoreHistoryEventsAndSnapshot 单章修复前后对比：事件正序、分维换算
// 同源展示；旧快照回填单条 snapshot 事件；无评分章节返回空清单不报错。
func TestGetChapterScoreHistoryEventsAndSnapshot(t *testing.T) {
	svc, db := newScoreTrendTest(t)
	ctx := context.Background()
	const novelID int64 = 702
	ch1ID, ch2ID := seedScoreTrend(t, svc, db, novelID)

	views, err := svc.GetChapterScoreHistory(ctx, novelID, ch1ID)
	require.NoError(t, err)
	require.Len(t, views, 2)
	assert.Equal(t, 60, views[0].Overall)
	assert.Equal(t, "先改", views[0].Critique)
	assert.Equal(t, quality.ScoreSourceNew, views[0].Source)
	assert.Equal(t, map[string]int{"emotion_hook": 40, "taboo": 90}, views[0].JudgeDims)
	assert.Equal(t, review.JudgeToAuditScores(map[string]int{"emotion_hook": 40, "taboo": 90}), views[0].AuditDims,
		"8 维同源分应与映射契约单源换算一致")
	assert.Equal(t, 75, views[1].Overall)
	assert.Equal(t, quality.ScoreSourceFullReplace, views[1].Source)

	snap, err := svc.GetChapterScoreHistory(ctx, novelID, ch2ID)
	require.NoError(t, err)
	require.Len(t, snap, 1, "旧快照回填单条")
	assert.Equal(t, 88, snap[0].Overall)
	assert.Equal(t, scoreSourceSnapshot, snap[0].Source)
	assert.Empty(t, snap[0].JudgeDims, "快照无分维")
	assert.Empty(t, snap[0].AuditDims)

	empty, err := svc.GetChapterScoreHistory(ctx, novelID, 9999)
	require.NoError(t, err)
	assert.Empty(t, empty)
}
