package snapshot

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
)

// 台账审计（WBS 2.4）：冻结前机械校验 series bible 结构自洽，防垃圾进垃圾出。
//
// 严重度分界（勿回退）：
//   - error = 结构性矛盾——悬挂引用、枚举越界、lifecycle 违规、包含环。
//     台账内部互相矛盾的行一旦冻结就成了「既定事实」，必须在冻结前修掉，
//     故 error 级问题阻断 Freeze（见 Service.Freeze）。
//   - warning = 可疑但合法——无从定位的存量 lifecycle 条目、缺回收章的 resolved
//     伏笔、同名条目、自环关系、gap 锚点缺标注。只报告不阻断。
//
// 语义张力类检测（「已死不复活」「限知 POV 不泄露」等 G7 一致性规则）需要正文
// 语境，属 S4 体检与阶段 3 一致性巡检，不在本机械审计范围内。
// lifecycle 规则以 internal/reader/lifecycle.go 为单源（写路径同款校验复用）。

// AuditSeverity 审计发现严重度。
type AuditSeverity string

const (
	// AuditError 结构性矛盾，阻断冻结。
	AuditError AuditSeverity = "error"
	// AuditWarning 可疑但合法，只报告。
	AuditWarning AuditSeverity = "warning"
)

// AuditIssue 一条台账不自洽发现。
type AuditIssue struct {
	Severity AuditSeverity `json:"severity"`
	// Table / RowID 定位到台账表与行，供前端跳转修复。
	Table string `json:"table"`
	RowID int64  `json:"row_id"`
	// Code 机器可读问题码（如 ref.dangling / lifecycle.unlocated）。
	Code string `json:"code"`
	// Message 中文可读描述（含具体引用值，可直接指导修复）。
	Message string `json:"message"`
}

// AuditReport 台账审计结果。
type AuditReport struct {
	NovelID      int64        `json:"novel_id"`
	Issues       []AuditIssue `json:"issues"`
	ErrorCount   int          `json:"error_count"`
	WarningCount int          `json:"warning_count"`
}

// LedgerInconsistentError 台账存在 error 级不自洽，冻结被拒（2.4 防垃圾进垃圾出）。
// Report 携带完整问题清单，前端可据此展示修复指引。
type LedgerInconsistentError struct {
	Report *AuditReport
}

// Error 摘要前几条错误，附完整计数。
func (e *LedgerInconsistentError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "台账审计未通过：%d 处结构性不自洽，已阻止冻结（防垃圾进垃圾出）", e.Report.ErrorCount)
	shown := 0
	for _, issue := range e.Report.Issues {
		if issue.Severity != AuditError || shown >= 3 {
			continue
		}
		fmt.Fprintf(&sb, "；%s#%d %s", issue.Table, issue.RowID, issue.Message)
		shown++
	}
	if e.Report.ErrorCount > shown {
		fmt.Fprintf(&sb, "；等共 %d 处，请调用台账审计接口查看全部", e.Report.ErrorCount)
	}
	return sb.String()
}

// auditData 审计输入：全部台账行 + 合法引用目标集合。
type auditData struct {
	characters   []character.Character
	relations    []character.CharacterRelation
	facts        []character.CharacterFactKnowledge
	locations    []location.Location
	arcs         []storyarc.StoryArc
	nodes        []storyarc.ArcNode
	entries      []timeline.TimelineEntry
	anchors      []timeline.ChronologyAnchor
	perspectives []reader.ReaderPerspective
	rosters      []reader.PovRoster
	// chapterOrder 章节 id → 阅读序（1-based）；键集兼作合法章节 id 集。
	chapterOrder map[int64]int
	// volumeIDs 合法卷 id 集。
	volumeIDs map[int64]bool
}

// AuditLedger 机械审计该小说全部 series bible 台账（WBS 2.4）。只读不写。
func (s *Service) AuditLedger(ctx context.Context, novelID int64) (*AuditReport, error) {
	return s.auditLedgerIn(ctx, s.db, novelID)
}

