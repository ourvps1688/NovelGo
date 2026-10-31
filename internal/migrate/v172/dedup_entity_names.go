// Package v172 注册四类台账（角色/地点/弧线/读者视角）的"按自然键去重 + 加唯一索引"
// 迁移，从 DB 层根治重复的台账记录。
//
// 背景：create_character / create_location / create_story_arc / create_reader_perspective_entry
// 以及对应 app 层创建入口历史上无条件 INSERT，导致世界观抽取/一致性巡检重跑时同一实体被
// 反复追加成多份相同记录。本项目 volumes 表早已用 uniqueIndex 强制"每本小说卷名唯一"，
// 但上述四类漏了。
//
// 做法分两层：
//  1. 代码层（见各 Store.GetByName / app 层 / MCP 工具）：创建入口改为 find-or-create，
//     同名/同内容已存在则复用、不覆盖，立即止血（零 schema 风险）。
//  2. DB 层（本迁移）：为 characters/locations/story_arcs 加 (novel_id, name) 唯一索引、
//     为 reader_perspectives 加 (novel_id, type, content_hash) 唯一索引，作为结构性兜底。
//
// 唯一索引只能建在已去重的数据上，因此本迁移的 PreSchemaStep 在 AutoMigrate 建索引之前，
// 先把存量重复行按自然键合并（保留"内容字段填得最多、其次 updated_at 最新、再其次 id 最大"
// 的一行，其余行的外键引用重指到保留行后删除），并折叠重指可能产生的子表重复边/节点。
// 该迁移标记为 Destructive，框架会在执行前自动备份全量数据。
package v172

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/migrate/engine"
	"github.com/novelgo/novelgo/internal/reader"
)

// ── characters 行快照（仅取去重评分与重指所需列）───────────────────────

type charRow struct {
	ID                int64
	NovelID           int64
	Name              string
	Description       string
	Personality       string
	Abilities         string
	Aliases           string
	Gender            string
	BirthYear         string
	AppearanceAnchors string
	LocationRef       string
	Faction           string
	Role              string
	CapabilityCeiling string
	VoiceTraits       string
	UpdatedAt         time.Time
}

type locRow struct {
	ID           int64
	NovelID      int64
	Name         string
	Description  string
	DetailJSON   string
	Tags         string
	LocationType string
	Controller   string
	State        string
	UpdatedAt    time.Time
}

type arcRow struct {
	ID           int64
	NovelID      int64
	Name         string
	Description  string
	Motif        string
	ReactivateAt string
	UpdatedAt    time.Time
}

type readerRow struct {
	ID      int64
	NovelID int64
	Type    string
	Content string
}

// ── 通用子过程 ────────────────────────────────────────────

// scoreChar 统计 characters 内容字段中非空（去空白后）的个数，作为"哪行更有价值"的评分。
func scoreChar(r charRow) int {
	s := 0
	for _, v := range []string{
		r.Description, r.Personality, r.Abilities, r.Aliases, r.Gender,
		r.BirthYear, r.AppearanceAnchors, r.LocationRef, r.Faction, r.Role,
		r.CapabilityCeiling, r.VoiceTraits,
	} {
		if strings.TrimSpace(v) != "" {
			s++
		}
	}
	return s
}

// scoreLoc 统计 locations 内容字段非空个数。
func scoreLoc(r locRow) int {
	s := 0
	for _, v := range []string{r.Description, r.DetailJSON, r.Tags, r.LocationType, r.Controller, r.State} {
		if strings.TrimSpace(v) != "" {
			s++
		}
	}
	return s
}

// scoreArc 统计 story_arcs 内容字段非空个数。
func scoreArc(r arcRow) int {
	s := 0
	for _, v := range []string{r.Description, r.Motif, r.ReactivateAt} {
		if strings.TrimSpace(v) != "" {
			s++
		}
	}
	return s
}

