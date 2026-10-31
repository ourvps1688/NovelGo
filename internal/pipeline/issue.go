package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ── issue 一等公民落表（R1 §4.3「修复工单化」）──────────────────────────────
// 体检 issue 不再只活在 GateContext.Audit 的 JSON 串里：落独立表 pipeline_issues，
// 带稳定键、结构化章节作用域（chapter_ids，替代 parseChapterRefs 正则猜测，F5 根治）、
// 修复路由字段（fix_type/target_scope/dimension/direction）、状态与轮次。
// JSON 副本仍是提示词/前端渲染载荷；本表是结构化事实源（作用域分组、状态流转、统计）。

// issue 工单状态（与 GateContext.FixStates 取值对齐；pending/fixing 是落表后的进行态）。
// R5-1 升格为发现项工单（提案 §10.2）：status 列同时承载工单状态机
// 「发现 → 确认 →（人工）修复中 → 复验通过/驳回」，新增 confirmed/verified/dismissed
// 三态；流水线路由态（rewrite/retcon/skipped/fixed）保留作兼容读与自动回路收口
// （R5-2 自动回路退役后工单态成为主用值）。
const (
	IssueStatusPending   = "pending"   // 发现（落表初始态，待确认）
	IssueStatusConfirmed = "confirmed" // 确认（作者认领，待人工修复）
	IssueStatusFixing    = "fixing"    // 修复中（人工修复中）
	IssueStatusFixed     = "fixed"     // 已修复（工单修复回执态，待复验；R5-2 起修复由工单显式触发）
	IssueStatusVerified  = "verified"  // 复验通过（终态，必带复验回执）
	IssueStatusDismissed = "dismissed" // 驳回（终态，非问题/不修，留人工理由）
	IssueStatusSkipped   = "skipped"
	IssueStatusRewrite   = "rewrite"
	IssueStatusRetcon    = "retcon"
	// IssueStatusVerifying 是「复验进行中」的瞬态：复验异步执行期间工单停留此态，
	// 复验收口（通过/退回/未确认）后离开。进程崩溃残留的 verifying 由启动期清扫复位（见 RecoverInterruptedRuns）。
	IssueStatusVerifying = "verifying"
)

// 工单流转的显式错误面（面板按钮路径：宁缺勿错——非法迁移显式报错，不静默改写）。
var (
	ErrIssueNotFound          = errors.New("issue 工单不存在")
	ErrIssueIllegalTransition = errors.New("issue 工单状态流转不合法")
	ErrIssueTerminal          = errors.New("issue 工单已终态，不可再流转")
)

// issueTicketNext 是发现项工单状态机的合法流转表（§10.2）：
// 发现（pending）→ 确认（confirmed）→（人工）修复中（fixing）→ 复验通过（verified）/驳回（dismissed）。
// 兼容流水线路由态的工单收口出口（rewrite/retcon/skipped/fixed 也走「去修复/驳回」收口，
// 复验退回 pending=退回待修，与显式复验 VerifyIssueTickets 同口径）。
// verified/dismissed 是终态无出边；→verified 一律只经 RecordIssueVerify（必带复验回执），
// 不经通用流转。本表只管工单面板显式流转，流水线内部同步（syncIssueRow）不校验（兼容写）。
var issueTicketNext = map[string][]string{
	IssueStatusPending:   {IssueStatusConfirmed, IssueStatusFixing, IssueStatusDismissed},
	IssueStatusConfirmed: {IssueStatusFixing, IssueStatusDismissed},
	IssueStatusFixing:    {IssueStatusPending, IssueStatusDismissed},
	IssueStatusFixed:     {IssueStatusFixing, IssueStatusPending, IssueStatusDismissed},
	IssueStatusRewrite:   {IssueStatusFixing, IssueStatusDismissed},
	IssueStatusSkipped:   {IssueStatusFixing, IssueStatusDismissed},
	IssueStatusRetcon:    {IssueStatusDismissed},
	// IssueStatusVerified / IssueStatusDismissed：终态，无出边。
}

