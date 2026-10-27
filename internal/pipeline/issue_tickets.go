package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/novelgo/novelgo/internal/contextasm"
)

// ── 发现项工单面板的服务入口（R5-1，提案 §10.2/§10.3/§10.7）──────────────────
// 流水线核心交付物收敛为「质量档案 + 发现项工单」（§10.2）：本文件只做工单流转与
// 显式复验，**不做自动修复派发**——修复动作永远由人显式触发，修复执行改工单被动
// 触发属 R5-2；复验沿用 §4.3-4 增量复验逻辑（signability-verify + verifyPrompt +
// 复验回执），只是不再自动串起来（§10.3）。

// IssueTicketRef 定位一条发现项工单（run 内稳定键，与 ListIssueTickets 返回行配套）。
type IssueTicketRef struct {
	RunID    string `json:"run_id"`
	IssueKey string `json:"issue_key"`
}

// IssueVerifySummary 是一次工单复验的收口计数（面板 toast/汇总展示）。
type IssueVerifySummary struct {
	Confirmed  int    `json:"confirmed"`  // 复验通过（→verified）
	Rejected   int    `json:"rejected"`   // 复验未通过（退回待修）
	Unverified int    `json:"unverified"` // 未列入回执（留待下轮）
	Detail     string `json:"detail"`     // 一行小结
}

// ListIssueTickets 列出一本书的全部发现项工单（工单面板列表，跨 run，新单在前）。
func (s *Service) ListIssueTickets(ctx context.Context, novelID int64) ([]PipelineIssue, error) {
	if s.store == nil {
		return nil, errors.New("issue 工单表未初始化")
	}
	return s.store.ListIssueTicketsByNovel(ctx, novelID)
}

// ConfirmIssueTickets 确认发现项工单（状态机：发现→确认）。
func (s *Service) ConfirmIssueTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) error {
	return s.transitionIssueTickets(ctx, novelID, refs, IssueStatusConfirmed, "")
}

// StartIssueFixTickets 「去修复」：工单进入（人工）修复中（状态机：→修复中），并异步派发
// 修复执行体（#240 R5-2 工单接执行：修复动作由人显式触发，执行体按 run 聚合全部 fixing
// 态工单被动处理，幂等去重——重复点击不双跑）。
func (s *Service) StartIssueFixTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) error {
	if err := s.transitionIssueTickets(ctx, novelID, refs, IssueStatusFixing, ""); err != nil {
		return err
	}
	// 按 run 分组派发（一个 run 一个执行体聚合本 run 全部 fixing 态工单）。队列不可用
	// 时 dispatchExecutor 降级直接执行（与迁移前逐字一致）。
	runIDs := map[string]bool{}
	for _, ref := range refs {
		if ref.RunID != "" {
			runIDs[ref.RunID] = true
		}
	}
	for runID := range runIDs {
		s.dispatchExecutor(taskKindIssueFix, runID, novelID, func() {
			s.runIssueFixTickets(runID, novelID)
		})
	}
	return nil
}

