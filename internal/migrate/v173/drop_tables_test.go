package v173

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestDropRetiredQualityTables 验证只删两张下线表、其它表不受影响，且步骤幂等可重入。
func TestDropRetiredQualityTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE platform_metrics (id INTEGER PRIMARY KEY)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE benchmark_books (id INTEGER PRIMARY KEY)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE chapter_quality (id INTEGER PRIMARY KEY)`).Error)

	needs, err := needsDrop(db)
	require.NoError(t, err)
	require.True(t, needs)

	step := dropRetiredQualityTablesStep{}
	log := slog.Default()
	require.NoError(t, step.Run(db, log))
	require.False(t, db.Migrator().HasTable(platformMetricsTable))
	require.False(t, db.Migrator().HasTable(benchmarkBooksTable))
	require.True(t, db.Migrator().HasTable("chapter_quality"), "其它业务表不得误删")

	// 中断恢复：重跑不报错，且此后不再判定为需迁移。
	require.NoError(t, step.Run(db, log))
	needs, err = needsDrop(db)
	require.NoError(t, err)
	require.False(t, needs)
}

// TestNeedsDropCleanDatabase 新库/已清理库无需迁移，直接登记完成（不触发备份）。
func TestNeedsDropCleanDatabase(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	needs, err := needsDrop(db)
	require.NoError(t, err)
	require.False(t, needs)
}
