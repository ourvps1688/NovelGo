package pipeline

// R2b-1/R2b-2 长程结构索引层（提案 §8.2 组件 A）接线：把确定性检查告警渲染为
// 「结构告警」块，组装进 L2 体检输入与档案汇总快审输入（§8.5 结构索引全量；
// 与 R2-4 档案汇总块同装配通道，空块逐字一致）。
//
// 告警来源（每批独立降级，宁缺勿错）：
//   - 伏笔 lifecycle（R2b-1）：time_entries 伏笔投影 → 超期未收/无收点规划/收未埋；
//   - 书写不变量（R2b-2）：pinned_invariants × characters 台账比对 → 违例告警。
//
// 降级零提示全路径：进度读取失败 → 整块空串；单批取数失败/零告警 → 跳过该批
// （其余批照常）；全部无告警 → 空块（提示词与无该块逐字一致）。只报告不改写
// （不进 issue 清单、不触发修复路由、不翻转裁决）。

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
)

// categoryForeshadowing 是伏笔条目分类（time_entries.category 枚举之一）。
const categoryForeshadowing = "foreshadowing"

// structureBlock 组装本书「结构告警」块（伏笔 lifecycle + 书写不变量确定性检查）。
// 取数失败/零告警 → 空串（降级零提示）。
func (s *Service) structureBlock(ctx context.Context, novelID int64) string {
	if s.timelineStore == nil || s.chapterStore == nil {
		return ""
	}
	now, err := s.chapterStore.CountByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Warn("结构告警块：读取章节进度失败（降级零提示）", "novel_id", novelID, "err", err)
		return ""
	}
	var groups [][]structure.StructFinding
	if page, err := s.timelineStore.ListByNovel(ctx, novelID, timeline.ListByNovelOptions{
		Category:   categoryForeshadowing,
		PageParams: storage.PageParams{Size: -1},
	}); err != nil {
		s.logger.Warn("结构告警块：读取伏笔台账失败（跳过伏笔批）", "novel_id", novelID, "err", err)
	} else {
		groups = append(groups, structure.CheckForeshadowLifecycle(projectForeshadows(page.Items), now))
	}
	if findings, ok := s.pinnedFindings(ctx, novelID, now); ok {
		groups = append(groups, findings)
	}
	return structure.RenderBlock(structure.MergeFindings(groups...))
}

// pinnedFindings 跑书写不变量比对批（pinned_invariants × characters 台账）。
// 任一取数失败 → 跳过该批（ok=false，宁缺勿错）；零违例返回空清单（ok=true）。
func (s *Service) pinnedFindings(ctx context.Context, novelID int64, now int) ([]structure.StructFinding, bool) {
	if s.structStore == nil || s.store == nil || s.store.db == nil {
		return nil, false
	}
	pins, err := s.structStore.ListByNovel(ctx, novelID)
	if err != nil {
		s.logger.Warn("结构告警块：读取书写不变量失败（跳过不变量批）", "novel_id", novelID, "err", err)
		return nil, false
	}
	if len(pins) == 0 {
		return nil, true // 无不变量=零违例（不查角色台账）
	}
	var rows []character.Character
	if err := s.store.db.WithContext(ctx).Where("novel_id = ?", novelID).Find(&rows).Error; err != nil {
		s.logger.Warn("结构告警块：读取角色台账失败（跳过不变量批）", "novel_id", novelID, "err", err)
		return nil, false
	}
	return structure.CheckPinnedInvariants(pins, projectCharacters(rows), now), true
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

// projectCharacters 把角色台账投影为不变量检查输入（character → structure 适配）。
// 别名 JSON 坏数据按无别名（宁缺勿错）。
func projectCharacters(rows []character.Character) []structure.CharacterState {
	out := make([]structure.CharacterState, 0, len(rows))
	for _, r := range rows {
		var aliases []string
		if strings.TrimSpace(r.Aliases) != "" {
			if err := json.Unmarshal([]byte(r.Aliases), &aliases); err != nil {
				aliases = nil
			}
		}
		out = append(out, structure.CharacterState{
			ID:                r.ID,
			Name:              r.Name,
			Aliases:           aliases,
			Alive:             r.Alive,
			CapabilityCeiling: r.CapabilityCeiling,
		})
	}
	return out
}
