package pipeline

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ── issueChapterIDs：结构化作用域优先，正则降兜底（F5 兼容读）────────────────

// TestIssueChapterIDsPrefersStructured 验证取值优先级：契约结构化字段优先；
// 空时回退 area/desc 正则（旧数据/LLM 未输出）；全空返回 nil。
func TestIssueChapterIDsPrefersStructured(t *testing.T) {
	structured := AuditIssue{ID: "I1", Area: "节奏爽点", Desc: "第6-10章爽点断档",
		ChapterIDs: []int64{6, 7, 8, 9, 10}}
	require.Equal(t, []int64{6, 7, 8, 9, 10}, issueChapterIDs(structured), "结构化字段应优先")

	// 结构化与文本冲突时仍以结构化为准（文本只是展示标签，不再是定位依据）。
	conflict := AuditIssue{ID: "I2", Area: "文风漂移·章节#12", Desc: "第3章跑偏", ChapterIDs: []int64{12}}
	require.Equal(t, []int64{12}, issueChapterIDs(conflict), "结构化字段与文本冲突时以结构化为准")

	fallback := AuditIssue{ID: "I3", Area: "节奏爽点", Desc: "第6-10章爽点断档"}
	require.Equal(t, []int64{6, 7, 8, 9, 10}, issueChapterIDs(fallback), "无结构化字段应回退正则兜底")

	// 台账引用 <type>#<id>（如 timeline#42）是台账实体ID，不是章节号——
	// id 为台账表自增主键，常达上千量级，回归见 fix 4c87ef37。解析结果应为空，
	// ledger ref 仅保留在 desc 文本里，绝不污染 chapter_ids。
	ledger := AuditIssue{ID: "I4", Area: "长线牵引", Desc: "timeline#42 伏笔未回收"}
	require.Empty(t, issueChapterIDs(ledger), "台账引用 <type>#<id> 不应被误判为章节")

	empty := AuditIssue{ID: "I5", Area: "总体", Desc: "没有具体章节"}
	require.Empty(t, issueChapterIDs(empty), "无章节线索应为空")
}

// ── 工单落表：整批替换 / 状态流转 ─────────────────────────────────────────

// TestStoreReplaceIssuesAndStatus 验证工单表核心语义：新体检整批替换（跨轮同号
// 不共用断点）、结构化作用域落列、状态流转连 skipped 原因一并留痕。
func TestStoreReplaceIssuesAndStatus(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	issues := []AuditIssue{
		{ID: "I1", Level: "P0", Area: "开篇过签力", Desc: "前三章未立人设",
			FixType: FixTypeBody, TargetScope: TargetScopeActiveVolume,
			Dimension: DimensionNone, Direction: DirectionForwardFix, ChapterIDs: []int64{1, 2, 3}},
		{ID: "I2", Level: "P1", Area: "长线牵引", Desc: "timeline#42 伏笔未回收",
			FixType: FixTypeLedger, TargetScope: TargetScopeActiveVolume,
			Dimension: "timeline", Direction: DirectionForwardFix},
	}
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 1, issues))

	rows, err := s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "I1", rows[0].IssueKey)
	require.Equal(t, IssueStatusPending, rows[0].Status)
	require.JSONEq(t, "[1,2,3]", rows[0].ChapterIDsJSON, "结构化章节作用域应落列")
	require.Empty(t, decodeIssueChapterIDs(rows[1].ChapterIDsJSON), "台账型 issue 不应将 <type>#<id> 误判为章节落列")
	require.Equal(t, 1, rows[0].Round)

	// 状态流转：skipped 连原因留痕。
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I1", IssueStatusFixed, ""))
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "I2", IssueStatusSkipped, "scope exceeds budget"))
	rows, err = s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Equal(t, IssueStatusFixed, rows[0].Status)
	require.Equal(t, IssueStatusSkipped, rows[1].Status)
	require.Equal(t, "scope exceeds budget", rows[1].SkipReason)

	// 新一轮体检整批替换：旧断点不残留（同号 I1 是新问题，回到 pending）。
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 2, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏", Desc: "复评新发现"},
	}))
	rows, err = s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Len(t, rows, 1, "整批替换后旧工单不应残留")
	require.Equal(t, IssueStatusPending, rows[0].Status)
	require.Empty(t, rows[0].SkipReason)
	require.Equal(t, 2, rows[0].Round)
}

