package review

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openAttemptDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&QualityAttempt{}))
	return db
}

func TestAttempt_IncrementAndClear(t *testing.T) {
	db := openAttemptDB(t)
	s := NewAttemptStore(db, slog.Default())
	ctx := context.Background()

	// 第 1 次：从 0 → 1
	n, err := s.Increment(ctx, 1, "chapter:5", 30, "太短")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// 第 2 次：→ 2
	n, err = s.Increment(ctx, 1, "chapter:5", 35, "注水")
	require.NoError(t, err)
	require.Equal(t, 2, n)

	// 清理后回到 0
	require.NoError(t, s.Clear(ctx, 1, "chapter:5"))
	n, err = s.Increment(ctx, 1, "chapter:5", 40, "ok")
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
