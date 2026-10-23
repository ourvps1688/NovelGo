//go:build cgo

package migrate_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/migrate"
	"github.com/novelgo/novelgo/internal/testsupport"
)

func TestRunInitializesNewDatabaseAndMarksAllMigrationsDone(t *testing.T) {
	setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}
	if !db.Migrator().HasTable("chapters") {
		t.Fatal("新库应创建当前 chapters schema")
	}
	assertActivitySchema(t, db)
	for _, migration := range []struct {
		name string
		want int64
	}{
		{"v1.2.0-chapter-number-column-names", 2},
		{"v1.6.0-chapter-id-refactor", 5},
		{"v1.6.1-writing-daily", 1},
		{"v1.7.0-series-bible-backfill", 2},
	} {
		var done int64
		if err := db.Table("migrate_state").Where("migration = ? AND status = ?", migration.name, "done").Count(&done).Error; err != nil {
			t.Fatal(err)
		}
		if done != migration.want {
			t.Fatalf("新库 %s done steps = %d, want %d", migration.name, done, migration.want)
		}
	}
}

func TestRunDropsResidualDirPathWhenMigrationStateIsMissing(t *testing.T) {
	setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)
	for _, query := range []string{
		`CREATE TABLE novels (id INTEGER PRIMARY KEY, title TEXT NOT NULL, dir_path TEXT)`,
		`CREATE TABLE chapters (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, sort_order INTEGER NOT NULL DEFAULT 0)`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}
	if db.Migrator().HasColumn("novels", "dir_path") {
		t.Fatal("最终章节 schema 中残留的 novels.dir_path 应被清理")
	}
	for _, migration := range []struct {
		name string
		want int64
	}{
		{"v1.2.0-chapter-number-column-names", 2},
		{"v1.6.0-chapter-id-refactor", 5},
		{"v1.6.1-writing-daily", 1},
		{"v1.7.0-series-bible-backfill", 2},
	} {
		var done int64
		if err := db.Table("migrate_state").Where("migration = ? AND status = ?", migration.name, "done").Count(&done).Error; err != nil {
			t.Fatal(err)
		}
		if done != migration.want {
			t.Fatalf("%s done steps = %d, want %d", migration.name, done, migration.want)
		}
	}
}

