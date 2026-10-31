//go:build cgo

package v172

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "v172.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	stmts := []string{
		`CREATE TABLE characters (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT, personality TEXT, abilities TEXT, aliases TEXT, gender TEXT, birth_year TEXT, appearance_anchors TEXT, location_ref TEXT, faction TEXT, role TEXT, capability_ceiling TEXT, voice_traits TEXT, updated_at DATETIME)`,
		`CREATE TABLE character_relations (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, source_character_id INTEGER NOT NULL, target_character_id INTEGER NOT NULL, relation_describe TEXT, relation_kind TEXT, description TEXT, is_current INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE group_members (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, group_id INTEGER NOT NULL, character_id INTEGER NOT NULL)`,
		`CREATE TABLE character_fact_knowledge (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, char_id INTEGER NOT NULL, fact_id INTEGER NOT NULL)`,
		`CREATE TABLE pov_roster (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, char_id INTEGER NOT NULL, mode TEXT NOT NULL, volume_id INTEGER)`,
		`CREATE TABLE story_arcs (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT, motif TEXT, reactivate_at TEXT, char_id INTEGER, updated_at DATETIME)`,
		`CREATE TABLE arc_nodes (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, story_arc_id INTEGER NOT NULL, title TEXT NOT NULL, target_reading_number INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE locations (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, name TEXT NOT NULL, description TEXT, detail_json TEXT, tags TEXT, location_type TEXT, controller TEXT, state TEXT, updated_at DATETIME)`,
		`CREATE TABLE location_relations (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, location_a INTEGER NOT NULL, location_b INTEGER NOT NULL, relation_type TEXT NOT NULL, description TEXT)`,
		`CREATE TABLE reader_perspectives (id INTEGER PRIMARY KEY AUTOINCREMENT, novel_id INTEGER NOT NULL, type TEXT NOT NULL, content TEXT NOT NULL, content_hash TEXT, related_truth TEXT, planted_chapter_id INTEGER, revealed_chapter_id INTEGER)`,
	}
	for _, s := range stmts {
		require.NoError(t, db.Exec(s).Error, s)
	}
	return db
}

func mustExec(t *testing.T, db *gorm.DB, q string, args ...any) {
	t.Helper()
	require.NoError(t, db.Exec(q, args...).Error, q)
}

func countWhere(t *testing.T, db *gorm.DB, q string, args ...any) int64 {
	t.Helper()
	var c int64
	require.NoError(t, db.Raw(q, args...).Scan(&c).Error, q)
	return c
}

// TestDedupCharacters_RepointsForeignKeys 锁死：同名角色去重保留"内容最多"的一份，
// 其余行的外键（关系/分组/认知/POV/弧线归属）重指到保留行，并折叠重指产生的重复关系边。
func TestDedupCharacters_RepointsForeignKeys(t *testing.T) {
	db := openTestDB(t)
	log := slog.Default()

	// 角色：林雷 两份（id=1 内容更丰满，id=2 几乎空白）；萧炎 两份（id=4 有内容，id=5 空白）。
	// 另有跨小说的同名（novel=2 的林雷）不应被并入。
	mustExec(t, db, `INSERT INTO characters (id, novel_id, name, description, personality) VALUES
		(1, 1, '林雷', '主角，身怀秘宝', '{"role":"主角"}'),
		(2, 1, '林雷', '', ''),
		(3, 2, '林雷', '另一本书的林雷', ''),
		(4, 1, '萧炎', '炎帝', ''),
		(5, 1, '萧炎', '', '')`)
	// 外键指向将被删除的重复行 id=2 / id=5
	mustExec(t, db, `INSERT INTO character_relations (id, novel_id, source_character_id, target_character_id, relation_describe, relation_kind, is_current) VALUES
		(1, 1, 2, 1, '宿敌', '', 1),
		(2, 1, 1, 1, '宿敌', '', 1)`) // 重指后会与 id=1 的"宿敌"(1->1) 撞成完全相同的边
	mustExec(t, db, `INSERT INTO group_members (id, novel_id, group_id, character_id) VALUES (1, 1, 7, 2)`)
	mustExec(t, db, `INSERT INTO character_fact_knowledge (id, novel_id, char_id, fact_id) VALUES (1, 1, 2, 99)`)
	mustExec(t, db, `INSERT INTO pov_roster (id, novel_id, char_id, mode, volume_id) VALUES (1, 1, 2, 'limited', NULL)`)
	mustExec(t, db, `INSERT INTO story_arcs (id, novel_id, name, char_id) VALUES (1, 1, '林雷线', 2)`)

	require.NoError(t, dedupCharacters(db, log))

	// 每个 (novel_id, name) 仅剩一行；保留的是内容更丰满的 id=1（林雷）与 id=4（萧炎）
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM characters WHERE novel_id=1 AND name='林雷'`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM characters WHERE novel_id=1 AND name='萧炎'`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM characters WHERE novel_id=2 AND name='林雷'`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM characters WHERE id=1`))
	require.Equal(t, int64(0), countWhere(t, db, `SELECT COUNT(*) FROM characters WHERE id IN (2,5)`))

	// 外键全部重指到保留行 id=1
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM character_relations WHERE id=1 AND source_character_id=1`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM group_members WHERE character_id=1`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM character_fact_knowledge WHERE char_id=1`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM pov_roster WHERE char_id=1`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM story_arcs WHERE char_id=1`))

	// 重指产生的重复关系边被折叠：character_relations 最终仅剩 1 条 (1->1)
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM character_relations`))

	// 去重后可顺利建立 (novel_id, name) 唯一索引
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX uk_t_char ON characters(novel_id, name)").Error)

	// 幂等：再跑一次无副作用
	require.NoError(t, dedupCharacters(db, log))
	require.Equal(t, int64(3), countWhere(t, db, `SELECT COUNT(*) FROM characters`))
}

