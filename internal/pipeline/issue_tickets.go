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
// 复验改为后台异步（审计 P0）后，VerifyIssueTickets 立即返回 Queued=已派发工单数，
// 实际逐项结果经 pipeline:verify_progress / pipeline:verify_done 事件流推送。
type IssueVerifySummary struct {
	Confirmed  int    `json:"confirmed"`  // 复验通过（→verified）
	Rejected   int    `json:"rejected"`   // 复验未通过（退回待修）
	Unverified int    `json:"unverified"` // 终态跳过 / 回执未解析未改动 / 未提及留待下轮
	Queued     int    `json:"queued"`     // 已派发后台复验的工单数（异步入口即时回执）
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

	// 收口：修复轮数 +1（工单轮次统计与增量复验分轮，R5-2 起只由工单修复收口递增），
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

// VerifyIssueTickets 是工单面板「复验」按钮的执行入口（§10.3 显式复验）。审计 P0 起改为
// **后台异步、逐工单、作用域化、可取消、有界超时**的复验：
//   - 入口只做校验 + 选票分组 + 置 verifying 态 + 按 run 派发后台执行体，立即返回
//     Queued=已派工单数（不再同步阻塞整段 LLM 调用）；
//   - 实际复验由 runVerifyTickets 逐工单执行：每个工单独立一次 signability-verify 调用，
//     **不注入整书审计上下文**（传空 ContextBlocks，复验本就「定点核对修过的作用域」，
//     上下文由 agent 经工具按需读取），scope 收窄到该工单 chapters + 本轮修复日志；
//   - 逐工单故障隔离：单工单 LLM 失败/回执未解析 → 标未复验（复位 pending）继续，
//     绝不整批失败；回执未解析**不再静默全过**（审计 P2）；
//   - 全程有界 ctx（verifyCallTimeout）+ 可取消（CancelIssueVerify），超时/取消后安全退出；
//   - 进度经 pipeline:verify_progress / pipeline:verify_done 事件流推送前端。
func (s *Service) VerifyIssueTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) (*IssueVerifySummary, error) {
	if s.store == nil {
		return nil, errors.New("issue 工单表未初始化")
	}
	if len(refs) == 0 {
		return nil, errors.New("未选择工单")
	}

	// 载入工单行：终态（verified/dismissed）不接受复验，跳过不计；其余置 verifying 态后按 run 派发。
	seen := map[string]bool{}
	summary := &IssueVerifySummary{}
	runIDs := map[string]bool{}
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
		// 置 verifying 瞬态（执行体按 run 聚合 verifying 态工单；进程崩溃残留由启动期清扫复位）。
		s.syncIssueRow(ctx, novelID, ref.RunID, ref.IssueKey, IssueStatusVerifying, "")
		runIDs[ref.RunID] = true
		summary.Queued++
	}
	if summary.Queued == 0 {
		return summary, nil
	}
	for runID := range runIDs {
		runID := runID
		s.dispatchExecutor(taskKindIssueVerify, runID, novelID, func() {
			s.runVerifyTickets(runID, novelID)
		})
	}
	summary.Detail = fmt.Sprintf("已启动复验 %d 项（后台执行，进度见面板）", summary.Queued)
	return summary, nil
}

