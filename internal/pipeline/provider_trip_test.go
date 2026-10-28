package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ── 供应商熔断扩展（P0 重设计 · 基于 breaker）────────────────────────────────
//
// 验收：①全派发点覆盖断言（源码锁定 skillRunner 调用点均过熔断判定）；
// ②熔断触发零 agent 派发 + 各点失败通道语义不变；③未触发逐字一致（既有 pipeline 测试零回退）。

// forceProviderTrip 把取数接缝钉在某 provider 处于 Open，确定性触发熔断判定（测试专用）。
func forceProviderTrip(t *testing.T) {
	t.Helper()
	orig := breakerOpenProviders
	breakerOpenProviders = func() []string { return []string{"deepseek"} }
	t.Cleanup(func() { breakerOpenProviders = orig })
}

// countSkillCalls 给 skillRunner 包一层计数桩（断言熔断触发时零 agent 派发）。
func countSkillCalls(svc *Service) *int {
	n := new(int)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		*n++
		return SkillOutput{Text: "ok"}, nil
	}
	return n
}

// TestProviderTrippedJudgment 锁定共享熔断判定语义（单源）：基于 breaker 的取数接缝、
// 触发口径（存在 Open provider 即熔断）、以及降级纪律「判定自身异常 → 照常派发（绝不误熔断）」。
func TestProviderTrippedJudgment(t *testing.T) {
	orig := breakerOpenProviders
	t.Cleanup(func() { breakerOpenProviders = orig })

	// 空 → 不熔断（无 Open provider，行为与判定引入前逐字一致）
	breakerOpenProviders = func() []string { return nil }
	tripped, open := providerTripped()
	require.False(t, tripped, "无 Open provider 不应熔断")
	require.Empty(t, open)

	// 非空 → 熔断（存在 Open provider）
	breakerOpenProviders = func() []string { return []string{"deepseek"} }
	tripped, open = providerTripped()
	require.True(t, tripped, "存在 Open provider 应熔断")
	require.Equal(t, []string{"deepseek"}, open)

	// 降级纪律（最高验收）：判定自身异常 → 照常派发，宁缺勿错。
	breakerOpenProviders = func() []string { panic("count boom") }
	tripped, open = providerTripped()
	require.False(t, tripped, "判定自身异常必须照常派发（绝不误熔断）")
	require.Empty(t, open)

	// 哨兵错误可被 errors.Is 识别，提示统一「疑似供应商挂起」口径且附 Open 的 provider 名。
	err := providerTripErr([]string{"deepseek"})
	require.ErrorIs(t, err, errProviderTripped)
	require.Contains(t, err.Error(), "疑似供应商挂起")
	require.Contains(t, err.Error(), "deepseek", "提示应附 Open 的 provider 名")
}

// TestSkillRunnerDispatchOnlyViaTripGuard 源码锁定（验收①）：internal/pipeline 生产代码里
// s.skillRunner 的直接调用只允许出现在 dispatchSkill（熔断判定唯一入口）内——任何新增
// 派发点必须走 dispatchSkill，天然过熔断判定（S2/S3/S4/S5/S6/G3/巡检全覆盖）。
func TestSkillRunnerDispatchOnlyViaTripGuard(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	total := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, rerr := os.ReadFile(f)
		require.NoError(t, rerr)
		total += strings.Count(string(src), "s.skillRunner(")
	}
	require.Equal(t, 1, total,
		"s.skillRunner( 直接调用只允许出现在 dispatchSkill 内（熔断判定唯一入口），实际 %d 处", total)
}

// TestProviderTripSkipsDispatchS2Blueprint 验收②：S2 蓝图熔断触发零派发，
// 失败通道语义不变（保留 running + 广播错误阶段事件供前端重试），提示统一口径。
func TestProviderTripSkipsDispatchS2Blueprint(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	forceProviderTrip(t)
	calls := countSkillCalls(svc)

	var notes []string
	t.Cleanup(swapEmitStage(func(_ context.Context, p StagePayload) {
		if p.Note != "" {
			notes = append(notes, p.Note)
		}
	}))

	run, err := svc.StartPipeline(ctx, 901, "new", nil, false)
	require.NoError(t, err)
	svc.executeBlueprint(run.RunID, 901)

	require.Zero(t, *calls, "熔断触发必须零 agent 派发")
	require.NotEmpty(t, notes)
	require.Contains(t, notes[0], "蓝图生成失败", "S2 失败通道语义不变（running+错误事件）")
	require.Contains(t, notes[0], "疑似供应商挂起", "提示统一「疑似供应商挂起」口径")
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StatusRunning, got.Status, "S2 失败通道语义不变：保留 running 供前端重试")
}

