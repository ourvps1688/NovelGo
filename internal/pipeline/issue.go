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
	// VerifyAttempts 是复验「明确未通过」的累计次数（只计 rejected，不计未确认 unverified）；
	// 达阈值（verifyEscalateAfter）自动升级为人工工单（skipped），避免无限空转。
	VerifyAttempts int `gorm:"column:verify_attempts;not null;default:0" json:"verify_attempts"`
	// Note 是工单面板流转时的人工备注（驳回理由/交接说明）。
	Note string `gorm:"column:note;type:text" json:"note,omitempty"`
	// PreVerifyStatus 是进入 verifying 瞬态前的状态快照（UpdateIssueStatus 置 verifying 时
	// 原子捕获）：复验「未确认/中断」回退时恢复原状态而非一律 pending——已修复（待复验）
	// 的工单不得被一次供应商超时/回执未解析打回「待确认」（#100 状态回退）。
	PreVerifyStatus string `gorm:"column:pre_verify_status" json:"pre_verify_status,omitempty"`
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

// filterChapterIDs 按本书有效章节范围 [1, maxChapter] 裁剪涉及章节号，剔除越界值：
//   - LLM 把台账实体 ID（如 217/252）误填进结构化 chapter_ids 的脏数据；
//   - LLM 引用尚未写出的规划章号（本书只写了 7 章却在 chapter_ids 写 8..20）。
//
// maxChapter<=0 表示本书暂无章节（含测试未建章节的情形），跳过裁剪以保留既有行为、
// 不破坏无章节测试。阅读序号由 GetReadingNumbersByNovel 顺序编号为连续 1..N，故有效
// 范围等价于 [1, 章节总数]，无需引入 chapter store 依赖，直接按 chapters 表计数即可。
func filterChapterIDs(chapters []int64, maxChapter int) []int64 {
	if maxChapter <= 0 {
		return chapters
	}
	out := make([]int64, 0, len(chapters))
	for _, c := range chapters {
		if c >= 1 && int(c) <= maxChapter {
			out = append(out, c)
		}
	}
	return out
}

// pipelineIssueRow 把一条契约 issue 投影为工单行（状态 pending，作用域取有效章节号）。
// validChapterMax 为本节有效章节上界（0 表示不裁剪，见 filterChapterIDs）。
func pipelineIssueRow(novelID int64, runID string, round int, it AuditIssue, validChapterMax int) PipelineIssue {
	chapters := filterChapterIDs(issueChapterIDs(it), validChapterMax)
	if chapters == nil {
		chapters = []int64{}
	}
	chaptersJSON, _ := json.Marshal(chapters)
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
		ChapterIDsJSON: string(chaptersJSON),
		Status:         IssueStatusPending,
		Round:          round,
	}
}

