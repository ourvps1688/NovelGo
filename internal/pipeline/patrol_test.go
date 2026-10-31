package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/stretchr/testify/require"
)

// patrolJSONFixture 是张力报告的结构化样例（全量契约：张力带 target_scope/dimension/direction）。
const patrolJSONFixture = `{"verdict":"needs_fix","score":0,"summary":"发现 2 处张力","schema_version":2,"issues":[` +
	`{"id":"T1","level":"P1","area":"timeline","desc":"第9卷称三年后，冻结锚点是五年","fix_type":"body","target_scope":"active_volume","dimension":"timeline","direction":"forward_fix"},` +
	`{"id":"T2","level":"P0","area":"reader_pov","desc":"已揭示真相被当未知写，需回改前卷才能自洽","fix_type":"ledger","target_scope":"frozen_volume","dimension":"reader_pov","direction":"retcon_candidate"}]}`

// capturePatrolEmit 把巡检事件发射器换成通道捕获（异步触发测试用），测试结束恢复。
func capturePatrolEmit(t *testing.T) <-chan map[string]any {
	t.Helper()
	ch := make(chan map[string]any, 4)
	t.Cleanup(swapEmitConsistencyPatrol(func(_ context.Context, p map[string]any) { ch <- p }))
	return ch
}

// TestPatrolPromptReadOnlyContract 验证张力报告提示词：全量契约片段 + 示例值 T1 +
// 「只报告不改写」声明 + 既定事实基准注入（WBS 2.3）。
func TestPatrolPromptReadOnlyContract(t *testing.T) {
	p := patrolPrompt(7, contextasm.ContextBlocks{Facts: "卷1（已冻结）：主角林川，时间线锚点五年"})
	require.Contains(t, p, auditFormatLine(true, issueSamplePatrol), "应引用全量契约 schema 片段")
	require.Contains(t, p, `"id":"T1"`, "应使用张力报告示例值 T1")
	require.Contains(t, p, issueFieldRules(), "应引用契约必填说明")
	require.Contains(t, p, "只报告不改写", "应声明只报告不改写")
	require.Contains(t, p, "不会触发任何自动修复", "应声明不进自动修复路由")
	require.Contains(t, p, "retcon_candidate", "应带方向建议枚举")
	require.Contains(t, p, "【已冻结卷既定事实（系列圣经快照）】", "应注入既定事实基准块")
	require.Contains(t, p, "时间线锚点五年", "应携带既定事实正文")
}

// TestConsistencyPatrolAgentReadOnly 机械验证「只报告不改写」（WBS 3.3 验收）：
// consistency-patrol 技能路由到专属巡检 agent，其白名单与签约体检同形（纯只读），
// 不含任何写工具/子代理派发。
func TestConsistencyPatrolAgentReadOnly(t *testing.T) {
	at := skillAgentType("consistency-patrol")
	require.Equal(t, agentcfg.ConsistencyPatrolAgent, at)
	require.Equal(t, agentTypePatrol, skillAgentTypeString("consistency-patrol"))

	al := agentcfg.Allowlist(at)
	require.NotEmpty(t, al)
	require.Equal(t, agentcfg.Allowlist(agentcfg.SignabilityAgent), al,
		"巡检白名单应与签约体检只读白名单同形")
	for tool := range al {
		require.NotContains(t, tool, "edit", "巡检不得有写工具")
		require.NotContains(t, tool, "create_", "巡检不得有写工具")
		require.NotContains(t, tool, "update_", "巡检不得有写工具")
		require.NotContains(t, tool, "delete", "巡检不得有删除工具")
		require.NotEqual(t, "run_subagent", tool, "巡检不得派发子代理")
	}
	require.NotEmpty(t, agentcfg.AgentIdentity(at), "巡检 agent 应有专属身份提示词")
}

