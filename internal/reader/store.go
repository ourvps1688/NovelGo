package reader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/deletion"
	"github.com/novelgo/novelgo/internal/storage"
)

// Store 管理 ReaderPerspective 持久化。DB 导出供调用方做简单 CRUD。
type Store struct {
	DB     *gorm.DB
	logger *slog.Logger
}

// NewStore 创建 reader 存储。
func NewStore(db *gorm.DB, logger *slog.Logger) *Store {
	return &Store{DB: db, logger: logger}
}

// GetByTypeContentHash 按 (novel_id, type, content_hash) 精确查找读者认知条目。未找到返回 (nil, nil)。
// content_hash 由 reader.ContentHash(content) 计算得到；创建入口用它做 find-or-create 幂等。
func (s *Store) GetByTypeContentHash(ctx context.Context, novelID int64, typ, contentHash string) (*ReaderPerspective, error) {
	var rp ReaderPerspective
	err := s.DB.WithContext(ctx).Where("novel_id = ? AND type = ? AND content_hash = ?", novelID, typ, contentHash).First(&rp).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reader store: get by type+hash: %w", err)
	}
	return &rp, nil
}

// ListByNovelOptions 是 ListByNovel 的可选参数。
type ListByNovelOptions struct {
	PageParams storage.PageParams
	Search     string // 空字符串=不过滤，按 content LIKE OR related_truth LIKE 模糊匹配
	Type       string // 空字符串=不过滤，"known"/"suspense"/"misconception"
	Order      string // 空字符串=默认 type, planted_chapter_id ASC
}

// ListByNovel 分页列出某小说的读者认知条目，支持搜索和按类型过滤。
// 前端全量拉取（Size=-1）和全局搜索复用。
func (s *Store) ListByNovel(ctx context.Context, novelID int64, opts ListByNovelOptions) (*storage.PageResult[ReaderPerspective], error) {
	pp := opts.PageParams
	pp.Normalize()

	q := s.DB.WithContext(ctx).Model(&ReaderPerspective{}).Where("novel_id = ?", novelID)

	if opts.Search != "" {
		q = q.Where("content LIKE ? OR related_truth LIKE ?", "%"+opts.Search+"%", "%"+opts.Search+"%")
	}
	if opts.Type != "" {
		q = q.Where("type = ?", opts.Type)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("reader store: count: %w", err)
	}

	order := opts.Order
	if order == "" {
		order = "type, planted_chapter_id ASC"
	}
	var items []ReaderPerspective
	if err := q.Order(order).Offset(pp.Offset()).Limit(pp.Size).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("reader store: list: %w", err)
	}

	s.logger.Debug("reader store: listed", "novel_id", novelID, "total", total, "page", pp.Page)
	return storage.NewPageResult(items, total, pp.Page, pp.Size), nil
}

// ListActive 返回全部未回收（revealed_chapter=0）的读者认知条目。
// 全量返回——活跃集合天然小，context builder 和 MCP 工具需要完整快照。
func (s *Store) ListActive(ctx context.Context, novelID int64) ([]ReaderPerspective, error) {
	var items []ReaderPerspective
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND revealed_chapter_id IS NULL", novelID).
		Order("type, planted_chapter_id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("reader store: list active: %w", err)
	}
	return items, nil
}

// ChapterDeletionBlockers 返回引用指定章节的读者认知条目。
func (s *Store) ChapterDeletionBlockers(ctx context.Context, novelID, chapterID int64) ([]deletion.Blocker, error) {
	var items []ReaderPerspective
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND (planted_chapter_id = ? OR revealed_chapter_id = ?)", novelID, chapterID, chapterID).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("reader store: query chapter deletion blockers: %w", err)
	}

	var blockers []deletion.Blocker
	for _, item := range items {
		if item.PlantedChapterID != nil && *item.PlantedChapterID == chapterID {
			blockers = append(blockers, deletion.Blocker{Kind: "reader_planted", ID: item.ID, Label: item.Content})
		}
		if item.RevealedChapterID != nil && *item.RevealedChapterID == chapterID {
			blockers = append(blockers, deletion.Blocker{Kind: "reader_revealed", ID: item.ID, Label: item.Content})
		}
	}
	return blockers, nil
}