// repointFKs 把子表中外键 dupID 全部重指到 keepID（按 novel_id 限定范围）。
func repointFKs(db *gorm.DB, novelID, dupID, keepID int64, refs [][2]string) error {
	for _, r := range refs {
		if err := db.Exec(
			fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ? AND novel_id = ?", r[0], r[1], r[1]),
			keepID, dupID, novelID,
		).Error; err != nil {
			return fmt.Errorf("repoint %s.%s: %w", r[0], r[1], err)
		}
	}
	return nil
}

// repointLocationRelations 重指 location_relations 中指向废弃地点 dupID 的边到 keepID。
// 与普通外键不同，location_relations 上有 uk_location_pair 唯一索引（location_a, location_b），
// 普通 UPDATE 在「重指后会与已有 (a,b) 对撞」时会被唯一索引拒绝。因此这里先删除会撞对的
// 冗余边，再把剩余边重指——保证迁移不会因唯一索引冲突而失败。
func repointLocationRelations(db *gorm.DB, novelID, dupID, keepID int64) error {
	for _, col := range []string{"location_a", "location_b"} {
		other := "location_b"
		if col == "location_b" {
			other = "location_a"
		}
		// 先删「重指后会撞 (location_a, location_b) 唯一对」的冗余边。
		if err := db.Exec(fmt.Sprintf(`
			DELETE FROM location_relations
			WHERE novel_id = ? AND %s = ?
			  AND EXISTS (
				SELECT 1 FROM location_relations r2
				WHERE r2.novel_id = location_relations.novel_id
				  AND r2.%s = ?
				  AND r2.%s = location_relations.%s
				  AND r2.id != location_relations.id
			  )`, col, col, other, other),
			novelID, dupID, keepID).Error; err != nil {
			return fmt.Errorf("delete colliding location_relation: %w", err)
		}
		// 再重指剩余边（此时已无撞对风险）。
		if err := db.Exec(fmt.Sprintf(
			"UPDATE location_relations SET %s = ? WHERE %s = ? AND novel_id = ?", col, col),
			keepID, dupID, novelID).Error; err != nil {
			return fmt.Errorf("repoint location_relations.%s: %w", col, err)
		}
	}
	return nil
}

// collapseDuplicates 保留每组 min(id)，删除其余，消除重指后产生的子表重复行。
// groupCols 描述"视为同一"的分组列（如 character_relations 的 来源/目标/方向/类型/描述）。
func collapseDuplicates(db *gorm.DB, log *slog.Logger, table string, groupCols []string) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	var cnt int64
	if err := db.Raw(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&cnt).Error; err != nil {
		return fmt.Errorf("count %s: %w", table, err)
	}
	if cnt == 0 {
		return nil
	}
	sub := fmt.Sprintf("SELECT MIN(id) FROM %s GROUP BY %s", table, strings.Join(groupCols, ", "))
	res := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE id NOT IN (%s)", table, sub))
	if res.Error != nil {
		return fmt.Errorf("collapse %s: %w", table, res.Error)
	}
	if res.RowsAffected > 0 {
		log.Info("台账去重：折叠子表重复行", "table", table, "rows", res.RowsAffected)
	}
	return nil
}

// ── 各表去重（PreSchemaStep）──────────────────────────────

