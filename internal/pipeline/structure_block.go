package pipeline

// R2b-1 长程结构索引层（提案 §8.2 组件 A）接线：把伏笔 lifecycle 确定性检查的告警
// 渲染为「结构告警」块，组装进 L2 体检输入与档案汇总快审输入（§8.5 结构索引全量；
// 与 R2-4 档案汇总块同装配通道，空块逐字一致）。
//
// 降级零提示全路径：台账读取失败 / 进度读取失败 / 零告警 → 空块（提示词与无该块
// 逐字一致），只留 Warn 日志；只报告不改写（结构告警供 L2 汇总裁决参照，不进 issue
// 清单、不触发修复路由、不翻转裁决）。

import (
	"context"

	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
)

// categoryForeshadowing 是伏笔条目分类（time_entries.category 枚举之一）。
const categoryForeshadowing = "foreshadowing"

// structureBlock 组装本书「结构告警」块（伏笔 lifecycle 确定性检查）。
// 取数失败/零告警 → 空串（降级零提示）。
func (s *Service) structureBlock(ctx context.Context, novelID int64) string {
	if s.timelineStore == nil || s.chapterStore == nil {
		return ""
	}
	page, err := s.timelineStore.ListByNovel(ctx, novelID, timeline.ListByNovelOptions{
		Category:   categoryForeshadowing,
		PageParams: storage.PageParams{Size: -1},
	})
	if err != nil {
		s.logger.Warn("结构告警块：读取伏笔台账失败（降级零提示）", "novel_id", novelID, "err", err)
		return ""
	}
	now, err := s.chapterStore.CountByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Warn("结构告警块：读取章节进度失败（降级零提示）", "novel_id", novelID, "err", err)
		return ""
	}
	return structure.RenderBlock(structure.CheckForeshadowLifecycle(projectForeshadows(page.Items), now))
}

// projectForeshadows 把台账伏笔条目投影为检查器输入（timeline → structure 适配，
// 保持 structure 纯函数零依赖）。
func projectForeshadows(entries []timeline.TimelineEntry) []structure.ForeshadowEntry {
	out := make([]structure.ForeshadowEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, structure.ForeshadowEntry{
			ID:                  e.ID,
			Title:               e.Title,
			Status:              e.Status,
			TargetReadingNumber: e.TargetReadingNumber,
			SourceChapterID:     e.SourceChapterID,
			ResolvedChapterID:   e.ResolvedChapterID,
		})
	}
	return out
}
