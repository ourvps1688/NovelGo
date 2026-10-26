package pipeline

// R1c 验收：增量复验 + skip 即转人工工单（提案 §4.3-4/5）。
//  ① 被 skip 的工程级问题立即转人工工单（ManualBacklog），G4 summary 列出，全 skip 不复验；
//  ② 增量复验只验 fixed 项及修过作用域（L1），全量复评仅存 volume/final/蓝图收口；
//  ③ 复验落地不整体清 FixStates/FixLog（与全量体检清零语义区分）。

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
	results, summary, found := parseVerifyReceipt(text)
	require.True(t, found)
	require.True(t, results["I1"].OK)
	require.Equal(t, "钩子已补", results["I1"].Note)
	require.False(t, results["I2"].OK)
	require.Equal(t, "1 项确认", summary)

	// 无围栏纯文本 + 旧回执无 summary 字段照常解析。
	results2, _, found2 := parseVerifyReceipt(`概况 {"verified":[{"id":"I9","ok":false,"note":"x"}]}`)
	require.True(t, found2)
	require.False(t, results2["I9"].OK)

	// 体检形状输出（verdict/issues）不得误认成复验回执。
	_, _, found3 := parseVerifyReceipt(`{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`)
	require.False(t, found3)

	// 完全缺失回执：found=false，调用方走「已修复确认」兼容回退。
	_, _, found4 := parseVerifyReceipt("我复验过了，没有 JSON。")
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

