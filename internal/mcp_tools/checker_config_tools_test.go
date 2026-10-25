package mcp_tools

// checker_config_tools_test.go —— 题材卡机制化（R2b-3）工具闭环：
// 局部覆盖语义、显式 false 落库（零值跳写坑）、契约拒收不落库、缺省口径返回。

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openCheckerConfigToolDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&quality.CheckerConfig{}))
	return db
}

func checkerConfigTc(db *gorm.DB) ToolContext {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ToolContext{DB: db, NovelID: 1, Logger: logger}
}

// TestCheckerConfigToolsClosedLoop 配置→查看闭环：局部覆盖、显式 false 落库、
// 空数组清空、契约拒收不落库、缺省返回。
func TestCheckerConfigToolsClosedLoop(t *testing.T) {
	db := openCheckerConfigToolDB(t)
	tc := checkerConfigTc(db)
	ctx := context.Background()
	up := &UpsertCheckerConfigTool{}
	get := &GetCheckerConfigTool{}

	// 缺省查看：未配置=现行默认。
	res, err := get.Execute(ctx, &GetCheckerConfigArgs{}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, true, res.Data["tail_hook_required"], "缺省钩子必需")
	require.Equal(t, 0, res.Data["window_n"])

	// 首次配置：全字段。
	f := false
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{
		BeatDensityMin:   ptrFloat(1.5),
		TailHookRequired: &f,
		WindowN:          ptrInt(3),
		TabooList:        []string{"虐主", "破墙"},
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, false, res.Data["tail_hook_required"], "显式 false 必须落库（零值跳写坑）")

	// 局部覆盖：只改 window_n，其余保持现值。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{WindowN: ptrInt(5)}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, false, res.Data["tail_hook_required"], "未给字段保持现值")
	require.Equal(t, 5, res.Data["window_n"])
	require.Equal(t, []string{"虐主", "破墙"}, res.Data["taboo_list"])

	// 空数组=清空清单。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{TabooList: []string{}}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Empty(t, res.Data["taboo_list"], "空数组=清空")

	// 契约拒收：负数不落库（业务拒绝 Success=false，不返回 error）。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{WindowN: ptrInt(-1)}, tc)
	require.NoError(t, err)
	require.False(t, res.Success)
	res, err = get.Execute(ctx, &GetCheckerConfigArgs{}, tc)
	require.NoError(t, err)
	require.Equal(t, 5, res.Data["window_n"], "拒收后保持原值")

	// 数据形状：get 返回解析后口径。
	require.Equal(t, 1.5, res.Data["beat_density_min"])
}

// TestCheckerConfigToolsIsolated 跨书隔离：工具只读写本书配置。
func TestCheckerConfigToolsIsolated(t *testing.T) {
	db := openCheckerConfigToolDB(t)
	tc1 := checkerConfigTc(db)
	tc2 := checkerConfigTc(db)
	tc2.NovelID = 2
	ctx := context.Background()

	_, err := (&UpsertCheckerConfigTool{}).Execute(ctx, &UpsertCheckerConfigArgs{WindowN: ptrInt(4)}, tc1)
	require.NoError(t, err)
	res, err := (&GetCheckerConfigTool{}).Execute(ctx, &GetCheckerConfigArgs{}, tc2)
	require.NoError(t, err)
	require.Equal(t, 0, res.Data["window_n"], "跨书隔离")
}

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }
