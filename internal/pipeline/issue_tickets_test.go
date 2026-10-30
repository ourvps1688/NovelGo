package pipeline

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/task"
)

// ── 发现项工单状态机（R5-1，§10.2）───────────────────────────────────────────

// TestStoreIssueTicketStateMachine 验收工单状态机：发现→确认→修复中→复验通过/驳回
// 合法迁移成功；非法迁移显式报错（不静默改写）；终态无出边；复验通过只经回执落表。
func TestStoreIssueTicketStateMachine(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇", Desc: "前三章未立人设"},
	}))

	// pending → confirmed → fixing 全链合法。
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusConfirmed, ""))
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusFixing, ""))
	rows, err := s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Equal(t, IssueStatusFixing, rows[0].Status)

	// 复验通过（verified）只经 RecordIssueVerify：通用流转拒绝。
	err = s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusVerified, "")
	require.ErrorIs(t, err, ErrIssueIllegalTransition, "verified 必须携带复验回执落表")

	// 非法迁移显式报错：fixing → confirmed 不在状态机表内。
	err = s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusConfirmed, "")
	require.ErrorIs(t, err, ErrIssueIllegalTransition)

	// 复验回执落表：通过 → verified 终态 + 回执/时间留痕。
	require.NoError(t, s.RecordIssueVerify(ctx, 7, "run-a", "I1", true, "第1-3章人设已立"))
	rows, err = s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Equal(t, IssueStatusVerified, rows[0].Status)
	require.Equal(t, "第1-3章人设已立", rows[0].VerifyReceipt)
	require.NotNil(t, rows[0].VerifyAt)

	// 终态无出边：verified 上任何流转/复验都被拒。
	err = s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusFixing, "")
	require.ErrorIs(t, err, ErrIssueTerminal)
	err = s.RecordIssueVerify(ctx, 7, "run-a", "I1", true, "再验一次")
	require.ErrorIs(t, err, ErrIssueTerminal)

	// 不存在的工单显式报错。
	err = s.TransitionIssueTicket(ctx, 7, "run-a", "I999", IssueStatusConfirmed, "")
	require.ErrorIs(t, err, ErrIssueNotFound)
}

// TestStoreIssueTicketDismissAndReverifyFail 验收驳回（终态 + 理由留痕）与
// 复验未通过退回待修（pending，回执照记）。
func TestStoreIssueTicketDismissAndReverifyFail(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏", Desc: "拖沓"},
		{ID: "I2", Level: "P1", Area: "人设", Desc: "弱"},
	}))

	// 驳回：pending → dismissed，理由写 note 列。
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusDismissed, "误报：编辑视角主观差异"))
	rows, err := s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Equal(t, IssueStatusDismissed, rows[0].Status)
	require.Equal(t, "误报：编辑视角主观差异", rows[0].Note)
	err = s.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusFixing, "")
	require.ErrorIs(t, err, ErrIssueTerminal, "dismissed 是终态")

	// 复验未通过：退回待修（pending），回执照记。
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "I2", IssueStatusFixing, ""))
	require.NoError(t, s.RecordIssueVerify(ctx, 7, "run-a", "I2", false, "只改了一半"))
	rows, err = s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Equal(t, IssueStatusPending, rows[1].Status)
	require.Equal(t, "只改了一半", rows[1].VerifyReceipt)
	require.NotNil(t, rows[1].VerifyAt)
}

// ── RewriteBacklog 的工单表投影（R5-1「读写改走工单」）────────────────────────

// TestStoreListRewriteTickets 验收重写清单工单投影源的筛选：body/V 系进、
// 台账级不进；已收口（verified/dismissed/retcon）不进。
func TestStoreListRewriteTickets(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇", Desc: "正文问题", FixType: FixTypeBody},
		{ID: "I2", Level: "P1", Area: "台账", Desc: "台账问题", FixType: FixTypeLedger},
		{ID: "V1", Level: "P1", Area: "文风漂移", Desc: "指标偏离", FixType: FixTypeLedger},
		{ID: "I3", Level: "P1", Area: "正文2", Desc: "正文问题2", FixType: FixTypeBody},
		{ID: "I4", Level: "P1", Area: "正文3", Desc: "正文问题3", FixType: FixTypeBody},
	}))

	rows, err := s.ListRewriteTickets(ctx, 7, "run-a")
	require.NoError(t, err)
	got := []string{}
	for _, r := range rows {
		got = append(got, r.IssueKey)
	}
	require.Equal(t, []string{"I1", "V1", "I3", "I4"}, got,
		"body 与 V 系进投影；台账级（I2）不进")

	// 已收口的工单退出清单：verified / dismissed 不进。
	require.NoError(t, s.RecordIssueVerify(ctx, 7, "run-a", "I3", true, "已修复"))
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "I4", IssueStatusDismissed, "误报"))
	rows, err = s.ListRewriteTickets(ctx, 7, "run-a")
	require.NoError(t, err)
	got = got[:0]
	for _, r := range rows {
		got = append(got, r.IssueKey)
	}
	require.Equal(t, []string{"I1", "V1"}, got, "verified/dismissed 应退出重写清单投影")
}

