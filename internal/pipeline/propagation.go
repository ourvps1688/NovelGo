package pipeline

// propagation.go —— R2b-8 修复传播检查（提案 §8.4 组件 C）接线。
//
// 修复/重写的副作用靠依赖图定向复验，不做全量复评也不裸奔：
//   - 依赖边来自台账（章节引用条目 X → 引用 X 的章节为下游依赖）：伏笔埋设/回收/
//     预期收章（time_entries）、时间线锚点改动章（chronology_anchors）、人物状态/
//     关系变更章（characters / character_relations）、来源标注章（ledger_provenance）；
//   - 改动章节后取依赖闭包（通常 2~5 章）做定向 L1 复验（「修复传播检查」块进
//     增量复验提示词）+ L0 全书规则扫描兜底（零 LLM 确定性规则逐章重扫落质量档案，
//     兜住依赖闭包外的机械性传播损伤）；
//   - 闭包超阈值（>8 章）= 结构性牵连，升级 L2 深审/人工裁决，不自动硬修
//     （传播块不注入、闸门 summary 明示升级）。
//
// 降级零提示全路径：无种子/读序映射失败/依赖边取数失败 → 空结果（提示词与无该块
// 逐字一致）；单边源取数失败 → 跳过该源其余照常；L0 扫描无读章能力/零章可扫 →
// 空串（summary 与无该段逐字一致）。只报告不改写（零正文/台账写入）。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
)

// propDep 是闭包内一个下游依赖章（渲染用）：全局阅读序号 + 共引台账条目。
type propDep struct {
	ReadingNumber int64
	Refs          []string
}

// propagationResult 是一轮修复传播检查的结果。
type propagationResult struct {
	// deps 是可渲染的下游依赖章（按阅读序升序）。
	deps []propDep
	// count 是依赖闭包原始章数（超阈值判定与 summary 口径）。
	count int
	// escalate 闭包超阈值（>structure.PropagationClosureCap）：结构性牵连。
	escalate bool
}

// propagationVerifyBlock 渲染「修复传播检查（依赖闭包定向复验）」块（R2b-8 §8.4）：
// 列出与本轮改动共引台账事实的下游依赖章，供增量复验定向核对修复传播损伤。
// 空清单返回空串（降级零提示，提示词与无该块逐字一致）。
func propagationVerifyBlock(deps []propDep) string {
	if len(deps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【修复传播检查（依赖闭包定向复验）】本轮修复改动的章节/台账条目与下列章节" +
		"共引同一批台账事实（伏笔埋收/时间线锚点/人物状态关系等），它们是下游依赖。" +
		"请逐章核对这些章节与共引条目相关的既有事实没有被修复波及" +
		"（只核对相关事实点，不通读全章，不做任何写操作）：\n")
	for _, d := range deps {
		fmt.Fprintf(&b, "- 第 %d 章（共引 %s）\n", d.ReadingNumber, strings.Join(d.Refs, "、"))
	}
	b.WriteString("若发现传播损伤（修复改 A 处导致 B 章不自洽），将其最相关的已修复问题标 ok=false 并在 note 一行说明。")
	return b.String()
}

// block 返回注入增量复验提示词的传播块（超阈值不注入=不硬修；空闭包=空串）。
func (r propagationResult) block() string {
	if r.escalate {
		return ""
	}
	return propagationVerifyBlock(r.deps)
}

// summary 返回闸门 summary 片段（空=零提示）：超阈值明示升级，否则报纳入定向复验的闭包规模。
func (r propagationResult) summary() string {
	switch {
	case r.escalate:
		return fmt.Sprintf("修复传播检查：依赖闭包 %d 章超阈值（>%d）——结构性牵连，不自动硬修，"+
			"已升级人工/深审（L2）裁决", r.count, structure.PropagationClosureCap)
	case r.count > 0:
		return fmt.Sprintf("修复传播检查：依赖闭包 %d 章已纳入定向复验", r.count)
	default:
		return ""
	}
}