// runIssueFixTickets 是工单「去修复」的执行体（#240，R5-2 工单接执行）：处理本 run 全部
// 处于 fixing 态的发现项工单，按问题类型分流到既有修复执行能力（台账级 signability-fix /
// blueprint-fix、正文级 chapter-rewrite），不派发任何自动回路、不自动回复评。V 系文风漂移
// 与冻结卷写保护项绝不派发，退回原状态留 note（只报告不改写 / 人工裁决）。
// 幂等重入：已不在 fixing 态的工单直接跳过；重复派发由队列 kind:run_id 去重合并。
func (s *Service) runIssueFixTickets(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil || run == nil {
		s.logger.Error("工单修复：加载流水线失败", "run_id", runID, "err", err)
		return
	}

	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}

	rows, err := s.store.ListFixingTickets(ctx, novelID, runID)
	if err != nil {
		s.logger.Error("工单修复：加载 fixing 工单失败", "run_id", runID, "err", err)
		return
	}
	if len(rows) == 0 {
		s.logger.Info("工单修复：本 run 无 fixing 态工单，跳过", "run_id", runID)
		return
	}

	// 显式修复执行开始（R5-2：由用户「去修复」触发，替代旧 S5 自动派发修复的 emitFixStart
	// 信号，供前端展示修复进行态）。
	keys := make([]string, 0, len(rows))
	for _, r := range rows {
		keys = append(keys, r.IssueKey)
	}
	emitFixStart(ctx, map[string]any{
		"run_id":       runID,
		"novel_id":     novelID,
		"ticket_count": len(rows),
		"ticket_keys":  keys,
	})

	// 分流：V 系 / 写保护 → 退回原状态（不派发）；body → 章节重写；其余 → 台账级修复。
	var bodyItems []RewriteItem
	var ledgerIssues []AuditIssue
	bounced := 0
	for _, r := range rows {
		switch {
		case isStyleExempt(r.IssueKey):
			// V 系只报告不改写：退回 rewrite 态，作者润色或驳回（绝不派发重写）。
			s.syncIssueRow(ctx, novelID, runID, r.IssueKey, IssueStatusRewrite,
				"V 系文风漂移只报告不改写：已退回待作者在写作页润色或驳回")
			bounced++
		case writeProtected(r.TargetScope, r.Direction):
			// 冻结卷写保护：退回 retcon 态，经 retcon 审批闸门人工裁决（绝不派发修复）。
			s.syncIssueRow(ctx, novelID, runID, r.IssueKey, IssueStatusRetcon,
				"命中冻结卷写保护：已退回 retcon 候选，经 retcon 审批闸门人工裁决")
			bounced++
		case r.FixType == FixTypeBody:
			bodyItems = append(bodyItems, RewriteItem{
				ID: r.IssueKey, Level: r.Level, Area: r.Area, Desc: r.Desc,
				Chapters: decodeIssueChapterIDs(r.ChapterIDsJSON),
			})
		default:
			ledgerIssues = append(ledgerIssues, AuditIssue{
				ID: r.IssueKey, Level: r.Level, Area: r.Area, Desc: r.Desc,
				FixType: r.FixType, TargetScope: r.TargetScope, Dimension: r.Dimension,
				Direction: r.Direction, ChapterIDs: decodeIssueChapterIDs(r.ChapterIDsJSON),
			})
		}
	}

	// 台账级修复（串行派发，FixLog 共享，台账变更入账）+ 正文级重定向重写一章一章。
	fixedLedger, skippedLedger := s.runLedgerFixTickets(ctx, runID, novelID, &ctxt, ledgerIssues)
	fixedBody, skippedBody := s.runBodyRewriteTickets(ctx, runID, novelID, bodyItems)

	// 收口：修复轮数 +1（供 auto_run 上限与工单轮次统计，R5-2 起只由工单修复收口递增），
	// 蓝图漂移巡检（G3，非阻断）结论写入 DriftReport 持久化，广播修复完成。
	ctxt.FixRounds++
	patrol := s.runBlueprintDriftPatrol(ctx, runID, novelID)
	if patrol != "" {
		ctxt.DriftReport = patrol
	}
	if updated, merr := json.Marshal(ctxt); merr == nil {
		if serr := s.store.SetStageStatus(ctx, runID, run.CurrentStage, run.Status, string(updated)); serr != nil {
			s.logger.Warn("工单修复：落库 FixRounds/DriftReport 失败（非阻断）", "err", serr)
		}
	}
	dispatched := fixedLedger + skippedLedger + fixedBody + skippedBody
	emitFixDone(ctx, map[string]any{
		"run_id":        runID,
		"novel_id":      novelID,
		"ok":            true,
		"tickets":       true,
		"fixed_count":   fixedLedger + fixedBody,
		"bounced_count": bounced,
		"error": fmt.Sprintf("工单修复执行完毕：%d 项派发修复（已修复 %d 项 / 跳过 %d 项），%d 项退回（V 系/写保护）；"+
			"请在工单面板查看回执，可复验/驳回/重试。%s",
			dispatched, fixedLedger+fixedBody, skippedLedger+skippedBody, bounced, patrol),
	})
}

