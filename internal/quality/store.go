package quality

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store 管理 chapter_quality 持久化（纯 CRUD + 汇总，无业务逻辑）。
type Store struct {
	DB     *gorm.DB
	logger *slog.Logger
}

// NewStore 创建章节质量档案存储。
func NewStore(db *gorm.DB, logger *slog.Logger) *Store {
	return &Store{DB: db, logger: logger}
}

// normalize 校验并规范化档案行（宁缺勿错）：
//   - novel_id/chapter_id 缺失拒收；
//   - 空 l1_status 规范化为 pending（档案行存在即待查）；
//   - 非法 l1_status 拒收（枚举值禁造）。
func normalize(e *ChapterQuality) error {
	if e == nil {
		return errors.New("quality store: 档案为空")
	}
	if e.NovelID <= 0 || e.ChapterID <= 0 {
		return errors.New("quality store: novel_id/chapter_id 缺失")
	}
	if e.L1Status == "" {
		e.L1Status = L1StatusPending
	}
	if !validL1Status(e.L1Status) {
		return fmt.Errorf("quality store: 非法 l1_status %q", e.L1Status)
	}
	return nil
}

// Upsert 全量幂等覆盖写入一份章节质量档案：(novel_id, chapter_id) 不存在则创建，
// 已存在则同一行覆盖全部业务列（只留最新）。
func (s *Store) Upsert(ctx context.Context, e *ChapterQuality) error {
	if err := normalize(e); err != nil {
		return fmt.Errorf("quality store: upsert: %w", err)
	}
	if err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "novel_id"}, {Name: "chapter_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"reading_number", "l0_metrics", "l0_findings", "l1_findings",
				"l1_status", "round", "checked_at", "updated_at",
			}),
		}).
		Create(e).Error; err != nil {
		return fmt.Errorf("quality store: upsert: %w", err)
	}
	return nil
}

// UpsertL0 只写 L0 侧字段（reading_number/l0_metrics/l0_findings/round/checked_at），
// 不动 L1 列——L0 同步落档与 L1 异步落档分阶段写入，互不覆盖。
// 行不存在时创建（此时 L1 侧为空清单 + pending）。
func (s *Store) UpsertL0(ctx context.Context, e *ChapterQuality) error {
	if err := normalize(e); err != nil {
		return fmt.Errorf("quality store: upsert L0: %w", err)
	}
	if err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "novel_id"}, {Name: "chapter_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"reading_number", "l0_metrics", "l0_findings", "round", "checked_at", "updated_at",
			}),
		}).
		Create(e).Error; err != nil {
		return fmt.Errorf("quality store: upsert L0: %w", err)
	}
	return nil
}

// UpsertL1 只写 L1 侧字段（l1_findings/l1_status/round/checked_at），
// 不动 L0 列——L0 同步落档与 L1 异步落档分阶段写入，互不覆盖。
// 行不存在时创建（此时 L0 侧为空）。
func (s *Store) UpsertL1(ctx context.Context, e *ChapterQuality) error {
	if err := normalize(e); err != nil {
		return fmt.Errorf("quality store: upsert L1: %w", err)
	}
	if err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "novel_id"}, {Name: "chapter_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"l1_findings", "l1_status", "round", "checked_at", "updated_at",
			}),
		}).
		Create(e).Error; err != nil {
		return fmt.Errorf("quality store: upsert L1: %w", err)
	}
	return nil
}

// GetByChapter 返回某书某章的质量档案；不存在返回 (nil, nil)。
func (s *Store) GetByChapter(ctx context.Context, novelID, chapterID int64) (*ChapterQuality, error) {
	var e ChapterQuality
	err := s.DB.WithContext(ctx).
		First(&e, "novel_id = ? AND chapter_id = ?", novelID, chapterID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("quality store: get: %w", err)
	}
	return &e, nil
}

// ListByNovel 列出某书全部章节质量档案，按阅读序号（reading_number）升序返回。
func (s *Store) ListByNovel(ctx context.Context, novelID int64) ([]ChapterQuality, error) {
	var rows []ChapterQuality
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("reading_number, id").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("quality store: list: %w", err)
	}
	return rows, nil
}

// Summary 汇总某书质量档案（提案 §4.1「档案汇总快审」输入）：
// 覆盖章数、L1 已查章数、发现总数、未修复数与按检查项/状态分布。
func (s *Store) Summary(ctx context.Context, novelID int64) (*Summary, error) {
	rows, err := s.ListByNovel(ctx, novelID)
	if err != nil {
		return nil, err
	}
	out := &Summary{
		ByCheck:  map[string]int{},
		ByStatus: map[string]int{},
	}
	out.Chapters = len(rows)
	for i := range rows {
		r := &rows[i]
		if r.L1Status == L1StatusChecked {
			out.L1Checked++
		}
		for _, f := range ParseFindings(r.L0Findings) {
			out.L0Findings++
			tally(out, f)
		}
		for _, f := range ParseFindings(r.L1Findings) {
			out.L1Findings++
			tally(out, f)
		}
	}
	return out, nil
}

// tally 累加一条发现到汇总（空状态/空检查项归入各自"未标注"口径，不丢计数）。
func tally(out *Summary, f Finding) {
	st := f.Status
	if st == "" {
		st = FindingStatusPending
	}
	out.ByStatus[st]++
	if st == FindingStatusPending {
		out.OpenFindings++
	}
	check := f.Check
	if check == "" {
		check = "unspecified"
	}
	out.ByCheck[check]++
}
