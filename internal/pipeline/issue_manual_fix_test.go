package pipeline

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// #270 双轨拆分（2026-11-10）：工单面板「去修复」原先把问题上下文发到 chat 的同时
// 隐含派发后台自动 LLM 修复执行体。chat 里人工修完后，后台执行体可能仍在长耗时调用里，
// issueFixActive 不摘 → #106 互斥让「复验」一直报「修复任务仍在进行中」，而面板又没有
// 「我修好了」的收口入口。本组测试锁住拆分后的三条不变量：
//   1. 人工「去修复」只置 fixing，绝不占用自动修复互斥窗口（复验可正常进入）；
//   2. 「标记已修复」/「取消修复」按状态机收口并留痕，非 fixing 不得误收；
//   3. 自动修复在途时，人工链路整体拒绝且不写任何状态（防双轨抢单）。
// 自动修复执行体本身的派发/排空/回执语义由既有 R5-2 测试覆盖，此处不重复触发 LLM。

func newManualFixService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	return svc, context.Background()
}

func TestManualIssueFixDoesNotOccupyFixMutex(t *testing.T) {
	svc, ctx := newManualFixService(t)
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "设定失真", Desc: "台账与正文不一致"},
		{ID: "I2", Level: "P1", Area: "设定失真", Desc: "执行者错位"},
	}))
	refs := []IssueTicketRef{
		{RunID: "run-a", IssueKey: "I1"},
		{RunID: "run-a", IssueKey: "I2"},
	}
	require.NoError(t, svc.StartManualIssueFixTickets(ctx, 7, refs))

	rows, err := svc.store.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Equal(t, IssueStatusFixing, r.Status)
	}
	// 复验入口的互斥判据就是 issueFixRunning；人工路径必须恒为 false，
	// 否则 chat 修完后仍会被「修复任务仍在进行中」拒之门外。
	require.False(t, svc.issueFixRunning("run-a"))

	// 幂等：重复点「去修复」不报错（与自动修复入口同口径）。
	require.NoError(t, svc.StartManualIssueFixTickets(ctx, 7, refs))
}

func TestCloseAndCancelManualIssueFix(t *testing.T) {
	svc, ctx := newManualFixService(t)
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "设定失真", Desc: "台账与正文不一致"},
		{ID: "I2", Level: "P1", Area: "设定失真", Desc: "执行者错位"},
		{ID: "I3", Level: "P1", Area: "节奏", Desc: "尚未修复"},
	}))
	refs := []IssueTicketRef{
		{RunID: "run-a", IssueKey: "I1"},
		{RunID: "run-a", IssueKey: "I2"},
	}
	require.NoError(t, svc.StartManualIssueFixTickets(ctx, 7, refs))

	// I1：chat 修完 → 标记已修复（fixed 待复验），note 留痕。
	require.NoError(t, svc.CloseIssueFixTickets(ctx, 7, refs[:1]))
	// I2：放弃修复 → 退回待确认。
	require.NoError(t, svc.CancelIssueFixTickets(ctx, 7, refs[1:]))

	rows, err := svc.store.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusFixed, byKey["I1"].Status)
	require.Contains(t, byKey["I1"].Note, "人工修复完成")
	require.Equal(t, IssueStatusPending, byKey["I2"].Status)
	require.Contains(t, byKey["I2"].Note, "取消修复")
	require.Equal(t, IssueStatusPending, byKey["I3"].Status)

	// 幂等收口；非 fixing 工单不得被误标已修复。
	require.NoError(t, svc.CloseIssueFixTickets(ctx, 7, refs[:1]))
	err = svc.CloseIssueFixTickets(ctx, 7, []IssueTicketRef{{RunID: "run-a", IssueKey: "I3"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "只有「修复中」可收口")

	// 终态保护：verified 不得被人工收口翻案（#106）。
	_, err = svc.store.RecordIssueVerify(ctx, 7, "run-a", "I1", true, "已核对")
	require.NoError(t, err)
	err = svc.CancelIssueFixTickets(ctx, 7, refs[:1])
	require.Error(t, err)
	rows, err = svc.store.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	for _, r := range rows {
		if r.IssueKey == "I1" {
			require.Equal(t, IssueStatusVerified, r.Status)
		}
	}
}

func TestManualFixBlockedWhileAutoFixInFlight(t *testing.T) {
	svc, ctx := newManualFixService(t)
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "设定失真", Desc: "台账与正文不一致"},
	}))
	refs := []IssueTicketRef{{RunID: "run-a", IssueKey: "I1"}}
	svc.issueFixActive = map[string]bool{"run-a": true}

	cases := map[string]func() error{
		"去修复": func() error { return svc.StartManualIssueFixTickets(ctx, 7, refs) },
		"标记已修复": func() error {
			return svc.CloseIssueFixTickets(ctx, 7, refs)
		},
		"取消修复": func() error {
			return svc.CancelIssueFixTickets(ctx, 7, refs)
		},
	}
	for name, fn := range cases {
		err := fn()
		require.Error(t, err, name)
		require.Contains(t, err.Error(), "仍在途", name)
	}
	rows, err := svc.store.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Equal(t, IssueStatusPending, rows[0].Status, "拒绝时不得写任何状态")
}
