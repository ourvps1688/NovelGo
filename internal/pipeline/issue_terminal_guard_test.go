package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateIssueStatusProtectsTerminal 验收 #106 状态回退修复：修复执行体（章节重写
// 可能耗时数分钟）比复验晚收口时，其兼容写不得把已复验通过的终态工单打回
// 「已修复（待复验）」，也不得抢写复验在途瞬态；正常流转不受影响。
//
// 事故原型：T1 21:22:29 复验通过落 verified，同 run 修复任务 21:28:12 收口时
// syncIssueRow(fixed) 无条件 UPDATE 覆盖了终态。
func TestUpdateIssueStatusProtectsTerminal(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "设定失真", Desc: "称谓漂移"},
		{ID: "I2", Level: "P1", Area: "误报", Desc: "驳回项"},
		{ID: "I3", Level: "P1", Area: "时间线", Desc: "复验在途"},
		{ID: "I4", Level: "P1", Area: "节奏", Desc: "正常流转"},
	}))

	// I1：复验通过 → verified 终态；随后模拟晚到的各种修复回写。
	_, err := s.RecordIssueVerify(ctx, 7, "run-a", "I1", true, "已统一称谓")
	require.NoError(t, err)
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I1", IssueStatusFixed, ""))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I1", IssueStatusSkipped, "派发失败"))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I1", IssueStatusRewrite, "V 系退回"))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I1", IssueStatusVerifying, ""))

	// I2：驳回终态同样不可被修复回写翻案。
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "I2", IssueStatusDismissed, "误报"))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I2", IssueStatusFixed, ""))

	// I3：复验进行中，修复收口不得抢写（否则复验未确认时会恢复错状态）。
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I3", IssueStatusVerifying, ""))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I3", IssueStatusFixed, ""))

	// I4：非保护态的正常流转必须照旧（别把修复闭环锁死）。
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I4", IssueStatusFixed, ""))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I4", IssueStatusSkipped, "正常跳过"))

	rows, err := s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusVerified, byKey["I1"].Status, "复验通过终态不得被修复回执打回 fixed")
	require.Equal(t, "已统一称谓", byKey["I1"].VerifyReceipt, "复验回执须原样保留")
	require.Equal(t, IssueStatusDismissed, byKey["I2"].Status, "驳回终态不得被翻案")
	require.Equal(t, IssueStatusVerifying, byKey["I3"].Status, "复验在途瞬态不得被抢写")
	require.Equal(t, IssueStatusSkipped, byKey["I4"].Status, "正常流转不受影响")
	require.Equal(t, "正常跳过", byKey["I4"].SkipReason)
}
