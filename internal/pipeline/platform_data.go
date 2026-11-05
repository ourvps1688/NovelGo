package pipeline

// P1-2 真实数据对账（服务层取数/落库编排）：粘贴文本 → 解析（quality.ParsePlatformMetrics
// 单源）→ 章号（全局阅读序号）解析为 chapter_id → quality.Store 幂等 upsert。
// 预览与落库同源解析，预览可先看「哪行能进、哪行跳过、哪个章号越界」。

import (
	"context"
	"errors"
	"fmt"

	"github.com/novelgo/novelgo/internal/quality"
)

// PlatformMetricView 一条平台数据展示行（含章节归属与标题）。
type PlatformMetricView struct {
	ChapterID      int64    `json:"chapter_id"`
	ReadingNumber  int      `json:"reading_number"`
	Title          string   `json:"title"`
	CompletionRate *float64 `json:"completion_rate"`
	RetentionRate  *float64 `json:"retention_rate"`
	Batch          string   `json:"batch"`
	UpdatedAt      string   `json:"updated_at"`
}

// PlatformMetricRowView 粘贴解析预览行（未落库）。
type PlatformMetricRowView struct {
	ReadingNumber  int      `json:"reading_number"`
	Title          string   `json:"title"`
	Matched        bool     `json:"matched"`
	CompletionRate *float64 `json:"completion_rate"`
	RetentionRate  *float64 `json:"retention_rate"`
}

// PlatformMetricsPreview 是粘贴解析预览结果：可入库行 + 跳过行 + 越界章号。
type PlatformMetricsPreview struct {
	Rows             []PlatformMetricRowView `json:"rows"`
	SkippedLines     []string                `json:"skipped_lines"`
	UnmatchedNumbers []int                   `json:"unmatched_numbers"`
}

// PlatformMetricsSaveResult 是落库结果（saved=全书当前全部平台数据行）。
type PlatformMetricsSaveResult struct {
	Saved            []PlatformMetricView `json:"saved"`
	SkippedLines     []string             `json:"skipped_lines"`
	UnmatchedNumbers []int                `json:"unmatched_numbers"`
}

// chapterBrief 是平台数据章号解析的最小章节视图（id+标题）。
type chapterBrief struct {
	ID    int64
	Title string
}

// platformDataChapterIndex 返回 阅读序号→章节(含标题) 索引（单次列章构建，避免逐行反查）。
func (s *Service) platformDataChapterIndex(ctx context.Context, novelID int64) (map[int]chapterBrief, error) {
	_, cs := s.scoreTrendStores()
	chapters, err := cs.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, err
	}
	rnMap, err := cs.GetReadingNumbersByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, err
	}
	idx := make(map[int]chapterBrief, len(chapters))
	for _, ch := range chapters {
		if rn := rnMap[ch.ID]; rn > 0 {
			idx[rn] = chapterBrief{ID: ch.ID, Title: ch.Title}
		}
	}
	return idx, nil
}

// PreviewPlatformMetrics 解析粘贴文本并解析章号归属（不落库）。
func (s *Service) PreviewPlatformMetrics(ctx context.Context, novelID int64, text string) (*PlatformMetricsPreview, error) {
	if novelID <= 0 {
		return nil, errors.New("平台数据预览: novel_id 缺失")
	}
	rows, skipped := quality.ParsePlatformMetrics(text)
	idx, err := s.platformDataChapterIndex(ctx, novelID)
	if err != nil {
		return nil, fmt.Errorf("平台数据预览: %w", err)
	}
	out := &PlatformMetricsPreview{
		Rows:             make([]PlatformMetricRowView, 0, len(rows)),
		SkippedLines:     skipped,
		UnmatchedNumbers: []int{},
	}
	for _, r := range rows {
		v := PlatformMetricRowView{
			ReadingNumber:  r.ReadingNumber,
			CompletionRate: r.CompletionRate,
			RetentionRate:  r.RetentionRate,
		}
		if ch, ok := idx[r.ReadingNumber]; ok {
			v.Matched = true
			v.Title = ch.Title
		} else {
			out.UnmatchedNumbers = append(out.UnmatchedNumbers, r.ReadingNumber)
		}
		out.Rows = append(out.Rows, v)
	}
	return out, nil
}