// TestRunConsistencyPatrolStoresReportAndEmits 验证张力报告链路：技能产出 → 结构化
// 落库 PatrolReport → 广播事件；报告不进任何修复路由字段（只报告不改写）。
func TestRunConsistencyPatrolStoresReportAndEmits(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 701, "new", nil, false)
	require.NoError(t, err)

	emitted := capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, name string, in SkillInput) (SkillOutput, error) {
		require.Equal(t, "consistency-patrol", name)
		require.Contains(t, in.Prompt, "张力报告")
		return SkillOutput{Text: "张力小结：发现 2 处。\n```json\n" + patrolJSONFixture + "\n```"}, nil
	}
	svc.runConsistencyPatrol(ctx, run.RunID, run.NovelID)

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	var report AuditData
	require.NoError(t, json.Unmarshal([]byte(ctxt.PatrolReport), &report))
	require.Equal(t, "needs_fix", report.Verdict)
	require.Len(t, report.Issues, 2)
	require.Equal(t, "T1", report.Issues[0].ID)
	require.Equal(t, "forward_fix", report.Issues[0].Direction)
	require.Equal(t, "retcon_candidate", report.Issues[1].Direction)
	require.Equal(t, TargetScopeFrozenVolume, report.Issues[1].TargetScope)

	// 只报告不改写：报告绝不进修复路由字段。
	require.Empty(t, ctxt.Audit, "张力报告不得混入修复用 Audit")
	require.Empty(t, ctxt.FixStates, "张力报告不得落修复断点")
	require.Empty(t, ctxt.RewriteBacklog, "张力报告不得进重写回路")
	require.Empty(t, ctxt.RetconBacklog, "张力报告不得自动进 retcon 候选（审批另走 3.1 闸门）")

	p := <-emitted
	require.Equal(t, run.RunID, p["run_id"])
	require.Equal(t, run.NovelID, p["novel_id"])
}

// TestRunConsistencyPatrolDegradesOnFailure 验证降级链：巡检失败只记日志
// （不落库、不广播、不阻断），兜底巡检不给流水线添堵。
func TestRunConsistencyPatrolDegradesOnFailure(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 702, "new", nil, false)
	require.NoError(t, err)

	emitted := capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{}, errors.New("llm down")
	}
	svc.runConsistencyPatrol(ctx, run.RunID, run.NovelID) // 不 panic、不阻断

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	if got.ContextJSON != "" {
		require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	}
	require.Empty(t, ctxt.PatrolReport, "失败不应落半截报告")
	select {
	case <-emitted:
		t.Fatal("失败不应广播巡检完成事件")
	default:
	}
}

// TestRunConsistencyPatrolRejectionStoredAsError 验证回归：模型以「拒绝/安全审核拦截」
// 形式返回（err==nil，但 content 是拒绝语）时，不得被 parseAudit 兜底成 needs_fix 的
// 假张力报告；应落地清晰的 verdict=error 失败态并广播事件，让面板展示失败原因。
func TestRunConsistencyPatrolRejectionStoredAsError(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 706, "new", nil, false)
	require.NoError(t, err)

	emitted := capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		require.Equal(t, "consistency-patrol", name)
		return SkillOutput{Text: "The request was rejected because it was considered high risk"}, nil
	}
	svc.runConsistencyPatrol(ctx, run.RunID, run.NovelID)

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.NotEmpty(t, ctxt.PatrolReport, "拒绝应落地失败态而非丢弃")

	var report AuditData
	require.NoError(t, json.Unmarshal([]byte(ctxt.PatrolReport), &report))
	require.Equal(t, "error", report.Verdict, "拒绝应标记为 error 而非 needs_fix")
	require.Empty(t, report.Issues, "拒绝不应产生任何张力项（杜绝垃圾清单）")
	require.Contains(t, report.Summary, "模型拒绝", "失败态应给出可读的原因与建议")

	p := <-emitted
	require.Equal(t, run.RunID, p["run_id"])
	require.Equal(t, run.NovelID, p["novel_id"])
}

