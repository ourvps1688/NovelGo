package mcp_tools_test

// feedback_tools_test.go —— 编辑反馈回流工具闭环（P1-1）：
// 两段式落库（原话先行/解析失败保留 raw）、F 系工单投影走既有闭环、
// 平台级样本不落工单、约束人工显式应用 + 幂等拒绝、平台级 checker 约束须指定小说。
// 外部测试包（mcp_tools 不能内部测试 import pipeline：pipeline→agent→mcp_tools 成环）。

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/pipeline"
	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/quality"
)

// testFeedbackJSON 是真实驳回原话（《我能听见水下三十六米》「故事代入感较弱、
// 开篇的切入点缺乏足够的吸引力」）的拆解回执（editor-feedback.md 示例同款）。
const testFeedbackJSON = `{"dims":["opening_hook"],"issues":[` +
	`{"id":"F1","dim":"opening_hook","level":"P0","area":"开篇过签力","desc":"编辑驳回：故事代入感较弱","fix_type":"body","chapter_ids":[1,2,3]},` +
	`{"id":"F2","dim":"opening_hook","level":"P0","area":"开篇过签力","desc":"编辑驳回：开篇的切入点缺乏足够的吸引力","fix_type":"body","chapter_ids":[1,2,3]}],` +
	`"constraints":[{"target":"checker_config","key":"premise_keywords","value":"[\"灾星\"]","reason":"编辑原话『切入点缺乏足够的吸引力』"},` +
	`{"target":"preference","key":"编辑口径","value":"开篇须个人利害绑定","reason":"编辑原话『故事代入感较弱』"}],` +
	`"summary":"开篇维度双杀：代入感弱+切入点无力"}`

var testFeedbackReport = "拆解完成。\n```json\n" + testFeedbackJSON + "\n```"

// testFeedbackConstraintsJSON 是闭环测试用约束修订（checker + preference 各一条）。
const testFeedbackConstraintsJSON = `[{"target":"checker_config","key":"premise_keywords","value":"[\"灾星\",\"吞星炼命\"]","reason":"编辑原话"},` +
	`{"target":"preference","key":"编辑口径","value":"开篇须个人利害绑定","reason":"编辑原话"}]`

func openFeedbackToolDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&quality.EditorFeedback{}, &quality.CheckerConfig{},
		&pipeline.PipelineRun{}, &pipeline.PipelineIssue{},
		&preference.PreferenceItem{},
	))
	return db
}

func feedbackTc(db *gorm.DB, runSub mcp_tools.RunSubAgentFunc) mcp_tools.ToolContext {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return mcp_tools.ToolContext{DB: db, NovelID: 1, Logger: logger, RunSubAgent: runSub}
}

// installFeedbackLander 装配真实落单器（app 启动注入同款），Cleanup 恢复。
func installFeedbackLander(t *testing.T) {
	t.Helper()
	mcp_tools.SetFeedbackTicketLander(pipeline.LandFeedbackTickets)
	t.Cleanup(func() { mcp_tools.SetFeedbackTicketLander(nil) })
}

// TestRecordEditorFeedbackClosedLoop 录入→拆解→样本回填→F 系工单落库全闭环。
func TestRecordEditorFeedbackClosedLoop(t *testing.T) {
	installFeedbackLander(t)
	db := openFeedbackToolDB(t)
	ctx := context.Background()
	var gotReq *mcp_tools.SubAgentRequest
	tc := feedbackTc(db, func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
		gotReq = &req
		return testFeedbackReport, nil
	})

	res, err := (&mcp_tools.RecordEditorFeedbackTool{}).Execute(ctx, &mcp_tools.RecordEditorFeedbackArgs{
		WorkTitle: "我在深地挖出青铜文明",
		Platform:  "七猫",
		Channel:   "男频悬疑",
		Verdict:   "综合质量一般、故事核心看点不够明确和突出",
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, quality.FeedbackParseParsed, res.Data["parse_status"])
	assert.Equal(t, 2, res.Data["issue_count"])
	assert.Equal(t, 2, res.Data["constraint_count"])
	assert.Equal(t, 2, res.Data["tickets_landed"])
	runID, _ := res.Data["run_id"].(string)
	assert.NotEmpty(t, runID, "novel_id>0 且有 issue 应落反馈容器")

	// 子 Agent 派发形态：editorfeedback + 原话内联 + 回执格式串。
	require.NotNil(t, gotReq)
	assert.Equal(t, "editorfeedback", gotReq.AgentType)
	assert.Contains(t, gotReq.Instruction, "综合质量一般、故事核心看点不够明确和突出")
	assert.Contains(t, gotReq.Instruction, quality.FeedbackReceiptFormatLine())

	// 样本回填（两段式第二步）。
	row, err := quality.NewStore(db, nil).GetEditorFeedback(ctx, res.Data["feedback_id"].(int64))
	require.NoError(t, err)
	assert.Equal(t, quality.FeedbackParseParsed, row.ParseStatus)
	assert.Equal(t, []string{quality.FeedbackDimOpening}, row.FeedbackDims())
	require.Len(t, row.FeedbackIssues(), 2)
	require.Len(t, row.FeedbackConstraints(), 2)
	assert.Equal(t, runID, row.RunID)

	// F 系工单落库（走既有修复闭环：pending 态、F 命名空间、body 修复出口）。
	issues, err := pipeline.NewStore(db).ListIssues(ctx, 1, runID)
	require.NoError(t, err)
	require.Len(t, issues, 2)
	assert.Equal(t, "F1", issues[0].IssueKey)
	assert.Equal(t, "F2", issues[1].IssueKey)
	for _, it := range issues {
		assert.Equal(t, pipeline.IssueStatusPending, it.Status)
		assert.Equal(t, pipeline.FixTypeBody, it.FixType)
		assert.Equal(t, "[1,2,3]", it.ChapterIDsJSON)
	}
	// 反馈容器形态：detect:feedback 家族、出生即 finished。
	var run pipeline.PipelineRun
	require.NoError(t, db.Where("run_id = ?", runID).First(&run).Error)
	assert.Equal(t, "detect:feedback", run.Mode)
	assert.Equal(t, pipeline.StatusFinished, run.Status)
}