// TestReplaceIssuesByPrefixKeepsWorkflowState 验证 E 系换血保留用户流转状态（#100 主修复）：
// 章保存写后链路反复刷新本章发现时，已修/已驳回等状态与复验/备注留痕不得被冲回 pending——
// 同键同 Area（同一发现）整组继承；键平移（位次键随发现集变化）走 Area 兜底继承；
// 真正的新发现才落 pending；消失的发现照删；跨前缀工单不受影响。
func TestReplaceIssuesByPrefixKeepsWorkflowState(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	// 首落：E9-* 两条 + 另一章 E10-* 一条（跨前缀隔离对照）。
	seed := []AuditIssue{
		{ID: "E9-1", Level: "P1", Area: "拖沓段", Desc: "9-12 章拖沓"},
		{ID: "E9-2", Level: "P1", Area: "爽点断档", Desc: "13-16 章无爽点"},
	}
	require.NoError(t, s.ReplaceIssuesByPrefix(ctx, 7, "run-a", "E9-", 0, seed))
	require.NoError(t, s.ReplaceIssuesByPrefix(ctx, 7, "run-a", "E10-", 0, []AuditIssue{
		{ID: "E10-1", Level: "P1", Area: "拖沓段", Desc: "17 章拖沓"},
	}))

	// 用户流转：E9-1 修复落 fixed（工单修复回执态）；E9-2 修复派发失败退回待修后复验
	// 「明确未通过」（留 verify_receipt/attempts），随后驳回（留 note）。
	require.NoError(t, s.UpdateIssueStatus(ctx, 7, "run-a", "E9-1", IssueStatusFixed, ""))
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "E9-2", IssueStatusFixing, ""))
	_, err := s.RecordIssueVerify(ctx, 7, "run-a", "E9-2", false, "只改了一半")
	require.NoError(t, err)
	require.NoError(t, s.TransitionIssueTicket(ctx, 7, "run-a", "E9-2", IssueStatusDismissed, "误报，主观差异"))
	oldRows, err := s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Len(t, oldRows, 3)
	created91 := oldRows[0].CreatedAt

	// 同发现重落（键+Area 不变，desc/level 换新——发现内容字段走刷新语义）。
	require.NoError(t, s.ReplaceIssuesByPrefix(ctx, 7, "run-a", "E9-", 0, []AuditIssue{
		{ID: "E9-1", Level: "P0", Area: "拖沓段", Desc: "9-12 章拖沓（区间漂移）"},
		{ID: "E9-2", Level: "P1", Area: "爽点断档", Desc: "13-16 章无爽点（区间漂移）"},
	}))
	rows, err := s.ListIssues(ctx, 7, "run-a")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	byKey := map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusFixed, byKey["E9-1"].Status, "已修工单不得被换血打回待确认（#100）")
	require.Equal(t, "P0", byKey["E9-1"].Level, "发现内容字段走刷新语义")
	require.Equal(t, "9-12 章拖沓（区间漂移）", byKey["E9-1"].Desc)
	require.WithinDuration(t, created91, byKey["E9-1"].CreatedAt, 2*time.Second, "创建时间保留（同一发现）")
	require.Equal(t, IssueStatusDismissed, byKey["E9-2"].Status, "已驳回状态保留")
	require.Equal(t, "误报，主观差异", byKey["E9-2"].Note, "人工备注留痕保留")
	require.Equal(t, "只改了一半", byKey["E9-2"].VerifyReceipt, "复验回执留痕保留")
	require.Equal(t, 1, byKey["E9-2"].VerifyAttempts, "复验次数留痕保留")
	require.Equal(t, IssueStatusPending, byKey["E10-1"].Status, "跨前缀工单不受影响")

	// 键平移（位次键随发现集变化）：Area 兜底继承——检查项才是发现的语义身份。
	require.NoError(t, s.ReplaceIssuesByPrefix(ctx, 7, "run-a", "E9-", 0, []AuditIssue{
		{ID: "E9-1", Level: "P1", Area: "爽点断档", Desc: "位次平移"},
		{ID: "E9-2", Level: "P1", Area: "拖沓段", Desc: "位次平移"},
	}))
	rows, _ = s.ListIssues(ctx, 7, "run-a")
	byKey = map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusDismissed, byKey["E9-1"].Status, "Area 兜底：继承旧「爽点断档」的驳回态")
	require.Equal(t, IssueStatusFixed, byKey["E9-2"].Status, "Area 兜底：继承旧「拖沓段」的已修态")

	// 新检查项 = 真正的新发现 → 落 pending。
	require.NoError(t, s.ReplaceIssuesByPrefix(ctx, 7, "run-a", "E9-", 0, []AuditIssue{
		{ID: "E9-1", Level: "P1", Area: "拖沓段", Desc: "d"},
		{ID: "E9-2", Level: "P1", Area: "爽点断档", Desc: "d"},
		{ID: "E9-3", Level: "P1", Area: "开篇坡度", Desc: "新发现"},
	}))
	rows, _ = s.ListIssues(ctx, 7, "run-a")
	byKey = map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusFixed, byKey["E9-1"].Status)
	require.Equal(t, IssueStatusDismissed, byKey["E9-2"].Status)
	require.Equal(t, IssueStatusPending, byKey["E9-3"].Status, "新发现落 pending")

	// 消失的发现照删（回填语义不变）；跨前缀照旧不受影响。
	require.NoError(t, s.ReplaceIssuesByPrefix(ctx, 7, "run-a", "E9-", 0, []AuditIssue{
		{ID: "E9-1", Level: "P1", Area: "拖沓段", Desc: "d"},
	}))
	rows, _ = s.ListIssues(ctx, 7, "run-a")
	byKey = map[string]PipelineIssue{}
	for _, r := range rows {
		byKey[r.IssueKey] = r
	}
	require.Len(t, rows, 2)
	require.Equal(t, IssueStatusFixed, byKey["E9-1"].Status)
	_, hasE92 := byKey["E9-2"]
	require.False(t, hasE92, "消失的发现照删")
	_, hasE93 := byKey["E9-3"]
	require.False(t, hasE93, "消失的发现照删")
	require.Equal(t, IssueStatusPending, byKey["E10-1"].Status, "跨前缀工单始终不受影响")
}

