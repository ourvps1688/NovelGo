// Package v170 注册 series bible 存量迁移（WBS 2.8 / G6，随 v1.7.0 发布）。
//
// 两个幂等步骤：
//  1. temporal 回填——7 张台账表（WBS §0.4 冻结清单中带 volume_id/changed_at 的既有表）
//     的存量行 changed_at 统一置为迁移时刻（「temporal 取迁移时刻」）。
//     volume_id 保持 NULL（语义「无卷/全书级」，见 WBS 2.8「旧条目 volume_id=NULL→无卷」），
//     changed_in_* 无从判定，保持 NULL。
//  2. 四维结构化字段尽力填充——见 rules.go：单一无歧义命中才写、只写空字段、永不覆盖。
package v170

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/migrate/engine"
)

// ledgerTemporalTables 是 §0.4 冻结清单中带 volume_id/changed_at 的 7 张既有台账表
// （第 8 张既有表 volumes 的冻结字段由 1.6 处理，无 volume_id/temporal 回填）。
// 新表（chronology_anchors / character_fact_knowledge / pov_roster 等）2.1 起才建，
// 无存量行，不在此列。
var ledgerTemporalTables = []string{
	"characters",
	"character_relations",
	"locations",
	"story_arcs",
	"arc_nodes",
	"time_entries",
	"reader_perspectives",
}

// backfillTemporalStep 把存量行 changed_at=NULL 统一置为迁移时刻。幂等：只写 NULL 行。
type backfillTemporalStep struct{}

func (backfillTemporalStep) Key() string { return "1-backfill-ledger-changed-at" }

func (backfillTemporalStep) Run(db *gorm.DB, log *slog.Logger) error {
	now := time.Now()
	for _, table := range ledgerTemporalTables {
		if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, "changed_at") {
			continue
		}
		res := db.Exec("UPDATE "+table+" SET changed_at = ? WHERE changed_at IS NULL", now)
		if res.Error != nil {
			return fmt.Errorf("backfill %s.changed_at: %w", table, res.Error)
		}
		if res.RowsAffected > 0 {
			log.Info("台账 temporal 回填", "table", table, "rows", res.RowsAffected)
		}
	}
	return nil
}

// extractStructuredStep 四维结构化字段尽力填充。幂等：只写空字段，永不覆盖已填值。
type extractStructuredStep struct{}

func (extractStructuredStep) Key() string { return "2-extract-structured-fields" }

func (extractStructuredStep) Run(db *gorm.DB, log *slog.Logger) error {
	var errs []error
	errs = append(errs, extractCharacters(db, log)...)
	errs = append(errs, extractRelations(db, log)...)
	errs = append(errs, extractLocations(db, log)...)
	errs = append(errs, extractStoryArcs(db, log)...)
	if len(errs) > 0 {
		return fmt.Errorf("structured field extraction: %w", errs[0])
	}
	return nil
}

func extractCharacters(db *gorm.DB, log *slog.Logger) []error {
	if !db.Migrator().HasTable("characters") {
		return nil
	}
	type row struct {
		ID          int64
		Name        string
		Description string
		Gender      string
		BirthYear   string
		Aliases     string
	}
	var rows []row
	if err := db.Raw(`SELECT id, name, COALESCE(description,'') AS description,
		COALESCE(gender,'') AS gender, COALESCE(birth_year,'') AS birth_year,
		COALESCE(aliases,'') AS aliases FROM characters`).Scan(&rows).Error; err != nil {
		return []error{fmt.Errorf("read characters: %w", err)}
	}
	var errs []error
	filled := 0
	for _, r := range rows {
		updates := map[string]any{}
		if r.Gender == "" {
			if v := matchCharacterGender(r.Name, r.Description); v != "" {
				updates["gender"] = v
			}
		}
		if r.BirthYear == "" {
			if v := matchCharacterBirthYear(r.Description); v != "" {
				updates["birth_year"] = v
			}
		}
		if r.Aliases == "" {
			if v := matchCharacterAliases(r.Description); v != "" {
				updates["aliases"] = v
			}
		}
		if len(updates) == 0 {
			continue
		}
		if err := db.Table("characters").Where("id = ?", r.ID).Updates(updates).Error; err != nil {
			errs = append(errs, fmt.Errorf("update character %d: %w", r.ID, err))
			continue
		}
		filled++
	}
	if filled > 0 {
		log.Info("角色结构化字段尽力填充", "rows", filled)
	}
	return errs
}

