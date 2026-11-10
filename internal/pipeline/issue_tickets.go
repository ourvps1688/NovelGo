package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/novelgo/novelgo/internal/contextasm"
)

// ── 发现项工单面板的服务入口（R5-1，提案 §10.2/§10.3/§10.7）──────────────────
// 流水线核心交付物收敛为「质量档案 + 发现项工单」（§10.2）：本文件只做工单流转与
// 显式复验，**不做任何自动修复派发**（#270 后续收口：后台 LLM 修复执行体已整体下线）——
// 修复一律由「去修复」把上下文发到 chat 人工/AI 协作完成，再点「标记已修复」收口；
// 复验沿用 §4.3-4 增量复验逻辑（signability-verify + verifyPrompt + 复验回执），
// 只是不再自动串起来（§10.3）。

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
	Escalated  int    `json:"escalated"`  // 复验多次未过自动转人工工单数（skipped）
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

// StartManualIssueFixTickets 是工单面板「去修复」的人工路径（#270 双轨拆分）：
// 把选中工单置「修复中」，前端同时把问题上下文发到 chat 交给人工/AI 协作修复。
// **不派发任何后台自动修复执行体，也不占用互斥窗口**——chat 里修完点
// 「标记已修复」收口后即可复验。后台 LLM 修复链路已整体下线，本方法是修复的唯一起点。
func (s *Service) StartManualIssueFixTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) error {
	if s.store == nil {
		return errors.New("issue 工单表未初始化")
	}
	if len(refs) == 0 {
		return errors.New("未选择工单")
	}
	var errs []error
	for _, ref := range refs {
		row, err := s.store.getIssueRow(ctx, novelID, ref.RunID, ref.IssueKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("工单 %s: %w", ref.IssueKey, err))
			continue
		}
		if row.Status == IssueStatusFixing {
			continue // 幂等：重复点「去修复」不报错
		}
		if err := s.store.TransitionIssueTicket(ctx, novelID, ref.RunID, ref.IssueKey, IssueStatusFixing, ""); err != nil {
			errs = append(errs, fmt.Errorf("工单 %s: %w", ref.IssueKey, err))
		}
	}
	return errors.Join(errs...)
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

// CloseIssueFixTickets 「标记已修复」：chat 人工修复完成后的显式收口（fixing → fixed）。
// fixed 是「已修复待复验」态，走状态机校验并留 note，不碰 verified/dismissed 终态
// （#106 保护）。后台自动修复执行体已下线，不再有晚到回执覆盖人工结论的窗口。
func (s *Service) CloseIssueFixTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) error {
	return s.closeManualFix(ctx, novelID, refs, IssueStatusFixed,
		"人工修复完成，显式收口（待复验）")
}

// CancelIssueFixTickets 「取消修复」：放弃这次人工修复，工单退回待确认
// （fixing → pending，状态机已有出边）。
func (s *Service) CancelIssueFixTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) error {
	return s.closeManualFix(ctx, novelID, refs, IssueStatusPending,
		"取消修复，退回待确认")
}

func (s *Service) closeManualFix(ctx context.Context, novelID int64, refs []IssueTicketRef, to, note string) error {
	if s.store == nil {
		return errors.New("issue 工单表未初始化")
	}
	if len(refs) == 0 {
		return errors.New("未选择工单")
	}
	var errs []error
	for _, ref := range refs {
		row, err := s.store.getIssueRow(ctx, novelID, ref.RunID, ref.IssueKey)
		if err != nil {
			errs = append(errs, fmt.Errorf("工单 %s: %w", ref.IssueKey, err))
			continue
		}
		if row.Status == to {
			continue // 幂等
		}
		if row.Status != IssueStatusFixing {
			errs = append(errs, fmt.Errorf("工单 %s 当前为「%s」，只有「修复中」可收口",
				ref.IssueKey, row.Status))
			continue
		}
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
//   - 实际复验由 runVerifyTickets 两段式执行（L0 快路径 + 批量合验，2026-11-05 终极提速）：
//     每批一次 signability-verify 调用（verifyBatchSize 合验 + 多目标回执逐 id 落库），
//     **不注入整书审计上下文**（传空 ContextBlocks，复验本就「定点核对修过的作用域」，
//     作用域证据由 verify_corpus 预取内联、禁止工具往返），scope 收窄到该批 chapters + 本轮修复日志；
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
	// 同时收集每个 run 本次要复验的工单键（runKeys），显式透传给执行体，避免执行体回扫
	// verifying 态的读己写竞态（该竞态曾导致执行体读到 0 行而提前返回，工单永久卡在 verifying）。
	// #106：复验必须针对定稿正文。后台自动修复执行体已下线，正文只由 chat 人工改写，
	// 不再存在「执行体在途 + 晚到回执抢写状态」的窗口；终态保护仍由条件 CAS 兜底。
	seen := map[string]bool{}
	summary := &IssueVerifySummary{}
	runIDs := map[string]bool{}
	runKeys := map[string][]string{}
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
		runKeys[ref.RunID] = append(runKeys[ref.RunID], ref.IssueKey)
		summary.Queued++
	}
	if summary.Queued == 0 {
		return summary, nil
	}
	for runID := range runIDs {
		runID := runID
		keys := runKeys[runID]
		s.dispatchExecutor(taskKindIssueVerify, runID, novelID, func() {
			s.runVerifyTickets(runID, novelID, keys)
		}, keys...)
	}
	summary.Detail = fmt.Sprintf("已启动复验 %d 项（后台执行，进度见面板）", summary.Queued)
	return summary, nil
}

