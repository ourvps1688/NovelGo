package v162

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/migrate/engine"
)

// backfillKind 在 novels 表新增 kind 列后，将历史行回填为 writing。
// AutoMigrate 加列时若带默认值，已有行会被填为 writing；此处做幂等兜底，
// 覆盖 GORM 未写默认值的情况，保证参考书分离功能在老库上可用。
type backfillKind struct{}

func (backfillKind) Key() string { return "1-backfill-novel-kind-writing" }

func (backfillKind) Run(db *gorm.DB, _ *slog.Logger) error {
	if !db.Migrator().HasColumn("novels", "kind") {
		return nil
	}
	if err := db.Exec(
		`UPDATE novels SET kind = 'writing' WHERE kind IS NULL OR kind = ''`,
	).Error; err != nil {
		return fmt.Errorf("backfill novel kind: %w", err)
	}
	return nil
}

func needsKindBackfill(db *gorm.DB) (bool, error) {
	if !db.Migrator().HasColumn("novels", "kind") {
		return true, nil
	}
	var cnt int64
	if err := db.Model(&struct{}{}).
		Raw(`SELECT COUNT(*) FROM novels WHERE kind IS NULL OR kind = ''`).
		Scan(&cnt).Error; err != nil {
		return false, fmt.Errorf("detect novel kind backfill: %w", err)
	}
	return cnt > 0, nil
}

func init() {
	engine.Register(engine.Migration{
		Name:            "v1.6.2-novel-kind",
		Description:     "为 novels 表新增 kind 列（writing/reference），历史书回填为 writing",
		Destructive:     false,
		NeedsMigration:  needsKindBackfill,
		PostSchemaSteps: []engine.Step{backfillKind{}},
	})
}
