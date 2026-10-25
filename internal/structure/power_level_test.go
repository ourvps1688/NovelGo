package structure

// power_level_test.go —— 境界等级表（R2b-6）契约与仓储：数值锚校验、同名覆盖、
// 确定性顺序、跨书隔离、RankOfLevel 宁缺勿错。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openPowerLevelDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&PowerLevel{}))
	return db
}

// TestPowerLevelValidate 契约拒收：名称空 / 数值锚 <1；TrimSpace 归一。
func TestPowerLevelValidate(t *testing.T) {
	ok := PowerLevel{NovelID: 1, Name: " 金丹期 ", Rank: 3}
	require.NoError(t, ok.Validate())
	require.Equal(t, "金丹期", ok.Name)

	bad := []PowerLevel{
		{NovelID: 1, Name: "   ", Rank: 3},
		{NovelID: 1, Name: "金丹期", Rank: 0},
		{NovelID: 1, Name: "金丹期", Rank: -2},
	}
	for i := range bad {
		require.Error(t, bad[i].Validate(), "case %d 应拒收", i)
	}
}

// TestPowerLevelStoreRoundTrip 仓储闭环：立/同名覆盖/弱→强顺序/跨书隔离。
func TestPowerLevelStoreRoundTrip(t *testing.T) {
	db := openPowerLevelDB(t)
	store := NewStore(db)
	ctx := context.Background()

	require.NoError(t, store.UpsertPowerLevel(ctx, &PowerLevel{NovelID: 1, Name: "元婴期", Rank: 5}))
	require.NoError(t, store.UpsertPowerLevel(ctx, &PowerLevel{NovelID: 1, Name: "金丹期", Rank: 3, Description: "初结金丹"}))
	// 同名覆盖：数值锚更新、不新增行。
	require.NoError(t, store.UpsertPowerLevel(ctx, &PowerLevel{NovelID: 1, Name: "金丹期", Rank: 4}))

	rows, err := store.ListPowerLevels(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 2, "同名覆盖不新增行")
	require.Equal(t, "金丹期", rows[0].Name)
	require.Equal(t, 4, rows[0].Rank, "覆盖数值锚")
	require.Equal(t, "", rows[0].Description, "同名 upsert=整条覆盖（含描述）")
	require.Equal(t, "元婴期", rows[1].Name, "rank ASC（弱→强）")

	// 契约拒收不落库。
	require.Error(t, store.UpsertPowerLevel(ctx, &PowerLevel{NovelID: 1, Name: "", Rank: 3}))
	require.Error(t, store.UpsertPowerLevel(ctx, &PowerLevel{NovelID: 1, Name: "化神期", Rank: 0}))
	rows, err = store.ListPowerLevels(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// 跨书隔离。
	other, err := store.ListPowerLevels(ctx, 2)
	require.NoError(t, err)
	require.Empty(t, other)
}

// TestRankOfLevel 数值锚查找：精确名命中（TrimSpace）；未登记/空名 ok=false（宁缺勿错）。
func TestRankOfLevel(t *testing.T) {
	levels := []PowerLevel{{Name: "金丹期", Rank: 3}, {Name: "元婴期", Rank: 5}, {Name: "坏条目", Rank: 0}}

	rank, ok := RankOfLevel(levels, " 元婴期 ")
	require.True(t, ok)
	require.Equal(t, 5, rank)

	_, ok = RankOfLevel(levels, "化神期")
	require.False(t, ok, "未登记境界不猜")
	_, ok = RankOfLevel(levels, "")
	require.False(t, ok)
	_, ok = RankOfLevel(levels, "坏条目")
	require.False(t, ok, "非法条目（rank<1）不作数")
}