// TestStoreReplaceIssuesIsolatedByRun 验证工单按 run 隔离：其它 run / 其它书不受影响。
func TestStoreReplaceIssuesIsolatedByRun(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-a", 0, []AuditIssue{{ID: "I1", Level: "P1", Area: "a", Desc: "d"}}))
	require.NoError(t, s.ReplaceIssues(ctx, 7, "run-b", 0, []AuditIssue{{ID: "I1", Level: "P1", Area: "b", Desc: "e"}}))
	require.NoError(t, s.ReplaceIssues(ctx, 8, "run-c", 0, []AuditIssue{{ID: "I1", Level: "P1", Area: "c", Desc: "f"}}))

	for _, tc := range []struct {
		novelID int64
		runID   string
		want    string
	}{{7, "run-a", "a"}, {7, "run-b", "b"}, {8, "run-c", "c"}} {
		rows, err := s.ListIssues(ctx, tc.novelID, tc.runID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, tc.want, rows[0].Area)
	}
}

// ── executeAudit 落表接线（R1）─────────────────────────────────────────────

// TestServiceExecuteAuditLandsIssueRows 验证体检 issue 清单整批落 pipeline_issues：
// 结构化作用域落列；台账型 issue 的 <type>#<id> 引用不污染章节列（落空）；
// 新一轮体检整批替换（旧工单不残留）。
func TestServiceExecuteAuditLandsIssueRows(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	auditOut := `{"verdict":"needs_fix","score":20,"summary":"体检出2条问题","issues":[` +
		`{"id":"I1","level":"P0","area":"开篇","desc":"前三章未立人设","fix_type":"body","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[1,2,3]},` +
		`{"id":"I2","level":"P1","area":"长线","desc":"timeline#42 伏笔未回收","fix_type":"ledger","target_scope":"active_volume","dimension":"timeline","direction":"forward_fix","chapter_ids":[]}` +
		`]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			return SkillOutput{Text: "结论\n```json\n" + auditOut + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, 460, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, `{}`))
	svc.executeAudit(run.RunID, run.NovelID)

	rows, err := svc.store.ListIssues(ctx, run.NovelID, run.RunID)
	require.NoError(t, err)
	require.Len(t, rows, 2, "体检 issue 应整批落表（无文风检测 provider 时零附加）")
	require.Equal(t, "I1", rows[0].IssueKey)
	require.JSONEq(t, "[1,2,3]", rows[0].ChapterIDsJSON, "结构化 chapter_ids 应落列")
	require.Equal(t, "body", rows[0].FixType)
	require.Equal(t, "I2", rows[1].IssueKey)
	require.Empty(t, decodeIssueChapterIDs(rows[1].ChapterIDsJSON), "台账型 issue 不应将 <type>#<id> 误判为章节落列")

	// 新一轮体检（复评）整批替换：同号 I1 是新问题，旧工单不残留。
	auditOut2 := `{"verdict":"needs_fix","score":22,"summary":"复评新发现","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏","desc":"复评新发现","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[]}` +
		`]}`
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			return SkillOutput{Text: "```json\n" + auditOut2 + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}
	// 回到 S4 running 模拟修复后复评入口。
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, `{}`))
	svc.executeAudit(run.RunID, run.NovelID)

	rows, err = svc.store.ListIssues(ctx, run.NovelID, run.RunID)
	require.NoError(t, err)
	require.Len(t, rows, 1, "新体检应整批替换工单")
	require.Equal(t, "节奏", rows[0].Area)
	require.Equal(t, IssueStatusPending, rows[0].Status)
}

