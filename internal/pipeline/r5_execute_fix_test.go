package pipeline

// R5-2 测试迁移载体（提案 §10.3）：S5 检测结果路由收口为「发现项工单」，修复执行改工单
// 被动触发（StartIssueFixTickets → runIssueFixTickets），复验/复评由用户显式触发。
// 本文件覆盖 R5-2 执行路径的关键语义：executeFix 四分区路由、工单修复执行（台账级 /
// 正文级重定向重写 / V 系与写保护退回）、熔断对修复派发的护栏、修复后蓝图巡检。
//
// 工单状态机 / 面板动作 / VerifyIssueTickets 的验收见 issue_tickets_test.go；陈旧体检
// 守卫见 service_test.go 的 TestServiceFixRejectsStaleAudit*；retcon 交回见
// service_test.go 的 TestServiceFixRetconHandoffWhenAllProtected 与
// TestServiceAutoRewriteSkipsProtectedBodyItems。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// startS5Run 把流水线钉在 S5/running 并写入给定审计 JSON（绕过 S1-S4 异步路径以便精确控制）。
// 同时按真实流程把审计清单落为发现项工单（模拟 S4 体检的 ReplaceIssues 契约）——R5-2 的 fix
// 路由对台账级 pending 问题只流转既有工单、不新建，缺少这步会导致 StartIssueFixTickets 找不到工单。
func startS5Run(t *testing.T, svc *Service, novelID int64, auditJSON string) string {
	t.Helper()
	run, err := svc.StartPipeline(context.Background(), novelID, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(context.Background(), run.RunID, StageS5, StatusRunning, string(ctxtJSON)))
	if auditData := parseAudit(auditJSON); len(auditData.Issues) > 0 {
		require.NoError(t, svc.store.ReplaceIssues(context.Background(), novelID, run.RunID, 0, auditData.Issues),
			"startS5Run：模拟体检落发现项工单失败")
	}
	return run.RunID
}

// TestServiceExecuteFixRoutesToTickets 验收 R5-2 四分区路由：台账级→待修工单、正文级→重写
// 工单、V 系文风漂移→重写工单（只报告不改写）、冻结卷写保护→retcon 候选工单；路由落盘后
// 统一交回用户（settleFixToTickets：落 paused + fix_done tickets:true），**全程零自动派发**
// （不派发 signability-fix / chapter-rewrite / 复评，修复动作由人显式触发）。
func TestServiceExecuteFixRoutesToTickets(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(701)

	var dispatched []string
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		dispatched = append(dispatched, name)
		return SkillOutput{Text: "ok"}, nil
	}

	// I1 台账级（active）→ 待修；I2 正文级（active）→ 重写；V1 文风漂移（active）→ 重写（豁免派发）；
	// I3 冻结卷（frozen）→ retcon 候选。
	auditJSON := `{"verdict":"needs_fix","score":12,"summary":"混合清单","issues":[` +
		`{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I2","level":"P0","area":"开篇过签力","desc":"前三章未立人设","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[3]},` +
		`{"id":"V1","level":"P1","area":"文风漂移·章节#4","desc":"第4章文风指标偏离基线","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I3","level":"P1","area":"设定自洽","desc":"与前卷冲突","fix_type":"ledger","target_scope":"frozen_volume","dimension":"character","direction":"retcon_candidate"}]}`

	var fixDonePayload map[string]any
	t.Cleanup(swapEmitFixDone(func(_ context.Context, p map[string]any) { fixDonePayload = p }))

	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID)

	// 零自动派发（修复/重写/复评全由人显式触发）。
	require.Empty(t, dispatched, "executeFix 路由收口不得自动派发任何修复/重写/复评")

	got, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, StageS5, got.CurrentStage)
	require.Equal(t, StatusPaused, got.Status, "路由收口应统一落 paused 交回用户")

	require.NotNil(t, fixDonePayload, "应广播 fix_done")
	require.Equal(t, true, fixDonePayload["tickets"], "fix_done 应带 tickets 标记（去工单面板处理）")

	rows, lerr := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, lerr)
	byKey := map[string]string{}
	for _, r := range rows {
		byKey[r.IssueKey] = r.Status
	}
	require.Equal(t, IssueStatusPending, byKey["I1"], "台账级→待修工单")
	require.Equal(t, IssueStatusRewrite, byKey["I2"], "正文级→重写工单")
	require.Equal(t, IssueStatusRewrite, byKey["V1"], "V 系→重写工单（只报告不改写）")
	require.Equal(t, IssueStatusRetcon, byKey["I3"], "冻结卷→retcon 候选工单")
}