// runVerifyTickets 是复验执行体（审计 P0；2026-11-05 终极提速重构）：处理本 run 的复验工单。
// 两段式执行：
//   - 阶段 1 L0 快路径（串行）：机械类工单确定性判定（tryL0Verify），命中免 LLM 直接落结论；
//   - 阶段 2 批量合验（批间有限并发）：余下工单按 verifyBatchSize 分批，每批一次
//     signability-verify 调用——证据由 verify_corpus 预取内联（禁止工具往返），
//     一次推理出多目标复验回执，按 id 逐条落库。调用次数 N→⌈N/verifyBatchSize⌉、
//     单次轮数 12→3，整体提速一个量级（根治「复验慢」：取数与推理分离 + 合验）。
//
// 语义不变量：verified→verified（终态）/ 未通过→pending（退回待修）/ 未确认（LLM 失败
// 或回执未解析/未提及）→ pending（留待下轮），绝不静默全过。整轮受 verifyCallTimeout
// 有界 ctx 约束，可被 CancelIssueVerify 取消。
//
// keys 为显式工单键（由 VerifyIssueTickets 透传）：优先按 key 精确加载，彻底消除「执行体回扫
// verifying 态」的读己写竞态（竞态曾让执行体读到 0 行而提前返回，工单永久卡在 verifying）。
// keys 为空（历史任务/降级路径）时回落按 run 扫描 verifying 态工单的旧口径，保持向后兼容。
func (s *Service) runVerifyTickets(runID string, novelID int64, keys []string) {
	ctx := s.beginVerifyCtx(runID)
	defer s.endVerifyCtx(runID)

	var rows []PipelineIssue
	if len(keys) > 0 {
		// 确定性复验：仅处理显式传入的工单键，不再依赖 verifying 态扫描。
		for _, issueKey := range keys {
			row, rerr := s.store.getIssueRow(ctx, novelID, runID, issueKey)
			if rerr != nil {
				if errors.Is(rerr, ErrIssueNotFound) {
					s.logger.Warn("复验：工单键不存在，跳过", "run_id", runID, "issue", issueKey)
					continue
				}
				s.logger.Error("复验：加载工单失败", "run_id", runID, "issue", issueKey, "err", rerr)
				continue
			}
			// 终态（verified/dismissed）已被其他路径收口，跳过不复验。
			if row.Status == IssueStatusVerified || row.Status == IssueStatusDismissed {
				continue
			}
			rows = append(rows, *row)
		}
	} else {
		var err error
		rows, err = s.store.ListVerifyingTickets(ctx, novelID, runID)
		if err != nil {
			s.logger.Error("复验：加载 verifying 工单失败", "run_id", runID, "err", err)
			return
		}
		if len(rows) == 0 {
			s.logger.Info("复验：本 run 无 verifying 态工单，跳过", "run_id", runID)
			return
		}
	}
	if len(rows) == 0 {
		s.logger.Info("复验：无待复验工单（显式键均终态/不存在），跳过", "run_id", runID)
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

	// ── 阶段 1：L0 确定性快路径（串行，本地判定，免 LLM）──
	// 机械类工单（字数/章尾钩子/AI味/称呼/死人/战力/文风）由 quality.RunL0Checks 确定性
	// 判定修复状态，直接落结论；未命中的进阶段 2 合验。tryL0Verify 内含台账装配（DB 读），
	// 置于并行段之前串行完成，保证阶段 2 的 worker 零 DB 读写（落库收敛在下方收集循环）。
	done := 0
	needLLM := make([]PipelineIssue, 0, len(rows))
	for _, r := range rows {
		if ctx.Err() != nil {
			break // 超时或被取消：剩余工单保持 verifying，收口时复位 pending。
		}
		if decided, verified, note := s.tryL0Verify(ctx, novelID, runID, r, s.l0Reader); decided {
			done++
			s.applyVerifyOutcome(ctx, summary, novelID, runID, r,
				verifyOutcome{verified: verified, rejected: !verified, note: note}, total, done)
			continue
		}
		needLLM = append(needLLM, r)
	}

	// ── 阶段 2：批量合验（语料内联 + 批间有限并发，终极提速）──
	// 每批一次 signability-verify 调用（证据内联、多目标回执），调用次数 ⌈N/verifyBatchSize⌉；
	// 批间 verifyBatchParallel 并发摊薄墙钟，落库统一在收集循环单 goroutine 串行写
	// （避开 SQLite 并发写锁竞争），计数/进度事件随落库推进。
	src := s.loadVerifyCorpusSource(ctx, novelID)
	batches := chunkIssues(needLLM, verifyBatchSize)
	type batchResult struct {
		idx      int
		outcomes map[string]verifyOutcome
	}
	resCh := make(chan batchResult, len(batches))
	sem := make(chan struct{}, verifyBatchParallel)
	for bi, batch := range batches {
		go func(bi int, batch []PipelineIssue) {
			sem <- struct{}{}
			defer func() { <-sem }()
			if ctx.Err() != nil {
				resCh <- batchResult{idx: bi} // 超时/取消：本批留 verifying，收口复位
				return
			}
			resCh <- batchResult{idx: bi, outcomes: s.verifyBatch(ctx, novelID, runID, batch, ledgerNotes, prop.block(), src)}
		}(bi, batch)
	}
	for range batches {
		br := <-resCh
		if br.outcomes == nil {
			continue
		}
		for _, r := range batches[br.idx] {
			oc, ok := br.outcomes[r.IssueKey]
			if !ok {
				oc = verifyOutcome{note: "复验回执未包含本工单，留待下轮"}
			}
			done++
			s.applyVerifyOutcome(ctx, summary, novelID, runID, r, oc, total, done)
		}
	}
	// 收口：任何残留 verifying 态（超时/取消中断）→ 复位（恢复进复验前快照，见
	// verifyRestoreStatus），按 run 收窄避免误伤其它 run 并行复验，避免永久卡死。
	if v, lerr := s.store.ResetStuckVerifying(ctx, novelID, runID); lerr == nil && v > 0 {
		s.logger.Warn("复验：收口复位残留 verifying 工单", "run_id", runID, "count", v)
	}

	summary.Detail = fmt.Sprintf("复验完成：确认 %d 项", summary.Confirmed)
	if summary.Rejected > 0 {
		summary.Detail += fmt.Sprintf("，%d 项未通过退回待修", summary.Rejected)
	}
	if summary.Unverified > 0 {
		summary.Detail += fmt.Sprintf("，%d 项未确认留待下轮", summary.Unverified)
	}
	if summary.Escalated > 0 {
		summary.Detail += fmt.Sprintf("，%d 项复验多次未过自动转人工工单", summary.Escalated)
	}
	if seg := prop.summary(); seg != "" {
		summary.Detail += "；" + seg
	}
	emitVerifyDone(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID,
		"confirmed": summary.Confirmed, "rejected": summary.Rejected,
		"unverified": summary.Unverified, "escalated": summary.Escalated, "detail": summary.Detail,
	})
}

