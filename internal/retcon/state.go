package retcon

import (
	"errors"
	"fmt"
)

// retcon 案件状态机（WBS 3.1：候选→审批→受控编辑→留痕→回滚点）。
const (
	// StatusCandidate 候选：等待作者裁决（起点，pipeline 沉淀 / author 发起均落此态）。
	StatusCandidate = "candidate"
	// StatusApproved 审批通过：允许进入受控编辑（仍不得被自动回改）。
	StatusApproved = "approved"
	// StatusRejected 驳回（终态）：不做 retcon，走前向适配。
	StatusRejected = "rejected"
	// StatusEditing 受控编辑中：回滚点已固化（RollbackSnapshotID），3.2 在此窗口内留痕编辑。
	StatusEditing = "editing"
	// StatusRecorded 已留痕：编辑完成、差异摘要已固化（回滚点齐备，可回滚）。
	StatusRecorded = "recorded"
	// StatusRolledBack 已回滚（终态）：台账已按回滚点还原。
	StatusRolledBack = "rolled_back"
)

// transitions 是状态机的合法迁移表（单向无环）。所有迁移都必须由显式 API 调用驱动，
// 没有任何自动流转路径——「retcon 必须显式审批，非循环自动」（WBS §4 验收）。
var transitions = map[string][]string{
	StatusCandidate: {StatusApproved, StatusRejected},
	StatusApproved:  {StatusEditing},
	StatusEditing:   {StatusRecorded},
	StatusRecorded:  {StatusRolledBack},
}

// 状态机错误。ErrInvalidTransition 覆盖「跳步/回退/终态再动」等一切非法迁移。
var (
	// ErrInvalidTransition 非法状态迁移（含终态再迁移）。
	ErrInvalidTransition = errors.New("retcon: 非法状态迁移")
	// ErrNoteRequired 裁决理由缺失（Approve/Reject 必须留痕理由）。
	ErrNoteRequired = errors.New("retcon: 裁决必须留痕理由")
	// ErrNotFrozen 目标卷不是已冻结卷（retcon 只针对已定稿前卷）。
	ErrNotFrozen = errors.New("retcon: 目标卷不是已冻结卷")
	// ErrNoRestorer 回滚执行体未接入（3.2 接入快照 Restore 前禁止回滚）。
	ErrNoRestorer = errors.New("retcon: 回滚执行体未接入")
	// ErrRollbackPointRequired 回滚点缺失（受控编辑必须先固化回滚点）。
	ErrRollbackPointRequired = errors.New("retcon: 受控编辑必须先固化回滚点")
	// ErrDiffRequired 差异摘要缺失（留痕必须记录差异摘要）。
	ErrDiffRequired = errors.New("retcon: 留痕必须记录差异摘要")
)

// CanTransition 判断 from→to 是否为合法迁移。
func CanTransition(from, to string) bool {
	for _, nxt := range transitions[from] {
		if nxt == to {
			return true
		}
	}
	return false
}

// ValidateTransition 校验 from→to 迁移合法性，非法返回包装 ErrInvalidTransition 的错误。
func ValidateTransition(from, to string) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
}

// IsTerminal 判断状态是否终态（无出边，不可再迁移）。
func IsTerminal(status string) bool {
	return len(transitions[status]) == 0
}
