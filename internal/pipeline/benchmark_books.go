package pipeline

// P2-3 对标书库（服务层录入/查询编排）：粘贴文本 → 解析（quality.ParseBenchmarkBooks
// 单源）→ quality.Store 幂等 upsert（同 (novel_id, title) 最新粘贴胜）。
// 预览与落库同源解析；「对标书库摘要」块（benchmarkBooksBlock）进签约体检提示词，
// dim2（题材契合 fit）评分证据从纯模型记忆变为样本库结构特征举证。

import (
	"context"
	"errors"

	"github.com/novelgo/novelgo/internal/quality"
)

// BenchmarkBookView 一条对标书样本展示行。
type BenchmarkBookView struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	HookType     string `json:"hook_type"`
	PleasureType string `json:"pleasure_type"`
	RhythmShape  string `json:"rhythm_shape"`
	Track        string `json:"track"`
	Notes        string `json:"notes"`
	UpdatedAt    string `json:"updated_at"`
}

// BenchmarkBooksPreview 是粘贴解析预览结果（未落库）：可入库行 + 跳过行。
type BenchmarkBooksPreview struct {
	Rows         []BenchmarkBookView `json:"rows"`
	SkippedLines []string            `json:"skipped_lines"`
}

// BenchmarkBooksSaveResult 是落库结果（saved=全书当前全部样本行）。
type BenchmarkBooksSaveResult struct {
	Saved        []BenchmarkBookView `json:"saved"`
	SkippedLines []string            `json:"skipped_lines"`
}

// benchmarkBookView 把存储行装配为展示行。
func benchmarkBookView(r quality.BenchmarkBook) BenchmarkBookView {
	return BenchmarkBookView{
		ID:           r.ID,
		Title:        r.Title,
		HookType:     r.HookType,
		PleasureType: r.PleasureType,
		RhythmShape:  r.RhythmShape,
		Track:        r.Track,
		Notes:        r.Notes,
		UpdatedAt:    r.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// PreviewBenchmarkBooks 解析粘贴文本为预览行（不落库）。
func (s *Service) PreviewBenchmarkBooks(ctx context.Context, novelID int64, text string) (*BenchmarkBooksPreview, error) {
	if novelID <= 0 {
		return nil, errors.New("对标书库预览: novel_id 缺失")
	}
	if text == "" {
		return nil, errors.New("对标书库预览: 文本为空")
	}
	rows, skipped := quality.ParseBenchmarkBooks(text)
	out := &BenchmarkBooksPreview{
		Rows:         make([]BenchmarkBookView, 0, len(rows)),
		SkippedLines: skipped,
	}
	for _, r := range rows {
		out.Rows = append(out.Rows, BenchmarkBookView{
			Title:        r.Title,
			HookType:     r.HookType,
			PleasureType: r.PleasureType,
			RhythmShape:  r.RhythmShape,
			Track:        r.Track,
			Notes:        r.Notes,
		})
	}
	return out, nil
}

// SaveBenchmarkBooksText 解析粘贴文本并落库（同书名 upsert，最新粘贴胜）。
// 跳过行如实回报；无可入库行时拒绝写入（宁缺勿错不空跑）。
func (s *Service) SaveBenchmarkBooksText(ctx context.Context, novelID int64, text string) (*BenchmarkBooksSaveResult, error) {
	if novelID <= 0 {
		return nil, errors.New("对标书库落库: novel_id 缺失")
	}
	rows, skipped := quality.ParseBenchmarkBooks(text)
	out := &BenchmarkBooksSaveResult{SkippedLines: skipped}
	if len(rows) == 0 {
		return out, errors.New("对标书库落库: 无可入库行")
	}
	batchRows := make([]quality.BenchmarkBook, 0, len(rows))
	for _, r := range rows {
		batchRows = append(batchRows, quality.BenchmarkBook{
			NovelID:      novelID,
			Title:        r.Title,
			HookType:     r.HookType,
			PleasureType: r.PleasureType,
			RhythmShape:  r.RhythmShape,
			Track:        r.Track,
			Notes:        r.Notes,
		})
	}
	qs, _ := s.scoreTrendStores()
	if err := qs.ReplaceBenchmarkBooks(ctx, novelID, batchRows); err != nil {
		return nil, err
	}
	saved, err := qs.ListBenchmarkBooks(ctx, novelID)
	if err != nil {
		return nil, err
	}
	out.Saved = make([]BenchmarkBookView, 0, len(saved))
	for _, r := range saved {
		out.Saved = append(out.Saved, benchmarkBookView(r))
	}
	return out, nil
}

// ListBenchmarkBooksView 列出某书全部对标书样本（按录入顺序升序）。
func (s *Service) ListBenchmarkBooksView(ctx context.Context, novelID int64) ([]BenchmarkBookView, error) {
	qs, _ := s.scoreTrendStores()
	rows, err := qs.ListBenchmarkBooks(ctx, novelID)
	if err != nil {
		return nil, err
	}
	out := make([]BenchmarkBookView, 0, len(rows))
	for _, r := range rows {
		out = append(out, benchmarkBookView(r))
	}
	return out, nil
}

// DeleteBenchmarkBook 删除单条对标书样本。
func (s *Service) DeleteBenchmarkBook(ctx context.Context, novelID, id int64) error {
	if novelID <= 0 || id <= 0 {
		return errors.New("对标书库删除: novel_id/id 缺失")
	}
	qs, _ := s.scoreTrendStores()
	return qs.DeleteBenchmarkBook(ctx, novelID, id)
}

// DeleteBenchmarkBooks 清空某书全部对标书样本（面板「清空」）。
func (s *Service) DeleteBenchmarkBooks(ctx context.Context, novelID int64) error {
	if novelID <= 0 {
		return errors.New("对标书库清空: novel_id 缺失")
	}
	qs, _ := s.scoreTrendStores()
	return qs.DeleteBenchmarkBooks(ctx, novelID)
}

// benchmarkBooksBlock 读取本书对标书样本并渲染「对标书库摘要」块（P2-3 dim2 题材
// 契合证据）。无样本 / 读取失败 / 渲染为空 → 空串（零提示，提示词与无该块逐字一致）。
func (s *Service) benchmarkBooksBlock(ctx context.Context, novelID int64) string {
	if s.qualityStore == nil {
		return ""
	}
	rows, err := s.qualityStore.ListBenchmarkBooks(ctx, novelID)
	if err != nil {
		s.logger.Warn("对标书库摘要块：读取样本失败（降级零提示）", "novel_id", novelID, "err", err)
		return ""
	}
	return quality.RenderBenchmarkBlock(rows)
}