// TestRunMigratesOldestSupportedDatabaseEndToEnd 是 migrate.Run 的黑盒总契约：从
// 最早仍受当前迁移链支持的 SQLite schema、真实数据和旧文件命名开始，只调用一次
// 公开入口，验证最终数据库、数据、文件、Git 和 migrate_state。
//
// 新增 migration 时，必须在这里补充其历史输入和最终断言，持续证明整条升级链可从
// 最早受支持旧库升级到当前版本。各版本包内的测试则保留为定位具体步骤边界的白盒测试。
func TestRunMigratesOldestSupportedDatabaseEndToEnd(t *testing.T) {
	dataDir := setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)
	execSQL := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	// 最早受支持 schema 的全部迁移相关旧表。source/resolved/writing/character 的
	// *_id 在该版本只是章节号的误名；这里故意不预建任何 v1.6.0 新列。
	for _, query := range []string{
		`CREATE TABLE novels (id INTEGER PRIMARY KEY, title TEXT NOT NULL, genre TEXT, description TEXT, dir_path TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE chapters (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, chapter_number INTEGER NOT NULL, title TEXT, summary TEXT, word_count INTEGER DEFAULT 0, created_at DATETIME, updated_at DATETIME)`,
		`CREATE UNIQUE INDEX uk_novel_chapter ON chapters(novel_id, chapter_number)`,
		`CREATE TABLE time_entries (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, category TEXT NOT NULL, status TEXT NOT NULL, title TEXT NOT NULL, content TEXT, detail_json TEXT, target_chapter INTEGER NOT NULL, importance INTEGER DEFAULT 3, source_chapter_id INTEGER, source TEXT, resolved_chapter_id INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE story_arcs (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT, arc_type TEXT NOT NULL, importance INTEGER DEFAULT 1, status TEXT NOT NULL, reactivate_at TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE arc_nodes (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, story_arc_id INTEGER NOT NULL, title TEXT NOT NULL, description TEXT, target_chapter INTEGER DEFAULT 0, actual_chapter INTEGER DEFAULT 0, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE reader_perspectives (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, type TEXT NOT NULL, content TEXT NOT NULL, related_truth TEXT, planted_chapter INTEGER NOT NULL, revealed_chapter INTEGER DEFAULT 0, created_at DATETIME)`,
		`CREATE TABLE writing_log (id INTEGER PRIMARY KEY, date TEXT NOT NULL, novel_id INTEGER NOT NULL DEFAULT 0, chapter_id INTEGER NOT NULL DEFAULT 0, word_delta INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE INDEX idx_writing_log_chapter_id ON writing_log(chapter_id)`,
		`CREATE TABLE character_relations (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, source_character_id INTEGER NOT NULL, target_character_id INTEGER NOT NULL, relation_describe TEXT NOT NULL, description TEXT, chapter_id INTEGER, is_current BOOLEAN NOT NULL, created_at DATETIME)`,
		// v1.7.1 前的 retcon_cases（无 kind 列）：存量案须回填 fact（全量回滚路由不变）。
		`CREATE TABLE retcon_cases (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, volume_id INTEGER, origin TEXT NOT NULL DEFAULT 'author', issue_id TEXT, target_scope TEXT, dimension TEXT, direction TEXT, title TEXT NOT NULL, description TEXT, status TEXT NOT NULL DEFAULT 'candidate', decision_note TEXT, decided_by TEXT, decided_at DATETIME, rollback_snapshot_id INTEGER, diff_summary TEXT, applied_by TEXT, applied_at DATETIME, rolled_back_by TEXT, rolled_back_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
	} {
		execSQL(query)
	}

	execSQL(`INSERT INTO novels (id, title) VALUES (1, 'n1'), (2, 'n2')`)
	execSQL(`INSERT INTO chapters (id, novel_id, chapter_number, title) VALUES (1, 1, 1, 'c1'), (2, 1, 2, 'c2'), (3, 1, 3, 'c3'), (4, 2, 1, 'c1')`)
	execSQL(`INSERT INTO time_entries (id, novel_id, category, status, title, target_chapter, importance, source_chapter_id, resolved_chapter_id) VALUES (1, 1, 'foreshadowing', 'pending', 'f1', 2, 3, 1, 0), (2, 1, 'foreshadowing', 'resolved', 'f2', 99, 3, 3, 2), (3, 2, 'foreshadowing', 'pending', 'f3', 1, 3, 1, 0)`)
	execSQL(`INSERT INTO story_arcs (id, novel_id, name, arc_type, status) VALUES (1, 1, 'arc', 'main', 'active')`)
	execSQL(`INSERT INTO arc_nodes (id, novel_id, story_arc_id, title, target_chapter, actual_chapter, status) VALUES (1, 1, 1, 'a1', 3, 0, 'pending'), (2, 1, 1, 'a2', 99, 1, 'pending')`)
	execSQL(`INSERT INTO reader_perspectives (id, novel_id, type, content, planted_chapter, revealed_chapter) VALUES (1, 1, 'known', 'p1', 1, 0), (2, 2, 'suspense', 'p2', 99, 1)`)
	execSQL(`INSERT INTO writing_log (id, date, novel_id, chapter_id, word_delta) VALUES (1, '2026-01-01', 1, 2, 100), (2, '2026-01-01', 1, 0, -50)`)
	execSQL(`INSERT INTO character_relations (id, novel_id, source_character_id, target_character_id, relation_describe, chapter_id, is_current) VALUES (1, 1, 1, 2, '朋友', 3, 1), (2, 1, 1, 2, '旧识', 0, 0)`)
	execSQL(`INSERT INTO retcon_cases (id, novel_id, origin, issue_id, title, status) VALUES (1, 1, 'pipeline', 'I1', '硬矛盾', 'candidate')`)

	novelDir := filepath.Join(dataDir, "novels", "1")
	writeLegacyFile(t, novelDir, "chapters/001.md", "chapter one")
	writeLegacyFile(t, novelDir, "outlines/001.md", "outline one")

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}

	assertActivitySchema(t, db)

	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.Raw(query).Scan(&n).Error; err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		return n
	}
	for _, assertion := range []struct {
		name  string
		query string
	}{
		{"timeline IDs and reading target", `SELECT COUNT(*) FROM time_entries WHERE id = 1 AND target_reading_number = 2 AND source_chapter_id = 1 AND resolved_chapter_id IS NULL`},
		{"timeline resolved ID", `SELECT COUNT(*) FROM time_entries WHERE id = 2 AND target_reading_number = 99 AND source_chapter_id = 3 AND resolved_chapter_id = 2`},
		{"timeline cross-novel ID", `SELECT COUNT(*) FROM time_entries WHERE id = 3 AND target_reading_number = 1 AND source_chapter_id = 4`},
		{"arc node pending", `SELECT COUNT(*) FROM arc_nodes WHERE id = 1 AND target_reading_number = 3 AND actual_chapter_id IS NULL`},
		{"arc node IDs and reading target", `SELECT COUNT(*) FROM arc_nodes WHERE id = 2 AND target_reading_number = 99 AND actual_chapter_id = 1`},
		{"reader planted ID", `SELECT COUNT(*) FROM reader_perspectives WHERE id = 1 AND planted_chapter_id = 1 AND revealed_chapter_id IS NULL`},
		{"reader IDs and orphan", `SELECT COUNT(*) FROM reader_perspectives WHERE id = 2 AND planted_chapter_id IS NULL AND revealed_chapter_id = 4`},
		{"writing daily legacy counts", `SELECT COUNT(*) FROM writing_log WHERE date = '2026-01-01' AND novel_id = 1 AND word_delta = 50 AND words_added = 100 AND words_deleted = 50`},
		{"character relation ID", `SELECT COUNT(*) FROM character_relations WHERE id = 1 AND chapter_id = 3`},
		{"character relation zero", `SELECT COUNT(*) FROM character_relations WHERE id = 2 AND chapter_id IS NULL`},
		{"retcon 存量案 kind 回填 fact", `SELECT COUNT(*) FROM retcon_cases WHERE id = 1 AND kind = 'fact'`},
	} {
		if n := count(assertion.query); n == 0 {
			t.Fatalf("迁移数据断言失败: %s", assertion.name)
		}
	}
	if n := count(`SELECT COUNT(*) FROM chapters WHERE (id = 1 AND sort_order = 1) OR (id = 2 AND sort_order = 2) OR (id = 3 AND sort_order = 3) OR (id = 4 AND sort_order = 1)`); n != 4 {
		t.Fatalf("sort_order 未按旧章节号初始化: matched=%d", n)
	}

	for _, rel := range []string{"chapters/id_1.md", "outlines/id_1.md", "volumes/.gitkeep"} {
		if _, err := os.Stat(filepath.Join(novelDir, rel)); err != nil {
			t.Fatalf("迁移后文件 %s 不存在: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(novelDir, "chapters/001.md")); !os.IsNotExist(err) {
		t.Fatal("旧章节文件应被改名移除")
	}
	repo, err := git.New(1, "", "", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if dirty, err := repo.HasUncommitted(); err != nil || dirty {
		t.Fatalf("迁移后的小说仓库应干净: dirty=%v err=%v", dirty, err)
	}

	for _, legacy := range []struct{ table, column string }{
		{"novels", "dir_path"},
		{"chapters", "chapter_number"},
		{"time_entries", "target_chapter"}, {"time_entries", "source_chapter"}, {"time_entries", "resolved_chapter"},
		{"arc_nodes", "target_chapter"}, {"arc_nodes", "actual_chapter"},
		{"reader_perspectives", "planted_chapter"}, {"reader_perspectives", "revealed_chapter"},
		{"writing_log", "chapter_number"}, {"writing_log", "chapter_id"}, {"character_relations", "chapter_number"},
	} {
		if db.Migrator().HasColumn(legacy.table, legacy.column) {
			t.Fatalf("旧列仍存在: %s.%s", legacy.table, legacy.column)
		}
	}
	if db.Migrator().HasIndex("chapters", "uk_novel_chapter") {
		t.Fatal("uk_novel_chapter 应随旧列删除")
	}
	if !db.Migrator().HasIndex("writing_log", "uk_writing_date_novel") {
		t.Fatal("写作日志应按日期和小说唯一")
	}
	for _, table := range []string{"volumes", "migrate_state"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("迁移后表不存在: %s", table)
		}
	}
	if n := count(`SELECT COUNT(*) FROM migrate_state WHERE migration = 'v1.6.0-chapter-id-refactor' AND status = 'done'`); n != 5 {
		t.Fatalf("v160 迁移步骤完成数 = %d, want 5", n)
	}
	if n := count(`SELECT COUNT(*) FROM migrate_state WHERE migration = 'v1.2.0-chapter-number-column-names' AND status = 'done'`); n != 2 {
		t.Fatalf("v120 迁移步骤完成数 = %d, want 2", n)
	}

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("幂等 migrate.Run: %v", err)
	}
	if err := db.Exec("DELETE FROM migrate_state").Error; err != nil {
		t.Fatal(err)
	}
	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("状态丢失后 migrate.Run: %v", err)
	}
	if !db.Migrator().HasColumn("time_entries", "source_chapter_id") ||
		db.Migrator().HasColumn("time_entries", "source_chapter") ||
		db.Migrator().HasColumn("writing_log", "chapter_id") ||
		db.Migrator().HasColumn("writing_log", "chapter_number") {
		t.Fatal("状态丢失重跑不应回退最终的 id schema")
	}
	if n := count(`SELECT COUNT(*) FROM writing_log`); n != 1 {
		t.Fatalf("重复迁移后应只有一条写作日汇总: got %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM migrate_state WHERE status = 'done'`); n != 12 {
		t.Fatalf("状态丢失后应重建全部迁移状态: got %d, want 12", n)
	}
}

