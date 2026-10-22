package retcon

import (
	"errors"
	"testing"
)

// TestTransitionsLegal 验证状态机的全部合法迁移（候选→审批→受控编辑→留痕→回滚点）。
func TestTransitionsLegal(t *testing.T) {
	legal := [][2]string{
		{StatusCandidate, StatusApproved},
		{StatusCandidate, StatusRejected},
		{StatusApproved, StatusEditing},
		{StatusEditing, StatusRecorded},
		{StatusRecorded, StatusRolledBack},
	}
	for _, tr := range legal {
		if !CanTransition(tr[0], tr[1]) {
			t.Errorf("迁移 %s→%s 应合法", tr[0], tr[1])
		}
		if err := ValidateTransition(tr[0], tr[1]); err != nil {
			t.Errorf("ValidateTransition(%s→%s) = %v, want nil", tr[0], tr[1], err)
		}
	}
}

// TestTransitionsIllegal 验证跳步/回退/终态再动全部被拒。
func TestTransitionsIllegal(t *testing.T) {
	illegal := [][2]string{
		// 跳步：未审批就编辑/留痕/回滚。
		{StatusCandidate, StatusEditing},
		{StatusCandidate, StatusRecorded},
		{StatusCandidate, StatusRolledBack},
		{StatusApproved, StatusRecorded},
		{StatusApproved, StatusRolledBack},
		// 回退：审批不得回候选、编辑不得回审批、留痕不得回编辑。
		{StatusApproved, StatusCandidate},
		{StatusEditing, StatusApproved},
		{StatusEditing, StatusCandidate},
		{StatusRecorded, StatusEditing},
		{StatusRecorded, StatusApproved},
		{StatusRecorded, StatusCandidate},
		// 自迁移。
		{StatusCandidate, StatusCandidate},
		{StatusApproved, StatusApproved},
		{StatusEditing, StatusEditing},
		{StatusRecorded, StatusRecorded},
		// 终态再动。
		{StatusRejected, StatusApproved},
		{StatusRejected, StatusCandidate},
		{StatusRejected, StatusRejected},
		{StatusRolledBack, StatusRecorded},
		{StatusRolledBack, StatusCandidate},
		{StatusRolledBack, StatusRolledBack},
	}
	for _, tr := range illegal {
		if CanTransition(tr[0], tr[1]) {
			t.Errorf("迁移 %s→%s 应非法", tr[0], tr[1])
		}
		if err := ValidateTransition(tr[0], tr[1]); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("ValidateTransition(%s→%s) = %v, want ErrInvalidTransition", tr[0], tr[1], err)
		}
	}
}

// TestStateMachineAcyclic 验证状态机无环（「非循环自动」的机械保证）：
// 从任意状态出发沿迁移边走不到自身。
func TestStateMachineAcyclic(t *testing.T) {
	const states = 6
	all := []string{StatusCandidate, StatusApproved, StatusRejected,
		StatusEditing, StatusRecorded, StatusRolledBack}
	if len(all) != states {
		t.Fatalf("状态枚举数漂移：%d", len(all))
	}
	var visit func(from, cur string, depth int) bool
	visit = func(from, cur string, depth int) bool {
		if depth > 0 && cur == from {
			return true // 找到回路
		}
		if depth > states {
			return true // 深度失控当环处理
		}
		for _, nxt := range transitions[cur] {
			if visit(from, nxt, depth+1) {
				return true
			}
		}
		return false
	}
	for _, st := range all {
		if visit(st, st, 0) {
			t.Errorf("状态 %s 可达自身：状态机有环", st)
		}
	}
}

// TestTerminalStates 验证终态判定：rejected/rolled_back 无出边，其余有出边。
func TestTerminalStates(t *testing.T) {
	terminal := map[string]bool{
		StatusCandidate:  false,
		StatusApproved:   false,
		StatusEditing:    false,
		StatusRecorded:   false,
		StatusRejected:   true,
		StatusRolledBack: true,
	}
	for st, want := range terminal {
		if got := IsTerminal(st); got != want {
			t.Errorf("IsTerminal(%s) = %v, want %v", st, got, want)
		}
	}
}