// TestServiceExecuteFixSettleAllResolved 验收断点续路由边界：审计清单全部已解决
// （fixed/rewrite/retcon）时，executeFix 不派发、不假报完成，统一收口（total=0）落 paused +
// tickets:true，提示「已全部收口」。
func TestServiceExecuteFixSettleAllResolved(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(702)

	var dispatched []string
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		dispatched = append(dispatched, name)
		return SkillOutput{Text: "ok"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"成稿质量","desc":"x","fix_type":"ledger","target_scope":"active_volume"}]}`
	run, rerr := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, rerr)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4, FixStates: map[string]string{"I1": "fixed"}}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning, string(ctxtJSON)))

	var fixDonePayload map[string]any
	t.Cleanup(swapEmitFixDone(func(_ context.Context, p map[string]any) { fixDonePayload = p }))

	svc.executeFix(run.RunID, novelID)

	require.Empty(t, dispatched, "全已解决不得派发")
	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StatusPaused, got.Status)
	require.NotNil(t, fixDonePayload)
	require.Equal(t, true, fixDonePayload["tickets"], "收口仍带 tickets 标记")
	require.Contains(t, fmt.Sprint(fixDonePayload["error"]), "已全部收口", "全收口应提示去面板查看历史工单")
}

// TestServiceRunIssueFixTicketsLedgerFix 验收工单修复执行体（#240）：台账级待修工单经
// StartIssueFixTickets 显式触发 → runIssueFixTickets → runLedgerFixTickets 派发 signability-fix
// 修复，回执落工单 fixed；修复轮数 +1。
func TestServiceRunIssueFixTicketsLedgerFix(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(703)

	var fixed []string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			return SkillOutput{Text: "已修复。\n```json\n{\"fixed\":[\"I1\"],\"skipped\":[]}\n```"}, nil
		case "blueprint-drift-check":
			fixed = append(fixed, "drift") // 仅记录，不校验
			return SkillOutput{Text: "ok"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID) // 路由 I1 → 待修工单

	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID, []IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	rows, lerr := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, lerr)
	require.Len(t, rows, 1)
	require.Equal(t, IssueStatusFixed, rows[0].Status, "工单修复执行后应落 fixed")

	got, gerr := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, gerr)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Equal(t, 1, finalCtxt.FixRounds, "工单修复收口应令 FixRounds +1")
}