func dedupCharacters(db *gorm.DB, log *slog.Logger) error {
	if !db.Migrator().HasTable("characters") {
		return nil
	}
	var rows []charRow
	if err := db.Table("characters").Order("id DESC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load characters: %w", err)
	}
	type key struct {
		n    int64
		name string
	}
	groups := map[key][]charRow{}
	for _, r := range rows {
		k := key{r.NovelID, r.Name}
		groups[k] = append(groups[k], r)
	}
	refs := [][2]string{
		{"character_relations", "source_character_id"},
		{"character_relations", "target_character_id"},
		{"group_members", "character_id"},
		{"character_fact_knowledge", "char_id"},
		{"pov_roster", "char_id"},
		{"story_arcs", "char_id"},
	}
	for k, grp := range groups {
		if len(grp) <= 1 {
			continue
		}
		best := grp[0] // 已按 id DESC，grp[0] 是最大 id
		bestScore := scoreChar(best)
		for _, r := range grp[1:] {
			if s := scoreChar(r); s > bestScore {
				best = r
				bestScore = s
			}
		}
		for _, r := range grp {
			if r.ID == best.ID {
				continue
			}
			if err := repointFKs(db, k.n, r.ID, best.ID, refs); err != nil {
				return err
			}
			if err := db.Exec("DELETE FROM characters WHERE id = ?", r.ID).Error; err != nil {
				return fmt.Errorf("delete dup character: %w", err)
			}
		}
	}
	if err := collapseDuplicates(db, log, "character_relations",
		[]string{"novel_id", "source_character_id", "target_character_id", "is_current", "relation_kind", "relation_describe"}); err != nil {
		return err
	}
	if err := collapseDuplicates(db, log, "group_members",
		[]string{"novel_id", "group_id", "character_id"}); err != nil {
		return err
	}
	if err := collapseDuplicates(db, log, "character_fact_knowledge",
		[]string{"novel_id", "char_id", "fact_id"}); err != nil {
		return err
	}
	if err := collapseDuplicates(db, log, "pov_roster",
		[]string{"novel_id", "char_id", "mode", "volume_id"}); err != nil {
		return err
	}
	return nil
}

func dedupLocations(db *gorm.DB, log *slog.Logger) error {
	if !db.Migrator().HasTable("locations") {
		return nil
	}
	var rows []locRow
	if err := db.Table("locations").Order("id DESC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load locations: %w", err)
	}
	type key struct {
		n    int64
		name string
	}
	groups := map[key][]locRow{}
	for _, r := range rows {
		k := key{r.NovelID, r.Name}
		groups[k] = append(groups[k], r)
	}
	for k, grp := range groups {
		if len(grp) <= 1 {
			continue
		}
		best := grp[0]
		bestScore := scoreLoc(best)
		for _, r := range grp[1:] {
			if s := scoreLoc(r); s > bestScore {
				best = r
				bestScore = s
			}
		}
		for _, r := range grp {
			if r.ID == best.ID {
				continue
			}
			// location_relations 有 uk_location_pair 唯一索引，必须用碰撞安全的重指。
			if err := repointLocationRelations(db, k.n, r.ID, best.ID); err != nil {
				return err
			}
			if err := db.Exec("DELETE FROM locations WHERE id = ?", r.ID).Error; err != nil {
				return fmt.Errorf("delete dup location: %w", err)
			}
		}
	}
	if err := collapseDuplicates(db, log, "location_relations",
		[]string{"novel_id", "location_a", "location_b"}); err != nil {
		return err
	}
	return nil
}

func dedupStoryArcs(db *gorm.DB, log *slog.Logger) error {
	if !db.Migrator().HasTable("story_arcs") {
		return nil
	}
	var rows []arcRow
	if err := db.Table("story_arcs").Order("id DESC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load story_arcs: %w", err)
	}
	type key struct {
		n    int64
		name string
	}
	groups := map[key][]arcRow{}
	for _, r := range rows {
		k := key{r.NovelID, r.Name}
		groups[k] = append(groups[k], r)
	}
	refs := [][2]string{
		{"arc_nodes", "story_arc_id"},
	}
	for k, grp := range groups {
		if len(grp) <= 1 {
			continue
		}
		best := grp[0]
		bestScore := scoreArc(best)
		for _, r := range grp[1:] {
			if s := scoreArc(r); s > bestScore {
				best = r
				bestScore = s
			}
		}
		for _, r := range grp {
			if r.ID == best.ID {
				continue
			}
			if err := repointFKs(db, k.n, r.ID, best.ID, refs); err != nil {
				return err
			}
			if err := db.Exec("DELETE FROM story_arcs WHERE id = ?", r.ID).Error; err != nil {
				return fmt.Errorf("delete dup story_arc: %w", err)
			}
		}
	}
	return nil
}

