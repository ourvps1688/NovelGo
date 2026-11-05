package pipeline

// P1-2 真实数据对账：「流失章 × 低分维」交叉诊断测试——流失判定（相邻率降 ≥5pp）、
// 低分维 6↔8 映射投影、lift 统计与摘要。

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

// newPlatformReconcileTest 迁移章节/卷/平台数据/评分明细/质量档案表并构造 Service。
func newPlatformReconcileTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&chapter.Chapter{}, &volume.Volume{},
		&quality.PlatformMetric{}, &quality.ChapterScoreEvent{}, &quality.ChapterQuality{},
	))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	return svc, db
}

func TestGetPlatformReconciliationEmpty(t *testing.T) {
	svc, _ := newPlatformReconcileTest(t)
	got, err := svc.GetPlatformReconciliation(context.Background(), 901)
	require.NoError(t, err)
	assert.Empty(t, got.Chapters)
	assert.Empty(t, got.DimStats)
	assert.NotEmpty(t, got.JudgeDims, "映射契约快照恒在")
	assert.Len(t, got.AuditDimOrder, 8)
	assert.Contains(t, got.Summary, "尚未录入平台数据")
}

func TestGetPlatformReconciliationChurnAndDimStats(t *testing.T) {
	svc, db := newPlatformReconcileTest(t)
	ctx := context.Background()
	const novelID int64 = 902
	qs := quality.NewStore(db, slog.Default())

	// 四章：ch1/ch2 稳定，ch3 完读骤降（流失），ch4 稳定。
	var ids []int64
	for i := 0; i < 4; i++ {
		ch := chapter.Chapter{NovelID: novelID, SortOrder: i + 1, Title: "章"}
		require.NoError(t, db.Create(&ch).Error)
		ids = append(ids, ch.ID)
	}
	rate := func(v float64) *float64 { return &v }
	require.NoError(t, qs.ReplacePlatformMetrics(ctx, novelID, "b1", []quality.PlatformMetric{
		{ChapterID: ids[0], ReadingNumber: 1, CompletionRate: rate(70), RetentionRate: rate(50)},
		{ChapterID: ids[1], ReadingNumber: 2, CompletionRate: rate(68), RetentionRate: rate(48)},
		{ChapterID: ids[2], ReadingNumber: 3, CompletionRate: rate(60), RetentionRate: rate(47)}, // -8pp → 流失
		{ChapterID: ids[3], ReadingNumber: 4, CompletionRate: rate(59), RetentionRate: rate(46)},
	}))

	// 评分：ch1 全高分（稳定无低分维）；ch3 情绪钩子/情节推进低（流失低分）。
	require.NoError(t, qs.AppendScoreEvent(ctx, &quality.ChapterScoreEvent{
		NovelID: novelID, ChapterID: ids[0], ReadingNumber: 1, Overall: 80,
		Dimensions: quality.MarshalScoreDims(map[string]int{"emotion_hook": 85, "plot_advance": 80}),
		Source:     quality.ScoreSourceNew,
	}))
	require.NoError(t, qs.AppendScoreEvent(ctx, &quality.ChapterScoreEvent{
		NovelID: novelID, ChapterID: ids[2], ReadingNumber: 3, Overall: 55,
		Dimensions: quality.MarshalScoreDims(map[string]int{"emotion_hook": 40, "plot_advance": 50}),
		Source:     quality.ScoreSourceNew,
	}))

	// ch3 一条未修复发现。
	require.NoError(t, qs.UpsertL0(ctx, &quality.ChapterQuality{
		NovelID: novelID, ChapterID: ids[2], ReadingNumber: 3,
		L0Findings: quality.MarshalFindings([]quality.Finding{
			{ID: "C1", Check: "tail_hook", Level: quality.FindingLevelDefault, Status: quality.FindingStatusPending},
		}),
		L1Status: quality.L1StatusPending,
	}))

	got, err := svc.GetPlatformReconciliation(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, got.Chapters, 4)

	c1, c3 := got.Chapters[0], got.Chapters[2]
	assert.False(t, c1.Churn)
	assert.InDelta(t, 0.0, c1.CompletionDrop, 1e-9, "首章无前值")
	assert.True(t, c3.Churn, "完读率 -8pp")
	assert.InDelta(t, 8.0, c3.CompletionDrop, 1e-9)
	assert.True(t, c3.Scored)
	assert.Equal(t, 55, c3.Score)
	assert.ElementsMatch(t, []string{"emotion_hook", "plot_advance"}, c3.LowJudgeDims)
	// emotion_hook → opening/reader；plot_advance → pacing/longArc。
	assert.ElementsMatch(t, []string{"opening", "reader", "pacing", "longArc"}, c3.LowAuditDims)
	assert.Equal(t, []string{"tail_hook"}, c3.OpenFindings)

	// 维统计：流失组 1 章有评分全低分 → 相关维 lift 高且在前。
	require.NotEmpty(t, got.DimStats)
	for _, dim := range []string{"opening", "reader", "pacing", "longArc"} {
		var st *PlatformDimStat
		for i := range got.DimStats {
			if got.DimStats[i].Dim == dim {
				st = &got.DimStats[i]
			}
		}
		require.NotNil(t, st, dim)
		assert.Equal(t, 1, st.ChurnLow, dim)
		assert.Equal(t, 1, st.ChurnTotal, dim)
		assert.Equal(t, 0, st.StableLow, dim)
		assert.Equal(t, 1, st.StableTotal, dim)
		assert.InDelta(t, 1.0, st.Lift, 1e-9, dim)
	}
	assert.True(t, got.DimStats[0].Lift >= got.DimStats[len(got.DimStats)-1].Lift, "lift 降序")
	assert.Contains(t, got.Summary, "流失章 1")
	// opening 的来源判官维=emotion_hook（映射单源）。
	var opening *PlatformDimStat
	for i := range got.DimStats {
		if got.DimStats[i].Dim == "opening" {
			opening = &got.DimStats[i]
		}
	}
	require.NotNil(t, opening)
	assert.Equal(t, []string{"emotion_hook"}, opening.JudgeSources)
}

func TestGetPlatformReconciliationNoChurnSummary(t *testing.T) {
	svc, db := newPlatformReconcileTest(t)
	ctx := context.Background()
	const novelID int64 = 903
	qs := quality.NewStore(db, slog.Default())
	var ids []int64
	for i := 0; i < 2; i++ {
		ch := chapter.Chapter{NovelID: novelID, SortOrder: i + 1, Title: "章"}
		require.NoError(t, db.Create(&ch).Error)
		ids = append(ids, ch.ID)
	}
	rate := func(v float64) *float64 { return &v }
	require.NoError(t, qs.ReplacePlatformMetrics(ctx, novelID, "b1", []quality.PlatformMetric{
		{ChapterID: ids[0], ReadingNumber: 1, CompletionRate: rate(70)},
		{ChapterID: ids[1], ReadingNumber: 2, CompletionRate: rate(68), RetentionRate: rate(48)},
	}))

	got, err := svc.GetPlatformReconciliation(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, got.Chapters, 2)
	assert.False(t, got.Chapters[1].Churn, "-2pp 不判流失")
	assert.Contains(t, got.Summary, "无流失章")
}
