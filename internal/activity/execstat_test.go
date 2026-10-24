package activity

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ── R0 观测：exec_run_stats（feature_usage_daily 同族新表）────────────
//
// 验收：①AddExecStat 尽力记录、空 skill 拒收（宁缺勿错）；
// ②SummarizeExecStats 按 skill 聚合（轮数=样本行数、耗时/turn 分布）且窗口正确。

func TestAddExecStatRejectsBlankSkillAndRecords(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	s.AddExecStat(ctx, ExecStat{})
	s.AddExecStat(ctx, ExecStat{Skill: "   "})
	var count int64
	require.NoError(t, db.Model(&ExecStat{}).Count(&count).Error)
	require.Zero(t, count, "空 skill 样本必须拒收（不写脏数据）")

	s.AddExecStat(ctx, ExecStat{NovelID: 1, RunID: "r1", Skill: "signability-audit",
		DurationMS: 1200, Turns: 9, MaxTurns: 24, Status: ExecStatusOK})
	require.NoError(t, db.Model(&ExecStat{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var row ExecStat
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, "signability-audit", row.Skill)
	require.Equal(t, "r1", row.RunID)
	require.EqualValues(t, 1200, row.DurationMS)
	require.Equal(t, 9, row.Turns)
	require.Equal(t, 24, row.MaxTurns)
	require.Equal(t, ExecStatusOK, row.Status)
	require.False(t, row.CreatedAt.IsZero())
}

func TestSummarizeExecStatsAggregatesBySkill(t *testing.T) {
	s, db := newTestStore(t)
	ctx := context.Background()
	day := s.now()
	rows := []ExecStat{
		// 今天：S5 修复两轮（同 run），其中一轮失败
		{NovelID: 1, RunID: "r1", Skill: "signability-fix", DurationMS: 100, Turns: 10, MaxTurns: 18, Status: ExecStatusOK, CreatedAt: day},
		{NovelID: 1, RunID: "r1", Skill: "signability-fix", DurationMS: 300, Turns: 18, MaxTurns: 18, Status: ExecStatusError, CreatedAt: day},
		// 今天：重写一章 + 熔断跳过一次
		{NovelID: 1, RunID: "r1", Skill: "chapter-rewrite", DurationMS: 400, Turns: 6, MaxTurns: 18, Status: ExecStatusOK, CreatedAt: day},
		{NovelID: 1, RunID: "r1", Skill: "chapter-rewrite", DurationMS: 0, Turns: 0, MaxTurns: 18, Status: ExecStatusTripped, CreatedAt: day},
		// 窗口外（days=3 → since = 今天-2）
		{NovelID: 1, RunID: "r0", Skill: "signability-fix", DurationMS: 9999, Turns: 99, MaxTurns: 18, Status: ExecStatusOK, CreatedAt: day.AddDate(0, 0, -5)},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}

	stats, err := s.SummarizeExecStats(ctx, 3)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	// 按总耗时降序：fix=400ms > rewrite=400ms？——fix(100+300)=400 与 rewrite(400+0)=400 平手，
	// 次序按 skill 名升序：chapter-rewrite 在前。
	require.Equal(t, "chapter-rewrite", stats[0].Skill)
	require.EqualValues(t, 2, stats[0].Runs)
	require.EqualValues(t, 0, stats[0].ErrorRuns)
	require.EqualValues(t, 1, stats[0].TrippedRuns)
	require.EqualValues(t, 400, stats[0].TotalDuration)
	require.EqualValues(t, 200, stats[0].AvgDuration)
	require.EqualValues(t, 400, stats[0].MaxDuration)
	require.EqualValues(t, 6, stats[0].TotalTurns)
	require.EqualValues(t, 3, stats[0].AvgTurns)
	require.EqualValues(t, 6, stats[0].MaxTurnsUsed)

	require.Equal(t, "signability-fix", stats[1].Skill)
	require.EqualValues(t, 2, stats[1].Runs, "S5 轮数 = 同 run 同 skill 的样本行数")
	require.EqualValues(t, 1, stats[1].ErrorRuns)
	require.EqualValues(t, 0, stats[1].TrippedRuns)
	require.EqualValues(t, 400, stats[1].TotalDuration)
	require.EqualValues(t, 28, stats[1].TotalTurns)
	require.EqualValues(t, 18, stats[1].MaxTurnsUsed)

	// days 非正按 30 天默认：窗口外行纳入（fix 总耗时反超，次序翻转为 fix 在前）
	stats, err = s.SummarizeExecStats(ctx, 0)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	bySkill := make(map[string]ExecStatSummary, len(stats))
	for _, st := range stats {
		bySkill[st.Skill] = st
	}
	require.EqualValues(t, 3, bySkill["signability-fix"].Runs, "30 天窗口须含 -5 天样本")
	require.EqualValues(t, 2, bySkill["chapter-rewrite"].Runs)

	// 无数据 = 空表（调用方静默降级口径）
	require.NoError(t, db.Exec("DELETE FROM exec_run_stats").Error)
	stats, err = s.SummarizeExecStats(ctx, 3)
	require.NoError(t, err)
	require.Empty(t, stats)

	// 时钟推进后窗口滑动（与 ListFeatureUsage 同语义）
	s.now = func() time.Time { return day.AddDate(0, 0, 1) }
	s.AddExecStat(ctx, ExecStat{Skill: "consistency-patrol", Status: ExecStatusOK, CreatedAt: day.AddDate(0, 0, 1)})
	stats, err = s.SummarizeExecStats(ctx, 1)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	require.Equal(t, "consistency-patrol", stats[0].Skill)
}