func dedupReaders(db *gorm.DB, log *slog.Logger) error {
	if !db.Migrator().HasTable("reader_perspectives") {
		return nil
	}
	var rows []readerRow
	if err := db.Table("reader_perspectives").Order("id DESC").Find(&rows).Error; err != nil {
		return fmt.Errorf("load reader_perspectives: %w", err)
	}
	// 按 (novel_id, type, 归一化内容哈希) 分组：空格/换行差异的同内容视为重复。
	type key struct {
		n    int64
		t    string
		hash string
	}
	groups := map[key][]readerRow{}
	for _, r := range rows {
		k := key{r.NovelID, r.Type, reader.ContentHash(r.Content)}
		groups[k] = append(groups[k], r)
	}
	refs := [][2]string{
		{"character_fact_knowledge", "fact_id"},
	}
	for k, grp := range groups {
		if len(grp) <= 1 {
			continue
		}
		best := grp[0] // 最大 id；内容相同，保留较晚创建的一份
		for _, r := range grp[1:] {
			if r.ID > best.ID {
				best = r
			}
		}
		for _, r := range grp {
			if r.ID == best.ID {
				continue
			}
			if err := repointFKs(db, k.n, r.ID, best.ID, refs); err != nil {
				return err
			}
			if err := db.Exec("DELETE FROM reader_perspectives WHERE id = ?", r.ID).Error; err != nil {
				return fmt.Errorf("delete dup reader_perspective: %w", err)
			}
		}
	}
	if err := collapseDuplicates(db, log, "character_fact_knowledge",
		[]string{"novel_id", "char_id", "fact_id"}); err != nil {
		return err
	}
	return nil
}

// populateReaderHash 在 AutoMigrate 补齐 content_hash 列与唯一索引之后，为存量行回填哈希，
// 使其满足 (novel_id, type, content_hash) 唯一约束。PreSchemaStep 已按该哈希去重，故不会冲突。
func populateReaderHash(db *gorm.DB, log *slog.Logger) error {
	if !db.Migrator().HasTable("reader_perspectives") {
		return nil
	}
	// content_hash 是本迁移才新增的列。必须在 AutoMigrate 建立 (novel_id, type, content_hash)
	// 唯一索引之前把它补齐：否则存量行 content_hash 全为空，唯一索引会因「多行空值」冲突而建不起来。
	if !db.Migrator().HasColumn("reader_perspectives", "content_hash") {
		if err := db.Migrator().AddColumn(&reader.ReaderPerspective{}, "ContentHash"); err != nil {
			return fmt.Errorf("add content_hash column: %w", err)
		}
	}
	var pending []struct {
		ID      int64
		Content string
	}
	if err := db.Raw(
		"SELECT id, content FROM reader_perspectives WHERE content_hash IS NULL OR content_hash = ''",
	).Scan(&pending).Error; err != nil {
		return fmt.Errorf("load pending reader hashes: %w", err)
	}
	for _, r := range pending {
		if err := db.Exec(
			"UPDATE reader_perspectives SET content_hash = ? WHERE id = ?",
			reader.ContentHash(r.Content), r.ID,
		).Error; err != nil {
			return fmt.Errorf("set reader hash: %w", err)
		}
	}
	if len(pending) > 0 {
		log.Info("台账去重：回填 reader content_hash", "rows", len(pending))
	}
	return nil
}

// ── 步骤与注册 ────────────────────────────────────────────

type dedupCharactersStep struct{}

func (dedupCharactersStep) Key() string { return "1-dedup-characters" }
func (dedupCharactersStep) Run(db *gorm.DB, log *slog.Logger) error {
	return dedupCharacters(db, log)
}