// auditLedgerIn 在指定连接/事务上审计（Freeze 闸门必须走事务句柄 tx，
// 否则 SQLite 多连接下事务内读到的是另一份库态）。
func (s *Service) auditLedgerIn(ctx context.Context, db *gorm.DB, novelID int64) (*AuditReport, error) {
	d, err := s.loadAuditData(ctx, db, novelID)
	if err != nil {
		return nil, err
	}
	return auditLedger(novelID, d), nil
}

// loadAuditData 按 id 升序加载审计所需行与引用目标集合。
func (s *Service) loadAuditData(ctx context.Context, db *gorm.DB, novelID int64) (*auditData, error) {
	d := &auditData{}
	load := func(name string, rows any) error {
		if err := db.WithContext(ctx).Where("novel_id = ?", novelID).Order("id ASC").Find(rows).Error; err != nil {
			return fmt.Errorf("snapshot: 审计加载 %s: %w", name, err)
		}
		return nil
	}
	if err := load("characters", &d.characters); err != nil {
		return nil, err
	}
	if err := load("character_relations", &d.relations); err != nil {
		return nil, err
	}
	if err := load("character_fact_knowledge", &d.facts); err != nil {
		return nil, err
	}
	if err := load("locations", &d.locations); err != nil {
		return nil, err
	}
	if err := load("story_arcs", &d.arcs); err != nil {
		return nil, err
	}
	if err := load("arc_nodes", &d.nodes); err != nil {
		return nil, err
	}
	if err := load("time_entries", &d.entries); err != nil {
		return nil, err
	}
	if err := load("chronology_anchors", &d.anchors); err != nil {
		return nil, err
	}
	if err := load("reader_perspectives", &d.perspectives); err != nil {
		return nil, err
	}
	if err := load("pov_roster", &d.rosters); err != nil {
		return nil, err
	}

	order, err := s.chapters.GetReadingNumbersByNovel(ctx, db, novelID)
	if err != nil {
		return nil, fmt.Errorf("snapshot: 审计加载章节阅读序: %w", err)
	}
	d.chapterOrder = order

	vols, err := s.volumes.ListByNovel(ctx, db, novelID)
	if err != nil {
		return nil, fmt.Errorf("snapshot: 审计加载卷: %w", err)
	}
	d.volumeIDs = make(map[int64]bool, len(vols))
	for _, v := range vols {
		d.volumeIDs[v.ID] = true
	}
	return d, nil
}

