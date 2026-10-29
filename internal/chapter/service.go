package chapter

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/deletion"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/volume"
)

// ChapterChunkCleaner 清理某章节派生的向量数据。
type ChapterChunkCleaner interface {
	DeleteChapterChunks(ctx context.Context, novelID, chapterID int64) error
}

// ChapterCacheInvalidator 清理某章节的搜索缓存。
type ChapterCacheInvalidator interface {
	DeleteCachedChapter(novelID, chapterID int64)
}

// DeleteResult 描述章节删除的结果。References 非空时 Deleted 为 false。
type DeleteResult struct {
	Deleted    bool               `json:"deleted"`
	References []deletion.Blocker `json:"references"`
}

// Service 编排章节用例涉及的数据库、Git 文件与派生数据生命周期。
type Service struct {
	store         *Store
	volumes       *volume.Store
	deletionGuard *deletion.Guard
	logger        *slog.Logger

	chunkCleaner     func() ChapterChunkCleaner
	cacheInvalidator func() ChapterCacheInvalidator
}

// NewService 创建章节领域服务。provider 允许可选基础设施在运行时延迟初始化。
func NewService(store *Store, volumes *volume.Store, deletionGuard *deletion.Guard, logger *slog.Logger,
	chunkCleaner func() ChapterChunkCleaner, cacheInvalidator func() ChapterCacheInvalidator) *Service {
	return &Service{
		store:            store,
		volumes:          volumes,
		deletionGuard:    deletionGuard,
		logger:           logger,
		chunkCleaner:     chunkCleaner,
		cacheInvalidator: cacheInvalidator,
	}
}

// CreateDefault 按既有规则在最后一卷末尾创建章节；没有卷时创建到未分卷组。
func (s *Service) CreateDefault(ctx context.Context, novelID int64, title string) (*Chapter, error) {
	lastVolume, err := s.volumes.LastByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, fmt.Errorf("get last volume: %w", err)
	}
	var volumeID *int64
	if lastVolume != nil {
		volumeID = &lastVolume.ID
	}
	return s.Place(ctx, PlaceInput{NovelID: novelID, Title: &title, TargetVolumeID: volumeID})
}

// Place 创建或移动章节，并将其放入目标章节组的指定位置。
func (s *Service) Place(ctx context.Context, input PlaceInput) (*Chapter, error) {
	if input.SourceChapterID == nil && input.Title == nil {
		return nil, fmt.Errorf("创建章节时必须提供标题")
	}
	if input.SourceChapterID != nil && input.Title != nil {
		return nil, fmt.Errorf("移动章节时不能提供标题")
	}

	var placed *Chapter
	if input.SourceChapterID == nil {
		err := s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			placed, err = s.store.Place(ctx, tx, input)
			if err != nil {
				return err
			}
			placed.FilePath = git.ChapterPath(placed.ID)
			placed.OutlineFilePath = git.OutlinePath(placed.ID)
			if err := git.WriteFile(input.NovelID, placed.FilePath, ""); err != nil {
				return fmt.Errorf("create chapter file: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		placed, err = s.store.Place(ctx, nil, input)
		if err != nil {
			return nil, err
		}
		placed.FilePath = git.ChapterPath(placed.ID)
		placed.OutlineFilePath = git.OutlinePath(placed.ID)
	}
	readingNumber, err := s.store.GetReadingNumberByID(ctx, nil, input.NovelID, placed.ID)
	if err != nil {
		return nil, fmt.Errorf("get chapter reading number: %w", err)
	}
	placed.ReadingNumber = readingNumber
	return placed, nil
}

// Delete 删除没有交叉引用的章节及其正文、大纲与派生索引。
func (s *Service) Delete(ctx context.Context, novelID, chapterID int64) (*DeleteResult, error) {
	references, err := s.deletionGuard.Blockers(ctx, deletion.Target{NovelID: novelID, Kind: deletion.EntityChapter, ID: chapterID})
	if err != nil {
		return nil, err
	}
	if len(references) > 0 {
		return &DeleteResult{References: references}, nil
	}
	// 1. 事务内只删 DB 记录，绝不碰文件系统——保证 DB 是真相源，避免「文件已删但
	//    事务回滚导致记录还在」或「事务提交前崩溃导致文件没了记录还在」的不一致。
	err = s.store.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.store.Delete(ctx, tx, novelID, chapterID)
	})
	if err != nil {
		return nil, err
	}
	// 2. DB 提交成功后才删文件，带有限重试。即使进程崩溃未删完，最坏情况是残留
	//    孤立文件，由启动期 ReconcileOrphanFiles 回收；不会与 DB 记录产生矛盾。
	for _, path := range []string{git.ChapterPath(chapterID), git.OutlinePath(chapterID)} {
		if err := s.removeFileWithRetry(ctx, novelID, path); err != nil {
			s.logger.Error("删除章节文件失败（已重试）", "novel_id", novelID, "chapter_id", chapterID, "path", path, "err", err)
		}
	}
	// 3. 派生索引清理（best-effort，可重放：按 chapterID 删除，重复执行无副作用）。
	if s.chunkCleaner != nil {
		if cleaner := s.chunkCleaner(); cleaner != nil {
			if err := cleaner.DeleteChapterChunks(ctx, novelID, chapterID); err != nil {
				s.logger.Warn("删除章节向量失败", "novel_id", novelID, "chapter_id", chapterID, "err", err)
			}
		}
	}
	if s.cacheInvalidator != nil {
		if invalidator := s.cacheInvalidator(); invalidator != nil {
			invalidator.DeleteCachedChapter(novelID, chapterID)
		}
	}
	return &DeleteResult{Deleted: true}, nil
}

// removeFileWithRetry 以有限重试删除单文件，规避瞬时 IO 失败；文件不存在视为成功。
func (s *Service) removeFileWithRetry(_ context.Context, novelID int64, path string) error {
	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := git.RemoveFile(novelID, path); err != nil {
			lastErr = err
			s.logger.Warn("删除章节文件重试", "attempt", attempt, "path", path, "err", err)
			continue
		}
		return nil
	}
	return lastErr
}

// ReconcileOrphanFiles 回收「DB 已删但磁盘文件未删」的孤儿章节文件，作为 Delete
// 因崩溃未能完成文件清理时的启动期兜底。DB 存活章节列表为真相源，不在其中的
// id_<N>.md 一律视为孤儿删除。返回实际删除的文件数。
func (s *Service) ReconcileOrphanFiles(ctx context.Context, novelID int64) (int, error) {
	live, err := s.store.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		return 0, fmt.Errorf("list chapters: %w", err)
	}
	liveIDs := make(map[int64]struct{}, len(live))
	for _, c := range live {
		liveIDs[c.ID] = struct{}{}
	}
	files, err := git.ListChapterFiles(novelID)
	if err != nil {
		return 0, fmt.Errorf("list chapter files: %w", err)
	}
	removed := 0
	for _, rel := range files {
		id, ok := git.ParseChapterFileID(rel)
		if !ok {
			continue
		}
		if _, live := liveIDs[id]; live {
			continue
		}
		if err := git.RemoveFile(novelID, rel); err != nil {
			s.logger.Warn("回收孤儿章节文件失败", "novel_id", novelID, "path", rel, "err", err)
			continue
		}
		removed++
		s.logger.Info("回收孤儿章节文件", "novel_id", novelID, "path", rel, "chapter_id", id)
	}
	return removed, nil
}