type dedupLocationsStep struct{}

func (dedupLocationsStep) Key() string { return "2-dedup-locations" }
func (dedupLocationsStep) Run(db *gorm.DB, log *slog.Logger) error {
	return dedupLocations(db, log)
}

type dedupStoryArcsStep struct{}

func (dedupStoryArcsStep) Key() string { return "3-dedup-story-arcs" }
func (dedupStoryArcsStep) Run(db *gorm.DB, log *slog.Logger) error {
	return dedupStoryArcs(db, log)
}

type dedupReadersStep struct{}

func (dedupReadersStep) Key() string { return "4-dedup-reader-perspectives" }
func (dedupReadersStep) Run(db *gorm.DB, log *slog.Logger) error {
	return dedupReaders(db, log)
}

type populateReaderHashStep struct{}

func (populateReaderHashStep) Key() string { return "5-populate-reader-hash" }
func (populateReaderHashStep) Run(db *gorm.DB, log *slog.Logger) error {
	return populateReaderHash(db, log)
}

// hasDupByName 检测某表是否存在 (novel_id, name) 重复组。
func hasDupByName(db *gorm.DB, table, col string) (bool, error) {
	if !db.Migrator().HasTable(table) {
		return false, nil
	}
	var c int64
	q := fmt.Sprintf(
		"SELECT COUNT(*) FROM (SELECT %s FROM %s GROUP BY novel_id, %s HAVING COUNT(*) > 1)",
		col, table, col,
	)
	if err := db.Raw(q).Scan(&c).Error; err != nil {
		return false, fmt.Errorf("detect dup %s.%s: %w", table, col, err)
	}
	return c > 0, nil
}

// needsDedup 识别既有库是否仍处于旧 schema（存在重复行，或 reader 缺 content_hash 回填）。
func needsDedup(db *gorm.DB) (bool, error) {
	for _, tc := range []struct{ table, col string }{
		{"characters", "name"},
		{"locations", "name"},
		{"story_arcs", "name"},
	} {
		need, err := hasDupByName(db, tc.table, tc.col)
		if err != nil {
			return false, err
		}
		if need {
			return true, nil
		}
	}
	if db.Migrator().HasTable("reader_perspectives") {
		if db.Migrator().HasColumn("reader_perspectives", "content_hash") {
			var c int64
			if err := db.Raw(
				"SELECT COUNT(*) FROM reader_perspectives WHERE content_hash IS NULL OR content_hash = ''",
			).Scan(&c).Error; err != nil {
				return false, fmt.Errorf("detect reader hash: %w", err)
			}
			if c > 0 {
				return true, nil
			}
		}
		var c int64
		if err := db.Raw(
			`SELECT COUNT(*) FROM (SELECT novel_id, type, content FROM reader_perspectives GROUP BY novel_id, type, content HAVING COUNT(*) > 1)`,
		).Scan(&c).Error; err != nil {
			return false, fmt.Errorf("detect dup reader: %w", err)
		}
		if c > 0 {
			return true, nil
		}
	}
	return false, nil
}

func init() {
	engine.Register(engine.Migration{
		Name:           "v1.7.2-entity-name-dedup",
		Description:    "characters/locations/story_arcs 按 (novel_id, name) 去重并加唯一索引；reader_perspectives 按 (novel_id, type, content) 去重 + content_hash 唯一索引。",
		Destructive:    true,
		NeedsMigration: needsDedup,
		// 注意顺序：先按自然键去重，再补齐 reader 的 content_hash 列与值，
		// 最后 AutoMigrate 才建立四个唯一索引——避免"存量空值"导致唯一索引建不起来。
		PreSchemaSteps:  []engine.Step{dedupCharactersStep{}, dedupLocationsStep{}, dedupStoryArcsStep{}, dedupReadersStep{}, populateReaderHashStep{}},
		PostSchemaSteps: []engine.Step{},
	})
}
