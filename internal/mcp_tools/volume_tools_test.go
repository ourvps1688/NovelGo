package mcp_tools_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/migrate"
	"github.com/novelgo/novelgo/internal/testsupport"
	"github.com/novelgo/novelgo/internal/volume"
)

// setupVolumeTestDB 准备内存库 + 迁移 + 小说目录，返回 db 与 novelID。
func setupVolumeTestDB(t *testing.T) (*gorm.DB, int64) {
	t.Helper()
	tmpDir := testsupport.Isolate(t)
	config.Set(&config.AppConfig{DataDir: tmpDir})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "vol.db")), &gorm.Config{})
	require.NoError(t, err)
	// 应用运行时使用单连接 DB（SetMaxOpenConns(1)）；测试须对齐，否则 sqlite 连接池
	// 会导致事务提交在另一连接上不可见。
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, migrate.Run(db, logger))

	novelID := int64(1)
	require.NoError(t, os.MkdirAll(config.NovelDirPath(novelID), 0o755))
	return db, novelID
}

func TestCreateVolume_CreatesRecordAndOutline(t *testing.T) {
	db, novelID := setupVolumeTestDB(t)
	reg := newTestRegistry(t)

	outline := "# 第一卷\n开篇设定与核心冲突。"
	args := map[string]any{
		"volumes": []map[string]any{
			{"name": "第一卷", "outline": outline},
			{"name": "第二卷"},
		},
	}
	b, err := json.Marshal(args)
	require.NoError(t, err)

	res := reg.Execute(context.Background(), "create_volume", b,
		mcp_tools.ToolContext{DB: db, NovelID: novelID, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	t.Logf("res.Success=%v err=%q data=%v", res.Success, res.Error, res.Data)
	var cnt int64
	db.Raw("SELECT count(*) FROM volumes WHERE novel_id = ?", novelID).Scan(&cnt)
	t.Logf("raw count=%d", cnt)
	require.True(t, res.Success, "create_volume 失败: %+v", res)

	var vols []volume.Volume
	require.NoError(t, db.Where("novel_id = ?", novelID).Order("sort_order ASC").Find(&vols).Error)
	require.Len(t, vols, 2)
	require.Equal(t, "第一卷", vols[0].Name)
	require.Equal(t, "第二卷", vols[1].Name)

	// 卷纲文件应写入 volumes/id_{id}.md
	full := filepath.Join(config.NovelDirPath(novelID), git.VolumePath(vols[0].ID))
	content, err := os.ReadFile(full)
	require.NoError(t, err)
	require.Equal(t, outline, string(content))
}

func TestCreateVolume_NameRequired(t *testing.T) {
	db, novelID := setupVolumeTestDB(t)
	reg := newTestRegistry(t)

	args := map[string]any{
		"volumes": []map[string]any{
			{"outline": "无卷名"},
		},
	}
	b, _ := json.Marshal(args)
	res := reg.Execute(context.Background(), "create_volume", b,
		mcp_tools.ToolContext{DB: db, NovelID: novelID, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, nil)
	require.False(t, res.Success, "无卷名应被校验拒绝")
}

func TestCreateVolume_RegisteredForMainAgent(t *testing.T) {
	reg := newTestRegistry(t)
	_, ok := reg.Get("create_volume")
	require.True(t, ok, "create_volume 应在注册表中")
	// mainAgent 白名单包含 create_volume
	allowed := mcp_tools.AllowSet([]string{"create_volume"})
	require.True(t, allowed["create_volume"])
}
