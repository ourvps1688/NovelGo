package structure

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store 管理书写不变量（pinned_invariants）持久化。
type Store struct {
	DB *gorm.DB
}

// NewStore 创建 structure 存储。
func NewStore(db *gorm.DB) *Store {
	return &Store{DB: db}
}

// Upsert 立/覆盖一条书写不变量（(novel_id, kind, statement) 唯一，同声明重复调用覆盖）。
// 先过 Validate：kind 非法/声明为空/规则 kind 参数不齐直接拒收，不落库。
func (s *Store) Upsert(ctx context.Context, p *PinnedInvariant) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "novel_id"}, {Name: "kind"}, {Name: "statement"}},
		DoUpdates: clause.AssignmentColumns([]string{"params", "confirmed", "updated_at"}),
	}).Create(p).Error
}

// ListByNovel 列出某书全部书写不变量（id ASC，确定性顺序）。
func (s *Store) ListByNovel(ctx context.Context, novelID int64) ([]PinnedInvariant, error) {
	var rows []PinnedInvariant
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("structure store: list pinned invariants: %w", err)
	}
	return rows, nil
}