// auditLedger 纯函数审计（无 IO，便于单测穷举规则）。
// 产出顺序固定：按 registry 表序、行 id 升序，同类检查按代码定义顺序。
func auditLedger(novelID int64, d *auditData) *AuditReport {
	r := &AuditReport{NovelID: novelID, Issues: []AuditIssue{}}
	add := func(severity AuditSeverity, table string, rowID int64, code, format string, args ...any) {
		r.Issues = append(r.Issues, AuditIssue{
			Severity: severity,
			Table:    table,
			RowID:    rowID,
			Code:     code,
			Message:  fmt.Sprintf(format, args...),
		})
	}

	// 合法引用目标集合（取自行集，天然 scoped 到本小说）。
	charIDs := make(map[int64]bool, len(d.characters))
	for _, c := range d.characters {
		charIDs[c.ID] = true
	}
	arcIDs := make(map[int64]bool, len(d.arcs))
	for _, a := range d.arcs {
		arcIDs[a.ID] = true
	}
	factIDs := make(map[int64]bool, len(d.perspectives))
	for _, p := range d.perspectives {
		factIDs[p.ID] = true
	}
	locIDs := make(map[int64]bool, len(d.locations))
	for _, l := range d.locations {
		locIDs[l.ID] = true
	}

	// ── 引用完整性（error：悬挂引用=结构断裂）─────────────────────────
	for _, rel := range d.relations {
		if !charIDs[rel.SourceCharacterID] {
			add(AuditError, "character_relations", rel.ID, "ref.dangling",
				"关系引用不存在的角色 source_character_id=%d", rel.SourceCharacterID)
		}
		if !charIDs[rel.TargetCharacterID] {
			add(AuditError, "character_relations", rel.ID, "ref.dangling",
				"关系引用不存在的角色 target_character_id=%d", rel.TargetCharacterID)
		}
		if rel.SourceCharacterID == rel.TargetCharacterID {
			add(AuditWarning, "character_relations", rel.ID, "relation.self_loop",
				"关系两端是同一角色（char_id=%d），疑似误写", rel.SourceCharacterID)
		}
		if rel.RelationKind != "" && !validRelationKind(rel.RelationKind) {
			add(AuditError, "character_relations", rel.ID, "enum.invalid",
				"relation_kind 非法值 %q（允许：mentor|rival|lover|kin|other）", rel.RelationKind)
		}
	}

	for _, f := range d.facts {
		if !charIDs[f.CharID] {
			add(AuditError, "character_fact_knowledge", f.ID, "ref.dangling",
				"认知边界引用不存在的角色 char_id=%d", f.CharID)
		}
		if !factIDs[f.FactID] {
			add(AuditError, "character_fact_knowledge", f.ID, "ref.dangling",
				"认知边界引用不存在的读者条目 fact_id=%d（reader_perspectives.id）", f.FactID)
		}
		if f.AsOfChapterID != nil && d.chapterOrder[*f.AsOfChapterID] == 0 {
			add(AuditError, "character_fact_knowledge", f.ID, "ref.dangling",
				"认知边界引用不存在的章节 as_of_chapter_id=%d", *f.AsOfChapterID)
		}
	}

	for _, l := range d.locations {
		if l.ParentLocationID != nil && !locIDs[*l.ParentLocationID] {
			add(AuditError, "locations", l.ID, "ref.dangling",
				"地点引用不存在的父地点 parent_location_id=%d", *l.ParentLocationID)
		}
		if l.State != "" && !validLocationState(l.State) {
			add(AuditError, "locations", l.ID, "enum.invalid",
				"state 非法值 %q（允许：intact|destroyed|blocked，空=未标注）", l.State)
		}
	}

	for _, a := range d.arcs {
		if a.CharID != nil && !charIDs[*a.CharID] {
			add(AuditError, "story_arcs", a.ID, "ref.dangling",
				"角色弧引用不存在的角色 char_id=%d", *a.CharID)
		}
		if !validArcType(a.ArcType) {
			add(AuditError, "story_arcs", a.ID, "enum.invalid",
				"arc_type 非法值 %q（允许：main|sub|character|background）", a.ArcType)
		}
		if !validArcStatus(a.Status) {
			add(AuditError, "story_arcs", a.ID, "enum.invalid",
				"status 非法值 %q（允许：active|paused|completed|abandoned）", a.Status)
		}
	}

	for _, n := range d.nodes {
		if !arcIDs[n.StoryArcID] {
			add(AuditError, "arc_nodes", n.ID, "ref.dangling",
				"弧线节点引用不存在的弧线 story_arc_id=%d", n.StoryArcID)
		}
		if n.ActualChapterID != nil && d.chapterOrder[*n.ActualChapterID] == 0 {
			add(AuditError, "arc_nodes", n.ID, "ref.dangling",
				"弧线节点引用不存在的章节 actual_chapter_id=%d", *n.ActualChapterID)
		}
		if !validNodeStatus(n.Status) {
			add(AuditError, "arc_nodes", n.ID, "enum.invalid",
				"status 非法值 %q（允许：pending|completed|abandoned）", n.Status)
		}
	}

	for _, e := range d.entries {
		if e.SourceChapterID != nil && d.chapterOrder[*e.SourceChapterID] == 0 {
			add(AuditError, "time_entries", e.ID, "ref.dangling",
				"伏笔引用不存在的埋设章 source_chapter_id=%d", *e.SourceChapterID)
		}
		if e.ResolvedChapterID != nil && d.chapterOrder[*e.ResolvedChapterID] == 0 {
			add(AuditError, "time_entries", e.ID, "ref.dangling",
				"伏笔引用不存在的回收章 resolved_chapter_id=%d", *e.ResolvedChapterID)
		}
		if !validTimelineCategory(e.Category) {
			add(AuditError, "time_entries", e.ID, "enum.invalid",
				"category 非法值 %q（允许：foreshadowing|user_directive）", e.Category)
		}
		if !validTimelineStatus(e.Status) {
			add(AuditError, "time_entries", e.ID, "enum.invalid",
				"status 非法值 %q（允许：pending|resolved|abandoned）", e.Status)
		}
		if e.Status == "resolved" && e.ResolvedChapterID == nil {
			add(AuditWarning, "time_entries", e.ID, "timeline.resolved_without_chapter",
				"伏笔已标记 resolved 但回收章为空，无法定位回收点")
		}
	}

	for _, a := range d.anchors {
		if a.AfterVolume != nil && !d.volumeIDs[*a.AfterVolume] {
			add(AuditError, "chronology_anchors", a.ID, "ref.dangling",
				"时间锚点引用不存在的卷 after_volume=%d", *a.AfterVolume)
		}
		if !validAnchorType(a.AnchorType) {
			add(AuditError, "chronology_anchors", a.ID, "enum.invalid",
				"anchor_type 非法值 %q（允许：era|date|age|season|gap）", a.AnchorType)
		}
		if a.AnchorType == timeline.AnchorTypeGap && (a.AfterVolume == nil || a.Duration == "") {
			add(AuditWarning, "chronology_anchors", a.ID, "anchor.gap_incomplete",
				"gap 锚点缺显式标注（after_volume=%v duration=%q），卷间空白期无法防推算漂移",
				a.AfterVolume, a.Duration)
		}
	}

	for _, p := range d.rosters {
		if !charIDs[p.CharID] {
			add(AuditError, "pov_roster", p.ID, "ref.dangling",
				"POV 名册引用不存在的角色 char_id=%d", p.CharID)
		}
		if !validPOVMode(p.Mode) {
			add(AuditError, "pov_roster", p.ID, "enum.invalid",
				"mode 非法值 %q（允许：limited|omniscient）", p.Mode)
		}
	}

	// ── lifecycle 不变量（规则单源 internal/reader/lifecycle.go）──────
	orderOf := func(chapterID int64) int { return d.chapterOrder[chapterID] }
	for _, e := range d.perspectives {
		if e.PlantedChapterID == nil {
			// 存量无从定位：设计允许 nil=历史记录无法关联章节。不依赖埋设章的基础
			// 规则仍显式查（写路径单源在 lifecycle.go）；unlocated 警告只给「基础检查
			// 通过、仅缺定位」的行——已命中 error 的行不再叠加噪声（warning=可疑但合法）。
			broken := false
			if e.Type != reader.TypeKnown && e.Type != reader.TypeSuspense && e.Type != reader.TypeMisconception {
				add(AuditError, "reader_perspectives", e.ID, "enum.invalid",
					"type 非法值 %q（允许：known|suspense|misconception）", e.Type)
				broken = true
			}
			if strings.TrimSpace(e.Content) == "" {
				add(AuditError, "reader_perspectives", e.ID, "lifecycle.content_empty",
					"条目内容为空")
				broken = true
			}
			if e.Type == reader.TypeKnown && e.RevealedChapterID != nil {
				add(AuditError, "reader_perspectives", e.ID, "lifecycle.known_with_revealed",
					"known 是单点生命周期，不应设置回收章 revealed_chapter_id=%d", *e.RevealedChapterID)
				broken = true
			}
			if !broken {
				add(AuditWarning, "reader_perspectives", e.ID, "lifecycle.unlocated",
					"无埋设/形成/揭示章，无法做时序校验（存量无从定位条目）")
			}
			continue
		}
		if err := reader.ValidateLifecycle(e, orderOf); err != nil {
			add(AuditError, "reader_perspectives", e.ID, "lifecycle.invalid", "%s", err.Error())
		}
	}

	// ── 包含环（error：地点树必须无环）──────────────────────────────
	for _, l := range d.locations {
		if l.ParentLocationID == nil {
			continue
		}
		seen := map[int64]bool{l.ID: true}
		for cur := l.ParentLocationID; cur != nil; {
			if seen[*cur] {
				add(AuditError, "locations", l.ID, "locations.parent_cycle",
					"地点包含关系成环（parent_location_id=%d），地点树必须无环", *cur)
				break
			}
			seen[*cur] = true
			next := (*int64)(nil)
			for _, cand := range d.locations {
				if cand.ID == *cur {
					next = cand.ParentLocationID
					break
				}
			}
			cur = next
		}
	}

	// ── 重复名（warning：同名可疑但不必然错）────────────────────────
	addDuplicateNames := func(table string, names map[string][]int64) {
		keys := make([]string, 0, len(names))
		for name := range names {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			ids := names[name]
			if len(ids) < 2 {
				continue
			}
			idStrs := make([]string, len(ids))
			for i, id := range ids {
				idStrs[i] = fmt.Sprintf("%d", id)
			}
			add(AuditWarning, table, ids[0], table+".duplicate_name",
				"同名条目 %q 出现 %d 次（id: %s），疑似重复创建", name, len(ids), strings.Join(idStrs, ", "))
		}
	}
	charNames := map[string][]int64{}
	for _, c := range d.characters {
		charNames[c.Name] = append(charNames[c.Name], c.ID)
	}
	addDuplicateNames("characters", charNames)
	locNames := map[string][]int64{}
	for _, l := range d.locations {
		locNames[l.Name] = append(locNames[l.Name], l.ID)
	}
	addDuplicateNames("locations", locNames)

	for _, issue := range r.Issues {
		if issue.Severity == AuditError {
			r.ErrorCount++
		} else {
			r.WarningCount++
		}
	}
	return r
}

