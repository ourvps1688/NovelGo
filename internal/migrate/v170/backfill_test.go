//go:build cgo

package v170

import (
	"log/slog"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
)

// openStepTestDB 建 2.1 形状（列已在、值未回填）的台账库，供步骤白盒测试。
func openStepTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "v170.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Windows 上 sqlite 文件句柄不释放会挡住 TempDir 清理。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db pool: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&character.Character{}, &character.CharacterRelation{},
		&location.Location{}, &storyarc.StoryArc{}, &storyarc.ArcNode{},
		&timeline.TimelineEntry{}, &reader.ReaderPerspective{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

// TestStepsAreIdempotentAndNeverOverwrite 锁定 2.8 步骤语义：
//  1. temporal 步骤只写 NULL 行，重跑不刷新迁移时刻；
//  2. 提取步骤只写空字段，永不覆盖已填值；
//  3. 两个步骤重跑均无副作用（幂等可重入，契合 engine.Step 契约）。
func TestStepsAreIdempotentAndNeverOverwrite(t *testing.T) {
	db := openStepTestDB(t)
	exec := func(query string) {
		t.Helper()
		if err := db.Exec(query).Error; err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO characters (id, novel_id, name, description, gender, birth_year, aliases) VALUES
		(1, 1, '陈三斗', '28岁，退役水下工兵。', '', '', ''),
		(2, 1, '预填', '99岁，别名甲乙。', '女', '预设', '["预设"]')`)
	exec(`INSERT INTO character_relations (id, novel_id, source_character_id, target_character_id, relation_describe, relation_kind, is_current) VALUES
		(1, 1, 1, 2, '小满是他的表侄。', '', 1),
		(2, 1, 1, 3, '夫妻一场。', 'kin', 1)`)
	exec(`INSERT INTO locations (id, novel_id, name, description, state) VALUES
		(1, 1, '铁栅门', '通道口被一道铁栅封住。', ''),
		(2, 1, '旧庙', '此地已毁于战火。', 'intact')`)
	exec(`INSERT INTO story_arcs (id, novel_id, name, description, arc_type, char_id, status) VALUES
		(1, 1, '陈三斗：收桩', '他的弧光。', 'character', NULL, 'active'),
		(2, 1, '陈三斗再起', '预填归属。', 'character', 99, 'active')`)

	log := slog.Default()
	if err := (backfillTemporalStep{}).Run(db, log); err != nil {
		t.Fatalf("temporal step: %v", err)
	}
	if err := (extractStructuredStep{}).Run(db, log); err != nil {
		t.Fatalf("extract step: %v", err)
	}

	str := func(query string) string {
		t.Helper()
		var v string
		if err := db.Raw(query).Row().Scan(&v); err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		return v
	}
	// ① temporal：changed_at 已回填；② 提取：空字段被填、预填值原样。
	for _, table := range []string{"characters", "character_relations", "locations", "story_arcs"} {
		var n int
		if err := db.Raw(`SELECT COUNT(*) FROM ` + table + ` WHERE changed_at IS NULL`).Row().Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("%s.changed_at 应全部回填: NULL rows=%d", table, n)
		}
	}
	if got := str(`SELECT COALESCE(gender,'') || '|' || COALESCE(birth_year,'') || '|' || COALESCE(aliases,'') FROM characters WHERE id = 1`); got != "|28岁|" {
		t.Fatalf("character 1 提取不符: %q", got)
	}
	if got := str(`SELECT COALESCE(gender,'') || '|' || COALESCE(birth_year,'') || '|' || COALESCE(aliases,'') FROM characters WHERE id = 2`); got != `女|预设|["预设"]` {
		t.Fatalf("character 2 预填值被覆盖: %q", got)
	}
	if got := str(`SELECT COALESCE(relation_kind,'') FROM character_relations WHERE id = 1`); got != "kin" {
		t.Fatalf("relation 1 应填 kin: %q", got)
	}
	if got := str(`SELECT COALESCE(relation_kind,'') FROM character_relations WHERE id = 2`); got != "kin" {
		t.Fatalf("relation 2 预填值被覆盖: %q", got)
	}
	if got := str(`SELECT COALESCE(state,'') FROM locations WHERE id = 1`); got != "blocked" {
		t.Fatalf("location 1 应填 blocked: %q", got)
	}
	if got := str(`SELECT COALESCE(state,'') FROM locations WHERE id = 2`); got != "intact" {
		t.Fatalf("location 2 预填值被覆盖: %q", got)
	}
	if got := str(`SELECT COALESCE(char_id, 0) FROM story_arcs WHERE id = 1`); got != "1" {
		t.Fatalf("story_arc 1 应填 char_id=1: %q", got)
	}
	if got := str(`SELECT COALESCE(char_id, 0) FROM story_arcs WHERE id = 2`); got != "99" {
		t.Fatalf("story_arc 2 预填值被覆盖: %q", got)
	}

	// ③ 幂等：两个步骤重跑均不改任何值。
	before := str(`SELECT changed_at FROM characters WHERE id = 1`)
	if err := (backfillTemporalStep{}).Run(db, log); err != nil {
		t.Fatalf("temporal step rerun: %v", err)
	}
	if err := (extractStructuredStep{}).Run(db, log); err != nil {
		t.Fatalf("extract step rerun: %v", err)
	}
	if after := str(`SELECT changed_at FROM characters WHERE id = 1`); after != before {
		t.Fatalf("重跑不应刷新迁移时刻: before=%q after=%q", before, after)
	}
	if got := str(`SELECT COALESCE(gender,'') || '|' || COALESCE(birth_year,'') || '|' || COALESCE(aliases,'') FROM characters WHERE id = 2`); got != `女|预设|["预设"]` {
		t.Fatalf("重跑不应覆盖预填值: %q", got)
	}
	if got := str(`SELECT COALESCE(relation_kind,'') FROM character_relations WHERE id = 2`); got != "kin" {
		t.Fatalf("重跑不应覆盖预填 relation_kind: %q", got)
	}
}