// TestRecordEditorFeedbackPlatformLevel 平台级样本（novel_id=0）：只沉淀口径不落工单。
func TestRecordEditorFeedbackPlatformLevel(t *testing.T) {
	installFeedbackLander(t)
	db := openFeedbackToolDB(t)
	ctx := context.Background()
	tc := feedbackTc(db, func(_ context.Context, _ mcp_tools.SubAgentRequest) (string, error) {
		return testFeedbackReport, nil
	})
	zero := int64(0)

	res, err := (&mcp_tools.RecordEditorFeedbackTool{}).Execute(ctx, &mcp_tools.RecordEditorFeedbackArgs{
		NovelID:   &zero,
		WorkTitle: "我能听见水下三十六米",
		Platform:  "七猫",
		Verdict:   "故事代入感较弱、开篇的切入点缺乏足够的吸引力",
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, quality.FeedbackParseParsed, res.Data["parse_status"])
	assert.Equal(t, 0, res.Data["tickets_landed"])
	assert.Empty(t, res.Data["run_id"], "平台级样本不落工单容器")

	row, err := quality.NewStore(db, nil).GetEditorFeedback(ctx, res.Data["feedback_id"].(int64))
	require.NoError(t, err)
	assert.Equal(t, int64(0), row.NovelID)
	assert.Empty(t, row.RunID)
	require.Len(t, row.FeedbackIssues(), 2, "拆解产物照常沉淀（口径库）")
}

// TestRecordEditorFeedbackDegraded 两段式降级：派发失败/回执坏/无子 Agent 保留 raw 原话行。
func TestRecordEditorFeedbackDegraded(t *testing.T) {
	db := openFeedbackToolDB(t)
	ctx := context.Background()
	store := quality.NewStore(db, nil)

	// 无子 Agent 能力：存原话即成功，拆解不执行。
	res, err := (&mcp_tools.RecordEditorFeedbackTool{}).Execute(ctx, &mcp_tools.RecordEditorFeedbackArgs{
		WorkTitle: "w", Platform: "七猫", Verdict: "v",
	}, feedbackTc(db, nil))
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, quality.FeedbackParseRaw, res.Data["parse_status"])

	// 回执解析失败：保留 raw。
	res, err = (&mcp_tools.RecordEditorFeedbackTool{}).Execute(ctx, &mcp_tools.RecordEditorFeedbackArgs{
		WorkTitle: "w2", Platform: "七猫", Verdict: "v2",
	}, feedbackTc(db, func(_ context.Context, _ mcp_tools.SubAgentRequest) (string, error) {
		return "没有回执块", nil
	}))
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, quality.FeedbackParseRaw, res.Data["parse_status"])

	// 派发失败：保留 raw。
	res, err = (&mcp_tools.RecordEditorFeedbackTool{}).Execute(ctx, &mcp_tools.RecordEditorFeedbackArgs{
		WorkTitle: "w3", Platform: "七猫", Verdict: "v3",
	}, feedbackTc(db, func(_ context.Context, _ mcp_tools.SubAgentRequest) (string, error) {
		return "", assert.AnError
	}))
	require.NoError(t, err)
	require.True(t, res.Success)
	assert.Equal(t, quality.FeedbackParseRaw, res.Data["parse_status"])

	// 三条原话行都在（留痕优先，失败不删行）。
	rows, err := store.ListEditorFeedbacks(ctx, 1)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	for _, r := range rows {
		assert.Equal(t, quality.FeedbackParseRaw, r.ParseStatus)
	}
}

