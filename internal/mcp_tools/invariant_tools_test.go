package mcp_tools

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/structure"
	"github.com/stretchr/testify/require"
)

func openInvariantToolDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&structure.PinnedInvariant{}))
	return db
}

func invariantTc(db *gorm.DB) ToolContext {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ToolContext{DB: db, NovelID: 1, Logger: logger}
}

// TestPinnedInvariantToolsClosedLoop 立→列工具闭环：落库、可筛选、同声明覆盖不重复。
func TestPinnedInvariantToolsClosedLoop(t *testing.T) {
	db := openInvariantToolDB(t)
	tc := invariantTc(db)
	ctx := context.Background()

	up := &UpsertPinnedInvariantTool{}
	res, err := up.Execute(ctx, &UpsertPinnedInvariantArgs{
		Kind:      structure.KindAliveBefore,
		Statement: "主角第 50 章前不死",
		Params:    `{"character":"主角","before_reading_number":50}`,
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success, "合法声明应落库")

	// 同声明覆盖（confirmed 置位，不重复落行）。
	res, err = up.Execute(ctx, &UpsertPinnedInvariantArgs{
		Kind:      structure.KindAliveBefore,
		Statement: "主角第 50 章前不死",
		Params:    `{"character":"主角","before_reading_number":60}`,
		Confirmed: true,
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)

	_, err = up.Execute(ctx, &UpsertPinnedInvariantArgs{
		Kind: structure.KindCapabilityCeiling, Statement: "力量体系上限 9000",
		Params: `{"character":"主角","ceiling":"9000"}`, Confirmed: true,
	}, tc)
	require.NoError(t, err)

	list := &ListPinnedInvariantsTool{}
	res, err = list.Execute(ctx, &ListPinnedInvariantsArgs{}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, 2, res.Data["count"], "同声明覆盖，共 2 条")

	res, err = list.Execute(ctx, &ListPinnedInvariantsArgs{Kind: structure.KindCapabilityCeiling}, tc)
	require.NoError(t, err)
	require.Equal(t, 1, res.Data["count"], "按 kind 筛选")
}

// TestPinnedInvariantToolBusinessReject 契约拒收走业务错误（Success=false，可修正重试）。
func TestPinnedInvariantToolBusinessReject(t *testing.T) {
	db := openInvariantToolDB(t)
	tc := invariantTc(db)

	up := &UpsertPinnedInvariantTool{}
	res, err := up.Execute(context.Background(), &UpsertPinnedInvariantArgs{
		Kind: structure.KindAliveBefore, Statement: "缺参数的声明",
	}, tc)
	require.NoError(t, err, "契约拒收是业务错误不是系统错误")
	require.False(t, res.Success)
	require.NotEmpty(t, res.Error)

	// 拒收不落库。
	list := &ListPinnedInvariantsTool{}
	lres, err := list.Execute(context.Background(), &ListPinnedInvariantsArgs{}, tc)
	require.NoError(t, err)
	require.Equal(t, 0, lres.Data["count"])
}

// TestFormatPinnedInvariant 渲染单源：声明原文 + 参数 + 确认标记。
func TestFormatPinnedInvariant(t *testing.T) {
	p := &structure.PinnedInvariant{Kind: structure.KindAliveBefore,
		Statement: "主角第 50 章前不死", ParamsJSON: `{"character":"主角","before_reading_number":50}`}
	require.Contains(t, FormatPinnedInvariant(p), "[character_alive_before][未确认] 主角第 50 章前不死")
	p.Confirmed = true
	require.Contains(t, FormatPinnedInvariant(p), "[已确认]")
}
