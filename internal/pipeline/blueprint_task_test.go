package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRunBlueprintTaskCreatesExplicitRun 验证 S2 显式入口：一次点击只落一个
// blueprint-task run（停在 S2 running），回执如实标注已派发；体检落工单后由用户
// 在工单面板显式「去修复」，本入口绝不推进后续阶段。
func TestRunBlueprintTaskCreatesExplicitRun(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// RunBlueprintTask 会 go executeBlueprint(...)：队列不可用时走 legacy 异步派发。
	// 测试里把 breaker 保持熔断直到该 goroutine 收敛，避免它跨到后续测试真实跑
	// 蓝图/体检链（会访问 volumes 等表并污染全局 slot）。收敛信号取失败分支的
	// emitStage(Note!= "")：熔断下 dispatchSkill 立即返回哨兵错误，executeBlueprint
	// 广播一次错误阶段事件后即返回，零 agent 派发、零真实 LLM。
	orig := breakerOpenProviders
	breakerOpenProviders = func() []string { return []string{"deepseek"} }
	done := make(chan struct{}, 1)
	restore := swapEmitStage(func(_ context.Context, p StagePayload) {
		if p.Note == "" {
			return
		}
		select {
		case done <- struct{}{}:
		default:
		}
	})
	t.Cleanup(func() {
		restore()
		breakerOpenProviders = orig
	})

	_, err := svc.RunBlueprintTask(ctx, 0)
	require.Error(t, err, "novelID<=0 必须拒绝，不得建 run")
	var n int64
	db.Model(&PipelineRun{}).Count(&n)
	require.Zero(t, n, "非法入参不得留下任何 run 记录")

	res, err := svc.RunBlueprintTask(ctx, 7701)
	require.NoError(t, err)
	require.True(t, res.Dispatched)
	require.NotEmpty(t, res.RunID)
	require.EqualValues(t, 7701, res.NovelID)

	run, err := svc.store.GetByRunID(ctx, res.RunID)
	require.NoError(t, err)
	require.Equal(t, blueprintTaskMode, run.Mode)
	require.Equal(t, StageS2, run.CurrentStage)
	require.Equal(t, StatusRunning, run.Status, "零 auto-advance：蓝图落工单后须由用户显式触发修复")

	select {
	case <-done: // 异步执行体已在熔断下收敛，不会串扰其它测试
	case <-time.After(2 * time.Second):
		t.Log("blueprint goroutine 未在 2s 内收敛（熔断态仍保持，不影响断言）")
	}
}

// TestRunBlueprintTaskIdempotentWhileRunning 同书已有 running 蓝图任务时合并点击：
// 不新增 run、不重复派发（「默认关闭、纯显式按钮」的防误触护栏，不重复烧 LLM）。
func TestRunBlueprintTaskIdempotentWhileRunning(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// 预置同类型 running 任务，使本次点击走幂等分支，全程零异步派发。
	require.NoError(t, svc.store.Create(ctx, &PipelineRun{
		NovelID: 7702, RunID: "blueprint-existing",
		Mode: blueprintTaskMode, CurrentStage: StageS2, Status: StatusRunning,
	}))

	res, err := svc.RunBlueprintTask(ctx, 7702)
	require.NoError(t, err)
	require.False(t, res.Dispatched)
	require.Equal(t, "blueprint-existing", res.RunID)

	var count int64
	db.Model(&PipelineRun{}).
		Where("novel_id = ? AND mode = ?", 7702, blueprintTaskMode).
		Count(&count)
	require.EqualValues(t, 1, count, "幂等合并不得新增 run")
}

// TestRunBlueprintTaskIdempotentWithNewerDetectRun 回归：同书先起蓝图、后起章节
// 检测（detect run 更新）时，再点蓝图仍须合并到蓝图 run。若幂等判断退回
// GetByNovel（只取全书最新一条非终态 run），会被 detect run 顶掉而重复派发。
func TestRunBlueprintTaskIdempotentWithNewerDetectRun(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	require.NoError(t, svc.store.Create(ctx, &PipelineRun{
		NovelID: 7703, RunID: "blueprint-first",
		Mode: blueprintTaskMode, CurrentStage: StageS2, Status: StatusRunning,
	}))
	// 后起的、id 更大的章节检测容器。
	require.NoError(t, svc.store.Create(ctx, &PipelineRun{
		NovelID: 7703, RunID: "chapter-detect-newer",
		Mode: detectModeOf(DetectionKindChapter), CurrentStage: StageS1, Status: StatusRunning,
	}))

	res, err := svc.RunBlueprintTask(ctx, 7703)
	require.NoError(t, err)
	require.False(t, res.Dispatched, "蓝图任务仍在跑时不得被后起的检测任务顶掉幂等判断")
	require.Equal(t, "blueprint-first", res.RunID)

	var count int64
	db.Model(&PipelineRun{}).
		Where("novel_id = ? AND mode = ?", 7703, blueprintTaskMode).
		Count(&count)
	require.EqualValues(t, 1, count, "不得重复新建蓝图任务")
}

