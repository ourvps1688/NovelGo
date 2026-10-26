package mcp_tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/mcp_tools"
)

func locationQueryFixture(t *testing.T) (*mcp_tools.Registry, mcp_tools.ToolContext) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&location.Location{}, &location.LocationRelation{}))
	parent := int64(1)
	for _, loc := range []location.Location{
		{ID: 1, NovelID: 1, Name: "黑铁城堡", LocationType: "城市"},
		{ID: 2, NovelID: 1, Name: "迷雾森林", LocationType: "森林"},
		{ID: 3, NovelID: 1, Name: "城堡地窖", LocationType: "洞穴", ParentLocationID: &parent},
		{ID: 4, NovelID: 2, Name: "其他小说地点", LocationType: "城市"},
	} {
		require.NoError(t, db.Create(&loc).Error)
	}
	require.NoError(t, db.Create(&location.LocationRelation{
		NovelID: 1, LocationA: 1, LocationB: 2, RelationType: "由山路连通",
	}).Error)
	return newTestRegistry(t), mcp_tools.ToolContext{DB: db, NovelID: 1}
}

// TestGetLocationsModeDefaultsToList 验收 #232：mode 缺省必须放行并走 list
// （与 get_characters 的 mode 口径一致）——查询模式类参数有天然缺省语义，
// 不得因 LLM 漏传而报「参数校验失败」；非法 mode 仍须拒绝。
func TestGetLocationsModeDefaultsToList(t *testing.T) {
	reg, tc := locationQueryFixture(t)
	for _, tt := range []struct {
		name string
		args string
	}{
		{"mode 缺省走 list", `{}`},
		{"显式 list", `{"mode":"list"}`},
		{"缺省 mode 带搜索", `{"search":"森林"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := reg.Execute(context.Background(), "get_locations", json.RawMessage(tt.args), tc, nil)
			require.True(t, result.Success, "%+v", result)
			require.NotContains(t, result.Error, "参数校验失败")
			items := result.Data["locations"].([]map[string]any)
			if tt.name == "缺省 mode 带搜索" {
				require.Len(t, items, 1)
				require.Equal(t, "迷雾森林", items[0]["name"])
				return
			}
			// list 只返回本书地点（3 个，不含其他小说的）。
			require.Len(t, items, 3)
		})
	}

	// 非法 mode 仍拒绝（oneof 边界不因缺省放行而失效）。
	bad := reg.Execute(context.Background(), "get_locations", json.RawMessage(`{"mode":"unknown"}`), tc, nil)
	require.False(t, bad.Success)
	require.Contains(t, bad.Error, "参数校验失败")
}

// TestGetLocationsDetailAndNetwork 验收 #232：mode 显式传值时三模式行为不变。
func TestGetLocationsDetailAndNetwork(t *testing.T) {
	reg, tc := locationQueryFixture(t)

	detail := reg.Execute(context.Background(), "get_locations",
		json.RawMessage(`{"mode":"detail","location_id":1}`), tc, nil)
	require.True(t, detail.Success, "%+v", detail)
	require.Contains(t, detail.Data["content"], "黑铁城堡")

	missing := reg.Execute(context.Background(), "get_locations",
		json.RawMessage(`{"mode":"detail"}`), tc, nil)
	require.False(t, missing.Success)
	require.Contains(t, missing.Error, "location_id")

	network := reg.Execute(context.Background(), "get_locations",
		json.RawMessage(`{"mode":"network"}`), tc, nil)
	require.True(t, network.Success, "%+v", network)
	require.Contains(t, network.Data["content"], "黑铁城堡")
}
