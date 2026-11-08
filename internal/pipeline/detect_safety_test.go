package pipeline

// detect_safety_test.go — #104「签约体检安全审核触犯内容定位」验收：
// 真实请求内容（提示词 + 本次 agent loop 工具返回）单元化、二分定位接线、双路径失败态，
// 以及检测-only 语义（零假工单、零 auto-advance）零回退。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/llm"
)

const detectRefusalText = "The request was rejected because it was considered high risk"

// TestSplitRequestTextContinuous 验收：片段与原文同源、连续、不重排（探针绝不报出
// 「未发送过」的文本），且单段受 rune 帽约束、Label 带序号便于作者定位。
func TestSplitRequestTextContinuous(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "第%d行 顾渊把绳尾拴进门内石鼻，确认活绳才过门缝。\n", i)
	}
	text := sb.String()
	units := splitRequestText(text, "体检提示词")
	require.Greater(t, len(units), 2, "长文本应切成多段供二分探测")

	parts := make([]string, 0, len(units))
	for i, u := range units {
		require.Equal(t, "request", u.Kind)
		require.LessOrEqual(t, utf8.RuneCountInString(u.Content), requestUnitMaxRunes)
		require.Contains(t, u.Label, fmt.Sprintf("%d/%d", i+1, len(units)))
		parts = append(parts, u.Content)
	}
	require.Equal(t, strings.TrimSpace(text), strings.Join(parts, "\n"))
	require.Empty(t, splitRequestText("  \n \t", "x"))
	require.Equal(t, "原文\n", probePatrolUnitsContent([]patrolUnit{
		{Kind: "request", Label: "不该出现在探针文本里", Content: "原文"},
	}), "请求侧单元须按原文重放，不加标题前缀")
}