// TestApplyFeedbackConstraintsClosedLoop 约束人工应用闭环：checker_config + preference
// 落库、标记已应用、重放幂等拒绝。
func TestApplyFeedbackConstraintsClosedLoop(t *testing.T) {
	db := openFeedbackToolDB(t)
	ctx := context.Background()
	tc := feedbackTc(db, nil)
	store := quality.NewStore(db, nil)

	row := &quality.EditorFeedback{
		NovelID: 1, Platform: "七猫", WorkTitle: "w", Verdict: "v",
		ParseStatus: quality.FeedbackParseParsed,
		Dims:        `["opening_hook"]`,
		Issues:      `[]`,
		Constraints: testFeedbackConstraintsJSON,
	}
	require.NoError(t, store.CreateEditorFeedback(ctx, row))

	res, err := (&mcp_tools.ApplyFeedbackConstraintsTool{}).Execute(ctx, &mcp_tools.ApplyFeedbackConstraintsArgs{FeedbackID: row.ID}, tc)
	require.NoError(t, err)
	require.True(t, res.Success, "应用应成功: %v", res.Error)
	assert.ElementsMatch(t, []string{"premise_keywords"}, res.Data["applied_checker"])
	assert.ElementsMatch(t, []string{"编辑口径"}, res.Data["applied_preference"])

	// checker_config 落库（清单归一化）。
	cfg, err := store.GetCheckerConfig(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, []string{"灾星", "吞星炼命"}, cfg.Premises())
	// preference 落库（novel 级）。
	var prefs []preference.PreferenceItem
	require.NoError(t, db.Where("category = ?", "编辑口径").Find(&prefs).Error)
	require.Len(t, prefs, 1)
	assert.Equal(t, "开篇须个人利害绑定", prefs[0].Content)
	assert.False(t, prefs[0].IsGlobal)
	// 标记已应用。
	applied, err := store.GetEditorFeedback(ctx, row.ID)
	require.NoError(t, err)
	require.NotNil(t, applied.ConstraintsAppliedAt)

	// 重放幂等拒绝。
	res, err = (&mcp_tools.ApplyFeedbackConstraintsTool{}).Execute(ctx, &mcp_tools.ApplyFeedbackConstraintsArgs{FeedbackID: row.ID}, tc)
	require.NoError(t, err)
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "幂等拒绝")
}

// TestApplyFeedbackConstraintsPlatformCheckerNeedsNovel 平台级样本应用 checker_config
// 约束必须显式指定目标小说；未拆解样本拒绝应用。
func TestApplyFeedbackConstraintsPlatformCheckerNeedsNovel(t *testing.T) {
	db := openFeedbackToolDB(t)
	ctx := context.Background()
	tc := feedbackTc(db, nil)
	store := quality.NewStore(db, nil)

	row := &quality.EditorFeedback{
		NovelID: 0, Platform: "七猫", WorkTitle: "我能听见水下三十六米", Verdict: "v",
		ParseStatus: quality.FeedbackParseParsed,
		Dims:        `["opening_hook"]`, Issues: `[]`,
		Constraints: `[{"target":"checker_config","key":"protagonist_names","value":"[\"林澈\"]","reason":"r"}]`,
	}
	require.NoError(t, store.CreateEditorFeedback(ctx, row))

	// 无目标小说：拒绝。
	res, err := (&mcp_tools.ApplyFeedbackConstraintsTool{}).Execute(ctx, &mcp_tools.ApplyFeedbackConstraintsArgs{FeedbackID: row.ID}, tc)
	require.NoError(t, err)
	assert.False(t, res.Success)
	assert.Contains(t, res.Error, "novel_id")

	// 显式指定目标小说：落到该小说配置。
	target := int64(7)
	res, err = (&mcp_tools.ApplyFeedbackConstraintsTool{}).Execute(ctx, &mcp_tools.ApplyFeedbackConstraintsArgs{FeedbackID: row.ID, NovelID: &target}, tc)
	require.NoError(t, err)
	require.True(t, res.Success, "应用应成功: %v", res.Error)
	cfg, err := store.GetCheckerConfig(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, []string{"林澈"}, cfg.Protagonists())

	// 未拆解样本：拒绝。
	rawRow := &quality.EditorFeedback{NovelID: 0, Platform: "p", WorkTitle: "w", Verdict: "v", ParseStatus: quality.FeedbackParseRaw}
	require.NoError(t, store.CreateEditorFeedback(ctx, rawRow))
	res, err = (&mcp_tools.ApplyFeedbackConstraintsTool{}).Execute(ctx, &mcp_tools.ApplyFeedbackConstraintsArgs{FeedbackID: rawRow.ID}, tc)
	require.NoError(t, err)
	assert.False(t, res.Success)
}