// TestServiceRewriteBacklogOfProjection 验收 Service 侧投影：写保护行过滤、
// 取数失败回退内存清单（非阻断）。
func TestServiceRewriteBacklogOfProjection(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇", Desc: "正文问题", FixType: FixTypeBody,
			TargetScope: TargetScopeActiveVolume, Direction: DirectionForwardFix},
		{ID: "I2", Level: "P0", Area: "冻结卷", Desc: "写保护正文", FixType: FixTypeBody,
			TargetScope: TargetScopeFrozenVolume, Direction: DirectionRetconCandidate},
	}))

	got := svc.rewriteBacklogOf(ctx, 7, "run-a", nil)
	require.Len(t, got, 1, "写保护行不进重写清单（与 bodyIssuesOf 同语义）")
	require.Equal(t, "I1", got[0].ID)

	// 工单表对本 run 零行（旧版 run/未落表兼容路径）→ 无事实源，回退内存清单。
	got = svc.rewriteBacklogOf(ctx, 7, "run-missing", []RewriteItem{{ID: "IX"}})
	require.Len(t, got, 1, "无工单行=无事实源，回退内存清单")
	require.Equal(t, "IX", got[0].ID)

	// 有行但全部收口（复验通过/驳回终态）→ 投影为空且不回退（收口即时出清单）。
	require.NoError(t, svc.store.RecordIssueVerify(ctx, 7, "run-a", "I1", true, "复验通过"))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 7, "run-a", "I2", IssueStatusDismissed, "误报"))
	got = svc.rewriteBacklogOf(ctx, 7, "run-a", []RewriteItem{{ID: "I1"}})
	require.Empty(t, got, "工单收口后投影为空，不回退内存清单")

	// 取数失败（表缺失）→ 回退内存清单（非阻断）。
	require.NoError(t, db.Migrator().DropTable(&PipelineIssue{}))
	fallback := []RewriteItem{{ID: "IX", Level: "P1"}}
	got = svc.rewriteBacklogOf(ctx, 7, "run-a", fallback)
	require.Equal(t, fallback, got, "取数失败回退内存清单")
}

// TestVerifyIssueTicketsCarriesKeysViaQueue 复验读己写竞态回归（验收点）：
// VerifyIssueTickets 必须把本次要复验的工单键显式透传到执行体（经 task payload 的 IssueKeys），
// 而非依赖执行体回扫 verifying 态——旧口径存在读己写竞态，执行体读到 0 行而提前返回，
// 工单永久卡在 verifying（用户侧表现为「复验无法走下去」）。本测试锁定键被携带、
// 执行体按键精确复验（不依赖 verifying 扫描）。
//
// 覆盖 issue_verify handler 为探针（不真跑 LLM），仅捕获 payload 中的 IssueKeys。
func TestVerifyIssueTicketsCarriesKeysViaQueue(t *testing.T) {
	db := newQueueTestDB(t)
	svc := newQueueService(t, db)
	ctx := context.Background()

	novelID := int64(9)
	runID := "run-verify-keys"
	require.NoError(t, svc.store.ReplaceIssues(ctx, novelID, runID, 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏", Desc: "拖沓"},
		{ID: "I2", Level: "P1", Area: "人设", Desc: "弱"},
		{ID: "I3", Level: "P1", Area: "逻辑", Desc: "矛盾"}, // 已 verified 终态，应被跳过
	}))
	require.NoError(t, svc.store.RecordIssueVerify(ctx, novelID, runID, "I3", true, "已通过"))

	var mu sync.Mutex
	var captured []string
	var hits atomic.Int32
	svc.taskRunner.Register(taskKindIssueVerify, func(_ context.Context, tk *task.Task) error {
		p, err := decodeTaskPayload(tk.Payload)
		require.NoError(t, err)
		mu.Lock()
		captured = append(captured, p.IssueKeys...)
		mu.Unlock()
		hits.Add(1)
		return nil
	})

	svc.StartTaskQueue()
	defer svc.StopTaskQueue()

	summary, err := svc.VerifyIssueTickets(ctx, novelID, []IssueTicketRef{
		{RunID: runID, IssueKey: "I1"},
		{RunID: runID, IssueKey: "I2"},
		{RunID: runID, IssueKey: "I3"}, // 终态，不应计入 Queued
	})
	require.NoError(t, err)
	require.Equal(t, 2, summary.Queued, "非终态 I1/I2 计入待复验")
	require.Equal(t, 1, summary.Unverified, "终态 I3 计入 Unverified 跳过不计")

	require.Eventually(t, func() bool { return hits.Load() == 1 }, 5*time.Second, 10*time.Millisecond,
		"复验执行体未派发")
	mu.Lock()
	got := append([]string(nil), captured...)
	mu.Unlock()
	require.ElementsMatch(t, []string{"I1", "I2"}, got, "执行体必须拿到显式工单键（竞态修复核心：不再依赖 verifying 扫描）")
}