// propagationCheck 计算本轮修复的依赖闭包（R2b-8 §8.4）。
// 种子=改动章节（fixed 项修过的作用域，契约=全局阅读序号）+ 改动台账条目
// （FixLog "L:" 台账变更履历）；闭包=与种子共引台账条目的章节（传递）。
// 降级零提示：任一前提缺失（读序映射失败/无种子）→ 空结果（提示词逐字一致）。
func (s *Service) propagationCheck(ctx context.Context, novelID int64, targets []verifyTarget, fixLog []FixLogEntry) propagationResult {
	if s.chapterStore == nil || s.store == nil || s.store.db == nil {
		return propagationResult{}
	}
	readingByID, err := s.chapterStore.GetReadingNumbersByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Debug("修复传播检查：读取章节阅读序失败（跳过）", "novel_id", novelID, "err", err)
		return propagationResult{}
	}
	idByReading := make(map[int64]int64, len(readingByID))
	for id, rn := range readingByID {
		idByReading[int64(rn)] = id
	}
	// 种子章节：fixed 项修过的作用域（issueChapterIDs 口径=全局阅读序号 → 章节 ID）。
	var seedChapters []int64
	seenSeed := map[int64]bool{}
	for _, tg := range targets {
		for _, rn := range tg.Chapters {
			id, ok := idByReading[rn]
			if !ok || seenSeed[id] {
				continue // 阅读序号无法定位（章已删/序漂移）→ 跳过（宁缺勿错）
			}
			seenSeed[id] = true
			seedChapters = append(seedChapters, id)
		}
	}
	seedEntries := s.fixLedgerChangedEntries(ctx, novelID, fixLog)
	if len(seedChapters) == 0 && len(seedEntries) == 0 {
		return propagationResult{}
	}
	deps := structure.DependencyClosure(s.depEdges(ctx, novelID, idByReading), seedChapters, seedEntries)
	if len(deps) == 0 {
		return propagationResult{}
	}
	// 章节 ID → 阅读序号渲染（无阅读序的依赖章跳过，宁缺勿错）。
	out := make([]propDep, 0, len(deps))
	for _, d := range deps {
		rn, ok := readingByID[d.ChapterID]
		if !ok {
			continue
		}
		out = append(out, propDep{ReadingNumber: int64(rn), Refs: d.EntryRefs})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ReadingNumber < out[j].ReadingNumber })
	return propagationResult{deps: out, count: len(deps), escalate: structure.PropagationEscalates(deps)}
}

// fixLedgerChangedEntries 从修复日志的台账变更条目（"L:<履历id>"）还原被改台账条目
// 定位（"表名:id"），供依赖闭包种子装配。解析/取数失败跳过（宁缺勿错）。
func (s *Service) fixLedgerChangedEntries(ctx context.Context, novelID int64, fixLog []FixLogEntry) []string {
	var ids []int64
	for _, e := range fixLog {
		if !strings.HasPrefix(e.ID, fixLogLedgerPrefix) {
			continue
		}
		n, perr := strconv.ParseInt(strings.TrimPrefix(e.ID, fixLogLedgerPrefix), 10, 64)
		if perr != nil || n <= 0 {
			continue
		}
		ids = append(ids, n)
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []storage.LedgerChangeHistory
	if err := s.store.db.WithContext(ctx).
		Where("novel_id = ? AND id IN ?", novelID, ids).Find(&rows).Error; err != nil {
		s.logger.Debug("修复传播检查：读取台账变更履历失败（跳过条目种子）", "novel_id", novelID, "err", err)
		return nil
	}
	refs := make([]string, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, fmt.Sprintf("%s:%d", r.EntryTable, r.EntryID))
	}
	return sortedUniqueStringsLocal(refs)
}

