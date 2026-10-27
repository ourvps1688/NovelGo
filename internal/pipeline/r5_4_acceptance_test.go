package pipeline

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR54Acceptance_GateDowngradeSourceLock 验收 A（不卡流程，grep 断言）：
// G3b 弹闸退役（advanceS3 零闸门发射，改非阻塞 stage note）+ auto_run 自动裁决
// 退役（autoFixCap 常量退役、AutoRun 不得再驱动裁决分支）+ 检测收口统一落
// awaiting_gate + 汇总闸门（emitGateDecision），裁决交还用户。
// 行为面互证：service_test 三条 note 测试（S3 零 gate 事件持续 running）+
// TestServiceExecuteAuditLandsGateKeepsAuditContext（autoRun=true 仍不自动 Advance）。
func TestR54Acceptance_GateDowngradeSourceLock(t *testing.T) {
	orch, err := os.ReadFile("orchestrator.go")
	require.NoError(t, err)
	orchSrc := string(orch)
	require.NotContains(t, orchSrc, "emitGate(", "G3b 弹闸退役：写作循环不得再发闸门事件")
	require.Contains(t, orchSrc, "milestoneSummary(", "里程碑复检建议（非阻塞 note）不得退役")

	svc, err := os.ReadFile("service.go")
	require.NoError(t, err)
	svcSrc := string(svc)
	require.NotContains(t, svcSrc, "const autoFixCap", "autoFixCap 随 auto_run 自动裁决退役")
	require.NotContains(t, svcSrc, "AutoRun &&", "auto_run 不得再驱动自动裁决分支")
	require.Contains(t, svcSrc, "s.emitGateDecision(ctx, runID, novelID, auditStage, audit)",
		"检测收口统一落汇总闸门（awaiting_gate + emitGateDecision，裁决交还用户）")
}

// TestR54Acceptance_HumanDecisionKept 验收 B（人决定 + 勿回退，grep 断言）：
// G1/G1b/G4/G终 停点保留（awaiting_gate 落点 + 四闸门广播），Advance 唯一裁决点
// 零改动。自动节奏四触发点源码锁见 TestR53Acceptance_AutoCadenceSourceLock（迁移后绿）。
func TestR54Acceptance_HumanDecisionKept(t *testing.T) {
	svc, err := os.ReadFile("service.go")
	require.NoError(t, err)
	svcSrc := string(svc)
	for _, gate := range []string{"GateG1,", "GateG1b,", "GateG4", "GateGEnd"} {
		require.Contains(t, svcSrc, gate, "停点保留：%s 闸门广播不得退役", gate)
	}
	require.Contains(t, svcSrc, "StatusAwaitingGate", "awaiting_gate 停点（结论就绪等人定夺）不得退役")

	orch, err := os.ReadFile("orchestrator.go")
	require.NoError(t, err)
	orchSrc := string(orch)
	require.Contains(t, orchSrc, "func (o *Orchestrator) Advance(", "Advance 唯一裁决点不得退役/改动")
}
