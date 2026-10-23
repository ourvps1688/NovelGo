package voice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store 管理 voice_dimensions 持久化（纯 CRUD，无业务逻辑）。
type Store struct {
	DB     *gorm.DB
	logger *slog.Logger
}

// NewStore 创建 voice 台账存储。
func NewStore(db *gorm.DB, logger *slog.Logger) *Store {
	return &Store{DB: db, logger: logger}
}

// Upsert 按 (novel_id, dimension) 幂等覆盖写入一条维度台账：
// 不存在则创建，已存在则同一行覆盖 content/metrics/evidence/source（只留最新）。
// 约束（宁缺勿错）：维度/来源必须是合法枚举值；无内容不落库。
func (s *Store) Upsert(ctx context.Context, e *Entry) error {
	if e == nil {
		return errors.New("voice store: upsert: entry 为空")
	}
	if e.NovelID <= 0 {
		return errors.New("voice store: upsert: novel_id 缺失")
	}
	if !Dimension(e.Dimension).Valid() {
		return fmt.Errorf("voice store: upsert: 非法维度 %q", e.Dimension)
	}
	if !Source(e.Source).Valid() {
		return fmt.Errorf("voice store: upsert: 非法来源 %q", e.Source)
	}
	if strings.TrimSpace(e.Content) == "" {
		return fmt.Errorf("voice store: upsert: 维度 %q 无内容，不落库", e.Dimension)
	}
	if err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "novel_id"}, {Name: "dimension"}},
			DoUpdates: clause.AssignmentColumns([]string{"content", "metrics", "evidence", "source", "updated_at"}),
		}).
		Create(e).Error; err != nil {
		return fmt.Errorf("voice store: upsert: %w", err)
	}
	return nil
}

// Get 返回某书某维度的一条台账；不存在返回 (nil, nil)。
func (s *Store) Get(ctx context.Context, novelID int64, dim Dimension) (*Entry, error) {
	var e Entry
	err := s.DB.WithContext(ctx).
		First(&e, "novel_id = ? AND dimension = ?", novelID, string(dim)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("voice store: get: %w", err)
	}
	return &e, nil
}

// List 列出某书全部维度台账，按 §0.1 固定维度顺序返回（渲染顺序单源）。
func (s *Store) List(ctx context.Context, novelID int64) ([]Entry, error) {
	var entries []Entry
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order(dimensionOrderExpr()).
		Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("voice store: list: %w", err)
	}
	return entries, nil
}

// DeleteByNovel 删除某书全部维度台账。
func (s *Store) DeleteByNovel(ctx context.Context, novelID int64) error {
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Delete(&Entry{}).Error; err != nil {
		return fmt.Errorf("voice store: delete by novel: %w", err)
	}
	return nil
}

// dimensionOrderExpr 生成按 §0.1 固定维度顺序排序的 SQL 表达式，
// 顺序取自 dimensions 单源（未知维度排最后，不丢行）。
func dimensionOrderExpr() string {
	var b strings.Builder
	b.WriteString("CASE dimension")
	for i, d := range dimensions {
		fmt.Fprintf(&b, " WHEN '%s' THEN %d", d, i)
	}
	b.WriteString(" ELSE 999 END")
	return b.String()
}
