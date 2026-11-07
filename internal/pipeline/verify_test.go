package pipeline

// R1c 验收：增量复验 + skip 即转人工工单（提案 §4.3-4/5）。
//  ① 被 skip 的工程级问题立即转人工工单（ManualBacklog），供 G4 summary 列出与全 skip 收口消费；
//  ② 增量复验只验 fixed 项及修过作用域（L1），全量复评仅存 volume/final/蓝图收口；
//  ③ 复验落地不整体清 FixStates/FixLog（与全量体检清零语义区分）。
//
// R5-2 迁移：复验入口由 executeVerify 改为工单面板 VerifyIssueTickets（增量复验契约单源，
// 见 issue_tickets_test.go 的 TestServiceVerifyIssueTickets 系列）；本文件保留「复验回执解析」
// 「skip→ManualBacklog」「去修复全 skip 不复验/不复评」「无回执兼容回退」的执行路径验收。

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseVerifyReceipt 验证复验回执解析：围栏 JSON（嵌套 verified 对象）、纯文本包围、
// 旧/缺失回执、体检形状输出（不误认回执）。
func TestParseVerifyReceipt(t *testing.T) {
	text := "复验完成。\n```json\n{\"verified\":[{\"id\":\"I1\",\"ok\":true,\"note\":\"钩子已补\"}," +
		"{\"id\":\"I2\",\"ok\":false,\"note\":\"只改了一半\"}],\"summary\":\"1 项确认\"}\n```"
	results, summary, found, partial := parseVerifyReceipt(text)
	require.True(t, found)
	require.False(t, partial, "完整回执不属截断")
	require.True(t, results["I1"].OK)
	require.Equal(t, "钩子已补", results["I1"].Note)
	require.False(t, results["I2"].OK)
	require.Equal(t, "1 项确认", summary)

	// 无围栏纯文本 + 旧回执无 summary 字段照常解析。
	results2, _, found2, _ := parseVerifyReceipt(`概况 {"verified":[{"id":"I9","ok":false,"note":"x"}]}`)
	require.True(t, found2)
	require.False(t, results2["I9"].OK)

	// 体检形状输出（verdict/issues）不得误认成复验回执。
	_, _, found3, _ := parseVerifyReceipt(`{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`)
	require.False(t, found3)

	// 完全缺失回执：found=false，调用方走「已修复确认」兼容回退。
	_, _, found4, _ := parseVerifyReceipt("我复验过了，没有 JSON。")
	require.False(t, found4)
}

// TestParseVerifyReceiptTruncatedSalvage 验证截断回执抢救（2026-11-06 I9-I12 事故形状）：
// thinking 吃满 token 预算致回执被硬切在半条 JSON——完整条目照用（宁缺勿错），残缺条目
// 及其后不取；partial=true 区分「截断缺失」与「未提及」。
func TestParseVerifyReceiptTruncatedSalvage(t *testing.T) {
	// 真实事故形状：3 条完整 + 第 4 条断在 note 中途，外层数组不闭合。
	text := "逐项定点核验完成。\n```json\n" +
		`{"verified":[{"id":"I12","ok":true,"note":"第10章已就近补交代"},` +
		`{"id":"I11","ok":true,"note":"第7章首次出现处已补命名"},` +
		`{"id":"I10","ok":true,"note":"第5章棚中议论已改为单数"},` +
		`{"id":"I9","ok":true,"note":"第8章`
	results, summary, found, partial := parseVerifyReceipt(text)
	require.True(t, found, "截断回执的完整条目应被抢救")
	require.True(t, partial, "回执被截断须如实标注")
	require.Empty(t, summary, "外层 summary 随截断丢失")
	require.True(t, results["I12"].OK)
	require.Equal(t, "第10章已就近补交代", results["I12"].Note)
	require.True(t, results["I11"].OK)
	require.True(t, results["I10"].OK)
	_, hasI9 := results["I9"]
	require.False(t, hasI9, "残缺条目不取（宁缺勿错）")

	// 外层 summary 损坏但 verified 数组完整：条目齐全、partial=false。
	text2 := `{"verified":[{"id":"I1","ok":false,"note":"修歪"}],"summary":"他说 "oops""}`
	results2, _, found2, partial2 := parseVerifyReceipt(text2)
	require.True(t, found2)
	require.False(t, partial2, "数组完整仅 summary 损坏不算截断")
	require.False(t, results2["I1"].OK)

	// 只有半条：抢救不出任何完整条目 → found=false 走整批未确认。
	_, _, found3, _ := parseVerifyReceipt(`{"verified":[{"id":"I1","ok":true,"note":"断`)
	require.False(t, found3)

	// 条目对象（无 verified 键）不触发抢救、不误认回执。
	_, _, found4, _ := parseVerifyReceipt(`{"id":"I1","ok":true,"note":"断`)
	require.False(t, found4)
}

