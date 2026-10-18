package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/volume"
)

// SkillRunnerFunc 是「执行某个 builtin skill」的可替换函数，默认走内部/agent.Run 多轮循环。
// 测试可注入 stub，使 S2 蓝图执行可确定性验证（无需真实 LLM）。
type SkillRunnerFunc func(ctx context.Context, name string, input SkillInput) (SkillOutput, error)

// Service 对外暴露流水线的生命周期控制：启动、查询、闸门续跑、运行控制。
// 由 app 层持有并通过转发方法绑定到前端。
type Service struct {
	store        *Store
	agent        *agent.Agent
	skillStore   *skill.Store
	llmClient    *llm.Client
	defaultProv  string
	defaultModel string
	appCtx       context.Context
	logger       *slog.Logger
	skillRunner  SkillRunnerFunc
}

// NewService 构造流水线服务。
//   - agent/skillStore/llmClient/defaultProv/defaultModel 用于 S2 蓝图阶段的多轮 agent 执行；
//     任一为 nil/空时，S2 降级为「直接落 G1 闸」（不实际跑蓝图，便于测试与降级）。
//   - appCtx 用于异步蓝图执行时的事件广播（须为 Wails 运行时 context）。
func NewService(db *gorm.DB, ag *agent.Agent, skillStore *skill.Store, llmClient *llm.Client, defaultProv, defaultModel string, appCtx context.Context, logger *slog.Logger) *Service {
	s := &Service{
		store:        NewStore(db),
		agent:        ag,
		skillStore:   skillStore,
		llmClient:    llmClient,
		defaultProv:  defaultProv,
		defaultModel: defaultModel,
		appCtx:       appCtx,
		logger:       logger,
	}
	if logger == nil {
		s.logger = slog.Default()
	}
	if appCtx == nil {
		s.appCtx = context.Background()
	}
	if ag != nil && skillStore != nil && llmClient != nil && defaultProv != "" && defaultModel != "" {
		s.skillRunner = func(ctx context.Context, name string, input SkillInput) (SkillOutput, error) {
			return RunSkill(ctx, ag, skillStore, llmClient, defaultProv, defaultModel, name, input)
		}
	} else {
		// 降级：不实际执行 skill（测试 / 未配置模型时），S2 直接落闸。
		s.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
			return SkillOutput{}, nil
		}
	}
	return s
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

	// S2 蓝图阶段：进入即异步执行 story-blueprint 多轮 agent，完成后落 G1 闸门。
	// 不在此处同步阻塞前端（蓝图可能耗时数分钟），先置 running 让前端显示「生成中」。
	if next == StageS2 {
		if err := s.store.SetStageStatus(ctx, runID, StageS2, StatusRunning, run.ContextJSON); err != nil {
			return err
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: StageS2, Status: StatusRunning})
		go s.executeBlueprint(run.RunID, run.NovelID)
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

// executeBlueprint 异步执行 S2 蓝图：跑 story-blueprint 多轮 agent（经 create_volume 建卷落库），
// 汇总卷结构写回 ContextJSON，最终置 S2 awaiting_gate 并广播 G1 闸门。
func (s *Service) executeBlueprint(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		s.logger.Error("S2 蓝图：加载流水线失败", "run_id", runID, "err", err)
		return
	}

	out, err := s.skillRunner(ctx, "story-blueprint", SkillInput{
		NovelID:   novelID,
		Prompt:    blueprintPrompt(novelID),
		MaxTurns:  12,
		SessionID: "pipeline:" + runID,
	})
	if err != nil {
		s.logger.Error("S2 蓝图执行失败", "run_id", runID, "err", err)
		// 失败时保留 S2 running 状态并广播错误阶段事件，供前端提示重试。
		emitStage(ctx, StagePayload{
			RunID:   runID,
			NovelID: novelID,
			Stage:   StageS2,
			Status:  StatusRunning,
			Note:    "蓝图生成失败：" + err.Error(),
		})
		return
	}

	// 汇总已由 create_volume 落库的卷记录，构建卷结构概览 JSON。
	blueprint := s.summarizeVolumes(ctx, novelID, out.Text)

	// 写回 ContextJSON 的 Blueprint 字段。
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	ctxt.Blueprint = blueprint
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S2 蓝图：序列化上下文失败", "err", merr)
		updated = []byte(run.ContextJSON)
	}
	// 守卫：若异步执行期间用户已推进（如点了「继续」进入 S3），不覆盖新状态。
	cur, gerr := s.store.GetByRunID(ctx, runID)
	if gerr == nil && (cur.CurrentStage != StageS2 || cur.Status != StatusRunning) {
		s.logger.Info("S2 蓝图：状态已变更，放弃落闸",
			"stage", cur.CurrentStage, "status", cur.Status)
		return
	}

	if err := s.store.SetStageStatus(ctx, runID, StageS2, StatusAwaitingGate, string(updated)); err != nil {
		s.logger.Error("S2 蓝图：落库闸门状态失败", "err", err)
		return
	}
	emitGate(ctx, GatePayload{
		RunID:   runID,
		NovelID: novelID,
		Stage:   StageS2,
		GateID:  GateG1,
		Summary: out.Text,
		Options: []string{"continue", "modify", "abort"},
	})
}

// summarizeVolumes 读取本书全部卷（create_volume 已落库），拼为前端可渲染的卷结构概览 JSON。
func (s *Service) summarizeVolumes(ctx context.Context, novelID int64, blueprintText string) string {
	vols, err := volume.NewStore(s.store.db, s.logger).ListByNovel(ctx, nil, novelID)
	if err != nil {
		s.logger.Warn("S2 蓝图：读取卷列表失败", "err", err)
	}
	type volView struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Sort int    `json:"sort_order"`
	}
	views := make([]volView, 0, len(vols))
	for _, v := range vols {
		views = append(views, volView{ID: v.ID, Name: v.Name, Sort: v.SortOrder})
	}
	payload := map[string]any{
		"summary": blueprintText,
		"volumes": views,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(b)
}

// blueprintPrompt 构造 S2 蓝图阶段的任务提示词。
func blueprintPrompt(novelID int64) string {
	return "请基于本书当前设定（先 read novelgo.md 与已有设定/卷纲了解现状），按 story-blueprint 方法论规划全书卷纲：" +
		"将全书划分为若干卷（通常 3-8 卷），每卷明确主题、核心冲突推进、爽点/情绪节奏与起止节点；" +
		"随后调用 create_volume 逐卷落库（name 必填、outline 写入该卷纲摘要），最后用中文给出一份卷结构概览作为回复。"
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
