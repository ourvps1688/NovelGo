package app

// #118 契约：主 chat 单轮预算（MaxTurns 分档 + 历史 tool 裁剪）与
// 超时结局事件的「未取消 ctx」接线。静态锁防回潮。

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatMaxTurnsForWritingTurn(t *testing.T) {
	assert.Equal(t, writingTurnMaxTurns, chatMaxTurnsFor([]string{"next"}))
	assert.Equal(t, writingTurnMaxTurns, chatMaxTurnsFor([]string{"next", "reader-immersion"}))
	assert.Equal(t, chatTurnMaxTurns, chatMaxTurnsFor(nil))
	assert.Equal(t, chatTurnMaxTurns, chatMaxTurnsFor([]string{"character-design"}))
	// 写章要跑写前对齐+任务卡+大纲+审批+正文，不能压到 chapterci 的 12 轮量级。
	assert.Greater(t, writingTurnMaxTurns, 20)
	assert.Less(t, writingTurnMaxTurns, 100, "恒 100 轮不得回潮")
}

// #119 P0：写章轮预算必须与普通轮分档，且斜杠 /next 同样命中。
func TestChatTimeoutForWritingTurn(t *testing.T) {
	for _, msg := range []string{
		"继续撰写第五章", "撰写下一章", "写下一章", "续写", "/next", "/next 写下一章",
	} {
		assert.Equal(t, writingChatTimeout, chatTimeoutForMessage(msg), "写章轮应给 60 分钟: %s", msg)
	}
	for _, msg := range []string{"你好", "帮我看看第四章", "总结一下", ""} {
		assert.Equal(t, mainChatTimeout, chatTimeoutForMessage(msg), "普通轮保持 30 分钟: %s", msg)
	}
	assert.Equal(t, 60*time.Minute, writingChatTimeout)
	assert.Equal(t, 30*time.Minute, mainChatTimeout)
	assert.Greater(t, writingChatTimeout, mainChatTimeout)
}

func TestClipHistoricalToolContent(t *testing.T) {
	long := strings.Repeat("甲", toolHistoryClipRunes+200)
	api := map[string]any{"role": "tool", "content": long}

	assert.False(t, clipHistoricalToolContent(api, false), "最近 N 条结果逐字不动")
	assert.Equal(t, long, api["content"])

	assert.True(t, clipHistoricalToolContent(api, true))
	got, _ := api["content"].(string)
	assert.Contains(t, got, toolHistoryClipNote)
	// TruncateRunes 超长时自带 1 个省略号，故允许 +1。
	assert.LessOrEqual(t, len([]rune(got)), toolHistoryClipRunes+len([]rune(toolHistoryClipNote))+1)

	short := map[string]any{"content": "短"}
	assert.False(t, clipHistoricalToolContent(short, true))
	assert.Equal(t, "短", short["content"])

	// 非字符串 content 不应 panic，也不应被改写。
	other := map[string]any{"content": 42}
	assert.False(t, clipHistoricalToolContent(other, true))
	assert.Equal(t, 42, other["content"])
}

func TestChatTimeoutOutcomeWiring(t *testing.T) {
	b, err := os.ReadFile("chat.go")
	require.NoError(t, err)
	s := string(b)
	for _, want := range []string{
		"context.WithoutCancel(ctx)", "outcomeCtx", "cancelOutcome",
		"chatMaxTurnsFor(activeSkills)", "clipHistoricalToolContent",
		"toolTotal", "toolSeen", "final_text_runes", "timedOut",
		// #119 P0：写章档超时 + auto-commit 不再复用过期 ctx。
		"chatTimeoutForMessage(input.Message)", "commitCtx", "cancelCommit",
	} {
		assert.Contains(t, s, want, "chat.go 缺少接线 "+want)
	}
	// 结局事件必须不再复用可能已过期的 ctx（00:38 因此 DB 无痕）。
	assert.NotContains(t, s, "a.session.AppendMessage(ctx, &session.Message{")
	assert.NotContains(t, s, `wails.EventsEmit(ctx, "chat:turn_outcome"`)
	assert.NotContains(t, s, "MaxTurns:        100")
	// 裁剪后必须重算 token，不能沿用持久化原值高估预算。
	assert.Contains(t, s, "n == 0 || clipped")

	policy, err := os.ReadFile("chat_policy.go")
	require.NoError(t, err)
	assert.Contains(t, string(policy), "text.TruncateRunes", "截断必须复用 #117 P0 单源")
	for _, banned := range []string{
		"StartIssueFixTickets", "autoFix", "RunSubAgent", "Upload",
	} {
		assert.NotContains(t, string(policy), banned)
	}
}
