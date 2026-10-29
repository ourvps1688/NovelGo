package app

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/git"
)

func TestIsChapterWritingIntent(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"帮我写第5章", true},
		{"续写这一段", true},
		{"接着上文写下去", true},
		{"写下一章的开头", true},
		{"展开本章的冲突", true},
		{"把这章正文润色一下", true},
		{"继续往下写", true},
		{"我们来聊聊主角的人设吧", false},
		{"帮我列个大纲", false},
		{"这本书的世界观设定是什么", false},
		{"hi", false},
		{"", false},
	}
	for _, c := range cases {
		require.Equalf(t, c.want, isChapterWritingIntent(c.msg), "msg=%q", c.msg)
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"你好世界", 10, "你好世界"},
		{"abcdefghij", 5, "abcde"},
		{"一二三四五六七八九十", 6, "一二三四五六"}, // 小预算不做截断注记
		{"一二三四五六七八九十一二", 10, "一二三四（已截断）"},
		{"abcd", 4, "abcd"},
		{"abcd", 0, ""},
		{"", 5, ""},
	}
	for _, c := range cases {
		require.Equalf(t, c.want, truncateRunes(c.in, c.max), "in=%q max=%d", c.in, c.max)
	}
}

// TestBuildPreContextInject_HasRecentChapter 验证：写章意图 + 存在可读前文 → 注入含标题与正文的前情块。
func TestBuildPreContextInject_HasRecentChapter(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)

	ch := createTestChapter(t, app, n.ID)
	require.NoError(t, git.WriteFile(n.ID, git.ChapterPath(ch.ID), "林惊羽拔出长剑，寒光映月。这是他与魔教决裂的一夜。"))

	block, err := app.buildPreContextInject(ctx, n.ID, "帮我写第2章，接着上文续写")
	require.NoError(t, err)
	require.NotEmpty(t, block, "应注入前情块")
	require.Contains(t, block, "前情回顾")
	require.Contains(t, block, ch.Title, "前情块应包含最新章标题")
	require.Contains(t, block, "林惊羽拔出长剑", "前情块应包含正文片段")
	require.Contains(t, block, "<system-reminder>")
}

// TestBuildPreContextInject_NonWritingMessage 验证：非写章意图不注入（避免脑暴/大纲等场景污染上下文）。
func TestBuildPreContextInject_NonWritingMessage(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)

	ch := createTestChapter(t, app, n.ID)
	require.NoError(t, git.WriteFile(n.ID, git.ChapterPath(ch.ID), "一些已有正文"))

	block, err := app.buildPreContextInject(ctx, n.ID, "我们来讨论一下世界观设定")
	require.NoError(t, err)
	require.Empty(t, block, "非写章意图不应注入前情")
}

// TestBuildPreContextInject_SlashCommand 验证：斜杠命令不走前情通道（与 autoInject 同口径）。
func TestBuildPreContextInject_SlashCommand(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)

	ch := createTestChapter(t, app, n.ID)
	require.NoError(t, git.WriteFile(n.ID, git.ChapterPath(ch.ID), "一些已有正文"))

	block, err := app.buildPreContextInject(ctx, n.ID, "/next 写下一章")
	require.NoError(t, err)
	require.Empty(t, block, "斜杠命令不应自动注入前情")
}

// TestBuildPreContextInject_NoChapter 验证：尚无任何章节时降级为空（不阻塞主流程）。
func TestBuildPreContextInject_NoChapter(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)

	block, err := app.buildPreContextInject(ctx, n.ID, "帮我写第1章")
	require.NoError(t, err)
	require.Empty(t, block, "无前文应降级为空")
}

// TestBuildPreContextInject_BudgetTruncation 验证：超长正文按 rune 预算截断并标注（已截断）。
func TestBuildPreContextInject_BudgetTruncation(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)

	ch := createTestChapter(t, app, n.ID)
	long := strings.Repeat("章", preContextBudgetRunes+500)
	require.NoError(t, git.WriteFile(n.ID, git.ChapterPath(ch.ID), long))

	block, err := app.buildPreContextInject(ctx, n.ID, "续写下一章")
	require.NoError(t, err)
	require.NotEmpty(t, block)
	require.Contains(t, block, "（已截断）", "超长前文应被截断并标注")
}
