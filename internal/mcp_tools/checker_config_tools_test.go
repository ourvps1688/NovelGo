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

	// 首次配置：全字段（含 P0-0 开篇体检关键词）。
	f := false
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{
		BeatDensityMin:   ptrFloat(1.5),
		TailHookRequired: &f,
		WindowN:          ptrInt(3),
		TabooList:        []string{"虐主", "破墙"},
		PremiseKeywords:  []string{"灾星"},
		CheatKeywords:    []string{"吞星炼命"},
		ProtagonistNames: []string{"林澈", "阿澈"},
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
	require.Equal(t, []string{"灾星"}, res.Data["premise_keywords"], "开篇关键词未给=保持现值")
	require.Equal(t, []string{"吞星炼命"}, res.Data["cheat_keywords"])
	require.Equal(t, []string{"林澈", "阿澈"}, res.Data["protagonist_names"])

	// 空数组=清空清单。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{TabooList: []string{}, PremiseKeywords: []string{}}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Empty(t, res.Data["taboo_list"], "空数组=清空")
	require.Empty(t, res.Data["premise_keywords"], "空数组=清空（开篇关键词同语义）")
	require.Equal(t, []string{"吞星炼命"}, res.Data["cheat_keywords"], "同批未给字段仍保持现值")

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

// TestCheckerConfigToolsPlatformProfile platform_profile 语义（P0-1）：
// nil=保持、空串=清除、别名归一落库、未注册名业务拒收不落库、get 回显。
func TestCheckerConfigToolsPlatformProfile(t *testing.T) {
	db := openCheckerConfigToolDB(t)
	tc := checkerConfigTc(db)
	ctx := context.Background()
	up := &UpsertCheckerConfigTool{}
	get := &GetCheckerConfigTool{}

	// 缺省：未配置为空。
	res, err := get.Execute(ctx, &GetCheckerConfigArgs{}, tc)
	require.NoError(t, err)
	require.Equal(t, "", res.Data["platform_profile"])

	// 设置（别名 "七猫" 归一 "qimao" 落库）。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{PlatformProfile: ptrStr("七猫")}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, "qimao", res.Data["platform_profile"], "别名归一为注册名")
	require.Equal(t, 1.0, res.Data["beat_density_effective"], "profile 密度下限经 EffectiveBeatDensityMin 回显")

	// nil=保持现值。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{WindowN: ptrInt(2)}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, "qimao", res.Data["platform_profile"], "未给字段保持现值")
	require.Equal(t, 2, res.Data["window_n"])

	// 未注册名：业务拒收（Success=false），不落库。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{PlatformProfile: ptrStr("fanqie")}, tc)
	require.NoError(t, err)
	require.False(t, res.Success)
	res, err = get.Execute(ctx, &GetCheckerConfigArgs{}, tc)
	require.NoError(t, err)
	require.Equal(t, "qimao", res.Data["platform_profile"], "拒收后保持原值")

	// 空串=清除（平台三查回到不武装）。
	res, err = up.Execute(ctx, &UpsertCheckerConfigArgs{PlatformProfile: ptrStr("")}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, "", res.Data["platform_profile"])
	require.Equal(t, 0.0, res.Data["beat_density_effective"], "清除后密度下限回落 0（不限）")
}

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }
func ptrStr(v string) *string     { return &v }