// TestServiceExecuteAuditLandsGateKeepsAuditContext（R5-4 auto_run 自动裁决退役）验证：
// 检测收口统一落 awaiting_gate + G4 汇总闸门（裁决交还用户，即便 autoRun=true 也不自动
// Advance），且本轮刚产出的体检 JSON 必须随上下文落库（不得丢体检结果）。
func TestServiceExecuteAuditLandsGateKeepsAuditContext(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	captured := withCapturedGates(t)
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			return SkillOutput{Text: "```json\n" +
				`{"verdict":"pass","score":31,"summary":"自动通过结论","issues":[]}` + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, 461, "new", nil, true) // autoRun（历史参数，自动裁决已退役）
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, `{}`))
	svc.executeAudit(run.RunID, run.NovelID)

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StatusAwaitingGate, got.Status, "R5-4：检测收口统一落等人态，零自动裁决")
	require.Equal(t, StageS4, got.CurrentStage, "不得被 autoRun 自动推进（pass 也不自动 continue）")
	require.Contains(t, got.ContextJSON, "自动通过结论", "体检结果必须随上下文落库")
	gates := captured.all()
	require.NotEmpty(t, gates, "应广播 G4 汇总闸门供检测汇总视图消费")
	require.Equal(t, GateG4, gates[len(gates)-1].GateID)
}

// ── filterChapterIDs：按本书有效章节范围裁剪脏数据 ──────────────────────────

// TestFilterChapterIDs 锁死「涉及章节」裁剪：剔除越界规划章号与误填的台账 ID。
// 阅读序号连续 1..N（N=章节总数），故有效范围等价 [1, 章节总数]。
func TestFilterChapterIDs(t *testing.T) {
	// 7 章书：剔除规划章号（8/20）与台账 ID（217/252）
	require.Equal(t, []int64{1, 7}, filterChapterIDs([]int64{1, 7, 8, 20, 217, 252}, 7))
	// 30 章书：剔除台账 ID（217/498）
	require.Equal(t, []int64{1, 12, 30}, filterChapterIDs([]int64{1, 12, 30, 217, 498}, 30))
	// 上界 0（本书无章节 / 测试未建章节）：跳过裁剪，保留既有行为
	require.Equal(t, []int64{1, 8, 217}, filterChapterIDs([]int64{1, 8, 217}, 0))
	// 负数等同 0
	require.Equal(t, []int64{5}, filterChapterIDs([]int64{5}, -1))
	// 负数且含越界：等同 0，不裁剪
	require.Equal(t, []int64{1, 999}, filterChapterIDs([]int64{1, 999}, -1))
	// 0 长度输入
	require.Empty(t, filterChapterIDs([]int64{}, 7))
	require.Empty(t, filterChapterIDs(nil, 7))
}

// TestPipelineIssueRow_FiltersOutOfRangeChapters 验证 pipelineIssueRow 在落库投影时
// 已按有效章节上界裁剪：结构化 chapter_ids 与兜底正则解析的越界值都被剔除。
func TestPipelineIssueRow_FiltersOutOfRangeChapters(t *testing.T) {
	// 结构化 chapter_ids 含越界规划章号与台账 ID（本书 7 章）
	row := pipelineIssueRow(19, "run-x", 0, AuditIssue{
		ID:         "I1",
		Area:       "节奏",
		Desc:       "第1章与第20章断档",
		ChapterIDs: []int64{1, 20, 217},
	}, 7)
	require.JSONEq(t, "[1]", row.ChapterIDsJSON)

	// 结构化为空时回退 desc 正则：第1章、第20章解析出 [1,20]，裁剪后仅剩 [1]
	row2 := pipelineIssueRow(19, "run-y", 0, AuditIssue{
		ID:   "I2",
		Area: "节奏",
		Desc: "第1章与第20章断档",
	}, 7)
	require.JSONEq(t, "[1]", row2.ChapterIDsJSON)

	// 无章节上界（0）：不裁剪，保留 LLM 原值（兼容无章节测试 / 历史数据）
	row3 := pipelineIssueRow(19, "run-z", 0, AuditIssue{
		ID:         "I3",
		Area:       "长线",
		Desc:       "timeline#42 伏笔未回收",
		ChapterIDs: []int64{},
	}, 0)
	require.JSONEq(t, "[]", row3.ChapterIDsJSON)
}