// TestApplyFixReceiptManualBacklog 验证 skip 即转人工工单：skipped 连原因沉淀进
// ManualBacklog（按 id upsert），供 G4 summary 列出与全 skip 收口消费。
func TestApplyFixReceiptManualBacklog(t *testing.T) {
	batch := []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏", Desc: "a", ChapterIDs: []int64{3}},
		{ID: "I2", Level: "P0", Area: "设定自洽", Desc: "整池悬念治理"},
	}
	out := `{"fixed":["I1"],"skipped":[{"id":"I2","reason":"scope exceeds single-batch budget"}]}`
	var ctxt GateContext
	fixedNow, skippedNow := applyFixReceipt(&ctxt, batch, out, slog.Default())
	require.Equal(t, []string{"I1"}, fixedNow)
	require.Equal(t, "scope exceeds single-batch budget", skippedNow["I2"])
	require.Equal(t, []ManualItem{{
		ID: "I2", Level: "P0", Area: "设定自洽", Desc: "整池悬念治理",
		Reason: "scope exceeds single-batch budget",
	}}, ctxt.ManualBacklog, "skipped 应连原因沉淀进人工工单")

	// 同 id 再次 skip：upsert 覆盖不重复。
	out2 := `{"fixed":[],"skipped":[{"id":"I2","reason":"更新后的原因"}]}`
	applyFixReceipt(&ctxt, batch, out2, slog.Default())
	require.Len(t, ctxt.ManualBacklog, 1)
	require.Equal(t, "更新后的原因", ctxt.ManualBacklog[0].Reason)
}

// TestServiceIssueFixAllSkipsToManualBacklog 验收「去修复全 skip 不复验/不复评」（F2 尾部空转根治）：
// 工单去修复时 signability-fix 全 skip → 工单 skip 断点保留、转人工工单（ManualBacklog）；
// R5-2 摘除自动复评，故修复收口不复评（verifyCalls==0）、不复评（auditCalls==0）。
func TestServiceIssueFixAllSkipsToManualBacklog(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var fixCalls, auditCalls int
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			fixCalls++
			return SkillOutput{Text: "修复回执：\n```json\n" +
				`{"fixed":[],"skipped":[{"id":"I1","reason":"工程级问题超出单批预算"}]}` + "\n```"}, nil
		case "signability-audit", "blueprint-audit":
			auditCalls++
			return SkillOutput{Text: "ok"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P0","area":"长线牵引","desc":"整池悬念治理","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	runID := startS5Run(t, svc, 406, auditJSON)
	svc.executeFix(runID, 406) // 路由 I1 → 待修工单

	require.NoError(t, svc.StartIssueFixTickets(ctx, 406, []IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	require.Equal(t, 1, fixCalls)
	require.Equal(t, 0, auditCalls, "R5-2 无自动复评（修复动作由人显式触发，完成后显式点复验）")
	rows, lerr := svc.store.ListIssues(ctx, 406, runID)
	require.NoError(t, lerr)
	require.Len(t, rows, 1)
	require.Equal(t, IssueStatusSkipped, rows[0].Status, "skip 断点保留，不再重试")
	require.Equal(t, "工程级问题超出单批预算", rows[0].SkipReason, "人工工单应带无法修复原因（落 skip_reason 列）")

	got, gerr := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, gerr)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Len(t, finalCtxt.ManualBacklog, 1, "skip 应转人工工单")
	require.Equal(t, "工程级问题超出单批预算", finalCtxt.ManualBacklog[0].Reason, "人工工单应带原因")
}

// TestServiceVerifyIssueTicketsNoReceiptCompat 验收 P2 修正：复验输出缺回执时**不再静默全过**，
// 工单状态复位 pending 留待下轮（审计 P2：静默全过是最大隐患，必须显式暴露未确认）。
func TestServiceVerifyIssueTicketsNoReceiptCompat(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "我复验过了，都没问题。"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第3章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	run, err := svc.StartPipeline(ctx, 409, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.store.ReplaceIssues(ctx, 409, run.RunID, 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏爽点", Desc: "第3章拖沓", FixType: FixTypeLedger},
	}))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 409, run.RunID, "I1", IssueStatusFixing, ""))

	_, err = svc.VerifyIssueTickets(ctx, 409, []IssueTicketRef{{RunID: run.RunID, IssueKey: "I1"}})
	require.NoError(t, err)

	rows, lerr := svc.store.ListIssues(ctx, 409, run.RunID)
	require.NoError(t, lerr)
	require.Equal(t, IssueStatusPending, rows[0].Status, "P2：回执未解析不再静默全过，工单复位 pending 留待下轮")
	require.Contains(t, rows[0].Note, "复验回执未解析", "未确认原因须写入可见 note 列，否则前端静默无回执")
}