// TestServiceAllSkipsHandbackNoVerify 验收「全 skip 不复验」（F2 尾部空转根治）：
// 本轮修复零修复确认、问题全部转人工时，不派发任何复验/复评，直接落 G4 闸门交回用户，
// summary 列出人工工单。
func TestServiceAllSkipsHandbackNoVerify(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	captured := withCapturedGates(t)

	var fixCalls, verifyCalls, auditCalls int
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			fixCalls++
			return SkillOutput{Text: `修复回执：` + "\n```json\n" +
				`{"fixed":[],"skipped":[{"id":"I1","reason":"工程级问题超出单批预算"}]}` + "\n```"}, nil
		case "signability-verify":
			verifyCalls++
			return SkillOutput{Text: "ok"}, nil
		case "signability-audit", "blueprint-audit":
			auditCalls++
			return SkillOutput{Text: "ok"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P0","area":"长线牵引","desc":"整池悬念治理","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	run, err := svc.StartPipeline(ctx, 406, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	require.Equal(t, 1, fixCalls)
	require.Equal(t, 0, verifyCalls, "全 skip 不复验（做了也只会把修不了的问题再端出来空转）")
	require.Equal(t, 0, auditCalls, "全 skip 不全量重审")
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StageS4, got.CurrentStage, "人工工单闸门落在体检阶段")
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Equal(t, "skipped", finalCtxt.FixStates["I1"], "skip 断点保留，不再重试")
	require.Equal(t, "工程级问题超出单批预算", finalCtxt.ManualBacklog[0].Reason, "人工工单应带无法修复原因")
	gates := captured.all()
	require.NotEmpty(t, gates)
	last := gates[len(gates)-1]
	require.Equal(t, GateG4, last.GateID)
	require.Contains(t, last.Summary, "转人工工单", "G4 summary 应列出人工工单")
	require.Contains(t, last.Summary, "整池悬念治理")
	require.Contains(t, last.Summary, "工程级问题超出单批预算", "人工工单应带原因")
}

// TestServiceVerifyFixedOnlyScopedNoWholesaleClear 验收增量复验（L1）核心语义：
// 只验 fixed 项及其修过作用域（skipped/pending 不进复验清单）；落地不整体清
// FixStates/FixLog；复验确认的销项并从体检清单摘除。
func TestServiceVerifyFixedOnlyScopedNoWholesaleClear(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	var verifyPromptText string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-verify" {
			verifyPromptText = input.Prompt
			return SkillOutput{Text: "```json\n" +
				`{"verified":[{"id":"I1","ok":true,"note":"第3章节奏已收紧"}],"summary":"1 项修复已确认"}` +
				"\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第3章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[3]},` +
		`{"id":"I2","level":"P0","area":"长线牵引","desc":"整池悬念治理","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I3","level":"P1","area":"人设卖点","desc":"配角记忆点弱","fix_type":"ledger","target_scope":"active_volume","dimension":"character","direction":"forward_fix"}]}`
	run, err := svc.StartPipeline(ctx, 407, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{
		Audit: auditJSON, AuditStage: StageS4, VerifyPending: true,
		FixStates:     map[string]string{"I1": "fixed", "I2": "skipped"},
		ManualBacklog: []ManualItem{{ID: "I2", Level: "P0", Area: "长线牵引", Desc: "整池悬念治理", Reason: "工程级问题"}},
		FixLog:        []FixLogEntry{{ID: "I1", Note: "重排了第3章节奏"}},
	}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	svc.executeVerify(run.RunID, 407)

	// 只验 fixed 项及其修过作用域：I1 进清单（含作用域与修复摘要），I2/I3 绝不进。
	require.Contains(t, verifyPromptText, `"id":"I1"`, "fixed 项应进复验清单")
	require.Contains(t, verifyPromptText, "重排了第3章节奏", "修复摘要应注入复验清单")
	require.Contains(t, verifyPromptText, `"chapters":[3]`, "修过的作用域章节应注入")
	require.NotContains(t, verifyPromptText, `"id":"I2"`, "skipped（转人工）项不复验")
	require.NotContains(t, verifyPromptText, `"id":"I3"`, "pending 项不复验（不在已修复清单）")

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StatusAwaitingGate, got.Status)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Equal(t, "verified", finalCtxt.FixStates["I1"], "复验确认应销项")
	require.Equal(t, "skipped", finalCtxt.FixStates["I2"], "复验落地不整体清 FixStates（skipped 断点保留）")
	require.Equal(t, []FixLogEntry{{ID: "I1", Note: "重排了第3章节奏"}}, finalCtxt.FixLog,
		"复验落地不整体清 FixLog")
	require.Equal(t, "工程级问题", finalCtxt.ManualBacklog[0].Reason, "人工工单不受复验影响")
	require.False(t, finalCtxt.VerifyPending, "复验落地应清除复验标记")
	require.NotContains(t, finalCtxt.Audit, `"id":"I1"`, "确认修复的问题应从体检清单摘除")
	require.Contains(t, finalCtxt.Audit, `"id":"I3"`, "未处理的问题保留在清单")
	require.Contains(t, finalCtxt.Audit, "增量复验（L1）：确认修复 1 项", "复验结论应落进体检摘要")
}

// TestServiceVerifyRejectedBackToPending 验收复验未通过的条目退回待修（不留假销项），
// 且回执未提及的条目保持 fixed 留待下轮再验。
func TestServiceVerifyRejectedBackToPending(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-verify" {
			return SkillOutput{Text: "```json\n" +
				`{"verified":[{"id":"I1","ok":false,"note":"只改了一半"}],"summary":"未通过"}` +
				"\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第3章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"人设卖点","desc":"配角弱","fix_type":"ledger","target_scope":"active_volume","dimension":"character","direction":"forward_fix"}]}`
	run, err := svc.StartPipeline(ctx, 408, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{
		Audit: auditJSON, AuditStage: StageS4,
		FixStates: map[string]string{"I1": "fixed", "I2": "fixed"},
	}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	// 先落 issue 工单行并置 fixed（模拟修复回执已同步），供复验回退断言行级流转。
	issueRows := []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏爽点", Desc: "第3章拖沓"},
		{ID: "I2", Level: "P1", Area: "人设卖点", Desc: "配角弱"},
	}
	require.NoError(t, svc.store.ReplaceIssues(ctx, 408, run.RunID, 0, issueRows))
	for _, it := range issueRows {
		require.NoError(t, svc.store.UpdateIssueStatus(ctx, 408, run.RunID, it.ID, IssueStatusFixed, ""))
	}

	svc.executeVerify(run.RunID, 408)

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.NotContains(t, finalCtxt.FixStates, "I1", "复验未通过应退回待修（不留假销项）")
	require.Equal(t, "fixed", finalCtxt.FixStates["I2"], "回执未提及的保持 fixed 留待下轮再验")
	require.Contains(t, finalCtxt.Audit, `"id":"I1"`, "未通过的保留在体检清单")
	require.Contains(t, finalCtxt.Audit, "1 项未通过复验已退回待修", "复验结论应落进体检摘要")

	rows, lerr := svc.store.ListIssues(ctx, 408, run.RunID)
	require.NoError(t, lerr)
	require.Len(t, rows, 2)
	byKey := map[string]string{}
	for _, r := range rows {
		byKey[r.IssueKey] = r.Status
		if r.IssueKey == "I1" {
			// R5-1 复验回执落工单：未通过也留一行结论（verify_receipt/verify_at）。
			require.Equal(t, "只改了一半", r.VerifyReceipt, "复验回执应落工单行")
			require.NotNil(t, r.VerifyAt)
		}
	}
	require.Equal(t, IssueStatusPending, byKey["I1"], "复验未通过的工单行应同步回 pending")
	require.Equal(t, IssueStatusFixed, byKey["I2"], "回执未提及的工单行保持 fixed 留待下轮")
}

// TestServiceVerifyNoReceiptCompat 验收兼容回退：复验输出缺回执时按已修复确认处理
// （与修复回执缺省口径一致，不卡死闭环）。
func TestServiceVerifyNoReceiptCompat(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "我复验过了，都没问题。"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第3章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	run, err := svc.StartPipeline(ctx, 409, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4,
		FixStates: map[string]string{"I1": "fixed"}}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	svc.executeVerify(run.RunID, 409)

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Equal(t, "verified", finalCtxt.FixStates["I1"], "无回执兼容回退按已修复确认处理")
	require.Equal(t, StatusAwaitingGate, got.Status)
}

// TestServicePostFixReassessRouting 验收复评口径路由（R1c）：卷收口/完本/蓝图收口仍全量
// 复评（L2 深审），其余走增量复验（L1）。
func TestServicePostFixReassessRouting(t *testing.T) {
	cases := []struct {
		name       string
		reason     string
		kind       string
		auditStage string
		wantSkill  string
	}{
		{"golden3 → 增量复验", "golden3", "", StageS4, "signability-verify"},
		{"milestone → 增量复验", "milestone", "", StageS4b, "signability-verify"},
		{"缺省口径 → 增量复验", "", "", StageS4, "signability-verify"},
		{"volume → 全量复评", "volume", "", StageS4b, "signability-audit"},
		{"final → 全量复评", "final", "", StageS6, "signability-audit"},
		{"蓝图 → 全量复评", "", "blueprint", StageS2, "blueprint-audit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
			ctx := context.Background()

			var dispatched []string
			svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
				dispatched = append(dispatched, name)
				switch name {
				case "signability-verify":
					return SkillOutput{Text: `{"verified":[{"id":"I1","ok":true,"note":"x"}]}`}, nil
				default:
					return SkillOutput{Text: `{"verdict":"pass","score":30,"summary":"通过","issues":[]}`}, nil
				}
			}

			auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
				`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第3章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
			run, err := svc.StartPipeline(ctx, 410, "new", nil, false)
			require.NoError(t, err)
			ctxt := GateContext{
				Audit: auditJSON, BlueprintAudit: auditJSON, AuditStage: tc.auditStage,
				AuditReason: tc.reason, AuditKind: tc.kind,
				FixStates: map[string]string{"I1": "fixed"},
			}
			ctxtJSON, merr := json.Marshal(ctxt)
			require.NoError(t, merr)
			// 修复收口前置 S5 running（finishFixAndReaudit 的守卫口径）。
			require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning, string(ctxtJSON)))

			svc.finishFixAndReaudit(ctx, run.RunID, 410, ctxt)

			require.NotEmpty(t, dispatched, "修复收口必须派发复评/复验")
			require.Equal(t, tc.wantSkill, dispatched[len(dispatched)-1],
				"复评口径路由错误：最后派发技能应为 %s（实际 %v）", tc.wantSkill, dispatched)
		})
	}
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
