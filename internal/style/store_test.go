package style

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Sample{}, &Profile{}))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewStore(db, logger)
}

func TestProfileCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// empty list
	list, err := s.ListProfiles(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, list)

	// create
	p := &Profile{
		NovelID:     1,
		IsGlobal:    false,
		Name:        "古风雅致",
		Description: "适合历史正剧",
		Content:     "# 古风雅致\n## 句式特征\n...",
	}
	require.NoError(t, s.CreateProfile(ctx, p))
	require.NotZero(t, p.ID)

	// list gets it back, scoped by novel
	list, err = s.ListProfiles(ctx, 1)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "古风雅致", list[0].Name)
	assert.Equal(t, p.ID, list[0].ID)

	// other novel sees nothing
	other, err := s.ListProfiles(ctx, 2)
	require.NoError(t, err)
	assert.Empty(t, other)

	// delete
	require.NoError(t, s.DeleteProfile(ctx, p.ID))
	list, err = s.ListProfiles(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, list)
}
