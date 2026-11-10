package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// ── 发现项工单面板 API（R5-1，提案 §10.1/§10.2）──────────────────────────────
// 工单面板按钮（去修复/标记已修复/取消修复/复验/驳回/确认）经此调用
// pipeline.Service 的工单入口。所有修复都走 chat 人工路径；后台 LLM 自动修复
// 执行体已整体下线，本层不再暴露任何自动修复入口。

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

// StartManualIssueFixTickets 「去修复」（人工路径）：选中工单进入「修复中」，由前端把
// 问题上下文发到 chat 协作修复；不派发后台自动修复执行体。
func (a *App) StartManualIssueFixTickets(novelID int64, refs []pipeline.IssueTicketRef) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.StartManualIssueFixTickets(a.ctx, novelID, refs)
}

// CloseIssueFixTickets 「标记已修复」：chat 人工修完后把「修复中」收口为「已修复待复验」。
func (a *App) CloseIssueFixTickets(novelID int64, refs []pipeline.IssueTicketRef) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.CloseIssueFixTickets(a.ctx, novelID, refs)
}

// CancelIssueFixTickets 「取消修复」：放弃本次人工修复，工单退回待确认。
func (a *App) CancelIssueFixTickets(novelID int64, refs []pipeline.IssueTicketRef) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.CancelIssueFixTickets(a.ctx, novelID, refs)
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

// CancelIssueVerify 取消一个 run 的后台复验（审计 P0）：用户在工单面板点「取消复验」后调用。
// 返回 cancelled 标识是否确有在飞复验被取消（未知 run 合法空操作）。
func (a *App) CancelIssueVerify(novelID int64, runID string) (bool, error) {
	if a.pipeline == nil {
		return false, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.CancelIssueVerify(novelID, runID)
}