// SavePlatformMetricsText 解析粘贴文本并落库（同章 upsert，最新粘贴胜）。
// 跳过行与越界章号如实回报；无可入库行时拒绝写入（宁缺勿错不空跑）。
func (s *Service) SavePlatformMetricsText(ctx context.Context, novelID int64, text, batch string) (*PlatformMetricsSaveResult, error) {
	if novelID <= 0 {
		return nil, errors.New("平台数据落库: novel_id 缺失")
	}
	rows, skipped := quality.ParsePlatformMetrics(text)
	idx, err := s.platformDataChapterIndex(ctx, novelID)
	if err != nil {
		return nil, fmt.Errorf("平台数据落库: %w", err)
	}
	out := &PlatformMetricsSaveResult{
		Saved:            []PlatformMetricView{},
		SkippedLines:     skipped,
		UnmatchedNumbers: []int{},
	}
	batchRows := make([]quality.PlatformMetric, 0, len(rows))
	for _, r := range rows {
		ch, ok := idx[r.ReadingNumber]
		if !ok {
			out.UnmatchedNumbers = append(out.UnmatchedNumbers, r.ReadingNumber)
			continue
		}
		batchRows = append(batchRows, quality.PlatformMetric{
			NovelID:        novelID,
			ChapterID:      ch.ID,
			ReadingNumber:  r.ReadingNumber,
			CompletionRate: r.CompletionRate,
			RetentionRate:  r.RetentionRate,
		})
	}
	if len(batchRows) == 0 {
		if len(out.UnmatchedNumbers) > 0 {
			return out, fmt.Errorf("平台数据落库: %d 行章号均超出当前章数范围", len(out.UnmatchedNumbers))
		}
		return out, errors.New("平台数据落库: 无可入库行")
	}
	qs, _ := s.scoreTrendStores()
	if err := qs.ReplacePlatformMetrics(ctx, novelID, batch, batchRows); err != nil {
		return nil, err
	}
	saved, err := qs.ListPlatformMetrics(ctx, novelID)
	if err != nil {
		return nil, err
	}
	out.Saved = s.platformMetricViews(ctx, novelID, saved)
	return out, nil
}

// ListPlatformMetricsView 列出某书全部平台数据（含章节标题），按全局阅读序号升序。
func (s *Service) ListPlatformMetricsView(ctx context.Context, novelID int64) ([]PlatformMetricView, error) {
	qs, _ := s.scoreTrendStores()
	rows, err := qs.ListPlatformMetrics(ctx, novelID)
	if err != nil {
		return nil, err
	}
	return s.platformMetricViews(ctx, novelID, rows), nil
}

// DeletePlatformMetrics 清空某书全部平台数据（面板「清空重录」）。
func (s *Service) DeletePlatformMetrics(ctx context.Context, novelID int64) error {
	qs, _ := s.scoreTrendStores()
	return qs.DeletePlatformMetrics(ctx, novelID)
}

// platformMetricViews 把存储行装配为展示行（标题从列章索引取，缺章节随行留空）。
func (s *Service) platformMetricViews(ctx context.Context, novelID int64, rows []quality.PlatformMetric) []PlatformMetricView {
	idx, err := s.platformDataChapterIndex(ctx, novelID)
	out := make([]PlatformMetricView, 0, len(rows))
	for _, r := range rows {
		v := PlatformMetricView{
			ChapterID:      r.ChapterID,
			ReadingNumber:  r.ReadingNumber,
			CompletionRate: r.CompletionRate,
			RetentionRate:  r.RetentionRate,
			Batch:          r.Batch,
			UpdatedAt:      r.UpdatedAt.Format("2006-01-02 15:04:05"),
		}
		if err == nil {
			if ch, ok := idx[r.ReadingNumber]; ok && ch.ID == r.ChapterID {
				v.Title = ch.Title
			}
		}
		out = append(out, v)
	}
	return out
}