// ── 封闭枚举（与模型注释/常量一致；无导出常量的沿模型文档字面值）──────

func validRelationKind(v string) bool {
	switch v {
	case character.RelationKindMentor, character.RelationKindRival,
		character.RelationKindLover, character.RelationKindKin, character.RelationKindOther:
		return true
	}
	return false
}

func validLocationState(v string) bool {
	switch v {
	case location.StateIntact, location.StateDestroyed, location.StateBlocked:
		return true
	}
	return false
}

func validArcType(v string) bool {
	switch v {
	case "main", "sub", "character", "background":
		return true
	}
	return false
}

func validArcStatus(v string) bool {
	switch v {
	case "active", "paused", "completed", "abandoned":
		return true
	}
	return false
}

func validNodeStatus(v string) bool {
	switch v {
	case "pending", "completed", "abandoned":
		return true
	}
	return false
}

func validTimelineCategory(v string) bool {
	switch v {
	case "foreshadowing", "user_directive":
		return true
	}
	return false
}

func validTimelineStatus(v string) bool {
	switch v {
	case "pending", "resolved", "abandoned":
		return true
	}
	return false
}

func validAnchorType(v string) bool {
	switch v {
	case timeline.AnchorTypeEra, timeline.AnchorTypeDate, timeline.AnchorTypeAge,
		timeline.AnchorTypeSeason, timeline.AnchorTypeGap:
		return true
	}
	return false
}

func validPOVMode(v string) bool {
	return v == reader.POVModeLimited || v == reader.POVModeOmniscient
}