// TestServiceRunIssueFixTicketsBodyRewrite 验收正文级工单修复：定位真实章节后逐章派发
// chapter-rewrite（整章重写），成功→fixed。
func TestServiceRunIssueFixTicketsBodyRewrite(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(704)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	for i := 1; i <= 5; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: novelID, Title: fmt.Sprintf("第%d章", i), SortOrder: i,
		}).Error)
	}
	seeded, err := svc.chapterStore.ListAllByNovel(ctx, nil, novelID)
	require.NoError(t, err)
	readingToID := map[int]int64{}
	for _, ch := range seeded {
		readingToID[ch.ReadingNumber] = ch.ID
	}

	var rewriteChapterID int64
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "chapter-rewrite":
			rewriteChapterID = extractChapterIDFromPrompt(t, input.Prompt)
			return SkillOutput{Text: "已整章重写"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	auditJSON := `{"verdict":"needs_fix","score":15,"summary":"正文问题","issues":[{"id":"I1","level":"P0","area":"开篇过签力","desc":"第3章未立人设","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[3]}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID) // 路由 I1 → 重写工单

	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID, []IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	require.Equal(t, readingToID[3], rewriteChapterID, "应重写第 3 章（按 chapter_ids 定位）")
	rows, lerr := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, lerr)
	require.Equal(t, IssueStatusFixed, rows[0].Status, "重写成功应落 fixed")
}

// TestServiceRunIssueFixTicketsBouncesStyleAndFrozen 验收工单修复执行体的安全分流：
// V 系文风漂移与冻结卷写保护工单绝不派发，退回原状态留 note（只报告不改写 / 人工裁决）；
// 其余台账级工单照常修复。
func TestServiceRunIssueFixTicketsBouncesStyleAndFrozen(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(705)

	var rewriteCalled bool
	var ledgerTargets []string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			if contains := func() bool {
				for _, k := range []string{"I3", "V1"} {
					if len(input.Prompt) > 0 && jsonContainsIssue(input.Prompt, k) {
						return true
					}
				}
				return false
			}; contains() {
				t.Errorf("V 系/写保护工单不得进修复批次")
			}
			ledgerTargets = append(ledgerTargets, "I1")
			return SkillOutput{Text: "已修复。\n```json\n{\"fixed\":[\"I1\"],\"skipped\":[]}\n```"}, nil
		case "chapter-rewrite":
			rewriteCalled = true // 不应被调用（V 系与写保护均不派发重写）
			return SkillOutput{Text: "已整章重写"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	auditJSON := `{"verdict":"needs_fix","score":12,"summary":"混合","issues":[` +
		`{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"V1","level":"P1","area":"文风漂移·章节#4","desc":"第4章文风偏离","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix"},` +
		`{"id":"I3","level":"P1","area":"设定自洽","desc":"与前卷冲突","fix_type":"ledger","target_scope":"frozen_volume","dimension":"character","direction":"retcon_candidate"}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID) // I1→待修(pending)；V1→重写(rewrite)；I3→retcon（终态，不可进修复）

	// 台账级 I1 与 V 系 V1 送「去修复」（rewrite→fixing 合法）；I3 是 retcon 终态，
	// 状态机拒绝 retcon→fixing，本就不该进修复批次——留待人工 retcon 裁决。
	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID, []IssueTicketRef{
		{RunID: runID, IssueKey: "I1"},
		{RunID: runID, IssueKey: "V1"},
	}))

	require.False(t, rewriteCalled, "V 系/写保护工单不得派发 chapter-rewrite")
	require.Equal(t, []string{"I1"}, ledgerTargets, "仅台账级 I1 进修复批次")

	rows, lerr := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, lerr)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusFixed, byKey["I1"].Status, "I1 修复落 fixed")
	require.Equal(t, IssueStatusRewrite, byKey["V1"].Status, "V 系退回重写态（只报告不改写，绝不派发重写）")
	require.NotEmpty(t, byKey["V1"].SkipReason, "V 系退回应留说明（落 skip_reason 列）")
	require.Equal(t, IssueStatusRetcon, byKey["I3"].Status, "冻结卷保持 retcon 态（人工裁决，绝不进修复批次）")
}

// jsonContainsIssue 简易判定：提示词里是否提及该 issue id（用于断言安全分流）。
func jsonContainsIssue(prompt, id string) bool {
	return len(prompt) > 0 && containsStr(prompt, `"id":"`+id+`"`)
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestProviderTripFixDispatchGuarded 验收熔断护栏覆盖工单修复派发点：熔断触发时
// StartIssueFixTickets 派发的 runIssueFixTickets 绝不派发任何 agent（零 skillRunner 调用），
// 待修工单标记 skipped（不谎报完成、不空转）。
func TestProviderTripFixDispatchGuarded(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(706)

	forceProviderTrip(t)
	calls := countSkillCalls(svc)

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID) // 路由 I1 → 待修工单

	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID, []IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	require.Zero(t, *calls, "熔断触发必须零 agent 派发（含工单修复批次与蓝图巡检）")
	rows, lerr := svc.store.ListIssues(ctx, novelID, runID)
	require.NoError(t, lerr)
	require.Equal(t, IssueStatusSkipped, rows[0].Status, "熔断下待修工单应标记 skipped（不谎报完成）")
}

// TestServiceExecuteChapterRewriteTripGuarded 验收熔断护栏覆盖章节重写派发点：
// executeChapterRewrite 在熔断下直接返回 errProviderTripped，零 agent 派发。
func TestServiceExecuteChapterRewriteTripGuarded(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(707)

	forceProviderTrip(t)
	calls := countSkillCalls(svc)

	err := svc.executeChapterRewrite(ctx, "run-x", novelID, RewriteItem{ID: "I1"}, chapter.Chapter{ID: 1})
	require.ErrorIs(t, err, errProviderTripped, "熔断下章节重写应返回哨兵错误（整循环中断语义）")
	require.Zero(t, *calls, "熔断触发必须零 agent 派发（含章节重写）")
}

// TestServiceTicketFixDriftPatrolAfterFix 验收 G3：工单修复执行体收口后自动触发一次蓝图
// 一致性巡检（只读非阻断），结论写入 DriftReport。
func TestServiceTicketFixDriftPatrolAfterFix(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(708)
	svc.chapterCount = func(_ context.Context, _ int64) (int, error) { return 8, nil } // 有已写章节才巡检

	var driftCalls int
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			return SkillOutput{Text: "已修复。\n```json\n{\"fixed\":[\"I1\"],\"skipped\":[]}\n```"}, nil
		case "blueprint-drift-check":
			driftCalls++
			return SkillOutput{Text: "巡检：\n```json\n{\"verdict\":\"needs_fix\",\"score\":0," +
				"\"summary\":\"发现 2 章与卷纲冲突\",\"issues\":[" +
				"{\"id\":\"D1\",\"level\":\"P1\",\"area\":\"章节#12\",\"desc\":\"卷纲承诺的反转未出现\"}]}\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[{"id":"I1","level":"P0","area":"成稿质量","desc":"开场灌水","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix"}]}`
	runID := startS5Run(t, svc, novelID, auditJSON)
	svc.executeFix(runID, novelID)

	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID, []IssueTicketRef{{RunID: runID, IssueKey: "I1"}}))

	require.Equal(t, 1, driftCalls, "工单修复收口后应恰好触发一次蓝图巡检")

	got, gerr := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, gerr)
	var finalCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &finalCtxt))
	require.Contains(t, finalCtxt.DriftReport, "蓝图巡检", "巡检结论应落库 DriftReport")
}