// ReplaceIssues 以新一轮体检结果整批替换该 run 的 issue 工单（新体检=新问题清单）。
// round 记录本轮修复轮数，供增量复验分轮与统计。落库前按本书真实章节数裁剪 chapter_ids，
// 剔除台账 ID / 越界规划章号等脏数据（签约体检契约要求 chapter_ids 必须是有效全局阅读序号）。
func (s *Store) ReplaceIssues(ctx context.Context, novelID int64, runID string, round int, issues []AuditIssue) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("novel_id = ? AND run_id = ?", novelID, runID).
			Delete(&PipelineIssue{}).Error; err != nil {
			return err
		}
		// 本书有效章节上界：章节总数（阅读序号连续 1..N）。无章节则 0（不裁剪）。
		// chapters 表可能尚未迁移（测试 / 极早期库），缺失时跳过裁剪以保留既有行为。
		validMax := 0
		if tx.Migrator().HasTable("chapters") {
			var chapterCount int64
			if err := tx.Raw("SELECT COUNT(*) FROM chapters WHERE novel_id = ?", novelID).
				Scan(&chapterCount).Error; err == nil {
				validMax = int(chapterCount)
			}
		}
		for _, it := range issues {
			row := pipelineIssueRow(novelID, runID, round, it, validMax)
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// matchCarryRows 把旧工单按「键+Area 精确 → Area 兜底」两遍匹配给新发现清单，
// 返回新清单每条 ID 对应的旧工单（nil=全新单）。E 系键是按发现顺序的位次键
// （E{章}-{seq}），章节重落后发现集合变化会平移序号，故键匹配必须验 Area
// （同键不同检查项=不同发现，不得继承）；键变了但 Area 相同（同章同检查项位次平移）
// 走 Area 兜底继承——检查项（Area）才是发现的语义身份。旧单每条只消费一次。
func matchCarryRows(old []PipelineIssue, issues []AuditIssue) map[string]*PipelineIssue {
	out := make(map[string]*PipelineIssue, len(issues))
	used := make([]bool, len(old))
	find := func(pred func(PipelineIssue) bool) *PipelineIssue {
		for i := range old {
			if !used[i] && pred(old[i]) {
				used[i] = true
				return &old[i]
			}
		}
		return nil
	}
	for _, it := range issues {
		if out[it.ID] != nil {
			continue
		}
		out[it.ID] = find(func(r PipelineIssue) bool {
			return r.IssueKey == it.ID && r.Area == it.Area
		})
	}
	for _, it := range issues {
		if out[it.ID] != nil {
			continue
		}
		out[it.ID] = find(func(r PipelineIssue) bool { return r.Area == it.Area })
	}
	return out
}

// carryIssueWorkflow 把旧工单的用户流转状态整组继承给换血重落的新行（#100 主修复）：
// 状态/驳回理由/复验回执与次数/人工备注/轮次/进复验前快照/创建时间全保留——
// 写后链路的发现刷新绝不回退用户已推进的工单状态（fixed/verified/dismissed 等
// 不得因「修复下一个问题触发章保存」被冲成待确认）。发现内容字段（level/area/desc/
// 修复路由/作用域）由新行自带（刷新语义）。
func carryIssueWorkflow(row *PipelineIssue, prev *PipelineIssue) {
	row.Status = prev.Status
	row.SkipReason = prev.SkipReason
	row.VerifyReceipt = prev.VerifyReceipt
	row.VerifyAt = prev.VerifyAt
	row.VerifyAttempts = prev.VerifyAttempts
	row.Note = prev.Note
	row.PreVerifyStatus = prev.PreVerifyStatus
	row.Round = prev.Round
	row.CreatedAt = prev.CreatedAt
}

// ReplaceIssuesByPrefix 按 issue key 前缀整批替换该 run 的一类工单（情绪账 E 系
// 按章前缀，见 tension_run.go）：先删同前缀旧行再插新清单，空清单=清该前缀全部旧工单
// （新结果=零发现的回填语义）；跨前缀工单（其它章/其它命名空间）不受影响。
// 换血保留用户流转状态（#100 主修复）：新清单里与旧工单同一发现（键+Area 匹配、
// Area 兜底）的行继承旧工单状态与复验/备注留痕（carryIssueWorkflow）——章保存写后
// 链路反复刷新本章发现时，用户已修/已确认/已驳回的状态不被冲回 pending；
// 真正的新发现才落 pending，消失的发现照删（回填语义不变）。
// round 语义与 ReplaceIssues 同（曲线发现恒 0 轮新单）。prefix 空串显式报错——
// 防误用成整 run 清除（整批替换请走 ReplaceIssues）。
func (s *Store) ReplaceIssuesByPrefix(ctx context.Context, novelID int64, runID, prefix string, round int, issues []AuditIssue) error {
	if prefix == "" {
		return errors.New("ReplaceIssuesByPrefix：prefix 不可为空（整批替换请走 ReplaceIssues）")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old []PipelineIssue
		if err := tx.Where("novel_id = ? AND run_id = ? AND issue_key LIKE ?", novelID, runID, prefix+"%").
			Find(&old).Error; err != nil {
			return err
		}
		carry := matchCarryRows(old, issues)
		if err := tx.Where("novel_id = ? AND run_id = ? AND issue_key LIKE ?", novelID, runID, prefix+"%").
			Delete(&PipelineIssue{}).Error; err != nil {
			return err
		}
		validMax := 0
		if tx.Migrator().HasTable("chapters") {
			var chapterCount int64
			if err := tx.Raw("SELECT COUNT(*) FROM chapters WHERE novel_id = ?", novelID).
				Scan(&chapterCount).Error; err == nil {
				validMax = int(chapterCount)
			}
		}
		for _, it := range issues {
			row := pipelineIssueRow(novelID, runID, round, it, validMax)
			if prev := carry[it.ID]; prev != nil {
				carryIssueWorkflow(&row, prev)
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateIssueStatus 流转一条工单状态（fixed/skipped/rewrite/retcon/...）；skipReason
// 非空时一并记录（skipped 即转人工工单的留痕依据）。流水线内部兼容写不校验
// issueTicketNext 状态机，但终态（verified/dismissed）与复验瞬态（verifying）
// 一律受保护、不可被晚到的修复回执覆盖（#106，条件 CAS 见函数尾注释）。
func (s *Store) UpdateIssueStatus(ctx context.Context, novelID int64, runID, issueKey, status, skipReason string) error {
	updates := map[string]any{"status": status, "updated_at": time.Now()}
	if skipReason != "" {
		updates["skip_reason"] = skipReason
	}
	if status == IssueStatusVerifying {
		// 重新进入复验态：清除此前「启动清扫/中断」残留备注，避免与「复验中」状态并排显示造成
		// 「卡在复验中」的误导错觉（UI 仅 VerifyIssueTickets 会把状态置为 verifying，此处收窄安全）。
		updates["note"] = ""
		// 进复验瞬态前原子快照原状态（pre_verify_status）：复验「未确认/中断」回退时恢复
		// 原状态而非一律 pending（#100——已修复（待复验）的工单不得被一次复验超时打回
		// 「待确认」）。已在 verifying 的行保留原快照（重复复验不覆盖，仍回最初的状态）。
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var row PipelineIssue
			err := tx.Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
				First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // 行不存在：与旧口径一致（条件更新 0 行、无错误）
			}
			if err != nil {
				return err
			}
			// #106：终态（复验通过/驳回）不得被重新拖回复验瞬态。
			if row.Status == IssueStatusVerified || row.Status == IssueStatusDismissed {
				return nil
			}
			if row.Status != IssueStatusVerifying {
				updates["pre_verify_status"] = row.Status
			}
			return tx.Model(&PipelineIssue{}).
				Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
				Updates(updates).Error
		})
	}
	// #106：流水线内部同步（修复回执 fixed/skipped/rewrite/retcon）不得覆盖终态或
	// 复验在途瞬态。章节重写可能耗时数分钟，修复执行体比复验晚收口时，无条件
	// UPDATE 会把已复验通过（verified）的工单打回「已修复（待复验）」——用条件
	// CAS 让晚到的修复回写静默失效，人工复验结论优先。verifying 同理：复验进行中
	// 的工单不接收修复回执，避免收口与回执互相抢写。
	return s.db.WithContext(ctx).Model(&PipelineIssue{}).
		Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
		Where("status NOT IN ?", []string{IssueStatusVerified, IssueStatusDismissed, IssueStatusVerifying}).
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

// verifyRestoreStatus 决定复验「未确认/中断」后的回退状态：优先进复验前快照
// （pre_verify_status，UpdateIssueStatus 置 verifying 时捕获）——已修复（待复验）的
// 工单被一次供应商超时/回执未解析后停在「已修复（待复验）」，可直接再点复验，
// 不打回「待确认」（#100 状态回退）。快照为空（旧数据）/瞬态/修复中等不可直回的
// 状态回退 pending（退回待修，兼容旧口径）。
func verifyRestoreStatus(pre string) string {
	switch pre {
	case IssueStatusFixed, IssueStatusConfirmed, IssueStatusPending, IssueStatusRewrite, IssueStatusSkipped, IssueStatusRetcon:
		return pre
	default:
		return IssueStatusPending
	}
}

// resetVerifyingRows 把满足 scope 的 verifying 态工单逐行复位为 verifyRestoreStatus
// （恢复进复验前快照）并写 note；逐行条件更新（id+status=verifying）防止读改写间隙
// 被其它路径收口的行遭二次改写。
func (s *Store) resetVerifyingRows(ctx context.Context, note string, scope func(*gorm.DB) *gorm.DB) (int64, error) {
	var rows []PipelineIssue
	if err := scope(s.db.WithContext(ctx).Model(&PipelineIssue{})).Find(&rows).Error; err != nil {
		return 0, err
	}
	var n int64
	for _, r := range rows {
		res := s.db.WithContext(ctx).Model(&PipelineIssue{}).
			Where("id = ? AND status = ?", r.ID, IssueStatusVerifying).
			Updates(map[string]any{
				"status":     verifyRestoreStatus(r.PreVerifyStatus),
				"note":       note,
				"updated_at": time.Now(),
			})
		if res.Error != nil {
			return n, res.Error
		}
		n += res.RowsAffected
	}
	return n, nil
}

// ResetStuckVerifying 将本 run 残留的 verifying 态工单收口复位（复验中断/超时残留清扫，
// 与 RecoverInterruptedRuns 同口径：执行体消亡后 DB 状态需收口复位）。按 run 收窄作用域，
// 不误伤其它 run 并行复验中的工单。回退目标走 verifyRestoreStatus（恢复进复验前快照）。
func (s *Store) ResetStuckVerifying(ctx context.Context, novelID int64, runID string) (int64, error) {
	return s.resetVerifyingRows(ctx, "复验执行中断，已复位待复验", func(q *gorm.DB) *gorm.DB {
		return q.Where("novel_id = ? AND run_id = ? AND status = ?", novelID, runID, IssueStatusVerifying)
	})
}

// ResetAllStuckVerifying 启动期（RecoverInterruptedRuns）全局清扫：把所有 novel 残留的
// verifying 态工单一次性复位。复验执行体随进程退出而消亡，若崩溃/重启发生在
// 复验中途，执行体收口逻辑不再运行，这些工单会永久停在 verifying；此处兜底复位，避免
// 永久卡死（issue.go:35 注释承诺的「启动期清扫复位」此前未真正实现，本次补齐）。
// 回退目标走 verifyRestoreStatus（恢复进复验前快照，已修复的不打回待确认）。
func (s *Store) ResetAllStuckVerifying(ctx context.Context) (int64, error) {
	return s.resetVerifyingRows(ctx, "应用重启导致复验中断，已复位待复验", func(q *gorm.DB) *gorm.DB {
		return q.Where("status = ?", IssueStatusVerifying)
	})
}

// ResetAllStuckFixing 启动期（RecoverInterruptedRuns）全局清扫：把所有 novel 残留的
// fixing 态工单一次性复位为 pending 并 note 明示中断。修复执行体随进程退出而消亡
// （任务队列启动清扫 AbandonActive），崩溃/重启发生在修复中途时，残留的 fixing 工单
// 会永久显示「修复中」（2026-11-04 00:14 dev 退出实况）；此处兜底复位，用户点「去修复」
// 即可重新执行（零自动派发契约不变——绝不自动重派修复执行体）。
// fixing→pending 是状态机合法迁移（issueTicketNext，与人工「退回待修」同语义）；人工
// 修复流（巡检卡片去修复→聊天手改）被打断时同样复位——重启后聊天上下文已断，重走
// 「去修复」重新取清理内容比永久卡「修复中」诚实。
func (s *Store) ResetAllStuckFixing(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).
		Model(&PipelineIssue{}).
		Where("status = ?", IssueStatusFixing).
		Updates(map[string]any{"status": IssueStatusPending, "note": "应用重启导致修复中断，已复位待修复（可点「去修复」重新执行）"})
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
// 未通过→pending（退回待修）或达阈值自动升级为人工工单（skipped）；verify_receipt/verify_at
// 逐次留痕。返回 escalated 标识本次未通过是否触发了自动升级。
// 终态工单（verified/dismissed）不接受复验。
//
// 自动升级（防无限空转）：对复验「明确未通过」（rejected）累计 VerifyAttempts；达
// verifyEscalateAfter 阈值即转 skipped（人工工单，契约本就规定修不了报 skipped 转人工、
// 不再重试），并在 skip_reason/note 写明原因，避免工单在「修复→复验失败」间无限循环。
// 未确认（unverified，LLM 失败/回执未解析）不计次数、不改状态，留待下轮。
func (s *Store) RecordIssueVerify(ctx context.Context, novelID int64, runID, issueKey string, ok bool, receipt string) (bool, error) {
	// 通过：→verified 终态，逐次留痕（尝试次数保持原值，不影响终态语义）。
	if ok {
		row, err := s.getIssueRow(ctx, novelID, runID, issueKey)
		if err != nil {
			return false, err
		}
		if row.Status == IssueStatusVerified || row.Status == IssueStatusDismissed {
			return false, fmt.Errorf("%w: %s 是终态", ErrIssueTerminal, row.Status)
		}
		now := time.Now()
		err = s.db.WithContext(ctx).Model(&PipelineIssue{}).
			Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
			Updates(map[string]any{
				"status": IssueStatusVerified, "verify_receipt": receipt, "verify_at": now, "updated_at": now,
			}).Error
		return false, err
	}
	// 未通过：事务内原子递增尝试次数，达阈值自动升级为人工工单（skipped）。
	var escalated bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r PipelineIssue
		if err := tx.Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
			First(&r).Error; err != nil {
			return err
		}
		if r.Status == IssueStatusVerified || r.Status == IssueStatusDismissed {
			return fmt.Errorf("%w: %s 是终态", ErrIssueTerminal, r.Status)
		}
		newAttempts := r.VerifyAttempts + 1
		now := time.Now()
		updates := map[string]any{
			"verify_attempts": newAttempts,
			"verify_receipt":  receipt,
			"verify_at":       now,
			"updated_at":      now,
		}
		if newAttempts >= verifyEscalateAfter {
			// 达阈值：自动转人工工单（契约语义 = 修不了转人工），原因写 skip_reason + note。
			msg := fmt.Sprintf("复验 %d 次未通过，自动转人工工单（请人工裁决：修正修复处方 / 手动复验 / 驳回）", newAttempts)
			updates["status"] = IssueStatusSkipped
			updates["skip_reason"] = msg
			updates["note"] = msg
			escalated = true
		} else {
			updates["status"] = IssueStatusPending
		}
		return tx.Model(&PipelineIssue{}).
			Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
			Updates(updates).Error
	})
	return escalated, err
}

// RecordIssueVerifyUnverified 记录复验「未确认」（LLM 调用失败 / 供应商熔断 / 回执未解析 /
// 回执未含本工单）：回退到进复验前的状态（verifyRestoreStatus——已修复（待复验）的工单
// 保留「已修复（待复验）」可直接再点复验，不被一次供应商超时打回「待确认」，#100），
// 原因写入 note（前端可见），不计 verify_attempts
// （与 rejected 区分，避免供应商抖动把尝试次数吃光而误转人工工单 skipped）。
// 已离开 verifying 瞬态的行只补写 note、不动状态（防把其它路径收口的状态回退成待确认）。
// 注意：此处必须写 note 而非 skip_reason——UpdateIssueStatus 的 skipReason 参数落的是 skip_reason
// 列，而工单面板从不渲染 skip_reason，会导致复验失败原因对用户完全不可见（静默翻回待确认）。
func (s *Store) RecordIssueVerifyUnverified(ctx context.Context, novelID int64, runID, issueKey, note string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row PipelineIssue
		err := tx.Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
			First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // 行已消失（如换血清单删除）：无处可写，与旧口径一致（0 行、无错误）
		}
		if err != nil {
			return err
		}
		now := time.Now()
		if row.Status != IssueStatusVerifying {
			// 已离开复验瞬态（换血继承/其它路径收口）：只补写 note 留痕，绝不回退状态。
			return tx.Model(&PipelineIssue{}).
				Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
				Updates(map[string]any{
					"note":       noteOrDefault(note, "复验未确认（工单已离开复验态，状态未改动）"),
					"updated_at": now,
				}).Error
		}
		return tx.Model(&PipelineIssue{}).
			Where("novel_id = ? AND run_id = ? AND issue_key = ?", novelID, runID, issueKey).
			Updates(map[string]any{
				"status":     verifyRestoreStatus(row.PreVerifyStatus),
				"note":       noteOrDefault(note, "复验未确认，已复位待复验"),
				"updated_at": now,
			}).Error
	})
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