func setupMigrationIntegrationEnv(t *testing.T) string {
	t.Helper()
	gitSrc := testsupport.RequireGit(t)
	dataDir := testsupport.Isolate(t)
	t.Setenv("NOVELGO_TESTING", "1")
	// 迁移会 git commit 文件改名，必须有真实 git；测试模式默认只认 DataDir 下的
	// bundled git，故显式注入系统 git，而非伪造一份 bundled 布局（Windows 上伪造不出来）。
	t.Setenv("NOVELGO_GIT_BIN", gitSrc)
	t.Setenv("HOME", t.TempDir())
	config.Set(&config.AppConfig{})
	return dataDir
}

func openMigrationIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// 显式关闭连接：Windows 无法删除仍被占用的 db 文件，不关会让 t.TempDir() 清理失败。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func writeLegacyFile(t *testing.T, novelDir, rel, content string) {
	t.Helper()
	path := filepath.Join(novelDir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunAddsSeriesBibleSchemaToLegacyDatabase 验证阶段 2 schema 落地（WBS §0.4）的升级路径：
// 带 pre-2.1 列的 7 张台账表 + 旧数据 → migrate.Run → ① 新列全部落位且旧数据有默认
// （volume_id=NULL=无卷、changed_at 回填迁移时刻、alive 缺省在世）；② 5 张 series bible
// 新表建成并可读写；③ volume_snapshots 的 (volume_id, snapshot_version) 唯一约束兜底「冻结即串行提交」。
func TestRunAddsSeriesBibleSchemaToLegacyDatabase(t *testing.T) {
	setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)
	execSQL := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	createPre21LedgerTables(t, db)
	execSQL(`INSERT INTO characters (id, novel_id, name) VALUES (1, 1, '主角')`)
	execSQL(`INSERT INTO time_entries (id, novel_id, category, status, title, target_reading_number) VALUES (1, 1, 'foreshadowing', 'pending', 'f1', 2)`)
	execSQL(`INSERT INTO reader_perspectives (id, novel_id, type, content) VALUES (1, 1, 'suspense', 'p1')`)

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}

	for _, col := range []struct{ table, column string }{
		{"characters", "aliases"}, {"characters", "gender"}, {"characters", "birth_year"},
		{"characters", "appearance_anchors"}, {"characters", "alive"}, {"characters", "location_ref"},
		{"characters", "faction"}, {"characters", "role"}, {"characters", "capability_ceiling"},
		{"characters", "sealed"}, {"characters", "voice_traits"},
		{"characters", "volume_id"}, {"characters", "changed_at"},
		{"characters", "changed_in_volume_id"}, {"characters", "changed_in_chapter_id"},
		{"character_relations", "relation_kind"}, {"character_relations", "volume_id"}, {"character_relations", "changed_at"},
		{"locations", "state"}, {"locations", "controller"}, {"locations", "volume_id"},
		{"locations", "changed_at"}, {"locations", "changed_in_volume_id"}, {"locations", "changed_in_chapter_id"},
		{"story_arcs", "char_id"}, {"story_arcs", "motif"}, {"story_arcs", "volume_id"}, {"story_arcs", "changed_at"},
		{"arc_nodes", "volume_id"}, {"arc_nodes", "changed_at"},
		{"time_entries", "volume_id"}, {"time_entries", "changed_at"},
		{"reader_perspectives", "volume_id"}, {"reader_perspectives", "changed_at"},
	} {
		if !db.Migrator().HasColumn(col.table, col.column) {
			t.Fatalf("新列缺失: %s.%s", col.table, col.column)
		}
	}
	for _, table := range []string{
		"chronology_anchors", "character_fact_knowledge", "pov_roster",
		"ledger_change_history", "volume_snapshots",
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("series bible 新表缺失: %s", table)
		}
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.Raw(query).Scan(&n).Error; err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		return n
	}
	// 旧数据有默认（WBS 2.8）：volume_id 保持 NULL（语义「无卷/全书级」）；changed_at 由
	// v1.7.0-series-bible-backfill 回填为迁移时刻（非 NULL）；存量角色缺省「在世」（alive 默认 true）。
	if n := count(`SELECT COUNT(*) FROM characters WHERE id = 1 AND name = '主角' AND volume_id IS NULL AND changed_at IS NOT NULL AND alive = 1`); n != 1 {
		t.Fatalf("characters 旧数据默认语义不符: matched=%d", n)
	}
	if n := count(`SELECT COUNT(*) FROM time_entries WHERE id = 1 AND volume_id IS NULL AND changed_at IS NOT NULL`); n != 1 {
		t.Fatalf("time_entries 旧数据默认语义不符: matched=%d", n)
	}
	if n := count(`SELECT COUNT(*) FROM reader_perspectives WHERE id = 1 AND volume_id IS NULL AND changed_at IS NOT NULL`); n != 1 {
		t.Fatalf("reader_perspectives 旧数据默认语义不符: matched=%d", n)
	}

	// 新表读写往返。
	execSQL(`INSERT INTO chronology_anchors (novel_id, label, anchor_type, era_date) VALUES (1, '新历元年', 'era', '新历')`)
	execSQL(`INSERT INTO character_fact_knowledge (novel_id, char_id, fact_id, knows) VALUES (1, 1, 1, 1)`)
	execSQL(`INSERT INTO pov_roster (novel_id, char_id, mode) VALUES (1, 1, 'limited')`)
	execSQL(`INSERT INTO ledger_change_history (novel_id, entry_table, entry_id, field, old_value, new_value, source, at) VALUES (1, 'characters', 1, 'alive', 'true', 'false', 'retcon', '2026-10-22 10:00:00')`)
	execSQL(`INSERT INTO volume_snapshots (novel_id, volume_id, snapshot_version, committed_at, payload) VALUES (1, 1, 1, '2026-10-22 10:00:00', '{}')`)
	if n := count(`SELECT COUNT(*) FROM ledger_change_history WHERE entry_table = 'characters' AND entry_id = 1 AND source = 'retcon'`); n != 1 {
		t.Fatalf("ledger_change_history 读写往返失败: matched=%d", n)
	}
	// 「冻结即串行提交」存储层兜底：同卷同版本快照只允许一份。
	if err := db.Exec(`INSERT INTO volume_snapshots (novel_id, volume_id, snapshot_version, committed_at, payload) VALUES (1, 1, 1, '2026-10-22 11:00:00', '{}')`).Error; err == nil {
		t.Fatal("同卷同版本快照重复提交应被唯一约束拒绝")
	}
	if n := count(`SELECT COUNT(*) FROM volume_snapshots`); n != 1 {
		t.Fatalf("重复快照不应落库: got %d", n)
	}
}