// TestDedupLocationsAndStoryArcs 锁死地点/弧线的去重与外键重指。
func TestDedupLocationsAndStoryArcs(t *testing.T) {
	db := openTestDB(t)
	log := slog.Default()

	mustExec(t, db, `INSERT INTO locations (id, novel_id, name, description, location_type) VALUES
		(1, 1, '王宫', '富丽堂皇', '宫殿'),
		(2, 1, '王宫', '', '')`)
	// 边 (1->9) 已存在（保留地点 1 出发）；废弃地点 2 也有一条 (2->9)。重指 2->1 会撞
	// uk_location_pair 唯一对，必须删除冗余边而非报错。最终仅剩 (1->9)。
	mustExec(t, db, `INSERT INTO location_relations (id, novel_id, location_a, location_b, relation_type) VALUES
		(1, 1, 1, 9, '相邻'),
		(2, 1, 2, 9, '相邻')`)
	mustExec(t, db, `INSERT INTO story_arcs (id, novel_id, name, description, motif) VALUES
		(1, 1, '复仇之路', '主线', 'revenge'),
		(2, 1, '复仇之路', '', '')`)
	mustExec(t, db, `INSERT INTO arc_nodes (id, novel_id, story_arc_id, title) VALUES (1, 1, 2, '发现仇人')`)

	require.NoError(t, dedupLocations(db, log))
	require.NoError(t, dedupStoryArcs(db, log))

	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM locations WHERE name='王宫'`))
	require.Equal(t, int64(0), countWhere(t, db, `SELECT COUNT(*) FROM locations WHERE id=2`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM location_relations`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM location_relations WHERE location_a=1 AND location_b=9`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM story_arcs WHERE name='复仇之路'`))
	require.Equal(t, int64(0), countWhere(t, db, `SELECT COUNT(*) FROM story_arcs WHERE id=2`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM arc_nodes WHERE story_arc_id=1`))

	require.NoError(t, db.Exec("CREATE UNIQUE INDEX uk_t_loc ON locations(novel_id, name)").Error)
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX uk_t_arc ON story_arcs(novel_id, name)").Error)
}

// TestDedupReaders 锁死读者视角按 (类型+归一化内容) 去重，
// 含空格差异的"相同内容"也合并，并重指 character_fact_knowledge.fact_id。

func TestDedupReaders(t *testing.T) {
	db := openTestDB(t)
	log := slog.Default()

	mustExec(t, db, `INSERT INTO reader_perspectives (id, novel_id, type, content) VALUES
		(1, 1, 'known', '读者知道宝藏位置'),
		(2, 1, 'known', '读者知道宝藏位置'),
		(3, 1, 'known', '  读者知道宝藏位置  '),
		(4, 1, 'suspense', '读者等待真相揭晓')`)
	// fact_id=2 指向将被删除的重复读者行 id=2
	mustExec(t, db, `INSERT INTO character_fact_knowledge (id, novel_id, char_id, fact_id) VALUES (2, 1, 1, 2)`)

	require.NoError(t, dedupReaders(db, log))

	// known+归一化"读者知道宝藏位置" 三份合并为 1 份（保留最大 id=3）
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM reader_perspectives WHERE novel_id=1 AND type='known'`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM reader_perspectives WHERE id=3`))
	require.Equal(t, int64(0), countWhere(t, db, `SELECT COUNT(*) FROM reader_perspectives WHERE id IN (1,2)`))
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM reader_perspectives WHERE id=4`))
	// fact_id 重指到保留行 3
	require.Equal(t, int64(1), countWhere(t, db, `SELECT COUNT(*) FROM character_fact_knowledge WHERE id=2 AND fact_id=3`))

	// 回填 content_hash 后满足 (novel_id, type, content_hash) 唯一约束
	require.NoError(t, populateReaderHash(db, log))
	require.Equal(t, int64(2), countWhere(t, db, `SELECT COUNT(*) FROM reader_perspectives WHERE content_hash <> ''`))
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX uk_t_rp ON reader_perspectives(novel_id, type, content_hash)").Error)
}

// TestNeedsDedup 锁死检测函数：有重复时返回 true，去重后返回 false。
func TestNeedsDedup(t *testing.T) {
	db := openTestDB(t)
	need, err := needsDedup(db)
	require.NoError(t, err)
	require.False(t, need, "空库不应需要迁移")

	mustExec(t, db, `INSERT INTO characters (id, novel_id, name) VALUES (1,1,'甲'),(2,1,'甲')`)
	need, err = needsDedup(db)
	require.NoError(t, err)
	require.True(t, need, "存在同名角色应需要迁移")

	require.NoError(t, dedupCharacters(db, slog.Default()))
	need, err = needsDedup(db)
	require.NoError(t, err)
	require.False(t, need, "去重后不应再需要迁移")
}
