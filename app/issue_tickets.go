package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// ── 发现项工单面板 API（R5-1，提案 §10.1/§10.2）──────────────────────────────
// 工单面板按钮（去修复/复验/驳回/确认）经此调用 pipeline.Service 的工单入口。
// 修复执行改工单被动触发属 R5-2；此处只做流转与显式复验。

// ListIssueTickets 列出一本书的全部发现项工单（跨 run，新单在前）。
func (a *App) ListIssueTickets(novelID int64) ([]pipeline.PipelineIssue, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ListIssueTickets(a.ctx, novelID)
}

// ConfirmIssueTickets 确认发现项工单（状态机：发现→确认）。
func (a *App) ConfirmIssueTickets(novelID int64, refs []pipeline.IssueTicketRef) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ConfirmIssueTickets(a.ctx, novelID, refs)
}

// StartIssueFixTickets 「去修复」：选中工单进入人工修复中（状态机：→修复中）。
func (a *App) StartIssueFixTickets(novelID int64, refs []pipeline.IssueTicketRef) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.StartIssueFixTickets(a.ctx, novelID, refs)
}

// VerifyIssueTickets 「复验」：对选中工单定点复验并落复验回执（复验通过/退回待修）。
func (a *App) VerifyIssueTickets(novelID int64, refs []pipeline.IssueTicketRef) (*pipeline.IssueVerifySummary, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.VerifyIssueTickets(a.ctx, novelID, refs)
}

// DismissIssueTickets 「驳回」：选中工单进终态 dismissed，必须给出理由（留痕）。
func (a *App) DismissIssueTickets(novelID int64, refs []pipeline.IssueTicketRef, reason string) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.DismissIssueTickets(a.ctx, novelID, refs, reason)
}
