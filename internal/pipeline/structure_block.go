package pipeline

// R2b-1/R2b-2 长程结构索引层（提案 §8.2 组件 A）接线：把确定性检查告警渲染为
// 「结构告警」块，组装进 L2 体检输入与档案汇总快审输入（§8.5 结构索引全量；
// 与 R2-4 档案汇总块同装配通道，空块逐字一致）。
//
// 告警来源（每批独立降级，宁缺勿错）：
//   - 伏笔 lifecycle（R2b-1）：time_entries 伏笔投影 → 超期未收/无收点规划/收未埋；
//   - 书写不变量（R2b-2）：pinned_invariants × characters 台账比对 → 违例告警；
//   - 台账对账（R2b-7）：ledger_provenance 来源标注 × 来源章正文 → 来源失效/来源失支撑。
//
// 降级零提示全路径：进度读取失败 → 整块空串；单批取数失败/零告警 → 跳过该批
// （其余批照常）；全部无告警 → 空块（提示词与无该块逐字一致）。只报告不改写
// （不进 issue 清单、不触发修复路由、不翻转裁决）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
)

// categoryForeshadowing 是伏笔条目分类（time_entries.category 枚举之一）。
const categoryForeshadowing = "foreshadowing"

// checkerConfig 读取本书检查器配置（题材卡机制化，R2b-3）。
// 未配置/读取失败一律回落缺省（Debug 日志），绝不因配置缺失改变检查口径。
func (s *Service) checkerConfig(ctx context.Context, novelID int64) *quality.CheckerConfig {
	if s.qualityStore == nil {
		return quality.DefaultCheckerConfig(novelID)
	}
	cfg, err := s.qualityStore.GetCheckerConfig(ctx, novelID)
	if err != nil {
		s.logger.Debug("检查器配置读取失败（回落缺省）", "novel_id", novelID, "err", err)
		return quality.DefaultCheckerConfig(novelID)
	}
	return cfg
}

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
	grace := s.checkerConfig(ctx, novelID).OverdueGrace()
	var groups [][]structure.StructFinding
	if page, err := s.timelineStore.ListByNovel(ctx, novelID, timeline.ListByNovelOptions{
		Category:   categoryForeshadowing,
		PageParams: storage.PageParams{Size: -1},
	}); err != nil {
		s.logger.Warn("结构告警块：读取伏笔台账失败（跳过伏笔批）", "novel_id", novelID, "err", err)
	} else {
		groups = append(groups, structure.CheckForeshadowLifecycle(projectForeshadows(page.Items), now, grace))
	}
	if findings, ok := s.pinnedFindings(ctx, novelID, now); ok {
		groups = append(groups, findings)
	}
	if findings, ok := s.reconcileFindings(ctx, novelID); ok {
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
	// 境界等级表（R2b-6 合流）：取数失败=nil=上限只按数值比（缺省=现行行为逐字一致）。
	var levels []structure.PowerLevel
	if ls, err := s.structStore.ListPowerLevels(ctx, novelID); err == nil {
		levels = ls
	} else {
		s.logger.Debug("结构告警块：读取境界等级表失败（上限回落数值比对）", "novel_id", novelID, "err", err)
	}
	return structure.CheckPinnedInvariants(pins, projectCharacters(rows), now, levels), true
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

// ── 台账对账批（R2b-7，提案 §8.3 组件 B）──────────────────────────────

// reconcileLabelSpec 是台账表的对账标签列（标签列必填；别名列可空）。
type reconcileLabelSpec struct{ labelCol, aliasCol string }

// reconcileLabelColumns 是双向对账的表白名单：只有带可靠短标签的表参与对账
// （宁缺勿错）；列名固定白名单，绝不接受外部输入拼 SQL。
var reconcileLabelColumns = map[string]reconcileLabelSpec{
	"characters":         {labelCol: "name", aliasCol: "aliases"},
	"locations":          {labelCol: "name"},
	"story_arcs":         {labelCol: "name"},
	"time_entries":       {labelCol: "title"},
	"chronology_anchors": {labelCol: "label"},
}

// reconcileRow 是一条带来源标注的台账条目定位。
type reconcileRow struct {
	table     string
	id        int64
	chapterID int64
}

// reconcileFindings 跑「正文↔台账双向对账」批（R2b-7）：对带 provenance 来源标注的
// 台账条目做来源支撑核验（来源章是否仍存在、正文是否仍支撑条目）。
// 冲突不预设谁对：可能台账过时/记错该修台账，也可能正文改坏该修正文（方向判断
// 进 L1 契约 fix_type）。任一取数失败 → 跳过该批（ok=false，宁缺勿错）。
func (s *Service) reconcileFindings(ctx context.Context, novelID int64) ([]structure.StructFinding, bool) {
	if s.store == nil || s.store.db == nil || s.chapterStore == nil || s.chapterTextReader == nil {
		return nil, false
	}
	provRows, err := storage.NewProvenanceStore(s.store.db).ListByNovel(ctx, novelID)
	if err != nil {
		s.logger.Debug("结构告警块：读取台账 provenance 失败（跳过对账批）", "novel_id", novelID, "err", err)
		return nil, false
	}
	// 只有带来源标注的条目参与对账（无 provenance=不判，宁缺勿错）。
	var sourced []reconcileRow
	seenChapter := map[int64]bool{}
	for _, p := range provRows {
		if p.SourceChapterID == nil {
			continue
		}
		sourced = append(sourced, reconcileRow{table: p.EntryTable, id: p.EntryID, chapterID: *p.SourceChapterID})
		seenChapter[*p.SourceChapterID] = true
	}
	if len(sourced) == 0 {
		return nil, true
	}
	chapterIDs := make([]int64, 0, len(seenChapter))
	for id := range seenChapter {
		chapterIDs = append(chapterIDs, id)
	}
	missing, err := s.chapterStore.MissingIDsByNovel(ctx, nil, novelID, chapterIDs)
	if err != nil {
		s.logger.Debug("结构告警块：核验来源章存在性失败（跳过对账批）", "novel_id", novelID, "err", err)
		return nil, false
	}
	gone := map[int64]bool{}
	for _, id := range missing {
		gone[id] = true
	}
	texts := map[int64]string{}
	for _, id := range chapterIDs {
		if gone[id] {
			continue
		}
		text, rerr := s.chapterTextReader(ctx, novelID, id)
		if rerr != nil {
			// 单章正文读取失败：该章条目不判支撑（宁缺勿错），其余照常。
			s.logger.Debug("结构告警块：读取来源章正文失败（该条目不判支撑）", "chapter_id", id, "err", rerr)
			continue
		}
		texts[id] = text
	}
	labels := s.reconcileLabels(ctx, novelID, sourced)
	entries := make([]structure.ReconcileEntry, 0, len(sourced))
	for _, row := range sourced {
		ref := fmt.Sprintf("%s:%d", row.table, row.id)
		lbs, ok := labels[ref]
		if !ok {
			continue // 表不在对账白名单/取数失败/条目已删：不参与（宁缺勿错）
		}
		entries = append(entries, structure.ReconcileEntry{
			Ref:        ref,
			Labels:     lbs,
			Sourced:    true,
			SourceGone: gone[row.chapterID],
			SourceText: texts[row.chapterID],
		})
	}
	return structure.CheckLedgerReconcile(entries), true
}

// reconcileLabels 按白名单表装配条目标签（名称/别名）。返回 "表名:id" → 标签集
// （跨表同 id 互不覆盖）；仅包含在白名单表中查询到的条目（表不在白名单/查无此行/
// 取数失败自然缺席）。
func (s *Service) reconcileLabels(ctx context.Context, novelID int64, rows []reconcileRow) map[string][]string {
	idsByTable := map[string][]int64{}
	for _, r := range rows {
		if _, ok := reconcileLabelColumns[r.table]; !ok {
			continue
		}
		idsByTable[r.table] = append(idsByTable[r.table], r.id)
	}
	out := map[string][]string{}
	for table, ids := range idsByTable {
		spec := reconcileLabelColumns[table]
		query := fmt.Sprintf("SELECT id, %s AS label", spec.labelCol)
		if spec.aliasCol != "" {
			query += ", " + spec.aliasCol + " AS aliases"
		}
		query += fmt.Sprintf(" FROM %s WHERE novel_id = ? AND id IN ?", table)
		var found []struct {
			ID      int64
			Label   string
			Aliases string
		}
		if err := s.store.db.WithContext(ctx).Raw(query, novelID, ids).Scan(&found).Error; err != nil {
			s.logger.Debug("结构告警块：读取台账标签失败（跳过该表对账）", "table", table, "err", err)
			continue
		}
		for _, f := range found {
			labels := []string{f.Label}
			if strings.TrimSpace(f.Aliases) != "" {
				var aliases []string
				if err := json.Unmarshal([]byte(f.Aliases), &aliases); err == nil {
					labels = append(labels, aliases...)
				} // 别名 JSON 坏数据按无别名（宁缺勿错）
			}
			out[fmt.Sprintf("%s:%d", table, f.ID)] = labels
		}
	}
	return out
}
