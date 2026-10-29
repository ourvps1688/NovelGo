// Package content 编排「保存章节正文」后的跨介质派生更新。
//
// 原本这段多介质编排（RAG 刷新 / 搜索缓存 / 字数统计 / 写作变更记录 / 章节字数落库）
// 散落在 app 绑定层的 SaveContent 中，违反「app 不承载领域编排、跨介质用例应走
// App → internal/<domain>.Service → Store」的分层约定。现下沉到领域层，统一编排顺序
// 与可观测性：各派生步骤相互独立且 best-effort，不回滚已落盘的正文（正文是真相源，
// 派生数据可重算/可重放）。
package content

import (
	"context"
	"log/slog"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/novelgo/novelgo/internal/text"
	"github.com/novelgo/novelgo/internal/writing"
)

// SearchUpdater 更新章节搜索缓存，由 app 注入 search.Service 的懒加载器实现，
// 避免 content 包反向依赖 search 包。
type SearchUpdater interface {
	UpdateCachedChapter(novelID, chapterID int64, newContent string)
}

// Service 编排保存章节正文后的跨介质派生更新。
type Service struct {
	logger       *slog.Logger
	chapterStore *chapter.Store
	writingStore *writing.Store
	searchLoader func() SearchUpdater
}

// NewService 创建内容派生编排服务。searchLoader 允许搜索服务在运行时懒加载
// （app 持有 atomic.Pointer[search.Service]，就绪前可为 nil）。
func NewService(logger *slog.Logger, chapterStore *chapter.Store, writingStore *writing.Store, searchLoader func() SearchUpdater) *Service {
	return &Service{
		logger:       logger,
		chapterStore: chapterStore,
		writingStore: writingStore,
		searchLoader: searchLoader,
	}
}

// DeriveAfterSave 在章节正文文件已成功落盘后调用，刷新全部派生介质。
// expected 非 nil 时额外记录写作字数增减变化（用于写作统计）。
func (s *Service) DeriveAfterSave(ctx context.Context, novelID, chapterID int64, content string, expected *string) {
	// 1. RAG 向量刷新（异步入队，不阻塞保存主流程）。
	rag.SubmitRefresh(novelID, chapterID, content)

	// 2. 搜索缓存更新（搜索服务未就绪时跳过）。
	if s.searchLoader != nil {
		if svc := s.searchLoader(); svc != nil {
			svc.UpdateCachedChapter(novelID, chapterID, content)
		}
	}

	// 3. 字数统计 + 写作变更记录 + 章节字数落库。
	stats := text.ComputeStats(content)
	if s.writingStore != nil && expected != nil {
		changes := text.ComputeWordChanges(*expected, content)
		s.writingStore.LogChanges(ctx, novelID, chapterID, changes.Added, changes.Deleted)
	}
	if err := s.chapterStore.UpdateWordCount(ctx, novelID, chapterID, stats.WordCount); err != nil {
		s.logger.Warn("更新字数失败", "novel_id", novelID, "chapter_id", chapterID, "err", err)
	}
}
