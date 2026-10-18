package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Service 对外暴露流水线的生命周期控制：启动、查询、闸门续跑、运行控制。
// 由 app 层持有并通过转发方法绑定到前端。
type Service struct {
	store *Store
}

// NewService 构造流水线服务。
func NewService(db *gorm.DB) *Service {
	return &Service{store: NewStore(db)}
}

// StartPipeline 启动一次新流水线，初始阶段 S1(running)，并广播阶段事件。
// 续写模式（mode=continue）由调用方在 startChapter 传入起始章号。
func (s *Service) StartPipeline(ctx context.Context, novelID int64, mode string, startChapter *int) (*PipelineRun, error) {
	if mode == "" {
		mode = "new"
	}
	run := &PipelineRun{
		NovelID:      novelID,
		RunID:        uuid.NewString(),
		Mode:         mode,
		StartChapter: startChapter,
		CurrentStage: StageS1,
		Status:       StatusRunning,
	}
	if err := s.store.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("创建流水线记录失败: %w", err)
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: novelID, Stage: StageS1, Status: StatusRunning})
	return run, nil
}

// GetPipelineState 返回该小说当前活跃的流水线（无则返回错误，前端据此判断是否展示向导）。
func (s *Service) GetPipelineState(ctx context.Context, novelID int64) (*PipelineRun, error) {
	run, err := s.store.GetByNovel(ctx, novelID)
	if err != nil {
		return nil, err
	}
	return run, nil
}

// ResumePipeline 由前端在阶段完成或闸门点调用，按 decision 推进流程。
//   - decision=continue：推进到下一阶段（或直接进入下一闸门 awaiting_gate）。
//   - decision=abort：终止流水线（finished 的变体，置 aborted）。
//   - decision=modify/feedback：记录反馈后同样推进（C 期与 continue 等价，反馈落库供回看）。
func (s *Service) ResumePipeline(ctx context.Context, runID, decision, feedback string) error {
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		return fmt.Errorf("找不到流水线 %s: %w", runID, err)
	}
	if run.Status == StatusAborted || run.Status == StatusFinished {
		return fmt.Errorf("流水线已 %s，无法推进", run.Status)
	}
	if decision == "abort" {
		if err := s.store.SetStatus(ctx, runID, StatusAborted, run.ContextJSON); err != nil {
			return err
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: run.CurrentStage, Status: StatusAborted})
		return nil
	}

	if err := s.store.AppendGateDecision(ctx, runID, run.CurrentStage, decision, feedback); err != nil {
		return err
	}

	next, gate, err := NextStage(run.CurrentStage, decision)
	if err != nil {
		return err
	}
	if next == "" {
		if err := s.store.SetStatus(ctx, runID, StatusFinished, run.ContextJSON); err != nil {
			return err
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: run.CurrentStage, Status: StatusFinished})
		return nil
	}
	if gate != "" {
		// 进入下一闸门，等待用户确认
		if err := s.store.SetStageStatus(ctx, runID, next, StatusAwaitingGate, run.ContextJSON); err != nil {
			return err
		}
		emitGate(ctx, GatePayload{
			RunID:   runID,
			NovelID: run.NovelID,
			Stage:   next,
			GateID:  gate,
			Summary: "",
			Options: []string{"continue", "modify", "abort"},
		})
		return nil
	}
	// 无闸门，直接推进到下一阶段
	if err := s.store.SetStageStatus(ctx, runID, next, StatusRunning, run.ContextJSON); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: next, Status: StatusRunning})
	return nil
}

// ControlPipeline 处理运行控制指令：pause / resume / stop。
func (s *Service) ControlPipeline(ctx context.Context, runID, cmd string) error {
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		return fmt.Errorf("找不到流水线 %s: %w", runID, err)
	}
	switch cmd {
	case "pause":
		if run.Status != StatusRunning && run.Status != StatusAwaitingGate {
			return fmt.Errorf("当前状态 %s 无法暂停", run.Status)
		}
		if err := s.store.SetStatus(ctx, runID, StatusPaused, run.ContextJSON); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: StatusPaused})
	case "resume":
		if run.Status != StatusPaused {
			return fmt.Errorf("当前状态 %s 无法恢复", run.Status)
		}
		if err := s.store.SetStatus(ctx, runID, StatusRunning, run.ContextJSON); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: StatusRunning})
	case "stop":
		if err := s.store.SetStatus(ctx, runID, StatusAborted, run.ContextJSON); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: StatusAborted})
	default:
		return fmt.Errorf("未知控制命令 %q", cmd)
	}
	return nil
}

// 可替换的事件发射器，便于测试注入无操作实现。
var (
	emitStage      = EmitStage
	emitGate       = EmitGate
	emitControlAck = EmitControlAck
)

// TimeNow 暴露 time.Now，便于测试时统一；服务内部不依赖外部时钟。
func TimeNow() time.Time { return time.Now() }