// runLedgerFixTickets 执行本 run 台账级工单的修复（#240）：作用域分组（planFixGroups）→
// 组内小批（planFixBatches）→ 不相交分组并行派发 signability-fix（蓝图源走 blueprint-fix），
// 修复日志共享（FixLog），台账变更入账（ledgerChangeWatermark 起点 + appendLedgerChangeLog）。
// 并行语义（R1b，F2 根治）：不相交分组各起 goroutine 并发派发，写冲突天然规避、互不拖慢；
// 组内小批串行（修复日志与 run 级计数由 mu 串行化）。每批回执逐条落工单 fixed/skipped，
// 并记 §9.4 命中率度量（confirmed/dismissed）。返回已修复数/已跳过数。
func (s *Service) runLedgerFixTickets(ctx context.Context, runID string, novelID int64, ctxt *GateContext, issues []AuditIssue) (int, int) {
	if len(issues) == 0 {
		return 0, 0
	}
	blocks := s.buildBlocks(ctx, novelID, contextasm.PurposeFix)
	// 摘要从本 run 审计结果 JSON 解析（蓝图源取 BlueprintAudit），与 executeFix 同口径，
	// 注入修复提示词作背景（无 Summary 字段，宁缺勿错降级为空串）。
	auditJSON := ctxt.Audit
	if ctxt.AuditKind == "blueprint" {
		auditJSON = ctxt.BlueprintAudit
	}
	summary := parseAudit(auditJSON).Summary
	groups := planFixGroups(issues)
	// 台账变更水位取 sweep 起点（修复前最大履历 id）：本轮修复动过的台账履历全部
	// id > sweepWatermark，收口时一次性入账（R2b-7，与 HEAD 同口径——用刷新后的末批
	// 水位会漏记末批自身写入的履历，导致复验缺下游影响核对块）。
	sweepWatermark := s.ledgerChangeWatermark(ctx, novelID)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		fixed   int
		skipped int
	)
	for _, group := range groups {
		group := group
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, batch := range planFixBatches(group) {
				// 提示词构建读取修复日志——先快照再释放锁，避免 dispatchSkill 被锁串行化
				// （并行派发依赖 dispatchSkill 不被锁挡住）。
				mu.Lock()
				logSnapshot := append([]FixLogEntry(nil), ctxt.FixLog...)
				mu.Unlock()

				var (
					prompt string
					skill  = "signability-fix"
				)
				if ctxt.AuditKind == "blueprint" {
					skill = "blueprint-fix"
					prompt = blueprintFixPrompt(novelID, batch, summary, logSnapshot)
				} else {
					prompt = fixPrompt(novelID, batch, summary, logSnapshot, blocks)
				}
				out, derr := s.dispatchSkill(ctx, skill, SkillInput{
					NovelID:   novelID,
					Prompt:    prompt,
					MaxTurns:  12,
					SessionID: "pipeline:" + runID + ":issue-fix:" + uuid.NewString(),
					RunID:     runID,
				})
				if derr != nil {
					s.logger.Warn("工单修复：台账级批次派发失败，整批标记 skipped", "run_id", runID, "err", derr)
					mu.Lock()
					for _, it := range batch {
						s.syncIssueRow(ctx, novelID, runID, it.ID, IssueStatusSkipped, "修复派发失败："+derr.Error())
						skipped++
					}
					mu.Unlock()
					continue
				}
				// 回执解析（applyFixReceipt 同步 FixStates/ManualBacklog/FixLog）+ 落库 +
				// 度量，全部在锁内串行化（跨组并发安全，与 HEAD 同口径）。
				mu.Lock()
				fixedNow, skipReasons := applyFixReceipt(ctxt, batch, out.Text, s.logger)
				for _, id := range fixedNow {
					s.syncIssueRow(ctx, novelID, runID, id, IssueStatusFixed, "")
					fixed++
				}
				for id, reason := range skipReasons {
					s.syncIssueRow(ctx, novelID, runID, id, IssueStatusSkipped, reason)
					skipped++
				}
				// §9.4 命中率度量：修复回执流转=告警确认/未确认（body 级走重写回路不经
				// 修复回执，不计入——见 alert_metrics.go 单源注释）。
				s.recordFeatureUsageN(ctx, featureAlertConfirmedL2, len(fixedNow))
				s.recordFeatureUsageN(ctx, featureAlertDismissedL2, len(skipReasons))
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// 收口：本轮修复动过的台账履历增量入账（id > sweepWatermark，keep-first 幂等）。
	s.appendLedgerChangeLog(ctx, novelID, ctxt, sweepWatermark)
	return fixed, skipped
}

// runBodyRewriteTickets 执行本 run 正文级工单的定向章节重写（#240）：resolveRewriteChapters
// 定位真实章节，对每章派发 chapter-rewrite（整章重写）；成功→fixed，失败→skipped 留原因。
// 返回已修复数/已跳过数。
func (s *Service) runBodyRewriteTickets(ctx context.Context, runID string, novelID int64, items []RewriteItem) (int, int) {
	if len(items) == 0 {
		return 0, 0
	}
	resolved, rerr := s.resolveRewriteChapters(ctx, novelID, items)
	if rerr != nil {
		s.logger.Warn("工单修复：正文级章节定位失败，整批标记 skipped", "run_id", runID, "err", rerr)
		for _, it := range items {
			s.syncIssueRow(ctx, novelID, runID, it.ID, IssueStatusSkipped, "章节定位失败："+rerr.Error())
		}
		return 0, len(items)
	}
	fixed, skipped := 0, 0
	for _, it := range items {
		chapters := resolved[it.ID]
		if len(chapters) == 0 {
			s.syncIssueRow(ctx, novelID, runID, it.ID, IssueStatusSkipped, "未定位到涉及章节，无法重写")
			skipped++
			continue
		}
		ok := true
		for _, ch := range chapters {
			if rerr := s.executeChapterRewrite(ctx, runID, novelID, it, ch); rerr != nil {
				s.logger.Warn("工单修复：章节重写失败", "run_id", runID, "issue", it.ID, "chapter_id", ch.ID, "err", rerr)
				s.syncIssueRow(ctx, novelID, runID, it.ID, IssueStatusSkipped, "章节重写失败："+rerr.Error())
				ok = false
				break
			}
		}
		if ok {
			s.syncIssueRow(ctx, novelID, runID, it.ID, IssueStatusFixed, "")
			fixed++
		} else {
			skipped++
		}
	}
	return fixed, skipped
}

// DismissIssueTickets 「驳回」：工单进终态 dismissed。必须给出理由（写 note 列留痕，
// §10.1「每步可视可审计」）；非终态非法迁移显式报错。
func (s *Service) DismissIssueTickets(ctx context.Context, novelID int64, refs []IssueTicketRef, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("驳回必须给出理由（将留痕到工单备注）")
	}
	return s.transitionIssueTickets(ctx, novelID, refs, IssueStatusDismissed, reason)
}