// createPre21LedgerTables 建 pre-2.1（阶段 1 结束时）形状的 7 张台账表：
// 只有既有列，无 volume_id/changed_at/四维判定列。
func createPre21LedgerTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, query := range []string{
		`CREATE TABLE characters (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT, personality TEXT, abilities TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE character_relations (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, source_character_id INTEGER NOT NULL, target_character_id INTEGER NOT NULL, relation_describe TEXT NOT NULL, description TEXT, chapter_id INTEGER, is_current BOOLEAN NOT NULL, created_at DATETIME)`,
		`CREATE TABLE locations (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, name TEXT NOT NULL, location_type TEXT, description TEXT, detail_json TEXT, parent_location_id INTEGER, tags TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE story_arcs (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT, arc_type TEXT NOT NULL, importance INTEGER DEFAULT 1, status TEXT NOT NULL, reactivate_at TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE arc_nodes (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, story_arc_id INTEGER NOT NULL, title TEXT NOT NULL, description TEXT, target_reading_number INTEGER DEFAULT 0, actual_chapter_id INTEGER, status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE time_entries (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, category TEXT NOT NULL, status TEXT NOT NULL, title TEXT NOT NULL, content TEXT, detail_json TEXT, target_reading_number INTEGER NOT NULL DEFAULT 0, importance INTEGER DEFAULT 3, source_chapter_id INTEGER, source TEXT, resolved_chapter_id INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE reader_perspectives (id INTEGER PRIMARY KEY, novel_id INTEGER NOT NULL, type TEXT NOT NULL, content TEXT NOT NULL, related_truth TEXT, planted_chapter_id INTEGER, revealed_chapter_id INTEGER, created_at DATETIME)`,
	} {
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
}