// TestRunConsistencyPatrolRejectionEchoesRawRefusal 回归：原实现「原始返回已忽略」把唯一可
// 定位的拒绝语丢弃，导致用户无法知道哪段内容被判高风险。修复后失败态 Summary 必须回显模型/
// 网关的原始拒绝语（截断后），让用户能据其点名的章节/内容做局部脱敏后重跑。
func TestRunConsistencyPatrolRejectionEchoesRawRefusal(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 707, "new", nil, false)
	require.NoError(t, err)

	capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		require.Equal(t, "consistency-patrol", name)
		// 模型软拒绝语点名具体章节：这是用户最需要的「哪段被判高风险」线索。
		return SkillOutput{Text: "I'm sorry, but I cannot continue: the graphic depiction of violence in chapter 12 of this manuscript violates our content policy."}, nil
	}
	svc.runConsistencyPatrol(ctx, run.RunID, run.NovelID)

	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.NotEmpty(t, ctxt.PatrolReport)

	var report AuditData
	require.NoError(t, json.Unmarshal([]byte(ctxt.PatrolReport), &report))
	require.Equal(t, "error", report.Verdict)
	require.Contains(t, report.Summary, "chapter 12", "失败态必须回显原始拒绝语中的具体章节线索")
	require.NotContains(t, report.Summary, "原始返回已忽略", "不应再丢弃原始拒绝语")
}

// TestControlPipelinePatrolDispatch 验证手动触发：控制指令 "patrol" 后台跑张力报告，
// 不改变当前阶段（WBS 3.3 触发=卷末+手动）。
func TestControlPipelinePatrolDispatch(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 703, "new", nil, false)
	require.NoError(t, err)

	emitted := capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		require.Equal(t, "consistency-patrol", name)
		return SkillOutput{Text: "```json\n{\"verdict\":\"pass\",\"score\":0,\"summary\":\"无张力\",\"issues\":[]}\n```"}, nil
	}
	require.NoError(t, svc.ControlPipeline(ctx, run.RunID, "patrol"))

	select {
	case p := <-emitted:
		require.Equal(t, run.RunID, p["run_id"])
	case <-time.After(3 * time.Second):
		t.Fatal("控制指令 patrol 未触发一致性巡检")
	}
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StageS1, got.CurrentStage, "巡检不应改变当前阶段")
	require.Equal(t, StatusRunning, got.Status, "巡检不应改变运行状态")
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Contains(t, ctxt.PatrolReport, "无张力", "手动触发的报告应落库")
}

// TestPatrolTriggeredAtVolumeFreeze 验证卷末自动触发（WBS 3.3 建议「卷末+手动」而非实时）：
// G_freeze 自动冻结成功后后台跑一次张力报告；冻结被拒则不触发（不白烧一次 LLM）。
func TestPatrolTriggeredAtVolumeFreeze(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 704, "new", nil, false)
	require.NoError(t, err)

	emitted := capturePatrolEmit(t)
	svc.volumeFreezer = func(_ context.Context, _, _ int64) error { return nil }
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		require.Equal(t, "consistency-patrol", name)
		return SkillOutput{Text: "```json\n" + patrolJSONFixture + "\n```"}, nil
	}
	svc.freezeVolumeWithNote(ctx, run.NovelID, 11, run.RunID, StageS3, StatusRunning)

	select {
	case p := <-emitted:
		require.Equal(t, run.RunID, p["run_id"])
		require.Equal(t, run.NovelID, p["novel_id"])
	case <-time.After(3 * time.Second):
		t.Fatal("卷冻结成功后未自动触发一致性巡检")
	}
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	require.Contains(t, ctxt.PatrolReport, "T1", "卷末自动报告应落库")

	// 冻结被拒：不触发巡检（同步路径无 goroutine，返回后即可断言）。
	failed := NewService(newTestDB(t), nil, nil, nil, "", "", context.Background(), slog.Default())
	run2, err := failed.StartPipeline(ctx, 705, "new", nil, false)
	require.NoError(t, err)
	failed.volumeFreezer = func(_ context.Context, _, _ int64) error { return errors.New("audit rejected") }
	failed.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		t.Errorf("冻结被拒不应触发巡检，却调用了 %s", name)
		return SkillOutput{}, errors.New("unreachable")
	}
	failed.freezeVolumeWithNote(ctx, run2.NovelID, 12, run2.RunID, StageS3, StatusRunning)
}
