package pipeline

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/activity"
)

// ── R0 观测（pipeline-core-redesign-proposal.md §5）────────────────
//
// 验收：①dispatchSkill 每次派发收口记一条 ExecStat（skill/耗时/turn 数/状态/归属 run）；
// ②熔断跳派发记 tripped 样本；③观测自身异常绝不影响派发结果（零行为变化）；
// ④默认落库口写 activity exec_run_stats（同族新表）。

// captureExecSinks 把观测落库口换成捕获桩，返回样本收集器。
func captureExecSinks(svc *Service) *[]activity.ExecStat {
	samples := new([]activity.ExecStat)
	svc.SetExecStatSink(func(_ context.Context, stat activity.ExecStat) {
		*samples = append(*samples, stat)
	})
	return samples
}

func TestDispatchSkillRecordsExecStat(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		require.Equal(t, "signability-fix", name)
		require.Equal(t, "run-1", input.RunID)
		return SkillOutput{Text: "done", Turns: 5}, nil
	}
	samples := captureExecSinks(svc)

	out, err := svc.dispatchSkill(context.Background(), "signability-fix", SkillInput{
		NovelID: 42, RunID: "run-1", MaxTurns: 18,
	})
	require.NoError(t, err)
	require.Equal(t, 5, out.Turns)

	require.Len(t, *samples, 1)
	got := (*samples)[0]
	require.Equal(t, "signability-fix", got.Skill)
	require.Equal(t, "run-1", got.RunID)
	require.Equal(t, int64(42), got.NovelID)
	require.Equal(t, 5, got.Turns)
	require.Equal(t, 18, got.MaxTurns)
	require.Equal(t, activity.ExecStatusOK, got.Status)
	require.GreaterOrEqual(t, got.DurationMS, int64(0))
}

func TestDispatchSkillRecordsErrorStatWithPartialTurns(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	wantErr := errors.New("skill boom")
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		// 失败路径回填已消耗轮数（RunSkill 语义）。
		return SkillOutput{Turns: 7}, wantErr
	}
	samples := captureExecSinks(svc)

	_, err := svc.dispatchSkill(context.Background(), "chapter-rewrite", SkillInput{
		NovelID: 7, RunID: "run-2", MaxTurns: 18,
	})
	require.ErrorIs(t, err, wantErr, "派发结果语义零变化")

	require.Len(t, *samples, 1)
	got := (*samples)[0]
	require.Equal(t, activity.ExecStatusError, got.Status)
	require.Equal(t, 7, got.Turns, "失败也应记已消耗轮数（瓶颈归因关键数据）")
}

func TestDispatchSkillRecordsTrippedStatAndSkipsRunner(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	calls := countSkillCalls(svc)
	samples := captureExecSinks(svc)
	forceProviderTrip(t)

	_, err := svc.dispatchSkill(context.Background(), "signability-audit", SkillInput{
		NovelID: 7, RunID: "run-3", MaxTurns: 24,
	})
	require.ErrorIs(t, err, errProviderTripped)
	require.Zero(t, *calls, "熔断必须零 agent 派发")

	require.Len(t, *samples, 1)
	got := (*samples)[0]
	require.Equal(t, activity.ExecStatusTripped, got.Status)
	require.Equal(t, 0, got.Turns)
	require.Equal(t, 24, got.MaxTurns)
}

// TestExecStatSinkPanicNeverAffectsDispatch 零行为变化：观测自身 panic 被吞掉，
// 派发结果逐字不受影响（观测是纯加法）。
func TestExecStatSinkPanicNeverAffectsDispatch(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "ok", Turns: 3}, nil
	}
	svc.SetExecStatSink(func(context.Context, activity.ExecStat) { panic("sink boom") })

	out, err := svc.dispatchSkill(context.Background(), "signability-audit", SkillInput{NovelID: 1, MaxTurns: 8})
	require.NoError(t, err, "观测 panic 不得影响派发结果")
	require.Equal(t, "ok", out.Text)
	require.Equal(t, 3, out.Turns)
}

// TestDispatchSkillDefaultSinkWritesExecRunStats 默认落库口走 activity 同族新表
// exec_run_stats（feature_usage_daily 同族，纯本地）。
func TestDispatchSkillDefaultSinkWritesExecRunStats(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&activity.ExecStat{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "ok", Turns: 2}, nil
	}

	_, err := svc.dispatchSkill(context.Background(), "consistency-patrol", SkillInput{
		NovelID: 9, RunID: "run-4", MaxTurns: 8,
	})
	require.NoError(t, err)

	var rows []activity.ExecStat
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "consistency-patrol", rows[0].Skill)
	require.Equal(t, "run-4", rows[0].RunID)
	require.Equal(t, 2, rows[0].Turns)
	require.Equal(t, activity.ExecStatusOK, rows[0].Status)
}

// TestRecordExecStatWithoutSinkIsNoop 未注入观测口时静默跳过（零行为变化）。
func TestRecordExecStatWithoutSinkIsNoop(t *testing.T) {
	svc := &Service{logger: slog.Default()}
	require.NotPanics(t, func() {
		svc.recordExecStat(context.Background(), activity.ExecStat{Skill: "x"})
	})
}