// transitionIssueTickets 按状态机批量流转（逐条独立，失败逐条报错不吞；空 refs 零操作）。
func (s *Service) transitionIssueTickets(ctx context.Context, novelID int64, refs []IssueTicketRef, to, note string) error {
	if s.store == nil {
		return errors.New("issue 工单表未初始化")
	}
	if len(refs) == 0 {
		return errors.New("未选择工单")
	}
	var errs []error
	for _, ref := range refs {
		if err := s.store.TransitionIssueTicket(ctx, novelID, ref.RunID, ref.IssueKey, to, note); err != nil {
			errs = append(errs, fmt.Errorf("工单 %s: %w", ref.IssueKey, err))
		}
	}
	return errors.Join(errs...)
}

// VerifyIssueTickets 是工单面板「复验」按钮的执行入口（§10.3 显式复验）：对选中工单按
// 增量复验逻辑（signability-verify + verifyPrompt）定点核验，复验回执逐条落工单
// （verify_receipt/verify_at），通过→verified、未通过→退回待修（pending）。
// 只记结论不改 verdict、不触发自动修复回流（检测核心）；终态工单跳过不验。
func (s *Service) VerifyIssueTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) (*IssueVerifySummary, error) {
	if s.store == nil {
		return nil, errors.New("issue 工单表未初始化")
	}
	if len(refs) == 0 {
		return nil, errors.New("未选择工单")
	}

	// 载入工单行：终态（verified/dismissed）不接受复验，跳过不计。
	type targetRow struct {
		row PipelineIssue
	}
	targets := make([]verifyTarget, 0, len(refs))
	rows := make([]targetRow, 0, len(refs))
	seen := map[string]bool{}
	fixLog := []FixLogEntry{}
	fixNote := map[string]string{}
	summary := &IssueVerifySummary{}
	for _, ref := range refs {
		key := ref.RunID + "\x00" + ref.IssueKey
		if seen[key] {
			continue
		}
		seen[key] = true
		row, err := s.store.getIssueRow(ctx, novelID, ref.RunID, ref.IssueKey)
		if err != nil {
			if errors.Is(err, ErrIssueNotFound) {
				return nil, fmt.Errorf("工单 %s: %w", ref.IssueKey, err)
			}
			return nil, err
		}
		if row.Status == IssueStatusVerified || row.Status == IssueStatusDismissed {
			summary.Unverified++ // 终态跳过
			continue
		}
		rows = append(rows, targetRow{row: *row})
		targets = append(targets, verifyTarget{
			ID: row.IssueKey, Level: row.Level, Area: row.Area, Desc: row.Desc,
			Chapters: issueChapterIDs(AuditIssue{
				Area: row.Area, Desc: row.Desc,
				ChapterIDs: decodeIssueChapterIDs(row.ChapterIDsJSON),
			}),
		})
		// 修复日志摘要（fix_note）随 run 上下文取：指引复验定点核对改动点。
		if run, rerr := s.store.GetByRunID(ctx, ref.RunID); rerr == nil && run.ContextJSON != "" {
			var bc GateContext
			if json.Unmarshal([]byte(run.ContextJSON), &bc) == nil {
				for _, e := range bc.FixLog {
					fixNote[e.ID] = e.Note
					fixLog = append(fixLog, e)
				}
			}
		}
	}
	if len(targets) == 0 {
		return summary, nil
	}
	for i := range targets {
		targets[i].FixNote = fixNote[targets[i].ID]
	}

	// 复验沿用 §4.3-4 增量复验逻辑（提示词/技能/回执契约单源）：台账变更核对与
	// 修复传播检查同套输入（聚合自选中工单所属 run 的修复日志）。
	ledgerNotes := ledgerChangeNotes(fixLog)
	prop := s.propagationCheck(ctx, novelID, targets, fixLog)
	// R0 观测归属：选中工单同属一个 run 时记账到该 run；跨 run 选择留空（合法空值）。
	obsRun := ""
	if len(rows) > 0 {
		obsRun = rows[0].row.RunID
		for _, tr := range rows[1:] {
			if tr.row.RunID != obsRun {
				obsRun = ""
				break
			}
		}
	}
	out, verr := s.dispatchSkill(ctx, "signability-verify", SkillInput{
		NovelID:   novelID,
		Prompt:    verifyPrompt(novelID, targets, ledgerNotes, prop.block(), s.buildBlocks(ctx, novelID, contextasm.PurposeAudit)),
		MaxTurns:  12,
		SessionID: "pipeline:issue-tickets:verify:" + uuid.NewString(),
		RunID:     obsRun,
	})
	if verr != nil {
		return nil, fmt.Errorf("复验执行失败：%w", verr)
	}
	results, verifySum, found := parseVerifyReceipt(out.Text)

	var errs []error
	for _, tr := range rows {
		if !found {
			// 兼容回退（与显式复验 VerifyIssueTickets 同口径）：无回执按已修复确认处理，不卡死闭环。
			if err := s.store.RecordIssueVerify(ctx, novelID, tr.row.RunID, tr.row.IssueKey, true, ""); err != nil {
				errs = append(errs, err)
			} else {
				summary.Confirmed++
			}
			continue
		}
		res, ok := results[tr.row.IssueKey]
		switch {
		case !ok:
			summary.Unverified++ // 未提及：留待下轮
		case res.OK:
			if err := s.store.RecordIssueVerify(ctx, novelID, tr.row.RunID, tr.row.IssueKey, true, res.Note); err != nil {
				errs = append(errs, err)
			} else {
				summary.Confirmed++
			}
		default:
			if err := s.store.RecordIssueVerify(ctx, novelID, tr.row.RunID, tr.row.IssueKey, false, res.Note); err != nil {
				errs = append(errs, err)
			} else {
				summary.Rejected++
			}
		}
	}

	summary.Detail = fmt.Sprintf("复验：确认修复 %d 项", summary.Confirmed)
	if summary.Rejected > 0 {
		summary.Detail += fmt.Sprintf("，%d 项未通过已退回待修", summary.Rejected)
	}
	if summary.Unverified > 0 {
		summary.Detail += fmt.Sprintf("，%d 项未复验留待下轮", summary.Unverified)
	}
	if verifySum != "" {
		summary.Detail += "；复验小结：" + verifySum
	}
	if seg := prop.summary(); seg != "" {
		summary.Detail += "；" + seg
	}
	return summary, errors.Join(errs...)
}