// runVerifyTickets 是复验执行体（审计 P0）：处理本 run 全部 verifying 态工单，逐工单定点复验。
// 每个工单一次独立 signability-verify 调用（作用域化、MaxTurns=verifyMaxTurns），故障隔离；
// verified→verified（终态）/ 未通过→pending（退回待修）/ 未确认（LLM 失败或回执未解析）→
// pending（留待下轮），绝不静默全过。整轮受 verifyCallTimeout 有界 ctx 约束，可被 CancelIssueVerify 取消。
func (s *Service) runVerifyTickets(runID string, novelID int64) {
	ctx := s.beginVerifyCtx(runID)
	defer s.endVerifyCtx(runID)

	rows, err := s.store.ListVerifyingTickets(ctx, novelID, runID)
	if err != nil {
		s.logger.Error("复验：加载 verifying 工单失败", "run_id", runID, "err", err)
		return
	}
	if len(rows) == 0 {
		s.logger.Info("复验：本 run 无 verifying 态工单，跳过", "run_id", runID)
		return
	}

	// 修复日志（fix_note）随 run 上下文取：指引复验定点核对改动点（与整批复验同口径）。
	var fixLog []FixLogEntry
	fixNote := map[string]string{}
	if run, rerr := s.store.GetByRunID(ctx, runID); rerr == nil && run.ContextJSON != "" {
		var bc GateContext
		if json.Unmarshal([]byte(run.ContextJSON), &bc) == nil {
			for _, e := range bc.FixLog {
				fixNote[e.ID] = e.Note
				fixLog = append(fixLog, e)
			}
		}
	}
	// 传播检查（依赖闭包定向复验块）按本 run 全部工单作用域算一次（5 表全扫已是最简，逐工单重扫浪费）。
	targets := make([]verifyTarget, 0, len(rows))
	for _, r := range rows {
		targets = append(targets, verifyTarget{
			ID: r.IssueKey, Level: r.Level, Area: r.Area, Desc: r.Desc,
			Chapters: issueChapterIDs(AuditIssue{
				Area: r.Area, Desc: r.Desc, ChapterIDs: decodeIssueChapterIDs(r.ChapterIDsJSON),
			}),
			FixNote: fixNote[r.IssueKey],
		})
	}
	prop := s.propagationCheck(ctx, novelID, targets, fixLog)
	ledgerNotes := ledgerChangeNotes(fixLog)

	summary := &IssueVerifySummary{}
	total := len(rows)
	emitVerifyProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "total": total, "done": 0, "status": "start",
	})
	for i, r := range rows {
		if ctx.Err() != nil {
			break // 超时或被取消：剩余工单保持 verifying，收口时复位 pending。
		}
		verified, rejected, note := s.verifyOneTicket(ctx, novelID, runID, r, ledgerNotes, prop.block())
		switch {
		case verified:
			if err := s.store.RecordIssueVerify(ctx, novelID, runID, r.IssueKey, true, note); err != nil {
				s.logger.Warn("复验：落库 verified 失败（非阻断，留待下轮）", "issue", r.IssueKey, "err", err)
				s.syncIssueRow(ctx, novelID, runID, r.IssueKey, IssueStatusPending, "复验通过落库失败，已复位待复验")
				summary.Unverified++
			} else {
				summary.Confirmed++
			}
		case rejected:
			if err := s.store.RecordIssueVerify(ctx, novelID, runID, r.IssueKey, false, note); err != nil {
				s.logger.Warn("复验：落库未通过失败（非阻断，留待下轮）", "issue", r.IssueKey, "err", err)
				s.syncIssueRow(ctx, novelID, runID, r.IssueKey, IssueStatusPending, "复验未通过落库失败，已复位待复验")
				summary.Unverified++
			} else {
				summary.Rejected++
			}
		default:
			// 未确认（LLM 失败 / 回执未解析 / 回执未提及）：复位 pending 留待下轮，绝不静默判通过（审计 P2）。
			s.syncIssueRow(ctx, novelID, runID, r.IssueKey, IssueStatusPending, noteOrDefault(note, "复验未确认，已复位待复验"))
			summary.Unverified++
		}
		emitVerifyProgress(ctx, map[string]any{
			"novel_id": novelID, "run_id": runID, "total": total, "done": i + 1,
			"last_key": r.IssueKey, "last_ok": verified, "last_rejected": rejected,
			"last_note": note, "status": "progress",
		})
	}
	// 收口：任何残留 verifying 态（超时/取消中断）→ 复位 pending，避免永久卡死。
	if v, lerr := s.store.ResetStuckVerifying(ctx, novelID); lerr == nil && v > 0 {
		s.logger.Warn("复验：收口复位残留 verifying 工单", "run_id", runID, "count", v)
	}

	summary.Detail = fmt.Sprintf("复验完成：确认 %d 项", summary.Confirmed)
	if summary.Rejected > 0 {
		summary.Detail += fmt.Sprintf("，%d 项未通过退回待修", summary.Rejected)
	}
	if summary.Unverified > 0 {
		summary.Detail += fmt.Sprintf("，%d 项未确认留待下轮", summary.Unverified)
	}
	if seg := prop.summary(); seg != "" {
		summary.Detail += "；" + seg
	}
	emitVerifyDone(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID,
		"confirmed": summary.Confirmed, "rejected": summary.Rejected,
		"unverified": summary.Unverified, "detail": summary.Detail,
	})
}