// TestProviderTripSkipsDispatchS4Audit 验收②：S4 体检熔断触发零派发，
// 失败通道语义不变（落 paused 可恢复）。
func TestProviderTripSkipsDispatchS4Audit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	forceProviderTrip(t)
	calls := countSkillCalls(svc)

	var notes []string
	t.Cleanup(swapEmitStage(func(_ context.Context, p StagePayload) {
		if p.Note != "" {
			notes = append(notes, p.Note)
		}
	}))

	run, err := svc.StartPipeline(ctx, 902, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, ""))

	svc.executeAudit(run.RunID, 902)

	require.Zero(t, *calls, "熔断触发必须零 agent 派发")
	require.NotEmpty(t, notes)
	require.Contains(t, notes[0], "签约体检失败", "S4 失败通道语义不变（落 paused 可恢复）")
	require.Contains(t, notes[0], "疑似供应商挂起", "提示统一「疑似供应商挂起」口径")
	require.Contains(t, notes[0], "已暂停，点击恢复将重新体检")
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StageS4, got.CurrentStage)
	require.Equal(t, StatusPaused, got.Status, "S4 失败通道=落 paused")
}

// TestProviderTripSkipsDispatchS6FinalAudit 验收②：S6 完本终检熔断触发零派发，
// 失败通道语义不变（落 paused 可恢复）。
func TestProviderTripSkipsDispatchS6FinalAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	forceProviderTrip(t)
	calls := countSkillCalls(svc)

	var notes []string
	t.Cleanup(swapEmitStage(func(_ context.Context, p StagePayload) {
		if p.Note != "" {
			notes = append(notes, p.Note)
		}
	}))

	run, err := svc.StartPipeline(ctx, 904, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS6, StatusRunning, ""))

	svc.executeFinalAudit(run.RunID, 904)

	require.Zero(t, *calls, "熔断触发必须零 agent 派发")
	require.NotEmpty(t, notes)
	require.Contains(t, notes[0], "完本终检失败", "S6 失败通道语义不变（落 paused 可恢复）")
	require.Contains(t, notes[0], "疑似供应商挂起", "提示统一「疑似供应商挂起」口径")
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StageS6, got.CurrentStage)
	require.Equal(t, StatusPaused, got.Status, "S6 失败通道=落 paused")
}

// TestProviderTripSkipsDispatchPatrols 验收②：三个非阻断派发点（G3 蓝图漂移巡检 /
// 一致性巡检 / 全量体检）熔断触发零派发，各点失败通道语义不变（跳过不阻断 / 不广播 /
// needs_fix+error 交上层）。
func TestProviderTripSkipsDispatchPatrols(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	forceProviderTrip(t)
	calls := countSkillCalls(svc)

	// G3 蓝图漂移巡检：失败通道=记日志返回空串（非阻断）。
	require.Empty(t, svc.runBlueprintDriftPatrol(ctx, "run-x", 905),
		"G3 巡检熔断应走原失败通道：返回空串（非阻断）")

	// 一致性巡检：失败通道=记日志跳过（不落库、不广播、不阻断）。
	patrolEmitted := false
	t.Cleanup(swapEmitConsistencyPatrol(func(_ context.Context, _ map[string]any) { patrolEmitted = true }))
	svc.runConsistencyPatrol(ctx, "run-x", 905)
	require.False(t, patrolEmitted, "一致性巡检失败语义不变：不广播")

	// 全量体检：失败通道=needs_fix + error（由 runFullAlign 广播失败 note）。
	data, serr := svc.runStockAudit(ctx, 905)
	require.Error(t, serr)
	require.Contains(t, serr.Error(), "疑似供应商挂起", "提示统一「疑似供应商挂起」口径")
	require.Equal(t, "needs_fix", data.Verdict, "全量体检失败通道语义不变：needs_fix+error")

	require.Zero(t, *calls, "三个巡检/体检派发点熔断时必须零 agent 派发")
}

// TestProviderTripBreaksRewriteLoop 已退役：自动重写循环（finishFixHandoffRewrite /
// RewriteRounds / autoRewriteCap）随 R5-2 自动修复摘除而删除，逐章重写熔断护栏改由
// r5_execute_fix_test.go 的 TestServiceExecuteChapterRewriteTripGuarded（executeChapterRewrite
// 返回 errProviderTripped）与 TestProviderTripFixDispatchGuarded（工单修复派发零 agent）承接，
// 单源 dispatchSkill 护栏 + 源码锁定 TestSkillRunnerDispatchOnlyViaTripGuard 保证整循环中断。