// decodeIssueChapterIDs 解析工单行的章节作用域列（JSON 数组）；解析失败返回空。
func decodeIssueChapterIDs(raw string) []int64 {
	if raw == "" {
		return nil
	}
	var out []int64
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// rewriteBacklogOf 是 RewriteBacklog 的工单表投影（R5-1「读写改走工单」）：正文级/V 系
// 工单行 → RewriteItem（写保护行不进清单，与 bodyIssuesOf 同语义）。两种情形回退
// fallback（非阻断：JSON 副本只是契约兼容投影，工单表才是事实源）：
//   - 取数失败（缺表等）；
//   - 工单表对本 run 零行（旧版 run、或体检结果未经 ReplaceIssues 落表的兼容路径）：
//     无事实源可投影，空投影不得覆盖内存清单里已路由的正文级问题。
//
// 表内有行则以投影为准——工单面板的驳回/复验收口（终态行）即时从清单消失。
func (s *Service) rewriteBacklogOf(ctx context.Context, novelID int64, runID string, fallback []RewriteItem) []RewriteItem {
	rows, err := s.store.ListRewriteTickets(ctx, novelID, runID)
	if err != nil {
		s.logger.Warn("RewriteBacklog 工单投影失败，回退内存清单（非阻断）",
			"run_id", runID, "err", err)
		return fallback
	}
	if len(rows) == 0 {
		// 区分「有行但全部收口/非重写」（投影为空=正确）与「压根无行」（无事实源→回退）。
		if all, aerr := s.store.ListIssues(ctx, novelID, runID); aerr != nil {
			s.logger.Warn("RewriteBacklog 工单行数核查失败，回退内存清单（非阻断）",
				"run_id", runID, "err", aerr)
			return fallback
		} else if len(all) == 0 {
			return fallback
		}
	}
	out := make([]RewriteItem, 0, len(rows))
	for _, r := range rows {
		if writeProtected(r.TargetScope, r.Direction) {
			continue
		}
		out = append(out, rewriteItemOfRow(r))
	}
	return out
}
