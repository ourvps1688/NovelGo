package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

// ── 测试模型（与真实台账同形的最小镜像，避免 storage←→域包循环导入）──

// LedgerTestEntry 镜像 characters 的关键列形态：字符串/JSON/指针三态/时间戳全套。
type LedgerTestEntry struct {
	ID                 int64      `gorm:"column:id;primaryKey;autoIncrement"`
	NovelID            int64      `gorm:"column:novel_id;not null;index"`
	Name               string     `gorm:"column:name;not null"`
	Description        string     `gorm:"column:description"`
	Abilities          string     `gorm:"column:abilities"`
	Alive              *bool      `gorm:"column:alive;default:true"`
	ChapterRef         *int64     `gorm:"column:chapter_ref"`
	CreatedAt          time.Time  `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time  `gorm:"column:updated_at;autoUpdateTime"`
	VolumeID           *int64     `gorm:"column:volume_id;index"`
	ChangedAt          *time.Time `gorm:"column:changed_at"`
	ChangedInVolumeID  *int64     `gorm:"column:changed_in_volume_id"`
	ChangedInChapterID *int64     `gorm:"column:changed_in_chapter_id"`
}

func (LedgerTestEntry) TableName() string { return "ledger_test_entries" }

// LedgerTestVolume 镜像 volumes 的冻结列（retcon 判定只读 frozen）。
type LedgerTestVolume struct {
	ID     int64 `gorm:"column:id;primaryKey;autoIncrement"`
	Frozen bool  `gorm:"column:frozen;not null;default:false"`
}

func (LedgerTestVolume) TableName() string { return "volumes" }

// LedgerTestPlain 无 changed_at 的普通表：写它不应产生任何履历。
type LedgerTestPlain struct {
	ID   int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name string `gorm:"column:name"`
}

func (LedgerTestPlain) TableName() string { return "ledger_test_plain" }

func setupLedgerTemporalDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ledger.db")), &gorm.Config{
		Logger: glog.Default.LogMode(glog.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// 显式关闭连接：Windows 无法删除仍被占用的 db 文件，不关会让 t.TempDir() 清理失败。
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&LedgerTestEntry{}, &LedgerTestVolume{}, &LedgerTestPlain{}, &LedgerChangeHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := RegisterLedgerTemporalHooks(db); err != nil {
		t.Fatalf("register ledger hooks: %v", err)
	}
	return db
}

func historyRows(t *testing.T, db *gorm.DB) []LedgerChangeHistory {
	t.Helper()
	var rows []LedgerChangeHistory
	if err := db.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("query history: %v", err)
	}
	return rows
}

// ── create ─────────────────────────────────────────────

// TestLedgerTemporalCreate 建行即置 changed_at + 记 (create) 摘要履历（source=author 兜底）。
func TestLedgerTemporalCreate(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	e := LedgerTestEntry{NovelID: 1, Name: "陈三斗", Description: "陈守拙之父"}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.ChangedAt == nil {
		t.Fatal("create 后 changed_at 不应为 NULL")
	}

	rows := historyRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("history rows = %d, want 1: %+v", len(rows), rows)
	}
	h := rows[0]
	if h.EntryTable != "ledger_test_entries" || h.EntryID != e.ID || h.NovelID != 1 {
		t.Errorf("history 定位错误: %+v", h)
	}
	if h.Field != HistoryFieldCreate || h.Old != "" || h.New != "陈三斗" {
		t.Errorf("history (create) 内容错误: %+v", h)
	}
	if h.Source != HistorySourceAuthor {
		t.Errorf("无 TurnInfo/标注的写入 source = %q, want author", h.Source)
	}
}

// ── update：语义变更 vs 格式调整 ─────────────────────────

// TestLedgerTemporalUpdateSemanticVsFormat 核心判定：
// 语义变更→changed_at+逐字段履历；纯格式调整→只落盘（updated_at 动，changed_at/履历不动）；
// 一次多字段语义变更→逐字段多条履历（at 各自唯一，不撞 (entry_table,entry_id,at) 唯一索引）。
func TestLedgerTemporalUpdateSemanticVsFormat(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	e := LedgerTestEntry{NovelID: 1, Name: "苏阿水", Description: "外号水鬼苏", Abilities: `["水性","潜水"]`}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	createdChangedAt := *e.ChangedAt

	// ① 格式调整：仅空白差异 → 无履历、changed_at 不动
	var cur LedgerTestEntry
	if err := db.First(&cur, e.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	updatedAtBefore := cur.UpdatedAt
	cur.Description = "外号水鬼苏  "
	if err := db.Save(&cur).Error; err != nil {
		t.Fatalf("save format-only: %v", err)
	}
	if rows := historyRows(t, db); len(rows) != 1 {
		t.Fatalf("格式调整不应记履历: %+v", rows)
	}
	if cur.ChangedAt == nil || !cur.ChangedAt.Equal(createdChangedAt) {
		t.Errorf("格式调整不应动 changed_at: %v -> %v", createdChangedAt, cur.ChangedAt)
	}
	if !cur.UpdatedAt.After(updatedAtBefore) {
		t.Errorf("格式调整应更新 updated_at: %v -> %v", updatedAtBefore, cur.UpdatedAt)
	}

	// ② JSON 等价重排（空白差异）→ 同样算格式调整
	cur.Abilities = `[ "水性", "潜水" ]`
	if err := db.Save(&cur).Error; err != nil {
		t.Fatalf("save json-format-only: %v", err)
	}
	if rows := historyRows(t, db); len(rows) != 1 {
		t.Fatalf("JSON 等价重排不应记履历: %+v", rows)
	}

	// ③ 一次 Save 两处语义变更 → 两条逐字段履历，at 各不相同
	cur.Name = "苏阿水（水鬼）"
	cur.Abilities = `["水性","潜水","闭气"]`
	if err := db.Save(&cur).Error; err != nil {
		t.Fatalf("save semantic: %v", err)
	}
	if cur.ChangedAt == nil || !cur.ChangedAt.After(createdChangedAt) {
		t.Errorf("语义变更应推进 changed_at: %v -> %v", createdChangedAt, cur.ChangedAt)
	}
	rows := historyRows(t, db)
	if len(rows) != 3 {
		t.Fatalf("history rows = %d, want 3: %+v", len(rows), rows)
	}
	byField := map[string]LedgerChangeHistory{}
	for _, h := range rows[1:] {
		byField[h.Field] = h
	}
	name := byField["name"]
	if name.Old != "苏阿水" || name.New != "苏阿水（水鬼）" {
		t.Errorf("name 履历错误: %+v", name)
	}
	ab := byField["abilities"]
	// 注意 Old 是步骤②格式重排后落库的字面值（`[ "水性", "潜水" ]`）——diff 比的是语义等价，
	// 但履历记的是 DB 里真实的旧值原文。
	if ab.Old != `[ "水性", "潜水" ]` || ab.New != `["水性","潜水","闭气"]` {
		t.Errorf("abilities 履历错误: %+v", ab)
	}
	if !name.At.Before(ab.At) {
		t.Errorf("同事件多字段履历 at 应互不相同: %v vs %v", name.At, ab.At)
	}
}

// TestLedgerTemporalUpdateNoOp 原样 Save（无任何差异）不记履历、不动 changed_at。
func TestLedgerTemporalUpdateNoOp(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	e := LedgerTestEntry{NovelID: 1, Name: "小满", Description: "周德贵的表侄"}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	createdChangedAt := *e.ChangedAt

	var cur LedgerTestEntry
	if err := db.First(&cur, e.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := db.Save(&cur).Error; err != nil {
		t.Fatalf("save noop: %v", err)
	}
	if rows := historyRows(t, db); len(rows) != 1 {
		t.Fatalf("no-op Save 不应记履历: %+v", rows)
	}
	if !cur.ChangedAt.Equal(createdChangedAt) {
		t.Errorf("no-op Save 不应动 changed_at: %v -> %v", createdChangedAt, cur.ChangedAt)
	}
}

// ── delete ─────────────────────────────────────────────

// TestLedgerTemporalDelete 删条目记 (delete) 摘要履历（旧值=摘要文本）。
func TestLedgerTemporalDelete(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	e := LedgerTestEntry{NovelID: 1, Name: "陈守拙"}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Delete(&e).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}

	rows := historyRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("history rows = %d, want 2: %+v", len(rows), rows)
	}
	h := rows[1]
	if h.Field != HistoryFieldDelete || h.Old != "陈守拙" || h.New != "" {
		t.Errorf("history (delete) 内容错误: %+v", h)
	}
}

// ── source 判定（auto / author / retcon）────────────────

// TestLedgerTemporalSource 三种 source 的判定链：显式标注 > TurnInfo > author 兜底，
// 冻结卷条目统一升级 retcon。
func TestLedgerTemporalSource(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	frozenID := int64(1)
	openID := int64(2)
	for _, v := range []LedgerTestVolume{{ID: frozenID, Frozen: true}, {ID: openID, Frozen: false}} {
		if err := db.Create(&v).Error; err != nil {
			t.Fatalf("create volume: %v", err)
		}
	}

	mk := func(t *testing.T, volID *int64) int64 {
		t.Helper()
		e := LedgerTestEntry{NovelID: 1, Name: "条目", VolumeID: volID}
		if err := db.Create(&e).Error; err != nil {
			t.Fatalf("create entry: %v", err)
		}
		return e.ID
	}
	touch := func(t *testing.T, id int64) {
		t.Helper()
		var e LedgerTestEntry
		if err := db.First(&e, id).Error; err != nil {
			t.Fatalf("load: %v", err)
		}
		e.Description = "改过了"
		if err := db.Save(&e).Error; err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	sourceOf := func(t *testing.T, entryID int64) string {
		t.Helper()
		var h LedgerChangeHistory
		if err := db.Where("entry_id = ? AND field = ?", entryID, HistoryFieldCreate).First(&h).Error; err != nil {
			t.Fatalf("query (create) history: %v", err)
		}
		return h.Source
	}

	// author 兜底（前端 CRUD 形态：无标注、无 TurnInfo）
	noVol := mk(t, nil)
	unfrozen := mk(t, &openID)
	frozen := mk(t, &frozenID)
	if got := sourceOf(t, noVol); got != HistorySourceAuthor {
		t.Errorf("无卷条目 source = %q, want author", got)
	}
	if got := sourceOf(t, unfrozen); got != HistorySourceAuthor {
		t.Errorf("未冻结卷条目 source = %q, want author", got)
	}
	if got := sourceOf(t, frozen); got != HistorySourceRetcon {
		t.Errorf("冻结卷条目 source = %q, want retcon", got)
	}

	// TurnInfo（agent 回合）→ auto
	turnCtx := WithTurn(context.Background(), "sess", 1)
	e := LedgerTestEntry{NovelID: 1, Name: "AI 建的"}
	if err := db.WithContext(turnCtx).Create(&e).Error; err != nil {
		t.Fatalf("create with turn: %v", err)
	}
	var h LedgerChangeHistory
	if err := db.Where("entry_id = ? AND field = ?", e.ID, HistoryFieldCreate).First(&h).Error; err != nil {
		t.Fatalf("query history: %v", err)
	}
	if h.Source != HistorySourceAuto {
		t.Errorf("agent 回合写入 source = %q, want auto", h.Source)
	}

	// 显式标注优先；冻结卷条目即便标注 auto 也升级 retcon
	autoCtx := WithLedgerSource(context.Background(), HistorySourceAuto)
	autoEntry := LedgerTestEntry{NovelID: 1, Name: "工具建的", VolumeID: &frozenID}
	if err := db.WithContext(autoCtx).Create(&autoEntry).Error; err != nil {
		t.Fatalf("create with auto source: %v", err)
	}
	if got := sourceOf(t, autoEntry.ID); got != HistorySourceRetcon {
		t.Errorf("冻结卷 + auto 标注 source = %q, want retcon 升级", got)
	}

	// 更新同样走冻结升级
	touch(t, frozen)
	var upd LedgerChangeHistory
	if err := db.Where("entry_id = ? AND field = ?", frozen, "description").First(&upd).Error; err != nil {
		t.Fatalf("query update history: %v", err)
	}
	if upd.Source != HistorySourceRetcon {
		t.Errorf("冻结卷条目更新 source = %q, want retcon", upd.Source)
	}
	touch(t, noVol)
	// 必须用全新变量：GORM 对 Dest 为 ModelType 结构体且 PK 非零时会自动把 PK 追加进 WHERE，
	// 复用上面已填过 ID 的 upd 会带上 id=<旧行ID> 条件而查不到。
	var upd2 LedgerChangeHistory
	if err := db.Where("entry_id = ? AND field = ?", noVol, "description").First(&upd2).Error; err != nil {
		t.Fatalf("query update history: %v", err)
	}
	if upd2.Source != HistorySourceAuthor {
		t.Errorf("无卷条目更新 source = %q, want author", upd2.Source)
	}
}

// ── 边界 ───────────────────────────────────────────────

// TestLedgerTemporalIgnoresNonLedgerTables 无 changed_at 的表完全无感（不写履历、不置位）。
func TestLedgerTemporalIgnoresNonLedgerTables(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	p := LedgerTestPlain{Name: "普通表"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	p.Name = "改了"
	if err := db.Save(&p).Error; err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := db.Delete(&p).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rows := historyRows(t, db); len(rows) != 0 {
		t.Fatalf("非台账表不应产生履历: %+v", rows)
	}
}

// TestLedgerTemporalBatchWhereSkipped 带 WHERE 的批量写（无单行 PK Dest）不拦截——
// 存量迁移/整书删除旁路不要求履历，且不得报错。
func TestLedgerTemporalBatchWhereSkipped(t *testing.T) {
	db := setupLedgerTemporalDB(t)

	e := LedgerTestEntry{NovelID: 1, Name: "批量"}
	if err := db.Create(&e).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Model(&LedgerTestEntry{}).Where("id = ?", e.ID).
		Update("description", "批量更新").Error; err != nil {
		t.Fatalf("batch update: %v", err)
	}
	if err := db.Where("novel_id = ?", 1).Delete(&LedgerTestEntry{}).Error; err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	rows := historyRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("批量写只应留下 create 的履历: %+v", rows)
	}
}