// ── 工单面板服务入口（确认/去修复/驳回/列表）──────────────────────────────────

// TestServiceIssueTicketPanelActions 验收面板三按钮的服务语义：确认/去修复流转状态机；
// 驳回必须给理由；批量逐条独立；空选择显式报错。
func TestServiceIssueTicketPanelActions(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇", Desc: "前三章未立人设"},
		{ID: "I2", Level: "P1", Area: "节奏", Desc: "拖沓"},
	}))

	// 列表：新单在前。
	rows, err := svc.ListIssueTickets(ctx, 7)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "I2", rows[0].IssueKey, "新单在前（id DESC）")

	// 确认 + 去修复（批量）。
	refs := []IssueTicketRef{{RunID: "run-a", IssueKey: "I1"}, {RunID: "run-a", IssueKey: "I2"}}
	require.NoError(t, svc.ConfirmIssueTickets(ctx, 7, refs[:1]))
	require.NoError(t, svc.StartIssueFixTickets(ctx, 7, refs))
	rows, err = svc.ListIssueTickets(ctx, 7)
	require.NoError(t, err)
	for _, r := range rows {
		require.Equal(t, IssueStatusFixing, r.Status)
	}

	// 驳回必须给理由（空理由显式报错，状态不动）。
	err = svc.DismissIssueTickets(ctx, 7, refs[:1], "  ")
	require.Error(t, err, "驳回理由不得为空")
	rows, _ = svc.ListIssueTickets(ctx, 7)
	require.Equal(t, IssueStatusFixing, rows[1].Status, "驳回失败不改状态")
	require.NoError(t, svc.DismissIssueTickets(ctx, 7, refs[:1], "误报，主观差异"))
	rows, _ = svc.ListIssueTickets(ctx, 7)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusDismissed, byKey["I1"].Status)
	require.Equal(t, "误报，主观差异", byKey["I1"].Note)

	// 空选择显式报错。
	require.Error(t, svc.ConfirmIssueTickets(ctx, 7, nil))
}