// verifyOutcome 是单张工单的复验结论（L0 确定性判定与 LLM 批量回执统一投影）：
//   - verified=true  → 复验通过（→verified）
//   - verified=false && rejected=true  → 复验明确未通过（→pending 退回待修）
//   - verified=false && rejected=false → 未确认：LLM 调用失败 / 回执未解析 / 回执未提及本工单
//     （→pending 留待下轮，绝不静默判通过）
type verifyOutcome struct {
	verified bool
	rejected bool
	note     string
}

// applyVerifyOutcome 落库单张工单的复验结论并推进计数/进度（L0 与 LLM 批量共用同一收口）。
// 语义：verified→verified；rejected→pending 计 verify_attempts（3 次自动转人工）；
// 未确认→恢复进复验前快照（verifyRestoreStatus，已修复的保留「已修复（待复验）」不被打回
// 待确认，#100）留待下轮且不计 attempts（防供应商抖动误转人工）。
func (s *Service) applyVerifyOutcome(ctx context.Context, summary *IssueVerifySummary, novelID int64, runID string, r PipelineIssue, oc verifyOutcome, total, done int) {
	switch {
	case oc.verified:
		if _, err := s.store.RecordIssueVerify(ctx, novelID, runID, r.IssueKey, true, oc.note); err != nil {
			s.logger.Warn("复验：落库 verified 失败（非阻断，留待下轮）", "issue", r.IssueKey, "err", err)
			// 回退走 RecordIssueVerifyUnverified：恢复进复验前快照（不打回待确认）+ note 可见。
			_ = s.store.RecordIssueVerifyUnverified(ctx, novelID, runID, r.IssueKey, "复验通过落库失败，已复位待复验")
			summary.Unverified++
		} else {
			summary.Confirmed++
		}
	case oc.rejected:
		escalated, err := s.store.RecordIssueVerify(ctx, novelID, runID, r.IssueKey, false, oc.note)
		if err != nil {
			s.logger.Warn("复验：落库未通过失败（非阻断，留待下轮）", "issue", r.IssueKey, "err", err)
			_ = s.store.RecordIssueVerifyUnverified(ctx, novelID, runID, r.IssueKey, "复验未通过落库失败，已复位待复验")
			summary.Unverified++
		} else {
			summary.Rejected++
			if escalated {
				summary.Escalated++
			}
		}
	default:
		// 未确认（LLM 失败 / 回执未解析 / 回执未提及）：复位 pending 留待下轮，绝不静默判通过（审计 P2）。
		// 原因写入 note（前端可见），不计 verify_attempts（与 rejected 区分，避免供应商抖动误转人工）。
		if werr := s.store.RecordIssueVerifyUnverified(ctx, novelID, runID, r.IssueKey, noteOrDefault(oc.note, "复验未确认，已复位待复验")); werr != nil {
			s.logger.Warn("复验：未确认落库失败（非阻断，留待下轮）", "issue", r.IssueKey, "err", werr)
		}
		summary.Unverified++
	}
	emitVerifyProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "total": total, "done": done,
		"last_key": r.IssueKey, "last_ok": oc.verified, "last_rejected": oc.rejected,
		"last_note": oc.note, "status": "progress",
	})
}