// depEdges 装配章节↔台账条目引用边（R2b-8：伏笔/时间线/人物状态表同理）。
// 各边源独立降级：单源取数失败跳过该源其余照常（Debug 日志，宁缺勿错）。
// idByReading 供「预期收章」（阅读序）定位章节；缺位的目标不造边。
func (s *Service) depEdges(ctx context.Context, novelID int64, idByReading map[int64]int64) []structure.DepEdge {
	var edges []structure.DepEdge

	// 伏笔/时间线（time_entries）：埋设章 / 回收章 / 预期收章。
	var teRows []struct {
		ID                  int64
		SourceChapterID     *int64
		ResolvedChapterID   *int64
		TargetReadingNumber int
	}
	if err := s.store.db.WithContext(ctx).Raw(
		"SELECT id, source_chapter_id, resolved_chapter_id, target_reading_number FROM time_entries WHERE novel_id = ?",
		novelID).Scan(&teRows).Error; err != nil {
		s.logger.Debug("修复传播检查：读取伏笔/时间线边失败（跳过该源）", "novel_id", novelID, "err", err)
	} else {
		for _, r := range teRows {
			ref := fmt.Sprintf("time_entries:%d", r.ID)
			if r.SourceChapterID != nil {
				edges = append(edges, structure.DepEdge{EntryRef: ref, ChapterID: *r.SourceChapterID})
			}
			if r.ResolvedChapterID != nil {
				edges = append(edges, structure.DepEdge{EntryRef: ref, ChapterID: *r.ResolvedChapterID})
			}
			if r.TargetReadingNumber > 0 {
				if id, ok := idByReading[int64(r.TargetReadingNumber)]; ok {
					edges = append(edges, structure.DepEdge{EntryRef: ref, ChapterID: id})
				}
			}
		}
	}

	// 时间线锚点（chronology_anchors）：锚点改动章。
	var anchorRows []struct {
		ID                 int64
		ChangedInChapterID *int64
	}
	if err := s.store.db.WithContext(ctx).Raw(
		"SELECT id, changed_in_chapter_id FROM chronology_anchors WHERE novel_id = ?", novelID).Scan(&anchorRows).Error; err != nil {
		s.logger.Debug("修复传播检查：读取时间线锚点边失败（跳过该源）", "novel_id", novelID, "err", err)
	} else {
		for _, r := range anchorRows {
			if r.ChangedInChapterID != nil {
				edges = append(edges, structure.DepEdge{
					EntryRef: fmt.Sprintf("chronology_anchors:%d", r.ID), ChapterID: *r.ChangedInChapterID})
			}
		}
	}

	// 人物状态（characters）：状态变更章。
	var charRows []struct {
		ID                 int64
		ChangedInChapterID *int64
	}
	if err := s.store.db.WithContext(ctx).Raw(
		"SELECT id, changed_in_chapter_id FROM characters WHERE novel_id = ?", novelID).Scan(&charRows).Error; err != nil {
		s.logger.Debug("修复传播检查：读取人物状态边失败（跳过该源）", "novel_id", novelID, "err", err)
	} else {
		for _, r := range charRows {
			if r.ChangedInChapterID != nil {
				edges = append(edges, structure.DepEdge{
					EntryRef: fmt.Sprintf("characters:%d", r.ID), ChapterID: *r.ChangedInChapterID})
			}
		}
	}

	// 人物关系（character_relations）：关系确立/变更章。
	var relRows []struct {
		ID        int64
		ChapterID *int64
	}
	if err := s.store.db.WithContext(ctx).Raw(
		"SELECT id, chapter_id FROM character_relations WHERE novel_id = ?", novelID).Scan(&relRows).Error; err != nil {
		s.logger.Debug("修复传播检查：读取人物关系边失败（跳过该源）", "novel_id", novelID, "err", err)
	} else {
		for _, r := range relRows {
			if r.ChapterID != nil {
				edges = append(edges, structure.DepEdge{
					EntryRef: fmt.Sprintf("character_relations:%d", r.ID), ChapterID: *r.ChapterID})
			}
		}
	}

	// 来源标注（ledger_provenance，全表）：条目来源章。
	var provRows []struct {
		EntryTable      string
		EntryID         int64
		SourceChapterID *int64
	}
	if err := s.store.db.WithContext(ctx).Raw(
		"SELECT entry_table, entry_id, source_chapter_id FROM ledger_provenance WHERE novel_id = ?", novelID).Scan(&provRows).Error; err != nil {
		s.logger.Debug("修复传播检查：读取来源标注边失败（跳过该源）", "novel_id", novelID, "err", err)
	} else {
		for _, r := range provRows {
			if r.SourceChapterID != nil && strings.TrimSpace(r.EntryTable) != "" && r.EntryID > 0 {
				edges = append(edges, structure.DepEdge{
					EntryRef: fmt.Sprintf("%s:%d", r.EntryTable, r.EntryID), ChapterID: *r.SourceChapterID})
			}
		}
	}
	return edges
}

// ── L0 全书规则扫描兜底（R2b-8 §8.4）────────────────────────────────────