// TestServiceVerifyIssueTickets 验收面板「复验」按钮：复用增量复验契约（signability-verify
// + 复验回执），回执逐条落工单（verify_receipt/verify_at），通过→verified、
// 未通过→退回待修；终态工单跳过不验。
func TestServiceVerifyIssueTickets(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 审计 P0：复验进度/结束事件应经发射槽发出（验收 swap 接缝，与 emitFixStart 同范式）。
	var vProgress, vDone int
	var vEvMu sync.Mutex
	t.Cleanup(swapEmitVerifyProgress(func(_ context.Context, _ map[string]any) {
		vEvMu.Lock()
		vProgress++
		vEvMu.Unlock()
	}))
	t.Cleanup(swapEmitVerifyDone(func(_ context.Context, _ map[string]any) {
		vEvMu.Lock()
		vDone++
		vEvMu.Unlock()
	}))

	var verifyPrompts []string
	var promptMu sync.Mutex
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-verify" {
			promptMu.Lock()
			verifyPrompts = append(verifyPrompts, input.Prompt)
			promptMu.Unlock()
			return SkillOutput{Text: "```json\n" +
				`{"verified":[{"id":"I1","ok":true,"note":"人设已立"},{"id":"I2","ok":false,"note":"只改了一半"}],"summary":"1 项确认"}` +
				"\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇", Desc: "前三章未立人设", FixType: FixTypeLedger, ChapterIDs: []int64{1, 2, 3}},
		{ID: "I2", Level: "P1", Area: "节奏", Desc: "拖沓", FixType: FixTypeLedger},
		{ID: "I3", Level: "P1", Area: "人设", Desc: "弱", FixType: FixTypeLedger},
	}))
	// I3 已驳回（终态）：不进复验。
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 7, "run-a", "I3", IssueStatusDismissed, "误报"))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 7, "run-a", "I2", IssueStatusFixing, ""))

	sum, err := svc.VerifyIssueTickets(ctx, 7, []IssueTicketRef{
		{RunID: "run-a", IssueKey: "I1"},
		{RunID: "run-a", IssueKey: "I2"},
		{RunID: "run-a", IssueKey: "I3"},
	})
	require.NoError(t, err)
	// 审计 P0：入口只派发后台复验，立即返回已派工单数；I3 终态跳过不派发。
	require.Equal(t, 2, sum.Queued, "I1/I2 进入后台复验队列")
	require.Equal(t, 1, sum.Unverified, "终态工单跳过不验")
	require.Equal(t, 0, sum.Confirmed, "异步入口只回派发数，不回逐项结论")
	require.Contains(t, sum.Detail, "已启动复验")
	// 逐工单定点复验：I1（第1-3章）作用域进复验清单，I3 终态不进；合并所有派发提示词断言。
	promptMu.Lock()
	allPrompts := strings.Join(verifyPrompts, "\n")
	promptMu.Unlock()
	require.Contains(t, allPrompts, `"id":"I1"`, "选中工单进复验清单")
	require.Contains(t, allPrompts, `"chapters":[1,2,3]`, "作用域章节进复验清单")
	require.NotContains(t, allPrompts, `"id":"I3"`, "终态工单不进复验清单")

	rows, lerr := svc.store.ListIssues(ctx, 7, "run-a")
	require.NoError(t, lerr)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusVerified, byKey["I1"].Status)
	require.Equal(t, "人设已立", byKey["I1"].VerifyReceipt)
	require.NotNil(t, byKey["I1"].VerifyAt)
	require.Equal(t, IssueStatusPending, byKey["I2"].Status, "未通过退回待修")
	require.Equal(t, "只改了一半", byKey["I2"].VerifyReceipt)
	require.Equal(t, IssueStatusDismissed, byKey["I3"].Status, "终态不受复验影响")
	require.Empty(t, byKey["I3"].VerifyReceipt)

	// 空选择显式报错。
	_, err = svc.VerifyIssueTickets(ctx, 7, nil)
	require.Error(t, err)

	// 复验进度/结束事件经发射槽发出：每工单一次进度（含 start）+ 一次 done。
	vEvMu.Lock()
	pn, dn := vProgress, vDone
	vEvMu.Unlock()
	require.GreaterOrEqual(t, pn, 1, "复验应发出进度事件")
	require.Equal(t, 1, dn, "复验结束应发出一次 done 事件")
}

// TestServiceVerifyIssueTicketsSkillError 验收复验执行失败（审计 P0 异步语义）：
// 派发入口成功即返回，LLM 失败发生在后台逐工单复验内——单工单未确认 → 复位
// pending 留待下轮，不伪造结论、不整批失败（检测核心：不静默判通过）。
func TestServiceVerifyIssueTicketsSkillError(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{}, assert.AnError
	}
	require.NoError(t, svc.store.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇", Desc: "x", FixType: FixTypeLedger},
	}))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 7, "run-a", "I1", IssueStatusFixing, ""))

	// 审计 P0：入口只派发后台复验，不因后台 LLM 失败报错。
	_, err := svc.VerifyIssueTickets(ctx, 7, []IssueTicketRef{{RunID: "run-a", IssueKey: "I1"}})
	require.NoError(t, err, "派发入口不应因后台 LLM 失败报错")
	rows, lerr := svc.store.ListIssues(ctx, 7, "run-a")
	require.NoError(t, lerr)
	require.Equal(t, IssueStatusPending, rows[0].Status, "复验失败不改工单状态（未确认留待下轮）")
}
