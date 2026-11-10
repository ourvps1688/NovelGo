package pipeline

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// blueprintTaskMode 是 S2 单次蓝图任务的 run 标记（#111 P2，§4.3）。
// 它是独立任务容器，不进 Orchestrator.Advance、不参与阶段自动推进；
// 与 detect:<kind> 容器并列，但**不是第五个检测 kind**。
const blueprintTaskMode = "blueprint-task"

// BlueprintTaskResult 是显式蓝图任务的派发回执。Dispatched=false 表示该书已有
// 同一个蓝图任务在跑，本次点击被幂等合并（不重复烧 LLM）。
type BlueprintTaskResult struct {
	RunID      string `json:"run_id"`
	NovelID    int64  `json:"novel_id"`
	Dispatched bool   `json:"dispatched"`
}

// RunBlueprintTask 是 S2 的**唯一新入口**（#111 P2，§4.3）：用户显式点击才启动一次
// 「全书蓝图生成 → 蓝图体检 → 落工单」。
//
// 硬约束（§4.2）：
//   - 不复用 StartPipeline（其自动裁决链路已退役），不调 DetermineEntryStage；
//   - 不往 DetectionKind* 加 blueprint（检测四 kind 独立启动是架构约束）；
//   - 不新增运行时质量闸门、不引入 auto-advance；
//   - 体检落工单后由用户在工单面板显式「去修复」触发 blueprint-fix，绝不自动派发修复。
//
// 复用既有链路：executeBlueprint → enterBlueprintAudit（唯一置 AuditKind=blueprint 处）
// → executeAudit 改派 blueprint-audit → runDetection 蓝图落法 → pipeline_issues；
// 既有 G1/G1b 是人工决策事件（awaiting_gate，无自动推进），沿用不改、不新增。
func (s *Service) RunBlueprintTask(ctx context.Context, novelID int64) (*BlueprintTaskResult, error) {
	if novelID <= 0 {
		return nil, fmt.Errorf("蓝图任务需要有效的小说 ID")
	}
	// 同书同类型任务 running 时幂等合并，避免误触重复跑长耗时 LLM 任务。
	// 必须按 mode 精确查：GetByNovel 只取全书最新一条非终态 run，后起的章节检测
	// 会把它顶掉，导致蓝图任务被重复派发。
	if active, err := s.store.GetRunningRunByMode(ctx, novelID, blueprintTaskMode); err == nil {
		return &BlueprintTaskResult{RunID: active.RunID, NovelID: novelID}, nil
	}

	run := &PipelineRun{
		NovelID:      novelID,
		RunID:        uuid.NewString(),
		Mode:         blueprintTaskMode,
		CurrentStage: StageS2,
		Status:       StatusRunning,
	}
	if err := s.store.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("创建蓝图任务记录失败: %w", err)
	}
	emitStage(ctx, StagePayload{
		RunID: run.RunID, NovelID: novelID, Stage: StageS2, Status: StatusRunning,
	})
	// 队列侧已注册 taskKindBlueprint → executeBlueprint；入队失败由 dispatchExecutor
	// 降级为后台 goroutine（非阻塞），legacy 分支与 advanceS1 范式一致。
	s.dispatchExecutor(taskKindBlueprint, run.RunID, novelID, func() {
		go s.executeBlueprint(run.RunID, novelID)
	})
	return &BlueprintTaskResult{
		RunID: run.RunID, NovelID: novelID, Dispatched: true,
	}, nil
}
