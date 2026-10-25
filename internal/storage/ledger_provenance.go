package storage

// ledger_provenance.go —— 台账条目 provenance（R2b-7，提案 §8.3 组件 B）：
// 事实条目带来源章节 + 提取置信/人工确认标记；核心事实可钉住（人工确认后
// 任何自动修改需走 retcon 级审批）。
//
// 设计要点：
//   - 旁表（side table）：(novel_id, entry_table, entry_id) 唯一，与 ledger_change_history
//     同款「表名:id」泛化定位，不给四维台账条目表加列（快照 schema 不动、AutoMigrate 只加表）；
//   - provenance 是增量标注：无行=未标注（存量条目缺省），不造「空 provenance」噪音；
//   - 钉住护栏（guardPinnedAuto）挂在 ledger_temporal 写路径单点：已钉住条目的语义
//     修改/删除仅放行 author（作者手动）与显式 retcon 标注（已过审批的回改），
//     auto（AI/流水线自动写）一律拒收——「自动修改需走 retcon 级审批」的强制面。
//
// 消费侧：双向对账巡检（structure.CheckLedgerReconcile，来源支撑核验）+
// statesync/作者标注（mcp_tools upsert_ledger_provenance）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 提取置信封闭枚举（Confidence 取值；空串=未标注）。
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// LedgerProvenance 是一条台账条目的来源/置信/确认标注。
// (novel_id, entry_table, entry_id) 唯一：同条目重复 upsert 覆盖。
type LedgerProvenance struct {
	ID      int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID int64 `gorm:"column:novel_id;not null;uniqueIndex:uk_ledger_prov,priority:1;index" json:"novel_id"`
	// EntryTable 台账表名（characters/time_entries/…），与 EntryID 联合定位被标注条目。
	EntryTable string `gorm:"column:entry_table;not null;uniqueIndex:uk_ledger_prov,priority:2" json:"entry_table"`
	EntryID    int64  `gorm:"column:entry_id;not null;uniqueIndex:uk_ledger_prov,priority:3" json:"entry_id"`
	// SourceChapterID 来源章节（chapters.id）：该事实从哪章正文提取/确立；nil=未标注。
	SourceChapterID *int64 `gorm:"column:source_chapter_id;index" json:"source_chapter_id,omitempty"`
	// Confidence 提取置信封闭枚举（Confidence* 常量）；空=未标注。
	Confidence string `gorm:"column:confidence" json:"confidence"`
	// Confirmed 人工确认标记：true=作者/人工已核对该条目为真。
	Confirmed bool `gorm:"column:confirmed;not null;default:false" json:"confirmed"`
	// Pinned 钉住（核心事实）：true 时任何自动语义修改需走 retcon 级审批
	// （guardPinnedAuto 拒收 auto 写；author/显式 retcon 标注放行）。
	Pinned    bool      `gorm:"column:pinned;not null;default:false" json:"pinned"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定 GORM 表名。
func (LedgerProvenance) TableName() string { return "ledger_provenance" }

// Validate 校验 provenance 契约：条目定位非空/正数、置信取值在封闭枚举内（宁缺勿错）。
func (p *LedgerProvenance) Validate() error {
	p.EntryTable = strings.TrimSpace(p.EntryTable)
	if p.EntryTable == "" {
		return fmt.Errorf("entry_table 不能为空")
	}
	if p.EntryID <= 0 {
		return fmt.Errorf("entry_id 非法：%d（须 > 0）", p.EntryID)
	}
	switch p.Confidence {
	case "", ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		return fmt.Errorf("confidence 非法：%q（只支持 %s|%s|%s 或留空）",
			p.Confidence, ConfidenceHigh, ConfidenceMedium, ConfidenceLow)
	}
	return nil
}

// ProvenanceStore 是 ledger_provenance 的读写口。
type ProvenanceStore struct {
	DB *gorm.DB
}

// NewProvenanceStore 构造 provenance 读写口。
func NewProvenanceStore(db *gorm.DB) *ProvenanceStore { return &ProvenanceStore{DB: db} }

// Upsert 立/覆盖一条 provenance（(novel_id, entry_table, entry_id) 唯一，整条覆盖含
// 确认/钉住显式落值——OnConflict AssignmentColumns 无 GORM 零值跳写）。
// 先过 Validate：定位缺失/置信非法直接拒收，不落库。
func (s *ProvenanceStore) Upsert(ctx context.Context, p *LedgerProvenance) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "novel_id"}, {Name: "entry_table"}, {Name: "entry_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_chapter_id", "confidence", "confirmed", "pinned", "updated_at",
		}),
	}).Create(p).Error
}

// Get 取单条 provenance；未标注（无行）返回 (nil, nil)。
func (s *ProvenanceStore) Get(ctx context.Context, novelID int64, entryTable string, entryID int64) (*LedgerProvenance, error) {
	var row LedgerProvenance
	err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND entry_table = ? AND entry_id = ?", novelID, entryTable, entryID).
		First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get ledger provenance: %w", err)
	}
	return &row, nil
}

// ListByNovel 列出某书全部 provenance 标注（id ASC，确定性顺序）。
func (s *ProvenanceStore) ListByNovel(ctx context.Context, novelID int64) ([]LedgerProvenance, error) {
	var rows []LedgerProvenance
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("storage: list ledger provenance: %w", err)
	}
	return rows, nil
}

// ── 钉住护栏（guardPinnedAuto）──────────────────────────────────────

// guardPinnedAuto 是钉住护栏（R2b-7 §8.3「核心事实可钉住，自动修改需走 retcon 级审批」）：
// 已钉住（pinned=true）条目的语义修改/删除，仅放行 author（作者手动编辑）与显式
// retcon 标注（已过审批的回改通道）；auto（AI/流水线自动写）一律返回错误拒收。
//
// 降级纪律：provenance 未标注/表缺失/查询失败 → 放行（缺省=现行行为逐字一致，
// 不给存量数据造写入断裂）；本护栏只在「确知钉住」时拦截（宁缺勿错的强制面）。
func guardPinnedAuto(db *gorm.DB, table string, entryID int64, ctx context.Context) error {
	if ledgerSourceFromContext(ctx) != HistorySourceAuto {
		return nil
	}
	var pinned bool
	row := db.Session(&gorm.Session{NewDB: true, SkipHooks: true}).
		Raw("SELECT pinned FROM ledger_provenance WHERE entry_table = ? AND entry_id = ?", table, entryID).Row()
	if row == nil {
		return nil
	}
	if err := row.Scan(&pinned); err != nil {
		return nil // 无行（未标注）/表缺失/查询失败：缺省放行
	}
	if !pinned {
		return nil
	}
	return fmt.Errorf("ledger pinned: %s:%d 已钉住核心事实，自动修改需走人工审批（retcon 级）；"+
		"请作者手动修改，或经审批流程后先取消钉住再改", table, entryID)
}
