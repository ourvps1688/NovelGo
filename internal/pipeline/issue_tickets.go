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

// StartIssueFixTickets 「去修复」：工单进入（人工）修复中（状态机：→修复中）。
// 只立修复意向态——修复动作由人显式执行，修复执行能力改工单被动触发属 R5-2。
func (s *Service) StartIssueFixTickets(ctx context.Context, novelID int64, refs []IssueTicketRef) error {
	return s.transitionIssueTickets(ctx, novelID, refs, IssueStatusFixing, "")
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
			// 兼容回退（与 executeVerify 同口径）：无回执按已修复确认处理，不卡死闭环。
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
