package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/event"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/session"
)

func TestRecordTokenUsage(t *testing.T) {
	a := newCompressTestAgent(t, "usage")
	require.NoError(t, a.db.AutoMigrate(&activity.DailyLLMUsage{}))
	require.NoError(t, a.session.UpdateSessionUsage(context.Background(), "usage", `{"total_tokens":9999,"prompt_cache_hit_tokens":9999}`))
	opts := RunOptions{SessionID: "usage", ProviderName: "provider", Model: &llm.ModelInfo{ID: "model"}, AgentType: "main"}
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":100,"completion_tokens":20,"total_tokens":125,"prompt_cache_hit_tokens":40,"prompt_cache_miss_tokens":60,"completion_tokens_details":{"reasoning_tokens":5}}`), &raw))
	a.recordTokenUsage(context.Background(), raw, opts)
	a.recordTokenUsage(context.Background(), raw, opts)
	var row activity.DailyLLMUsage
	require.NoError(t, a.db.First(&row).Error)
	require.EqualValues(t, 2, row.UsageCount)
	require.Equal(t, activity.TokenUsage{InputTokens: 200, OutputTokens: 40, TotalTokens: 250, CacheHitTokens: 80, CacheMissTokens: 120, ReasoningTokens: 10}, row.TokenUsage)
	require.Equal(t, "chat", row.Purpose)
	require.Equal(t, float64(40), raw["prompt_cache_hit_tokens"])

	for _, next := range []RunOptions{
		{ProviderName: "other-provider", Model: opts.Model, AgentType: "main"},
		{ProviderName: opts.ProviderName, Model: &llm.ModelInfo{ID: "other-model"}, AgentType: "main"},
		{ProviderName: opts.ProviderName, Model: opts.Model, AgentType: "review"},
	} {
		a.recordTokenUsage(context.Background(), raw, next)
	}
	var rows []activity.DailyLLMUsage
	require.NoError(t, a.db.Find(&rows).Error)
	require.Len(t, rows, 4)
	for _, item := range rows {
		if item.Provider == "provider" && item.Model == "model" && item.Purpose == "chat" {
			continue
		}
		require.EqualValues(t, 1, item.UsageCount)
		require.EqualValues(t, 125, item.TotalTokens)
	}
}

func TestRecordTokenUsageMissingFieldsAndFailure(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		count     int64
		want      activity.TokenUsage
	}{
		{"missing", `null`, 0, activity.TokenUsage{}},
		{"empty", `{}`, 0, activity.TokenUsage{}},
		{"zero", `{"total_tokens":0}`, 1, activity.TokenUsage{}},
		{"derived total", `{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":5}}`, 1, activity.TokenUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CacheHitTokens: 40, ReasoningTokens: 5}},
		{"partial", `{"prompt_tokens":100}`, 1, activity.TokenUsage{InputTokens: 100}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := newCompressTestAgent(t, "usage")
			require.NoError(t, a.db.AutoMigrate(&activity.DailyLLMUsage{}))
			var raw map[string]any
			require.NoError(t, json.Unmarshal([]byte(tt.raw), &raw))
			opts := RunOptions{ProviderName: "provider", Model: &llm.ModelInfo{ID: "model"}, AgentType: "main"}
			a.recordTokenUsage(context.Background(), raw, opts)
			var rows []activity.DailyLLMUsage
			require.NoError(t, a.db.Find(&rows).Error)
			if tt.count == 0 {
				require.Empty(t, rows)
				return
			}
			require.Len(t, rows, 1)
			require.Equal(t, tt.count, rows[0].UsageCount)
			require.Equal(t, tt.want, rows[0].TokenUsage)
			require.NoError(t, a.db.Exec(`CREATE TRIGGER reject_usage BEFORE INSERT ON llm_usage_daily
				BEGIN SELECT RAISE(ABORT, 'statistics unavailable'); END`).Error)
			require.NotPanics(t, func() { a.recordTokenUsage(context.Background(), raw, opts) })
		})
	}
}

// #121：后台子 Agent（app.backgroundRunOptions）没有 SessionID，旧实现对空串
// 查 sessions 反复打 "record not found"；跳过 session 级读写，但 purpose 级
// 用量仍落 llm_usage_daily，主 chat 的 session.Usage 口径逐字不变。
func TestUpdateUsageSkipsSessionlessBackgroundAgent(t *testing.T) {
	a := newCompressTestAgent(t, "usage-main")
	a.emitter = event.NopEmitter{}
	require.NoError(t, a.db.AutoMigrate(&activity.DailyLLMUsage{}))
	var logs bytes.Buffer
	a.logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":100,"completion_tokens":20,"total_tokens":125}`), &raw))
	running := map[string]int{"system": 10, "user": 20, "assistant": 30, "tool": 40}

	bg := RunOptions{ProviderName: "provider", Model: &llm.ModelInfo{ID: "model", ContextWindow: 1000}, AgentType: "chapterci"}
	require.NotPanics(t, func() { a.updateUsage(ctx, raw, running, bg) })
	require.NotContains(t, logs.String(), "持久化 usage 失败")
	var sessions []session.Session
	require.NoError(t, a.db.Find(&sessions).Error)
	require.Len(t, sessions, 1, "零会话归属不得造空串 session 行")
	require.Equal(t, "usage-main", sessions[0].SessionID)
	var daily activity.DailyLLMUsage
	require.NoError(t, a.db.Where("purpose = ?", "chapterci").First(&daily).Error)
	require.EqualValues(t, 125, daily.TotalTokens, "purpose 级用量仍是真相源")

	main := RunOptions{SessionID: "usage-main", TurnID: 3, ProviderName: "provider", Model: &llm.ModelInfo{ID: "model", ContextWindow: 1000}, AgentType: "main"}
	a.updateUsage(ctx, raw, running, main)
	sess, err := a.session.GetSession(ctx, "usage-main")
	require.NoError(t, err)
	require.Contains(t, sess.Usage, "usage_ratio", "主 chat 的 session 级用量不受影响")

	src, err := os.ReadFile("tokens.go")
	require.NoError(t, err)
	require.Contains(t, string(src), `hasSessionScope := opts.SessionID != ""`)
	require.Contains(t, string(src), "if hasSessionScope {")
}