// TestRunBackfillsLedgerTemporalAndStructuredFields 验证 WBS 2.8 存量迁移（闭 G6）：
// pre-2.1 存量库 → migrate.Run → ① temporal 回填（7 表 changed_at=迁移时刻，volume_id
// 保持 NULL=无卷）；② 四维结构化字段尽力填充（单一无歧义命中才写）；
// ③ 迁移只跑一次（再次 Run 不改任何值）。验收「旧书加载不炸、字段有默认」。
func TestRunBackfillsLedgerTemporalAndStructuredFields(t *testing.T) {
	setupMigrationIntegrationEnv(t)
	db := openMigrationIntegrationDB(t)
	execSQL := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	createPre21LedgerTables(t, db)

	// 存量数据取自真实开发库文风（自由文本描述）。
	execSQL(`INSERT INTO characters (id, novel_id, name, description) VALUES
		(1, 1, '陈三斗', '28岁，退役水下工兵，陈家第十七代起桩人。右手只剩四指。'),
		(2, 1, '陈守拙', '陈三斗之父，六十岁上下，名义上二十年前死在三峡。'),
		(3, 1, '姓何的女人', '三十来岁，失踪潜水员何建国的家属（正文只称“那个姓何的女人”）。'),
		(4, 1, '苏阿水', '52岁，疍家出身，外号水鬼苏。水里是神。'),
		(5, 1, '小满', '二十出头，周德贵的表侄，他娘让周德贵带他出来。'),
		(6, 1, '预填角色', '99岁，别名甲乙。')`)
	execSQL(`INSERT INTO character_relations (id, novel_id, source_character_id, target_character_id, relation_describe, is_current) VALUES
		(1, 1, 1, 2, '陈三斗对陈守拙：二十年不相往来，见面只谈桩。', 1),
		(2, 1, 1, 5, '小满是他的表侄，二十出头、头一回下深水。', 1),
		(3, 1, 1, 8, '旧雇主与手艺人，已闹翻：借口扣工钱。', 1),
		(4, 1, 9, 10, '夫妻：她带着七八岁的孩子守在黑泉坑沿。', 1),
		(5, 1, 1, 11, '亦师亦敌，从师徒到仇人。', 1),
		(6, 1, 1, 12, '夫妻一场，聚少离多。', 1)`)
	execSQL(`INSERT INTO locations (id, novel_id, name, description) VALUES
		(1, 1, '石台尽头的铁栅门', '石台再往里的通道口被一道铁栅封住。'),
		(2, 1, '黑泉天坑', '前年塌出来的一口天坑，宽二十来米。'),
		(3, 1, '旧庙', '庙门坍塌，仅存山墙。'),
		(4, 1, '预填地点', '此地已毁于战火。')`)
	execSQL(`INSERT INTO story_arcs (id, novel_id, name, description, arc_type, status) VALUES
		(1, 1, '陈三斗：从还债到收桩', '他的弧光。', 'character', 'active'),
		(2, 1, '水鬼苏的最后一趟水', '旧江湖最后一代舵把子。', 'character', 'active'),
		(3, 1, '守网人与续脉派', '信条之争。', 'sub', 'active'),
		(4, 1, '陈三斗再起', '预填归属。', 'character', 'active')`)
	execSQL(`INSERT INTO arc_nodes (id, novel_id, story_arc_id, title, status) VALUES (1, 1, 1, '发现仇人身份', 'pending')`)
	execSQL(`INSERT INTO time_entries (id, novel_id, category, status, title, target_reading_number) VALUES (1, 1, 'foreshadowing', 'pending', '三长两短的求救号', 2)`)
	execSQL(`INSERT INTO reader_perspectives (id, novel_id, type, content) VALUES (1, 1, 'suspense', '谁在夜里敲桩')`)

	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.Raw(query).Scan(&n).Error; err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		return n
	}
	// ① temporal 回填：7 张台账表 changed_at 全部非 NULL；volume_id 保持 NULL（无卷）。
	for _, table := range []string{
		"characters", "character_relations", "locations", "story_arcs",
		"arc_nodes", "time_entries", "reader_perspectives",
	} {
		if n := count(`SELECT COUNT(*) FROM ` + table + ` WHERE changed_at IS NULL`); n != 0 {
			t.Fatalf("%s.changed_at 应全部回填为迁移时刻: NULL rows=%d", table, n)
		}
		if n := count(`SELECT COUNT(*) FROM ` + table + ` WHERE volume_id IS NOT NULL`); n != 0 {
			t.Fatalf("%s.volume_id 存量行应保持 NULL（无卷）: 非 NULL rows=%d", table, n)
		}
	}

	// ② 结构化尽力填充（永不覆盖已填值由 v170 步骤测试覆盖）。
	for _, c := range []struct {
		id                    int64
		gender, born, aliases string
	}{
		{1, "", "28岁", ""},
		{2, "男", "六十岁上下", ""},
		{3, "女", "三十来岁", ""},
		{4, "", "52岁", `["水鬼苏"]`},
		{5, "", "二十出头", ""},
		{6, "", "99岁", `["甲乙"]`},
	} {
		var gender, born, aliases string
		if err := db.Raw(`SELECT COALESCE(gender,''), COALESCE(birth_year,''), COALESCE(aliases,'') FROM characters WHERE id = ?`, c.id).
			Row().Scan(&gender, &born, &aliases); err != nil {
			t.Fatalf("read character %d: %v", c.id, err)
		}
		if gender != c.gender || born != c.born || aliases != c.aliases {
			t.Fatalf("character %d 填充不符: gender=%q born=%q aliases=%q, want gender=%q born=%q aliases=%q",
				c.id, gender, born, aliases, c.gender, c.born, c.aliases)
		}
	}
	for _, r := range []struct {
		id   int64
		kind string
	}{
		{1, ""},      // 零命中不写
		{2, "kin"},   // 表侄
		{3, "rival"}, // 闹翻
		{4, "lover"}, // 夫妻
		{5, ""},      // 师徒+仇人多组命中不写
		{6, "lover"}, // 夫妻一场
	} {
		var kind string
		if err := db.Raw(`SELECT COALESCE(relation_kind,'') FROM character_relations WHERE id = ?`, r.id).Row().Scan(&kind); err != nil {
			t.Fatalf("read relation %d: %v", r.id, err)
		}
		if kind != r.kind {
			t.Fatalf("relation %d kind=%q, want %q", r.id, kind, r.kind)
		}
	}
	for _, l := range []struct {
		id    int64
		state string
	}{
		{1, "blocked"},   // 被铁栅封住
		{2, ""},          // 「塌出来」不误判为 destroyed
		{3, "destroyed"}, // 坍塌
		{4, "destroyed"}, // 已毁于战火
	} {
		var state string
		if err := db.Raw(`SELECT COALESCE(state,'') FROM locations WHERE id = ?`, l.id).Row().Scan(&state); err != nil {
			t.Fatalf("read location %d: %v", l.id, err)
		}
		if state != l.state {
			t.Fatalf("location %d state=%q, want %q", l.id, state, l.state)
		}
	}
	for _, a := range []struct {
		id     int64
		charID any // nil=不填
	}{
		{1, int64(1)}, // 前缀「陈三斗：」精确命中
		{2, nil},      // 别名「水鬼苏」不参与匹配
		{3, nil},      // 非角色弧不填
		{4, int64(1)}, // 子串唯一命中
	} {
		var charID *int64
		if err := db.Raw(`SELECT char_id FROM story_arcs WHERE id = ?`, a.id).Row().Scan(&charID); err != nil {
			t.Fatalf("read story_arc %d: %v", a.id, err)
		}
		switch want := a.charID.(type) {
		case nil:
			if charID != nil {
				t.Fatalf("story_arc %d char_id=%d, want NULL", a.id, *charID)
			}
		case int64:
			if charID == nil || *charID != want {
				t.Fatalf("story_arc %d char_id=%v, want %d", a.id, charID, want)
			}
		}
	}

	// ④ 迁移只跑一次：再次 Run 不改任何值（migrate_state 记 done，步骤跳过）。
	before := count(`SELECT COUNT(*) FROM characters WHERE changed_at IS NOT NULL`)
	firstChanged := count(`SELECT CAST(strftime('%s', changed_at) AS INTEGER) FROM characters WHERE id = 1`)
	if err := migrate.Run(db, slog.Default()); err != nil {
		t.Fatalf("migrate.Run (second): %v", err)
	}
	if after := count(`SELECT COUNT(*) FROM characters WHERE changed_at IS NOT NULL`); after != before {
		t.Fatalf("二次 Run 不应改动 changed_at: before=%d after=%d", before, after)
	}
	if second := count(`SELECT CAST(strftime('%s', changed_at) AS INTEGER) FROM characters WHERE id = 1`); second != firstChanged {
		t.Fatalf("二次 Run 不应刷新迁移时刻: first=%d second=%d", firstChanged, second)
	}
	if n := count(`SELECT COUNT(*) FROM character_relations WHERE COALESCE(relation_kind,'') = 'lover' AND id = 6`); n != 1 {
		t.Fatalf("二次 Run 不应改动已填 relation_kind: matched=%d", n)
	}
}
