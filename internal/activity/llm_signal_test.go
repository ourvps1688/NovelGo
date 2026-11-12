package activity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// #114 P2 供应商可观测：按天×provider×model×task_kind×signal 原子累加，
// 只记标识与计数、不记正文；空信号拒收、空维度归一、窗口外不进汇总。

func TestAddLLMSignalAccumulatesByDimensions(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&LLMSignalDaily{}))
	ctx := context.Background()

	s.AddLLMSignal(ctx, "deepseek", "deepseek-v4", "main", "empty_stream")
	s.AddLLMSignal(ctx, "deepseek", "deepseek-v4", "main", "empty_stream")
	s.AddLLMSignal(ctx, "deepseek", "deepseek-v4", "chapterci", "tool_args_invalid")
	s.AddLLMSignal(ctx, "", "", "", "breaker_open")              // 空维度归一为 unknown
	s.AddLLMSignal(ctx, "deepseek", "deepseek-v4", "main", "  ") // 空信号拒收

	var total int64
	require.NoError(t, db.Model(&LLMSignalDaily{}).Count(&total).Error)
	require.EqualValues(t, 3, total, "同键累加成一行；不同任务/信号各一行；空信号不落行")

	var row LLMSignalDaily
	require.NoError(t, db.Where("signal = ?", "empty_stream").First(&row).Error)
	require.EqualValues(t, 2, row.Count)
	require.Equal(t, "2027-01-01", row.Date)

	var unknown LLMSignalDaily
	require.NoError(t, db.Where("signal = ?", "breaker_open").First(&unknown).Error)
	require.Equal(t, "unknown", unknown.Provider)
	require.Equal(t, "unknown", unknown.Model)
	require.Equal(t, "unknown", unknown.TaskKind)
}

func TestSummarizeLLMSignalsGroupsByModelTaskAndSignal(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&LLMSignalDaily{}))
	ctx := context.Background()

	s.AddLLMSignal(ctx, "deepseek", "model-a", "main", "empty_stream")
	s.AddLLMSignal(ctx, "deepseek", "model-a", "main", "empty_stream")
	s.AddLLMSignal(ctx, "deepseek", "model-b", "review", "truncated_by_length")
	// 窗口外：不进近 30 天汇总。
	require.NoError(t, db.Create(&LLMSignalDaily{
		Date: "2026-01-01", Provider: "old", Model: "old",
		TaskKind: "old", Signal: "old", Count: 99,
	}).Error)

	stats, err := s.SummarizeLLMSignals(ctx, 30)
	require.NoError(t, err)
	require.Len(t, stats, 2, "按 provider×model×task_kind×signal 分组，窗口外不进")

	require.Equal(t, "deepseek", stats[0].Provider)
	require.Equal(t, "model-a", stats[0].Model)
	require.Equal(t, "main", stats[0].TaskKind)
	require.Equal(t, "empty_stream", stats[0].Signal)
	require.EqualValues(t, 2, stats[0].Count)
	require.Equal(t, "2027-01-01", stats[0].LastDate)

	require.Equal(t, "model-b", stats[1].Model)
	require.Equal(t, "review", stats[1].TaskKind)
	require.Equal(t, "truncated_by_length", stats[1].Signal)
	require.EqualValues(t, 1, stats[1].Count)
}

func TestSummarizeLLMSignalsEmptyTable(t *testing.T) {
	s, db := newTestStore(t)
	require.NoError(t, db.AutoMigrate(&LLMSignalDaily{}))
	stats, err := s.SummarizeLLMSignals(context.Background(), 7)
	require.NoError(t, err)
	require.Empty(t, stats, "旧版本/无信号时返回空表，前端静默降级")
}

func TestLLMSignalTableName(t *testing.T) {
	require.Equal(t, "llm_signal_daily", (LLMSignalDaily{}).TableName())
}