// l0RuleSweep 是修复传播检查的 L0 全书规则扫描兜底：零 LLM 确定性规则（字数/章尾
// 钩子/AI 味/禁忌/称呼混用/死人出镜/战力数值序/文风漂移）逐章重扫并刷新质量档案
// （UpsertL0 只写 L0 列），兜住依赖闭包外的机械性传播损伤（台账改动改变检查输入：
// 死人出镜/称呼/战力序会跨章显形）。
// 只报告不改写：产出仅闸门 summary 计数 + 质量档案。降级零提示：无读章能力/
// 无章节/列表失败 → 空串（summary 与无该段逐字一致）；单章读取失败跳过该章
// （档案不覆盖为「已查」，宁缺勿错）。
func (s *Service) l0RuleSweep(ctx context.Context, novelID int64) string {
	if s.chapterTextReader == nil || s.chapterStore == nil || s.qualityStore == nil {
		return ""
	}
	chapters, err := s.chapterStore.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Debug("L0 全书规则扫描：读取章节列表失败（跳过）", "novel_id", novelID, "err", err)
		return ""
	}
	if len(chapters) == 0 {
		return ""
	}
	cfg := s.checkerConfig(ctx, novelID)
	var baseline map[string]float64
	if s.voiceMetricsProvider != nil {
		if b, berr := s.voiceMetricsProvider(ctx, novelID); berr == nil {
			baseline = b
		}
	}
	chars := s.l0Characters(ctx, novelID)
	var levels []quality.PowerLevelRef
	if s.structStore != nil {
		if rows, perr := s.structStore.ListPowerLevels(ctx, novelID); perr == nil {
			for _, r := range rows {
				levels = append(levels, quality.PowerLevelRef{Name: r.Name, Rank: r.Rank})
			}
		}
	}
	genre := s.novelGenre(ctx, novelID)

	checked, findings := 0, 0
	byCheck := map[string]int{}
	for _, ch := range chapters {
		text, terr := s.chapterTextReader(ctx, novelID, ch.ID)
		if terr != nil {
			s.logger.Debug("L0 全书规则扫描：读取章节正文失败（跳过该章）", "chapter_id", ch.ID, "err", terr)
			continue
		}
		metrics, fs := quality.RunL0Checks(quality.L0Input{
			Text:             text,
			ReadingNumber:    ch.ReadingNumber,
			Baseline:         baseline,
			Genre:            genre,
			TailHookRequired: cfg.TailHookRequired,
			TabooList:        cfg.Taboos(),
			Characters:       chars,
			PowerLevels:      levels,
		})
		if uerr := s.qualityStore.UpsertL0(ctx, &quality.ChapterQuality{
			NovelID:       novelID,
			ChapterID:     ch.ID,
			ReadingNumber: ch.ReadingNumber,
			L0Metrics:     quality.MarshalL0Metrics(metrics),
			L0Findings:    quality.MarshalFindings(fs),
			CheckedAt:     time.Now(),
		}); uerr != nil {
			s.logger.Debug("L0 全书规则扫描：落质量档案失败（不阻断）", "chapter_id", ch.ID, "err", uerr)
		}
		checked++
		findings += len(fs)
		for _, f := range fs {
			byCheck[f.Check]++
		}
	}
	// §9.4 命中率度量：L0 规则发现落档（曝光口径，见 alert_metrics.go 单源注释）。
	s.recordFeatureUsageN(ctx, featureAlertRaisedL1, findings)
	if checked == 0 {
		return ""
	}
	line := fmt.Sprintf("L0 全书规则扫描：受检 %d 章", checked)
	if findings == 0 {
		return line + "，零规则告警"
	}
	keys := make([]string, 0, len(byCheck))
	for k := range byCheck {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, byCheck[k]))
	}
	return line + fmt.Sprintf("，规则告警 %d 条（%s）", findings, strings.Join(parts, "、"))
}

// l0Characters 投影角色台账到 L0 输入（称呼混用/死人出镜，与章节 CI 同口径）。
// 取数失败/坏别名 JSON 按无别名（宁缺勿错，无台账=角色类检查自然跳过）。
func (s *Service) l0Characters(ctx context.Context, novelID int64) []quality.L0Character {
	if s.store == nil || s.store.db == nil {
		return nil
	}
	var rows []character.Character
	if err := s.store.db.WithContext(ctx).Where("novel_id = ?", novelID).Order("id").Find(&rows).Error; err != nil {
		s.logger.Debug("L0 全书规则扫描：读取角色台账失败（角色类检查跳过）", "novel_id", novelID, "err", err)
		return nil
	}
	out := make([]quality.L0Character, 0, len(rows))
	for _, r := range rows {
		var aliases []string
		if strings.TrimSpace(r.Aliases) != "" {
			if jerr := json.Unmarshal([]byte(r.Aliases), &aliases); jerr != nil {
				aliases = nil
			}
		}
		out = append(out, quality.L0Character{Name: r.Name, Aliases: aliases, Alive: r.Alive})
	}
	return out
}

// novelGenre 取本书题材（AI 味题材豁免，与章节 CI 同源）；取不到返回空串
// （L0 缺省口径：保守不豁免）。
func (s *Service) novelGenre(ctx context.Context, novelID int64) string {
	if s.store == nil || s.store.db == nil {
		return ""
	}
	var genre string
	if err := s.store.db.WithContext(ctx).
		Raw("SELECT genre FROM novels WHERE id = ?", novelID).Row().Scan(&genre); err != nil {
		return ""
	}
	return genre
}

// sortedUniqueStringsLocal 升序去重（本包小工具，不跨包导出）。
func sortedUniqueStringsLocal(vals []string) []string {
	if len(vals) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(vals))
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
