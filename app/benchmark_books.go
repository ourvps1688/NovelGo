package app

// P2-3 对标书库绑定（薄委托）：对标书结构特征样本粘贴预览/落库/列表/单删/清空。
// 领域编排在 internal/pipeline，本层只做 DTO 暴露与空值守卫。

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// PreviewBenchmarkBooks 解析粘贴的对标书文本为预览行（不落库）。
func (a *App) PreviewBenchmarkBooks(novelID int64, text string) (*pipeline.BenchmarkBooksPreview, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.PreviewBenchmarkBooks(a.ctx, novelID, text)
}

// SaveBenchmarkBooksText 解析粘贴文本并落库（同书名最新粘贴覆盖）。
func (a *App) SaveBenchmarkBooksText(novelID int64, text string) (*pipeline.BenchmarkBooksSaveResult, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.SaveBenchmarkBooksText(a.ctx, novelID, text)
}

// ListBenchmarkBooksView 列出某书全部对标书样本。
func (a *App) ListBenchmarkBooksView(novelID int64) ([]pipeline.BenchmarkBookView, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ListBenchmarkBooksView(a.ctx, novelID)
}

// DeleteBenchmarkBook 删除单条对标书样本。
func (a *App) DeleteBenchmarkBook(novelID, id int64) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.DeleteBenchmarkBook(a.ctx, novelID, id)
}

// DeleteBenchmarkBooks 清空某书全部对标书样本。
func (a *App) DeleteBenchmarkBooks(novelID int64) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.DeleteBenchmarkBooks(a.ctx, novelID)
}
