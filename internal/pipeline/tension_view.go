package pipeline

// tension_view.go —— 单章张力曲线消费视图（P0-2 情绪账前端可视化）。
//
// 只读视图：生效曲线（EffectiveTensionCurve 合并，同 seg L1 标注优先于 L0 基线）
// + 图表标记位（quality.MarkTensionCurve，与曲线三查判定同源不漂移）。
// 无曲线行（未检测/章节不存在/qualityStore 未注入）返回 Exists=false 空视图，不报错。

import (
	"context"

	"github.com/novelgo/novelgo/internal/quality"
)

// ChapterTensionView 是单章张力曲线的前端消费视图。
type ChapterTensionView struct {
	ChapterID int64 `json:"chapter_id"`
	// ReadingNumber 是全局阅读序号（前端跳章定位用）。
	ReadingNumber int `json:"reading_number"`
	// Points 是生效曲线段（seg 升序，L1 优先合并后；TensionPoint 的
	// StartRune/EndRune 供「标记→正文段落」定位）。
	Points []quality.TensionPoint `json:"points"`
	// Markers 是图表标记位（拖沓段区间/情绪峰 segs/开篇窗口段）。
	Markers quality.TensionMarkers `json:"markers"`
	Exists  bool                   `json:"exists"`
}

// GetChapterTensionCurve 返回指定章节的张力曲线视图（生效曲线 + 标记位）。
// 无曲线返回 Exists=false 空视图，不报错（与 GetChapterQualityArchive 口径一致）。
func (s *Service) GetChapterTensionCurve(ctx context.Context, novelID, chapterID int64) (*ChapterTensionView, error) {
	out := &ChapterTensionView{ChapterID: chapterID, Exists: false}
	if s.qualityStore == nil {
		return out, nil
	}
	rows, err := s.qualityStore.ListTensionCurve(ctx, novelID, chapterID)
	if err != nil || len(rows) == 0 {
		return out, nil
	}
	effective := quality.EffectiveTensionCurve(rows)
	if len(effective) == 0 {
		return out, nil
	}
	reading := 0
	for _, r := range effective {
		if r.ReadingNumber > reading {
			reading = r.ReadingNumber
		}
	}
	pts := quality.TensionPointsFromRows(effective)
	out = &ChapterTensionView{
		ChapterID:     chapterID,
		ReadingNumber: reading,
		Points:        pts,
		Markers:       quality.MarkTensionCurve(pts),
		Exists:        true,
	}
	return out, nil
}
