package app

// rhythm_inject_test.go —— P2-2 平台节奏模板写章注入：写章意图 + 平台 profile 指名
// 模板才注入；非写章/斜杠/未配 profile/未指名模板 = 空串（缺省不模板）。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/quality"
)

func TestBuildRhythmInject(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)
	qs := quality.NewStore(app.db, app.logger)

	// 未配 profile：写章意图也不注入（缺省不模板）。
	block, err := app.buildRhythmInject(ctx, n.ID, "帮我写第2章，接着上文续写")
	require.NoError(t, err)
	require.Empty(t, block, "未配平台 profile 应零注入")

	// 配七猫 profile：写章意图 → 注入节奏模板块。
	require.NoError(t, qs.SaveCheckerConfig(ctx, &quality.CheckerConfig{
		NovelID: n.ID, PlatformProfile: "qimao",
	}))
	block, err = app.buildRhythmInject(ctx, n.ID, "帮我写第2章，接着上文续写")
	require.NoError(t, err)
	require.NotEmpty(t, block)
	require.Contains(t, block, "<system-reminder>")
	require.Contains(t, block, "【平台节奏模板】")
	require.Contains(t, block, "打脸")

	// 非写章意图 / 斜杠命令：不注入。
	block, err = app.buildRhythmInject(ctx, n.ID, "我们聊聊主角的人设吧")
	require.NoError(t, err)
	require.Empty(t, block)
	block, err = app.buildRhythmInject(ctx, n.ID, "/scene-beats 写一段打戏")
	require.NoError(t, err)
	require.Empty(t, block)
}
