package pipeline

import (
	"context"
	"log/slog"
	"testing"

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

	none := AuditIssue{ID: "I4", Area: "长线牵引", Desc: "timeline#42 伏笔未回收"}
	require.Equal(t, []int64{42}, issueChapterIDs(none), "正则兜底应识别 #N 写法")

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
	require.JSONEq(t, "[42]", rows[1].ChapterIDsJSON, "无结构化字段时正则兜底值应落列")
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
// 结构化作用域落列、正则兜底值落列；新一轮体检整批替换（旧工单不残留）。
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
	require.JSONEq(t, "[42]", rows[1].ChapterIDsJSON, "chapter_ids 为空时正则兜底值应落列")

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

// TestServiceExecuteAuditAutoRunKeepsAuditContext 验证 auto_run 自动裁决路径不丢体检结果：
// Advance(ctx, run, ...) 持久化的是 run.ContextJSON，必须携带本轮刚产出的体检 JSON
// （否则自动修复闭环会拿着陈旧/空 Audit 派发）。
func TestServiceExecuteAuditAutoRunKeepsAuditContext(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			return SkillOutput{Text: "```json\n" +
				`{"verdict":"pass","score":31,"summary":"自动通过结论","issues":[]}` + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	run, err := svc.StartPipeline(ctx, 461, "new", nil, true) // autoRun
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, `{}`))
	svc.executeAudit(run.RunID, run.NovelID)

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Equal(t, StageS3, got.CurrentStage, "auto pass 应自动回写作")
	require.Contains(t, got.ContextJSON, "自动通过结论", "auto_run 路径的体检结果必须随上下文落库")
}