// verifyBatch 对一小批工单做一次「语料内联」的 signability-verify 调用，按回执 id 逐条
// 返回逐工单结论（2026-11-05 复验终极提速：证据预取内联（verify_corpus）+ 多目标合验）。
// 失败分层 note（P1-C 三档）保持：整轮超时/取消 / 单批预算耗尽 / 一般执行失败。
// 故障隔离语义（审计 P0/P2）保持：调用失败/回执未解析 → 整批未确认（复位 pending 留待下轮），
// 绝不静默全过；回执逐 id 拆分，未提及的工单单列未确认。
func (s *Service) verifyBatch(ctx context.Context, novelID int64, runID string, batch []PipelineIssue, ledgerNotes []string, propBlock string, src verifyCorpusSource) map[string]verifyOutcome {
	targets := make([]verifyTarget, 0, len(batch))
	for _, row := range batch {
		chs := issueChapterIDs(AuditIssue{
			Area: row.Area, Desc: row.Desc, ChapterIDs: decodeIssueChapterIDs(row.ChapterIDsJSON),
		})
		targets = append(targets, verifyTarget{
			ID: row.IssueKey, Level: row.Level, Area: row.Area, Desc: row.Desc, Chapters: chs, FixNote: row.Note,
		})
	}
	// RAG-Verify（verify_retrieval.go）：逐工单向量检索定位证据切片，零命中/无向量库
	// 回退窗口语料——证据密度高、思维量数量级下降，输入体量与书长无关。
	corpus := s.buildVerifyEvidenceBatch(ctx, novelID, src, targets)
	// 单批独立墙钟预算：某批卡住（供应商静默/慢轮）单独超时复位 pending，后续批次继续；
	// 墙钟预算（15min）宽于 thinking 模型 9 分钟空闲看门狗（真死连接由看门狗先斩断，
	// 本预算兜「活着但慢」的轮次，见 detect.go）。
	tctx, tcancel := context.WithTimeout(ctx, verifyTicketTimeout)
	defer tcancel()
	out, derr := s.dispatchSkill(tctx, "signability-verify", SkillInput{
		NovelID:   novelID,
		Prompt:    verifyPrompt(novelID, targets, ledgerNotes, propBlock, contextasm.ContextBlocks{}) + corpus,
		MaxTurns:  verifyMaxTurns,
		MaxTokens: verifyMaxTokens,
		// 证据已全部内联（corpus）→ 工具零暴露：不给模型任何工具 schema，调用结构性
		// 不可能，单轮必出回执（杜绝「提示词禁工具、schema 又给工具」的空转，2026-11-05
		// 复验二次提速）。
		Whitelist: []string{},
		SessionID: "pipeline:issue-tickets:verify:" + uuid.NewString(),
		RunID:     runID,
	})
	outcomes := make(map[string]verifyOutcome, len(batch))
	if derr != nil {
		// 供应商熔断/超时/网络失败：整批未确认，留待下轮（不报错中断整轮）。
		// 失败原因分层成文（P1-C「超时未验证」UI 明示）：整轮超时/取消 与 单批预算耗尽
		// 与 一般执行失败 区分措辞，note 原文进前端两面板（工单面板/巡检卡片），避免含混。
		note := "复验执行失败（" + derr.Error() + "），已复位待复验"
		switch {
		case ctx.Err() != nil:
			s.logger.Warn("复验：整轮超时/取消，本批标未确认留待下轮", "run_id", runID, "batch", len(batch))
			note = "复验整轮超时或取消，已复位待复验"
		case errors.Is(derr, context.DeadlineExceeded) || tctx.Err() == context.DeadlineExceeded:
			s.logger.Warn("复验：单批超时未验证，本批标未确认留待下轮", "run_id", runID, "batch", len(batch), "err", derr)
			note = fmt.Sprintf("复验超时未验证（单批 %d 分钟预算耗尽），已复位待复验", int(verifyTicketTimeout.Minutes()))
		default:
			s.logger.Warn("复验：单批执行失败，本批标未确认留待下轮", "run_id", runID, "batch", len(batch), "err", derr)
		}
		for _, row := range batch {
			outcomes[row.IssueKey] = verifyOutcome{note: note}
		}
		return outcomes
	}
	results, _, found, partial := parseVerifyReceipt(out.Text)
	if !found {
		// 审计 P2：回执未解析 → 不再静默全过，整批标未确认留待下轮。
		s.logger.Warn("复验：回执未解析，本批工单状态未改动（复位 pending 留待下轮）", "run_id", runID, "batch", len(batch))
		for _, row := range batch {
			outcomes[row.IssueKey] = verifyOutcome{note: "复验回执未解析，状态未改动"}
		}
		return outcomes
	}
	if partial {
		// 截断抢救出部分条目：显式条目照落，残缺条目按截断口径留待下轮（见 parseVerifyReceipt）。
		s.logger.Warn("复验：回执被截断，已抢救完整条目（残缺条目留待下轮）", "run_id", runID, "salvaged", len(results), "batch", len(batch))
	}
	for _, row := range batch {
		res, ok := results[row.IssueKey]
		if !ok {
			note := "复验回执未包含本工单，留待下轮"
			if partial {
				note = "复验回执被截断未含本工单，留待下轮"
			}
			outcomes[row.IssueKey] = verifyOutcome{note: note}
			continue
		}
		if res.OK {
			outcomes[row.IssueKey] = verifyOutcome{verified: true, note: res.Note}
		} else {
			outcomes[row.IssueKey] = verifyOutcome{rejected: true, note: res.Note}
		}
	}
	return outcomes
}

// chunkIssues 把工单按 size 切批（末批可小），批间并发、批内合验。
func chunkIssues(rows []PipelineIssue, size int) [][]PipelineIssue {
	if size <= 0 {
		size = 1
	}
	var batches [][]PipelineIssue
	for len(rows) > 0 {
		n := size
		if n > len(rows) {
			n = len(rows)
		}
		batches = append(batches, rows[:n])
		rows = rows[n:]
	}
	return batches
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