// PipelineIssue 是一条体检 issue 的持久化工单。
type PipelineIssue struct {
	ID      int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID int64  `gorm:"column:novel_id;not null;index"      json:"novel_id"`
	RunID   string `gorm:"column:run_id;not null;index;uniqueIndex:idx_pipeline_issues_run_key,priority:1" json:"run_id"`
	// IssueKey 是 run 内稳定键（issue 契约 ID，如 I1/V1/D1）。每轮体检整批替换
	// （新体检=新问题清单，与 executeAudit 重置 FixStates 同语义），跨轮同号
	// 不共用断点。
	IssueKey    string `gorm:"column:issue_key;not null;uniqueIndex:idx_pipeline_issues_run_key,priority:2" json:"issue_key"`
	Level       string `gorm:"column:level;not null" json:"level"`
	Area        string `gorm:"column:area"           json:"area"`
	Desc        string `gorm:"column:desc;type:text" json:"desc"`
	FixType     string `gorm:"column:fix_type"       json:"fix_type,omitempty"`
	TargetScope string `gorm:"column:target_scope"   json:"target_scope,omitempty"`
	Dimension   string `gorm:"column:dimension"      json:"dimension,omitempty"`
	Direction   string `gorm:"column:direction"      json:"direction,omitempty"`
	// ChapterIDsJSON 是涉及章节全局阅读序号（JSON 数组序列化，如 [1,2,3]）。
	// 存的是 issueChapterIDs 的有效值（结构化字段优先，正则兜底），供作用域分组直接消费。
	ChapterIDsJSON string `gorm:"column:chapter_ids;type:text" json:"chapter_ids,omitempty"`
	Status         string `gorm:"column:status;not null;index"  json:"status"`
	SkipReason     string `gorm:"column:skip_reason;type:text"  json:"skip_reason,omitempty"`
	// VerifyReceipt 是复验回执的一行结论（复验通过/未通过的依据，来自复验回执 note
	// 或人工复验备注）；复验落表时写入（RecordIssueVerify），供工单面板展示。
	VerifyReceipt string `gorm:"column:verify_receipt;type:text" json:"verify_receipt,omitempty"`
	// VerifyAt 是最近一次复验落表时间。
	VerifyAt *time.Time `gorm:"column:verify_at" json:"verify_at,omitempty"`
	// Note 是工单面板流转时的人工备注（驳回理由/交接说明）。
	Note string `gorm:"column:note;type:text" json:"note,omitempty"`
	// Round 是记录/流转时的修复轮数（GateContext.FixRounds），供轮次统计与增量复验分轮。
	Round     int       `gorm:"column:round;not null;default:0" json:"round"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 指定物理表名。
func (PipelineIssue) TableName() string { return "pipeline_issues" }

// issueChapterIDs 返回 issue 的涉及章节全局阅读序号：契约结构化字段 chapter_ids 优先；
// 空时回退 parseChapterRefs 从 area/desc 文本正则兜底（兼容读：旧数据/LLM 未输出）。
// F5 的正则猜测从「唯一来源」降级为「兜底」，新数据一律走结构化字段。
func issueChapterIDs(it AuditIssue) []int64 {
	if len(it.ChapterIDs) > 0 {
		return it.ChapterIDs
	}
	return parseChapterRefs(it.Area, it.Desc)
}

// pipelineIssueRow 把一条契约 issue 投影为工单行（状态 pending，作用域取有效章节号）。
func pipelineIssueRow(novelID int64, runID string, round int, it AuditIssue) PipelineIssue {
	chapters, _ := json.Marshal(issueChapterIDs(it))
	return PipelineIssue{
		NovelID:        novelID,
		RunID:          runID,
		IssueKey:       it.ID,
		Level:          it.Level,
		Area:           it.Area,
		Desc:           it.Desc,
		FixType:        it.FixType,
		TargetScope:    it.TargetScope,
		Dimension:      it.Dimension,
		Direction:      it.Direction,
		ChapterIDsJSON: string(chapters),
		Status:         IssueStatusPending,
		Round:          round,
	}
}

// ReplaceIssues 以新一轮体检结果整批替换该 run 的 issue 工单（新体检=新问题清单）。
// round 记录本轮修复轮数，供增量复验分轮与统计。
func (s *Store) ReplaceIssues(ctx context.Context, novelID int64, runID string, round int, issues []AuditIssue) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("novel_id = ? AND run_id = ?", novelID, runID).
			Delete(&PipelineIssue{}).Error; err != nil {
			return err
		}
		for _, it := range issues {
			row := pipelineIssueRow(novelID, runID, round, it)
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateIssueStatus 流转一条工单状态（fixed/skipped/rewrite/retcon/...）；skipReason
// 非空时一并记录（skipped 即转人工工单的留痕依据）。
func (s *Store) UpdateIssueStatus(ctx context.Context, novelID int64, runID, issueKey, status, skipReason string) error {
	updates := map[string]any{"status": status, "updated_at": time.Now()}
	if skipReason != "" {
		updates["skip_reason"] = skipReason
	}
	if status == IssueStatusVerifying {
		// 重新进入复验态：清除此前「启动清扫/中断」残留备注，避免与「复验中」状态并排显示造成
		// 「卡在复验中」的误导错觉（UI 仅 VerifyIssueTickets 会把状态置为 verifying，此处收窄安全）。
		updates["note"] = ""
	}
	return s.db.WithContext(ctx).Model(&PipelineIssue{}).
		Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
		Updates(updates).Error
}

// ListIssues 列出该 run 的全部 issue 工单（落表顺序即体检清单顺序）。
func (s *Store) ListIssues(ctx context.Context, novelID int64, runID string) ([]PipelineIssue, error) {
	var rows []PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND run_id = ?", novelID, runID).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListFixingTickets 列出该 run 处于 fixing 态的发现项工单（工单「去修复」执行体的聚合入口，
// #240 R5-2 工单接执行：执行体按 run 处理全部 fixing 态工单，不传 refs）。
func (s *Store) ListFixingTickets(ctx context.Context, novelID int64, runID string) ([]PipelineIssue, error) {
	var rows []PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND run_id = ? AND status = ?", novelID, runID, IssueStatusFixing).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListVerifyingTickets 列出该 run 处于 verifying 态的发现项工单（复验执行体的聚合入口，
// 与 ListFixingTickets 同构：复验按 run 处理全部 verifying 态工单，不传 refs）。
func (s *Store) ListVerifyingTickets(ctx context.Context, novelID int64, runID string) ([]PipelineIssue, error) {
	var rows []PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND run_id = ? AND status = ?", novelID, runID, IssueStatusVerifying).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ResetStuckVerifying 将残留的 verifying 态工单复位为 pending（进程崩溃/中断残留清扫，
// 与 RecoverInterruptedRuns 同口径：执行体随进程退出而消亡，DB 状态需人工/启动期复位）。
func (s *Store) ResetStuckVerifying(ctx context.Context, novelID int64) (int64, error) {
	res := s.db.WithContext(ctx).
		Model(&PipelineIssue{}).
		Where("novel_id = ? AND status = ?", novelID, IssueStatusVerifying).
		Updates(map[string]any{"status": IssueStatusPending, "note": "复验执行中断，已复位待复验"})
	return res.RowsAffected, res.Error
}

// ResetAllStuckVerifying 启动期（RecoverInterruptedRuns）全局清扫：把所有 novel 残留的
// verifying 态工单一次性复位为 pending。复验执行体随进程退出而消亡，若崩溃/重启发生在
// 复验中途，执行体收口逻辑不再运行，这些工单会永久停在 verifying；此处兜底复位，避免
// 永久卡死（issue.go:35 注释承诺的「启动期清扫复位」此前未真正实现，本次补齐）。
func (s *Store) ResetAllStuckVerifying(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).
		Model(&PipelineIssue{}).
		Where("status = ?", IssueStatusVerifying).
		Updates(map[string]any{"status": IssueStatusPending, "note": "应用重启导致复验中断，已复位待复验"})
	return res.RowsAffected, res.Error
}

// ── 发现项工单（R5-1，提案 §10.2/§10.7）──────────────────────────────────────
// issue 台账升格为工单：状态机（发现→确认→（人工）修复中→复验通过/驳回）+ 复验回执。
// 工单面板按钮经 Service 走本组方法；流水线内部同步（UpdateIssueStatus）不走状态机
// （兼容写，R5-2 自动回路退役后收口）。

// getIssueRow 按 run 内稳定键取一行工单（不存在返回 ErrIssueNotFound）。
func (s *Store) getIssueRow(ctx context.Context, novelID int64, runID, issueKey string) (*PipelineIssue, error) {
	var row PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrIssueNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// TransitionIssueTicket 按工单状态机流转一条发现项工单（面板「确认/去修复/驳回/退回待修」）。
// to 仅限 issueTicketNext 表内的合法迁移，非法迁移/终态一律显式报错（宁缺勿错）；
// to=verified 一律拒绝——复验通过必须经 RecordIssueVerify 携带复验回执落表。
// note 非空时写入 note 列（驳回理由/人工备注），为空不覆盖既有备注。
func (s *Store) TransitionIssueTicket(ctx context.Context, novelID int64, runID, issueKey, to, note string) error {
	if to == IssueStatusVerified {
		return fmt.Errorf("%w: 复验通过须经复验回执落表（RecordIssueVerify）", ErrIssueIllegalTransition)
	}
	row, err := s.getIssueRow(ctx, novelID, runID, issueKey)
	if err != nil {
		return err
	}
	allowed := issueTicketNext[row.Status]
	ok := false
	for _, a := range allowed {
		if a == to {
			ok = true
			break
		}
	}
	if !ok {
		if len(allowed) == 0 {
			return fmt.Errorf("%w: %s 是终态", ErrIssueTerminal, row.Status)
		}
		return fmt.Errorf("%w: %s → %s", ErrIssueIllegalTransition, row.Status, to)
	}
	updates := map[string]any{"status": to, "updated_at": time.Now()}
	if note != "" {
		updates["note"] = note
	}
	return s.db.WithContext(ctx).Model(&PipelineIssue{}).
		Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
		Updates(updates).Error
}

// RecordIssueVerify 落一条复验回执（§10.2「状态机 + 复验回执」）：通过→verified（终态），
// 未通过→pending（退回待修，与显式复验 VerifyIssueTickets 同口径）；verify_receipt/verify_at
// 逐次留痕。终态工单（verified/dismissed）不接受复验。
func (s *Store) RecordIssueVerify(ctx context.Context, novelID int64, runID, issueKey string, ok bool, receipt string) error {
	row, err := s.getIssueRow(ctx, novelID, runID, issueKey)
	if err != nil {
		return err
	}
	if row.Status == IssueStatusVerified || row.Status == IssueStatusDismissed {
		return fmt.Errorf("%w: %s 是终态", ErrIssueTerminal, row.Status)
	}
	to := IssueStatusPending // 复验未通过：退回待修
	if ok {
		to = IssueStatusVerified
	}
	now := time.Now()
	return s.db.WithContext(ctx).Model(&PipelineIssue{}).
		Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
		Updates(map[string]any{
			"status": to, "verify_receipt": receipt, "verify_at": now, "updated_at": now,
		}).Error
}

// ListIssueTicketsByNovel 列出一本书的全部发现项工单（工单面板列表，跨 run，新单在前）。
func (s *Store) ListIssueTicketsByNovel(ctx context.Context, novelID int64) ([]PipelineIssue, error) {
	var rows []PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("id DESC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListRewriteTickets 列出回流 S3 重写回路的正文级工单（RewriteBacklog 的工单表投影源）：
// fix_type=body 或 V 系（isStyleExempt）且未收口的行（verified/dismissed/retcon 除外）。
// 写保护（writeProtected）行由消费方 rewriteBacklogOf 过滤（与 bodyIssuesOf 同语义）。
func (s *Store) ListRewriteTickets(ctx context.Context, novelID int64, runID string) ([]PipelineIssue, error) {
	var rows []PipelineIssue
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND run_id = ? AND status NOT IN ?",
			novelID, runID, []string{IssueStatusVerified, IssueStatusDismissed, IssueStatusRetcon}).
		Order("id").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PipelineIssue, 0, len(rows))
	for _, r := range rows {
		if r.FixType == FixTypeBody || isStyleExempt(r.IssueKey) {
			out = append(out, r)
		}
	}
	return out, nil
}

// rewriteItemOfRow 把一条正文级工单行投影为 RewriteItem（JSON 契约副本形状，前端兼容读）。
func rewriteItemOfRow(r PipelineIssue) RewriteItem {
	var chapters []int64
	if r.ChapterIDsJSON != "" {
		_ = json.Unmarshal([]byte(r.ChapterIDsJSON), &chapters)
	}
	return RewriteItem{
		ID:          r.IssueKey,
		Level:       r.Level,
		Area:        r.Area,
		Desc:        r.Desc,
		Chapters:    chapters,
		TargetScope: r.TargetScope,
		Dimension:   r.Dimension,
		Direction:   r.Direction,
	}
}
