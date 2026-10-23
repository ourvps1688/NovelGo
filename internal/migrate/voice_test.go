//go:build cgo

package migrate_test

import (
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/migrate"
)

// TestRunCreatesVoiceDimensionsTable 验证 voice_dimensions 注册进 migrate 建表清单：
// 建表成功、(novel_id, dimension) 唯一索引生效、不加 changed_at（P1 WBS §0.2）。
func TestRunCreatesVoiceDimensionsTable(t *testing.T) {
	setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}
	if !db.Migrator().HasTable("voice_dimensions") {
		t.Fatal("migrate.Run 应创建 voice_dimensions")
	}
	if !db.Migrator().HasIndex("voice_dimensions", "uk_voice_dimension") {
		t.Fatal("voice_dimensions 应有 (novel_id, dimension) 唯一索引 uk_voice_dimension")
	}
	if db.Migrator().HasColumn("voice_dimensions", "changed_at") {
		t.Fatal("voice_dimensions 契约不加 changed_at（不走 temporal/history）")
	}

	// 唯一索引生效：同 (novel_id, dimension) 第二条写入必须失败。
	if err := db.Exec(`INSERT INTO voice_dimensions
		(novel_id, dimension, content, source, created_at, updated_at)
		VALUES (1, 'rhythm', 'v1', 'manual', '2026-10-23 00:00:00', '2026-10-23 00:00:00')`).Error; err != nil {
		t.Fatalf("首条写入: %v", err)
	}
	if err := db.Exec(`INSERT INTO voice_dimensions
		(novel_id, dimension, content, source, created_at, updated_at)
		VALUES (1, 'rhythm', 'v2', 'manual', '2026-10-23 00:00:00', '2026-10-23 00:00:00')`).Error; err == nil {
		t.Fatal("同 (novel_id, dimension) 重复写入应被唯一索引拒绝")
	}
}
