package mcp_tools

// power_level_tools_test.go —— 境界等级表工具（R2b-6）接线闭环：
// 立/同名覆盖/查看、契约拒收、跨书隔离。

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPowerLevelToolsRoundTrip 等级表工具闭环：立两条+同名覆盖；弱→强查看；
// 契约拒收（名称空/rank<1）；跨书隔离。
func TestPowerLevelToolsRoundTrip(t *testing.T) {
	db := openPlanCheckToolDB(t)
	tc := planCheckTc(db)

	// 立两条 + 同名覆盖（覆盖数值锚）。
	res, err := (&UpsertPowerLevelTool{}).Execute(context.Background(), &UpsertPowerLevelArgs{Name: "元婴期", Rank: 5, Description: "元神出窍"}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	res, err = (&UpsertPowerLevelTool{}).Execute(context.Background(), &UpsertPowerLevelArgs{Name: "金丹期", Rank: 3}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	res, err = (&UpsertPowerLevelTool{}).Execute(context.Background(), &UpsertPowerLevelArgs{Name: "金丹期", Rank: 4}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.Equal(t, 4, res.Data["rank"], "同名覆盖数值锚")

	// 查看：弱→强排序、覆盖生效、不新增行。
	res, err = (&ListPowerLevelsTool{}).Execute(context.Background(), &ListPowerLevelsArgs{}, tc)
	require.NoError(t, err)
	levels, _ := res.Data["power_levels"].([]map[string]any)
	require.Len(t, levels, 2)
	require.Equal(t, "金丹期", levels[0]["name"])
	require.Equal(t, 4, levels[0]["rank"])
	require.Equal(t, "元婴期", levels[1]["name"])

	// 契约拒收：名称空 / rank<1（业务拒绝 Success=false，不计入中断）。
	bad, err := (&UpsertPowerLevelTool{}).Execute(context.Background(), &UpsertPowerLevelArgs{Name: "  ", Rank: 3}, tc)
	require.NoError(t, err)
	require.False(t, bad.Success)
	bad, err = (&UpsertPowerLevelTool{}).Execute(context.Background(), &UpsertPowerLevelArgs{Name: "化神期", Rank: 0}, tc)
	require.NoError(t, err)
	require.False(t, bad.Success)

	// 跨书隔离。
	other := ToolContext{DB: db, NovelID: 2, Logger: tc.Logger}
	res, err = (&ListPowerLevelsTool{}).Execute(context.Background(), &ListPowerLevelsArgs{}, other)
	require.NoError(t, err)
	levels, _ = res.Data["power_levels"].([]map[string]any)
	require.Empty(t, levels)
}

// TestPowerLevelToolsRegistered 双工具注册进注册表（主 Agent 可派发）。
func TestPowerLevelToolsRegistered(t *testing.T) {
	r := NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterPowerLevelTools(r)
	_, ok := r.Get("upsert_power_level")
	require.True(t, ok)
	_, ok = r.Get("list_power_levels")
	require.True(t, ok)
}