// stripGoLineComments 去掉整行注释与行尾注释，供静态契约断言只看真实代码。
// 注释里会正当地出现「不复用 StartPipeline」这类被禁词，不能据此误判。
func stripGoLineComments(src string) string {
	var b strings.Builder
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// TestBlueprintTaskStaticContract 源码级锁住 §4.2 硬约束：不恢复 StartPipeline
// 自动裁决、不新增检测 kind、零 auto-advance、零自动修复/复评，且复用既有
// executeBlueprint 链与显式前端按钮。
func TestBlueprintTaskStaticContract(t *testing.T) {
	raw, err := os.ReadFile("blueprint_task.go")
	require.NoError(t, err)
	code := stripGoLineComments(string(raw))

	for _, banned := range []string{
		"StartPipeline", "DetermineEntryStage", "DetectionKind",
		"StartIssueFixTickets", "VerifyIssueTickets", ".Advance(", "RunDetectionTask",
		"GetByNovel", // 幂等判断必须按 mode 精确查，不得退回「全书最新一条」
	} {
		require.NotContains(t, code, banned, "S2 入口不得出现 %s", banned)
	}
	for _, required := range []string{
		"taskKindBlueprint", "executeBlueprint", "StageS2", "StatusRunning",
		"blueprintTaskMode", "dispatchExecutor", "s.store.Create(ctx, run)",
		"GetRunningRunByMode",
	} {
		require.Contains(t, code, required, "S2 入口须复用 %s", required)
	}

	// 独立任务容器，不是 detect:<kind>，也不是第五个检测 kind。
	require.Equal(t, "blueprint-task", blueprintTaskMode)
	require.NotContains(t, blueprintTaskMode, "detect:")

	// 检测 kind 常量恒为四个（chapter/volume/book/structure），P2 不得加第五个。
	detectRaw, err := os.ReadFile("detect.go")
	require.NoError(t, err)
	kinds := regexp.MustCompile(`(?m)^\s*DetectionKind[A-Za-z]+\s*=`).
		FindAllString(stripGoLineComments(string(detectRaw)), -1)
	require.Len(t, kinds, 4, "检测四 kind 独立启动是架构约束，S2 不得新增第五 kind")

	// app 层只允许薄转发，不得夹带自动裁决/自动修复。
	appRaw, err := os.ReadFile(filepath.Join("..", "..", "app", "blueprint_task.go"))
	require.NoError(t, err)
	appCode := stripGoLineComments(string(appRaw))
	require.Contains(t, appCode, "a.pipeline.RunBlueprintTask(a.ctx, novelID)")
	for _, banned := range []string{
		"StartPipeline", "StartIssueFixTickets", "VerifyIssueTickets", "DetectionKind",
	} {
		require.NotContains(t, appCode, banned)
	}

	// 前端按钮必须显式：RunBlueprintTask 只能由 onClick 触发，不得在挂载/effect
	// 里自动启动（零 auto-advance 的 UI 侧护栏），且旧流水线入口无残留。
	panelRaw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "components", "skill", "BlueprintTaskPanel.tsx"))
	require.NoError(t, err)
	panel := string(panelRaw)
	require.Contains(t, panel, "RunBlueprintTask(")
	require.Contains(t, panel, "onClick={start}")
	require.NotContains(t, panel, "useEffect", "蓝图任务不得在挂载时自动启动")
	require.NotContains(t, panel, "StartPipeline", "前端不得残留旧流水线入口")

	viewRaw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "components", "skill", "SkillMatrixView.tsx"))
	require.NoError(t, err)
	require.Contains(t, string(viewRaw), "<BlueprintTaskPanel", "显式面板须挂到技能矩阵视图")
}
