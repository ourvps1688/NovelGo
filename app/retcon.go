package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/retcon"
)

// RetconCaseInput 是发起 retcon 候选的前端输入（作者主动发起）。
type RetconCaseInput = retcon.CreateInput

// ListRetconCases 返回该小说的全部 retcon 案件（WBS 3.1 审批闸门清单，含候选/已批/已驳等）。
func (a *App) ListRetconCases(novelID int64) ([]retcon.RetconCase, error) {
	rows, err := a.retcons.ListByNovel(a.ctx, novelID)
	if err != nil {
		return nil, fmt.Errorf("list retcon cases: %w", err)
	}
	return rows, nil
}

// GetRetconCase 按 id 取单条 retcon 案件。
func (a *App) GetRetconCase(id int64) (*retcon.RetconCase, error) {
	c, err := a.retcons.GetByID(a.ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get retcon case: %w", err)
	}
	return c, nil
}

// CreateRetconCase 作者主动发起一条 retcon 候选（候选→审批起点）。目标卷若指定必须
// 是已冻结卷；发起即进 candidate 状态，等待 ApproveRetconCase/RejectRetconCase 显式裁决。
func (a *App) CreateRetconCase(input RetconCaseInput) (*retcon.RetconCase, error) {
	c, err := a.retcons.CreateCandidate(a.ctx, input)
	if err != nil {
		return nil, fmt.Errorf("create retcon case: %w", err)
	}
	return c, nil
}

// ApproveRetconCase 显式批准 retcon 候选（candidate→approved）：retcon 必须作者拍板，
// 绝不自动。note 必填（裁决理由留痕）。批准后进入受控编辑窗口（3.2 留痕编辑、可回滚）。
func (a *App) ApproveRetconCase(id int64, actor, note string) (*retcon.RetconCase, error) {
	c, err := a.retcons.Approve(a.ctx, id, actor, note)
	if err != nil {
		return nil, fmt.Errorf("approve retcon case: %w", err)
	}
	return c, nil
}

// RejectRetconCase 显式驳回 retcon 候选（candidate→rejected，终态）：走前向适配，
// 不回改前卷。note 必填（裁决理由留痕）。
func (a *App) RejectRetconCase(id int64, actor, note string) (*retcon.RetconCase, error) {
	c, err := a.retcons.Reject(a.ctx, id, actor, note)
	if err != nil {
		return nil, fmt.Errorf("reject retcon case: %w", err)
	}
	return c, nil
}
