// Package v171 注册 retcon_cases.kind 列迁移（P2 T4.2）：历史案回填为 fact
// （全量 Restore 路由），保证存量 retcon 案行为逐字不变。
package v171

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/migrate/engine"
)

// backfillRetconKind 在 retcon_cases 表新增 kind 列后，将历史行回填为 fact。
// AutoMigrate 加列时若带默认值，已有行会被填为 fact；此处做幂等兜底，
// 覆盖 GORM 未写默认值的情况，保证存量案的 Rollback 全量回滚路由不变。
type backfillRetconKind struct{}

func (backfillRetconKind) Key() string { return "1-backfill-retcon-kind-fact" }

func (backfillRetconKind) Run(db *gorm.DB, _ *slog.Logger) error {
	if !db.Migrator().HasColumn("retcon_cases", "kind") {
		return nil
	}
	if err := db.Exec(
		`UPDATE retcon_cases SET kind = 'fact' WHERE kind IS NULL OR kind = ''`,
	).Error; err != nil {
		return fmt.Errorf("backfill retcon kind: %w", err)
	}
	return nil
}

func needsRetconKindBackfill(db *gorm.DB) (bool, error) {
	if !db.Migrator().HasColumn("retcon_cases", "kind") {
		return true, nil
	}
	var cnt int64
	if err := db.Model(&struct{}{}).
		Raw(`SELECT COUNT(*) FROM retcon_cases WHERE kind IS NULL OR kind = ''`).
		Scan(&cnt).Error; err != nil {
		return false, fmt.Errorf("detect retcon kind backfill: %w", err)
	}
	return cnt > 0, nil
}

func init() {
	engine.Register(engine.Migration{
		Name:            "v1.7.1-retcon-kind",
		Description:     "为 retcon_cases 表新增 kind 列（fact/style），历史案回填为 fact",
		Destructive:     false,
		NeedsMigration:  needsRetconKindBackfill,
		PostSchemaSteps: []engine.Step{backfillRetconKind{}},
	})
}