// verifyOneTicket 对单个工单做一次作用域化复验：只注入该工单 chapters + 本轮修复日志
// （不含整书审计上下文），经 signability-verify 定点核验。返回 (verified, rejected, note)：
//   - verified=true  → 复验通过（→verified）
//   - verified=false && rejected=true  → 复验明确未通过（→pending 退回待修）
//   - verified=false && rejected=false → 未确认：LLM 调用失败 / 回执未解析 / 回执未提及本工单
//     （→pending 留待下轮，绝不静默判通过）
func (s *Service) verifyOneTicket(ctx context.Context, novelID int64, runID string, row PipelineIssue, ledgerNotes []string, propBlock string) (verified, rejected bool, note string) {
	out, derr := s.dispatchSkill(ctx, "signability-verify", SkillInput{
		NovelID:   novelID,
		Prompt:    verifyPrompt(novelID, []verifyTarget{{ID: row.IssueKey, Level: row.Level, Area: row.Area, Desc: row.Desc, Chapters: issueChapterIDs(AuditIssue{Area: row.Area, Desc: row.Desc, ChapterIDs: decodeIssueChapterIDs(row.ChapterIDsJSON)}), FixNote: row.Note}}, ledgerNotes, propBlock, contextasm.ContextBlocks{}),
		MaxTurns:  verifyMaxTurns,
		SessionID: "pipeline:issue-tickets:verify:" + uuid.NewString(),
		RunID:     runID,
	})
	if derr != nil {
		// 供应商熔断/超时/网络失败：整工单未确认，留待下轮（不报错中断整批）。
		s.logger.Warn("复验：单工单执行失败，标未确认留待下轮", "run_id", runID, "issue", row.IssueKey, "err", derr)
		return false, false, "复验执行失败（" + derr.Error() + "），已复位待复验"
	}
	results, _, found := parseVerifyReceipt(out.Text)
	if !found {
		// 审计 P2：回执未解析 → 不再静默全过，标未确认留待下轮。
		s.logger.Warn("复验：回执未解析，工单状态未改动（复位 pending 留待下轮）", "run_id", runID, "issue", row.IssueKey)
		return false, false, "复验回执未解析，状态未改动"
	}
	res, ok := results[row.IssueKey]
	if !ok {
		return false, false, "复验回执未包含本工单，留待下轮"
	}
	if res.OK {
		return true, false, res.Note
	}
	return false, true, res.Note
}

// noteOrDefault 返回非空 note，否则回退默认文案（避免空备注）。
func noteOrDefault(note, def string) string {
	if strings.TrimSpace(note) == "" {
		return def
	}
	return note
}

// beginVerifyCtx 从 appCtx 派生「带超时+取消」的复验 ctx 并登记 cancel（审计 P0 可取消）。
func (s *Service) beginVerifyCtx(runID string) context.Context {
	ctx, cancel := context.WithTimeout(s.appCtx, verifyCallTimeout)
	s.verifyCancelFns.Store(verifyKey(runID), cancel)
	return ctx
}

// endVerifyCtx 复验收口：撤销超时/取消派生 ctx + 注销 cancel 登记。
func (s *Service) endVerifyCtx(runID string) {
	v, ok := s.verifyCancelFns.LoadAndDelete(verifyKey(runID))
	if !ok {
		return
	}
	if cancel, ok := v.(context.CancelFunc); ok {
		cancel()
	}
}

func verifyKey(runID string) string { return "verify:" + runID }

// CancelIssueVerify 取消一个 run 的后台复验（审计 P0）：取出 cancel 使本轮复验 ctx 立即
// Done，逐工单循环感知后安全退出（未复验工单复位 pending）；返回 cancelled 标识是否确有
// 在飞复验被取消。未知/无在飞 run 返回 false（合法空操作）。
func (s *Service) CancelIssueVerify(novelID int64, runID string) (bool, error) {
	if s.store == nil {
		return false, errors.New("issue 工单表未初始化")
	}
	v, ok := s.verifyCancelFns.Load(verifyKey(runID))
	if !ok {
		return false, nil
	}
	if cancel, ok := v.(context.CancelFunc); ok {
		cancel()
	}
	EmitVerifyCancelled(s.appCtx, map[string]any{"novel_id": novelID, "run_id": runID})
	return true, nil
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