// TestSessionToolUnitsBaseline 验收：只圈定本次执行产生的工具返回（id > baseline），
// 不把同一检测容器跨天残留的历史会话误当成本次请求内容。
func TestSessionToolUnitsBaseline(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE messages (
		id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO messages (id,session_id,role,content) VALUES
		(1,'detect:s1','tool','旧会话残留 OLD-TOOL'),
		(2,'detect:s1','assistant','旧回复'),
		(3,'detect:other','tool','别的会话')`).Error)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	baseline := svc.sessionMessageBaseline(ctx, "detect:s1")
	require.Equal(t, int64(2), baseline)
	require.Empty(t, svc.collectSessionToolUnits(ctx, "detect:s1", baseline),
		"水位之后无新消息时不得凭空造单元")

	require.NoError(t, db.Exec(`INSERT INTO messages (id,session_id,role,content) VALUES
		(4,'detect:s1','tool','NEW-TOOL-CONTENT 第14章已知信息')`).Error)
	units := svc.collectSessionToolUnits(ctx, "detect:s1", baseline)
	require.NotEmpty(t, units)
	joined := ""
	for _, u := range units {
		joined += u.Content
	}
	require.Contains(t, joined, "NEW-TOOL-CONTENT")
	require.NotContains(t, joined, "OLD-TOOL")
	require.Contains(t, units[0].Label, "工具返回 #4")
}

// TestSessionToolUnitsMissingTableDegrades 验收：测试桩/降级部署无 messages 表时
// 优雅降级（不报错、不阻断检测），定位仍可由提示词单元承担。
func TestSessionToolUnitsMissingTableDegrades(t *testing.T) {
	db := newTestDB(t) // 只有 pipeline_runs/pipeline_issues/gate_objects
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	require.Zero(t, svc.sessionMessageBaseline(ctx, "detect:missing"))
	require.Empty(t, svc.collectSessionToolUnits(ctx, "detect:missing", 0))
}

// locatingProbe 是测试用假上游：控制探针放行，仅含 marker 的片段被拦。
func locatingProbe(marker string) patrolSafetyProbe {
	return func(_ context.Context, content string) patrolProbeState {
		if content == patrolSafetyControlProbe {
			return probeClean
		}
		if strings.Contains(content, marker) {
			return probeBlocked
		}
		return probeClean
	}
}

func newDetectSafetyService(t *testing.T, runner SkillRunnerFunc) (*Service, context.Context) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.skillRunner = runner
	return svc, context.Background()
}

// TestRunDetectionRefusalLocatesContent 回归 #104：签约体检软拒绝（err==nil、content
// 是拒绝语）时，失败态必须给出「哪段请求内容」的定位结果与原始拒绝语，且零假工单。
func TestRunDetectionRefusalLocatesContent(t *testing.T) {
	svc, ctx := newDetectSafetyService(t, func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: detectRefusalText}, nil
	})
	svc.safetyProbeFn = func() patrolSafetyProbe { return locatingProbe("BLOCK-ME-DELTA") }

	prompt := strings.Repeat("顾渊把绳尾拴进门内石鼻，确认活绳才过门缝。\n", 200) +
		"BLOCK-ME-DELTA 这一段是触发审核的台账摘录\n"
	run, err := svc.StartPipeline(ctx, 491, "new", nil, false)
	require.NoError(t, err)
	var ctxt GateContext
	out, derr := svc.runDetection(ctx, detectRequest{
		runID: run.RunID, novelID: run.NovelID,
		skill: "signability-audit", prompt: prompt, sessionID: "detect:test-locate",
		maxTurns: 4, kind: "", landing: detectLandNewRound, label: "检测任务",
	}, &ctxt, "")
	require.NoError(t, derr)
	require.Equal(t, "error", out.Audit.Verdict)
	require.Empty(t, out.Audit.Issues, "拒绝绝不能生成假问题清单")

	s := out.Audit.Summary
	require.Contains(t, s, "【触犯内容自动定位】", "应给出已确认命中的片段")
	require.Contains(t, s, "BLOCK-ME-DELTA", "命中片段须带原文摘录")
	require.Contains(t, s, detectRefusalText, "原始拒绝语必须回显")
	require.Contains(t, s, "本次请求可定位片段", "应写明请求体量供排查")
	require.Contains(t, s, "定位说明")
	require.Contains(t, s, "未生成任何工单")

	var stored AuditData
	require.NoError(t, json.Unmarshal([]byte(out.Context.Audit), &stored))
	require.Contains(t, stored.Summary, "【触犯内容自动定位】")
	rows, lerr := svc.store.ListIssues(ctx, run.NovelID, run.RunID)
	require.NoError(t, lerr)
	require.Empty(t, rows, "失败态零工单")
}

// TestRunDetectionSafetyErrorLandsReport 回归 #104：以 error 形式返回的上游审核拦截
// （HTTP 4xx content_filter）不再静默降级丢失线索，而是落可见 error 报告 + 定位结果。
func TestRunDetectionSafetyErrorLandsReport(t *testing.T) {
	apiErr := &llm.APIError{StatusCode: 400, Message: `content_filter: high risk on ledger block`}
	svc, ctx := newDetectSafetyService(t, func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{}, fmt.Errorf("skill \"signability-audit\" 执行失败: %w", apiErr)
	})
	svc.safetyProbeFn = func() patrolSafetyProbe { return locatingProbe("BLOCK-ME-GAMMA") }

	run, err := svc.StartPipeline(ctx, 492, "new", nil, false)
	require.NoError(t, err)
	var ctxt GateContext
	out, derr := svc.runDetection(ctx, detectRequest{
		runID: run.RunID, novelID: run.NovelID,
		skill:     "signability-audit",
		prompt:    strings.Repeat("正常设定行\n", 200) + "BLOCK-ME-GAMMA 台账行\n",
		sessionID: "detect:test-api-error", maxTurns: 4,
		kind: "", landing: detectLandReportOnly, label: "S6 终检",
	}, &ctxt, "")
	require.NoError(t, derr, "安全错误应落可见报告而非静默降级")
	require.Equal(t, "error", out.Audit.Verdict)
	require.Contains(t, out.Audit.Summary, "网关/API 错误原文")
	require.Contains(t, out.Audit.Summary, "content_filter")
	require.Contains(t, out.Audit.Summary, "【触犯内容自动定位】")
	require.Contains(t, out.Audit.Summary, "BLOCK-ME-GAMMA")
}

// TestRunDetectionNonSafetyErrorStillReturns 契约③零回退：非安全类派发失败仍原样返回
// err（既有 TestRunDetectionDegradesOnFailure 语义），不得被新分支吞掉。
func TestRunDetectionNonSafetyErrorStillReturns(t *testing.T) {
	svc, ctx := newDetectSafetyService(t, func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{}, fmt.Errorf("llm down")
	})
	svc.safetyProbeFn = func() patrolSafetyProbe { return locatingProbe("never") }
	var ctxt GateContext
	out, derr := svc.runDetection(ctx, detectRequest{
		runID: "r", novelID: 1, skill: "signability-audit", prompt: "p",
		sessionID: "detect:test-down", maxTurns: 4, label: "检测任务",
	}, &ctxt, "")
	require.Error(t, derr)
	require.Empty(t, out.Context.Audit, "失败不得落半截报告")
}

// TestDetectSafetySummaryPreWrapped 前端以 whitespace-pre-wrap 渲染多行定位结果；
// 后端 Summary 必须是真正的多行文本（不是把整段挤成一行）。
func TestDetectSafetySummaryMultiLine(t *testing.T) {
	svc, ctx := newDetectSafetyService(t, func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: detectRefusalText}, nil
	})
	// llmClient=nil → 探针不可用 → 降级词面提示，仍应给出定位说明与建议。
	var ctxt GateContext
	out, derr := svc.runDetection(ctx, detectRequest{
		runID: "r", novelID: 1, skill: "signability-audit",
		prompt: "体检提示词 血腥描写需要核查", sessionID: "detect:test-hint",
		maxTurns: 4, label: "检测任务",
	}, &ctxt, "")
	require.NoError(t, derr)
	require.Contains(t, out.Audit.Summary, "\n")
	require.Contains(t, out.Audit.Summary, "定位说明：")
	require.Contains(t, out.Audit.Summary, "【词面提示】", "探针不可用时须降级为本地词面提示")
	require.Contains(t, out.Audit.Summary, "已改用本地词面提示")
}