func extractRelations(db *gorm.DB, log *slog.Logger) []error {
	if !db.Migrator().HasTable("character_relations") {
		return nil
	}
	type row struct {
		ID               int64
		RelationDescribe string
		Description      string
		RelationKind     string
	}
	var rows []row
	if err := db.Raw(`SELECT id, COALESCE(relation_describe,'') AS relation_describe,
		COALESCE(description,'') AS description, COALESCE(relation_kind,'') AS relation_kind
		FROM character_relations`).Scan(&rows).Error; err != nil {
		return []error{fmt.Errorf("read character_relations: %w", err)}
	}
	var errs []error
	filled := 0
	for _, r := range rows {
		if r.RelationKind != "" {
			continue
		}
		v := matchRelationKind(r.RelationDescribe, r.Description)
		if v == "" {
			continue
		}
		if err := db.Table("character_relations").Where("id = ?", r.ID).
			Update("relation_kind", v).Error; err != nil {
			errs = append(errs, fmt.Errorf("update relation %d: %w", r.ID, err))
			continue
		}
		filled++
	}
	if filled > 0 {
		log.Info("关系软枚举尽力填充", "rows", filled)
	}
	return errs
}

func extractLocations(db *gorm.DB, log *slog.Logger) []error {
	if !db.Migrator().HasTable("locations") {
		return nil
	}
	type row struct {
		ID          int64
		Description string
		DetailJSON  string
		Tags        string
		State       string
	}
	var rows []row
	if err := db.Raw(`SELECT id, COALESCE(description,'') AS description,
		COALESCE(detail_json,'') AS detail_json, COALESCE(tags,'') AS tags,
		COALESCE(state,'') AS state FROM locations`).Scan(&rows).Error; err != nil {
		return []error{fmt.Errorf("read locations: %w", err)}
	}
	var errs []error
	filled := 0
	for _, r := range rows {
		if r.State != "" {
			continue
		}
		v := matchLocationState(r.Description, r.DetailJSON, r.Tags)
		if v == "" {
			continue
		}
		if err := db.Table("locations").Where("id = ?", r.ID).
			Update("state", v).Error; err != nil {
			errs = append(errs, fmt.Errorf("update location %d: %w", r.ID, err))
			continue
		}
		filled++
	}
	if filled > 0 {
		log.Info("地点存续状态尽力填充", "rows", filled)
	}
	return errs
}

func extractStoryArcs(db *gorm.DB, log *slog.Logger) []error {
	if !db.Migrator().HasTable("story_arcs") || !db.Migrator().HasTable("characters") {
		return nil
	}
	var chars []arcChar
	if err := db.Raw(`SELECT id, COALESCE(name,'') AS name FROM characters`).
		Scan(&chars).Error; err != nil {
		return []error{fmt.Errorf("read characters for arcs: %w", err)}
	}
	type row struct {
		ID      int64
		Name    string
		ArcType string
		CharID  *int64
	}
	var rows []row
	if err := db.Raw(`SELECT id, COALESCE(name,'') AS name, COALESCE(arc_type,'') AS arc_type,
		char_id FROM story_arcs WHERE COALESCE(arc_type,'') = 'character'`).Scan(&rows).Error; err != nil {
		return []error{fmt.Errorf("read story_arcs: %w", err)}
	}
	var errs []error
	filled := 0
	for _, r := range rows {
		if r.CharID != nil {
			continue
		}
		id := matchStoryArcCharID(r.Name, chars)
		if id == 0 {
			continue
		}
		if err := db.Table("story_arcs").Where("id = ?", r.ID).
			Update("char_id", id).Error; err != nil {
			errs = append(errs, fmt.Errorf("update story_arc %d: %w", r.ID, err))
			continue
		}
		filled++
	}
	if filled > 0 {
		log.Info("角色弧归属尽力填充", "rows", filled)
	}
	return errs
}

// needsSeriesBibleBackfill 识别库是否仍处于 2.8 之前的存量状态：
//   - 任一台账表缺 changed_at 列（pre-2.1 schema，列由 AutoMigrate 在步骤前补齐）→ 需要；
//   - 任一存量行 changed_at IS NULL（2.1 加列但未回填）→ 需要。
//
// 表缺失跳过（极旧/裁剪库由步骤端同样防御）。仅在 migrate_state 无本迁移记录时调用。
func needsSeriesBibleBackfill(db *gorm.DB) (bool, error) {
	for _, table := range ledgerTemporalTables {
		if !db.Migrator().HasTable(table) {
			continue
		}
		if !db.Migrator().HasColumn(table, "changed_at") {
			return true, nil
		}
		var n int64
		if err := db.Table(table).Where("changed_at IS NULL").Count(&n).Error; err != nil {
			return false, fmt.Errorf("detect series bible backfill %s: %w", table, err)
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

func init() {
	engine.Register(engine.Migration{
		Name:        "v1.7.0-series-bible-backfill",
		Description: "series bible 存量迁移：台账 temporal 回填（changed_at=迁移时刻，volume_id 保持 NULL=无卷）+ 四维结构化字段尽力填充",
		Destructive: false,
		// 数据回填非破坏性（只写 NULL/空字段）；回填本身不改写既有值，无需迁移前备份。
		NeedsMigration:  needsSeriesBibleBackfill,
		PostSchemaSteps: []engine.Step{backfillTemporalStep{}, extractStructuredStep{}},
	})
}
