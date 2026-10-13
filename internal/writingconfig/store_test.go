package writingconfig

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&WritingConfig{}))
	return db
}

func TestGet_Defaults(t *testing.T) {
	db := openDB(t)
	s := NewStore(db, slog.Default())
	cfg, err := s.Get(context.Background(), 99)
	require.NoError(t, err)
	require.Equal(t, int64(99), cfg.NovelID)
	require.True(t, cfg.AutoScoreEnabled)
	require.Equal(t, 60, cfg.ScoreThreshold)
	require.Equal(t, 3, cfg.MaxRetries)
}

func TestSave_Upsert(t *testing.T) {
	db := openDB(t)
	s := NewStore(db, slog.Default())
	ctx := context.Background()

	require.NoError(t, s.Save(ctx, &WritingConfig{NovelID: 7, AutoScoreEnabled: false, ScoreThreshold: 80, MaxRetries: 5}))

	cfg, err := s.Get(ctx, 7)
	require.NoError(t, err)
	require.False(t, cfg.AutoScoreEnabled)
	require.Equal(t, 80, cfg.ScoreThreshold)
	require.Equal(t, 5, cfg.MaxRetries)

	// upsert 覆盖
	require.NoError(t, s.Save(ctx, &WritingConfig{NovelID: 7, AutoScoreEnabled: true, ScoreThreshold: 70, MaxRetries: 2}))
	cfg, err = s.Get(ctx, 7)
	require.NoError(t, err)
	require.True(t, cfg.AutoScoreEnabled)
	require.Equal(t, 70, cfg.ScoreThreshold)
	require.Equal(t, 2, cfg.MaxRetries)
}

func TestSave_NormalizesZero(t *testing.T) {
	db := openDB(t)
	s := NewStore(db, slog.Default())
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, &WritingConfig{NovelID: 3, AutoScoreEnabled: true, ScoreThreshold: 0, MaxRetries: 0}))
	cfg, err := s.Get(ctx, 3)
	require.NoError(t, err)
	require.Equal(t, 60, cfg.ScoreThreshold)
	require.Equal(t, 3, cfg.MaxRetries)
}