// TestServiceVerifyIssueTicketsTruncatedReceipt 验收截断回执的批量层行为（2026-11-06
// I9-I12 事故回归）：回执被 token 上限硬切时，完整条目照常落 verified，残缺条目复位
// pending 并以「截断」口径措辞（区分未提及），绝不整批丢判定。
func TestServiceVerifyIssueTicketsTruncatedReceipt(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 真实事故形状：I1 完整、I2 断在 note 中途。
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "```json\n" +
			`{"verified":[{"id":"I1","ok":true,"note":"已修复"},` +
			`{"id":"I2","ok":true,"note":"断在半` + "\n```"}, nil
	}

	run, err := svc.StartPipeline(ctx, 410, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))
	require.NoError(t, svc.store.ReplaceIssues(ctx, 410, run.RunID, 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏爽点", Desc: "第3章拖沓", FixType: FixTypeLedger},
		{ID: "I2", Level: "P1", Area: "人设", Desc: "动机缺失", FixType: FixTypeLedger},
	}))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 410, run.RunID, "I1", IssueStatusFixing, ""))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, 410, run.RunID, "I2", IssueStatusFixing, ""))

	_, err = svc.VerifyIssueTickets(ctx, 410, []IssueTicketRef{
		{RunID: run.RunID, IssueKey: "I1"}, {RunID: run.RunID, IssueKey: "I2"},
	})
	require.NoError(t, err)

	rows, lerr := svc.store.ListIssues(ctx, 410, run.RunID)
	require.NoError(t, lerr)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusVerified, byKey["I1"].Status, "截断回执中的完整条目照常落已复验")
	require.Equal(t, IssueStatusPending, byKey["I2"].Status, "残缺条目复位 pending 留待下轮")
	require.Contains(t, byKey["I2"].Note, "截断", "残缺条目按截断口径措辞（区别于未提及）")
}

// TestManualBacklogSummary 验证人工工单摘要渲染（列表截断 + 原因）。
func TestManualBacklogSummary(t *testing.T) {
	require.Empty(t, manualBacklogSummary(nil))
	s := manualBacklogSummary([]ManualItem{
		{ID: "I1", Area: "长线牵引", Desc: "整池悬念治理", Reason: "超预算"},
		{ID: "I2", Area: "人设卖点", Desc: "配角弱", Reason: "需重写"},
		{ID: "I3", Area: "节奏", Desc: "拖沓", Reason: "x"},
		{ID: "I4", Area: "合规", Desc: "口径", Reason: "y"},
	})
	require.Contains(t, s, "4 项已转人工工单")
	require.Contains(t, s, "整池悬念治理（超预算）")
	require.Contains(t, s, "等")
	require.False(t, strings.Contains(s, "合规：口径"), "列表只展开前 3 条")
}
