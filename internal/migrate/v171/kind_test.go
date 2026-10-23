package v171

// v1.7.1-retcon-kind 白盒：kind 空值回填 fact、style 行原样、幂等。

import (
	"log/slog"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openStepTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Exec(`CREATE TABLE retcon_cases (
		id integer PRIMARY KEY,
		novel_id integer NOT NULL,
		kind TEXT NOT NULL DEFAULT 'fact'
	)`).Error; err != nil {
		t.Fatalf("create retcon_cases: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO retcon_cases (id, novel_id, kind) VALUES (1, 1, '')`,
		`INSERT INTO retcon_cases (id, novel_id, kind) VALUES (2, 1, 'style')`,
		`INSERT INTO retcon_cases (id, novel_id, kind) VALUES (3, 1, 'fact')`,
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return db
}

// TestBackfillRetconKind 验证空 kind 回填 fact、显式 kind 不动、重复 Run 幂等。
func TestBackfillRetconKind(t *testing.T) {
	db := openStepTestDB(t)
	log := slog.Default()

	if err := (backfillRetconKind{}).Run(db, log); err != nil {
		t.Fatalf("run: %v", err)
	}
	var blank int64
	if err := db.Raw(`SELECT COUNT(*) FROM retcon_cases WHERE kind IS NULL OR kind = ''`).Scan(&blank).Error; err != nil {
		t.Fatal(err)
	}
	if blank != 0 {
		t.Fatalf("空 kind 应全部回填 fact: 空行=%d", blank)
	}
	var style int64
	if err := db.Raw(`SELECT COUNT(*) FROM retcon_cases WHERE id = 2 AND kind = 'style'`).Scan(&style).Error; err != nil {
		t.Fatal(err)
	}
	if style != 1 {
		t.Fatal("style 行不得被回填覆盖")
	}

	// 幂等：二次 Run 不报错、不改值。
	if err := (backfillRetconKind{}).Run(db, log); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	var fact int64
	if err := db.Raw(`SELECT COUNT(*) FROM retcon_cases WHERE id = 1 AND kind = 'fact'`).Scan(&fact).Error; err != nil {
		t.Fatal(err)
	}
	if fact != 1 {
		t.Fatal("回填值应为 fact")
	}

	// 列缺失时安全返回（老库无表场景）。
	db2, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := (backfillRetconKind{}).Run(db2, log); err != nil {
		t.Fatalf("无表 run: %v", err)
	}
}
