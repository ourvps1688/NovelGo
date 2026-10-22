package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/retcon"
	"github.com/novelgo/novelgo/internal/snapshot"
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

// BeginRetconEdit 进入受控编辑窗口（WBS 3.2，approved→editing）：绑定目标冻结卷并
// 固化回滚点快照（编辑前状态），此后才允许在窗口内回改冻结卷（走既有台账/正文写路径，
// 冻结卷改动自动标 source=retcon，见 2.6）。
func (a *App) BeginRetconEdit(caseID, volumeID int64, actor string) (*retcon.RetconCase, error) {
	c, err := a.retcons.BeginControlledEdit(a.ctx, caseID, volumeID, actor)
	if err != nil {
		return nil, fmt.Errorf("begin retcon edit: %w", err)
	}
	return c, nil
}

// RecordRetconEdit 留痕收口（editing→recorded）：自动以回滚点快照与当前台账的差异
// 摘要固化留痕（「每次 retcon 有 diff + 回滚点」），并刷新既定事实快照。
func (a *App) RecordRetconEdit(caseID int64, actor string) (*retcon.RetconCase, error) {
	c, err := a.retcons.RecordWithDiff(a.ctx, caseID, actor)
	if err != nil {
		return nil, fmt.Errorf("record retcon edit: %w", err)
	}
	return c, nil
}

// RollbackRetconCase 回滚受控编辑（recorded→rolled_back）：台账整体还原到回滚点快照
// 后落终态，并刷新既定事实快照。破坏性操作，由作者显式确认后调用。
func (a *App) RollbackRetconCase(caseID int64, actor, note string) (*retcon.RetconCase, error) {
	c, err := a.retcons.Rollback(a.ctx, caseID, actor, note)
	if err != nil {
		return nil, fmt.Errorf("rollback retcon case: %w", err)
	}
	return c, nil
}

// GetRetconEditDiff 查看受控编辑窗口内「回滚点 vs 当前台账」的结构化差异
// （审批弹窗/编辑对照用，只读）。
func (a *App) GetRetconEditDiff(caseID int64) (*snapshot.Delta, error) {
	c, err := a.retcons.GetByID(a.ctx, caseID)
	if err != nil {
		return nil, fmt.Errorf("get retcon case: %w", err)
	}
	if c.RollbackSnapshotID == nil {
		return nil, fmt.Errorf("get retcon edit diff: 尚未固化回滚点")
	}
	delta, err := a.snapshots.DiffAgainstCurrent(a.ctx, *c.RollbackSnapshotID)
	if err != nil {
		return nil, fmt.Errorf("get retcon edit diff: %w", err)
	}
	return delta, nil
}
