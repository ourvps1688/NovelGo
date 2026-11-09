// Package v173 清理已下线功能的存量数据表：「平台数据对账」(platform_metrics)
// 与「对标书库」(benchmark_books)。两功能于 2026-11-09 整体移除，代码与 UI 不再
// 读写这两张表；GORM AutoMigrate 只增不减，故须显式 DROP，避免老库残留孤儿表。
package v173

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/migrate/engine"
)

const (
	platformMetricsTable = "platform_metrics"
	benchmarkBooksTable  = "benchmark_books"
)

type dropRetiredQualityTablesStep struct{}

func (dropRetiredQualityTablesStep) Key() string { return "1-drop-retired-quality-tables" }

// needsDrop 只在存量库仍残留这两张孤儿表时返回 true；新库与已清理库直接登记完成。
func needsDrop(db *gorm.DB) (bool, error) {
	return db.Migrator().HasTable(platformMetricsTable) ||
		db.Migrator().HasTable(benchmarkBooksTable), nil
}

// Run 幂等可重入：逐表判存再 DROP，表已不存在时静默跳过（中断恢复安全）。
// 表名来自本包常量，不接收外部输入，无 SQL 注入面。
func (dropRetiredQualityTablesStep) Run(db *gorm.DB, log *slog.Logger) error {
	for _, table := range []string{platformMetricsTable, benchmarkBooksTable} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if err := db.Exec("DROP TABLE IF EXISTS " + table).Error; err != nil {
			return fmt.Errorf("drop retired table %s: %w", table, err)
		}
		log.Info("已移除下线功能残留表", "table", table)
	}
	return nil
}

func init() {
	engine.Register(engine.Migration{
		Name:        "v1.7.3-drop-retired-quality-tables",
		Description: "移除已下线的平台数据对账与对标书库残留表（platform_metrics / benchmark_books）",
		// DROP TABLE 属破坏性操作：migrate.Run 会在执行前自动全量备份，样本数据可恢复。
		Destructive:     true,
		NeedsMigration:  needsDrop,
		PostSchemaSteps: []engine.Step{dropRetiredQualityTablesStep{}},
	})
}
