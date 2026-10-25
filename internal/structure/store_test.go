package structure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&PinnedInvariant{}))
	return db
}

// TestStoreUpsertIdempotent 同声明重复 upsert 覆盖（(novel_id, kind, statement) 唯一），
// 不同声明并存；契约拒收不落库。
func TestStoreUpsertIdempotent(t *testing.T) {
	db := newStoreTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	p := &PinnedInvariant{NovelID: 7, Kind: KindAliveBefore, Statement: "主角第 50 章前不死",
		ParamsJSON: `{"character":"主角","before_reading_number":50}`}
	require.NoError(t, s.Upsert(ctx, p))
	require.NotZero(t, p.ID)

	p2 := &PinnedInvariant{NovelID: 7, Kind: KindAliveBefore, Statement: "主角第 50 章前不死",
		ParamsJSON: `{"character":"主角","before_reading_number":60}`, Confirmed: true}
	require.NoError(t, s.Upsert(ctx, p2))
	rows, err := s.ListByNovel(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 1, "同声明覆盖不重复落行")
	require.Equal(t, 60, mustAliveBefore(t, rows[0].ParamsJSON).BeforeReadingNumber)
	require.True(t, rows[0].Confirmed)

	other := &PinnedInvariant{NovelID: 7, Kind: KindCapabilityCeiling, Statement: "力量体系上限 9000",
		ParamsJSON: `{"character":"主角","ceiling":"9000"}`}
	require.NoError(t, s.Upsert(ctx, other))
	rows, err = s.ListByNovel(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 2, "不同声明并存")
	require.Equal(t, int64(7), rows[0].NovelID)

	bad := &PinnedInvariant{NovelID: 7, Kind: "bogus", Statement: "x"}
	require.Error(t, s.Upsert(ctx, bad), "契约拒收不落库")
	rows, err = s.ListByNovel(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// 跨书隔离。
	rows, err = s.ListByNovel(ctx, 8)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func mustAliveBefore(t *testing.T, params string) AliveBeforeParams {
	t.Helper()
	p, ok := ParseAliveBeforeParams(params)
	require.True(t, ok)
	return p
}
