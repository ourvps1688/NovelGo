package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
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
	// chapterCount 返回本书已落库章节数，用于「黄金三章后自动进入 S4a 签约体检」的触发判定。
	// 测试可替换为 stub 以确定性驱动 S3→S4 流转。
	chapterCount func(ctx context.Context, novelID int64) (int, error)
	// volumeBoundary 判断本书最新两章是否分属不同卷（即刚跨入新卷）。用于 PR4 卷边界复检触发。
	// 测试可替换为 stub 以确定性驱动。
	volumeBoundary func(ctx context.Context, novelID int64) (bool, error)
	// hasLedger 判断本书是否已做过台账同步/建卷（以是否存在 volumes 记录为代理标志），
	// 供 PR5 的 DetermineEntryStage 在导入/续写时判定是否走 S0 对齐入口。可替换。
	hasLedger func(ctx context.Context, novelID int64) (bool, error)
	// orchestrator 是统一的阶段流转状态机（PR5 引入），ResumePipeline 委托它推进。
	orchestrator *Orchestrator
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
	if s.store != nil && s.store.db != nil {
		s.chapterCount = func(ctx context.Context, novelID int64) (int, error) {
			var c int64
			if err := s.store.db.WithContext(ctx).Table("chapters").Where("novel_id = ?", novelID).Count(&c).Error; err != nil {
				return 0, err
			}
			return int(c), nil
		}
		s.volumeBoundary = s.defaultVolumeBoundary
		s.hasLedger = s.defaultHasLedger
	}
	s.orchestrator = NewOrchestrator(s)
	return s
}

// StartPipeline 启动一次新流水线，并广播阶段事件。
// 续写模式（mode=continue）由调用方在 startChapter 传入起始章号。
// autoRun 开启后，流水线在自动化的阶段（S2 蓝图 / S4 体检 / S5 修复）自动连跑，
// 仅在闸门（G1 / G终）与需人工的写作（S1 孵化 / S3 逐章）处停下（详见 Orchestrator）。
// 初始阶段由 DetermineEntryStage 依本书现状自动选择：新书→S1；存量书（有章节无台账）→S0；
// 已就绪（有章节且有台账）→S3。
func (s *Service) StartPipeline(ctx context.Context, novelID int64, mode string, startChapter *int, autoRun bool) (*PipelineRun, error) {
	if mode == "" {
		mode = "new"
	}
	entry, err := s.DetermineEntryStage(ctx, novelID)
	if err != nil {
		// 判定失败（如章节表暂不可查）保守从 S1 起步，不阻断启动。
		entry = StageS1
	}
	run := &PipelineRun{
		NovelID:      novelID,
		RunID:        uuid.NewString(),
		Mode:         mode,
		StartChapter: startChapter,
		CurrentStage: entry,
		Status:       StatusRunning,
		AutoRun:      autoRun,
	}
	if err := s.store.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("创建流水线记录失败: %w", err)
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: novelID, Stage: entry, Status: StatusRunning})
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

	// PR5 起，所有阶段推进逻辑统一收敛到 Orchestrator.Advance（internal/pipeline/orchestrator.go），
	// 此处不再保留任何阶段分支（消除与 orchestrator_c.go 的 NextStage 双源）。
	return s.orchestrator.Advance(ctx, run, decision)
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

// ── S4 签约体检 / S5 修复闭环（PR3）────────────────────────────────

// abortRun 终止流水线并广播终止事件（供特殊阶段处理复用）。
func (s *Service) abortRun(ctx context.Context, run *PipelineRun) error {
	if err := s.store.SetStatus(ctx, run.RunID, StatusAborted, run.ContextJSON); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: run.CurrentStage, Status: StatusAborted})
	return nil
}

// reachedGoldenThree 判断本书是否已写完「黄金三章」，触发 S4a 签约体检。
func (s *Service) reachedGoldenThree(ctx context.Context, novelID int64) bool {
	if s.chapterCount == nil {
		return false
	}
	n, err := s.chapterCount(ctx, novelID)
	if err != nil {
		s.logger.Warn("S4 触发：统计章节数失败", "err", err)
		return false
	}
	return n >= 3
}

// crossedVolumeBoundary 判断本书最新两章是否分属不同卷（即刚跨入新卷）。
// 用于 PR4「卷边界复检」触发：一旦进入新卷，里程碑闸即提供「进入签约体检」选项。
func (s *Service) crossedVolumeBoundary(ctx context.Context, novelID int64) bool {
	if s.volumeBoundary == nil {
		return false
	}
	crossed, err := s.volumeBoundary(ctx, novelID)
	if err != nil {
		s.logger.Warn("卷边界判定：查询失败", "err", err)
		return false
	}
	return crossed
}

// defaultVolumeBoundary 取本书按全局阅读序最新的两章，比较其 volume_id 是否不同。
// 复用了 chapter 仓储的排序口径（未分卷 NULL 优先、再按 volumes.sort_order、chapters.sort_order、id）。
func (s *Service) defaultVolumeBoundary(ctx context.Context, novelID int64) (bool, error) {
	if s.store == nil || s.store.db == nil {
		return false, nil
	}
	type volRow struct {
		VolumeID *int64
	}
	var rows []volRow
	err := s.store.db.WithContext(ctx).
		Table("chapters").
		Select("chapters.volume_id").
		Joins("LEFT JOIN volumes ON volumes.id = chapters.volume_id").
		Where("chapters.novel_id = ?", novelID).
		Order("chapters.volume_id IS NULL DESC, volumes.sort_order DESC, chapters.sort_order DESC, chapters.id DESC").
		Limit(2).
		Scan(&rows).Error
	if err != nil {
		return false, err
	}
	if len(rows) < 2 {
		return false, nil
	}
	a, b := rows[0].VolumeID, rows[1].VolumeID
	if a == nil && b == nil {
		return false, nil
	}
	if a == nil || b == nil {
		return true, nil
	}
	return *a != *b, nil
}

// auditPassed 返回本书是否已做过签约体检（S4Audited 标记）。
func (s *Service) auditPassed(ctx context.Context, runID string) bool {
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		return false
	}
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	return ctxt.S4Audited
}

// markAuditedJSON 在原 ContextJSON 上置 S4Audited=true 并记录当前已体检到的章节数，
// 返回序列化结果。chapterCount 用于 PR4 里程碑闸的间距判定（避免每章重复复检）。
func (s *Service) markAuditedJSON(contextJSON string, chapterCount int) string {
	var ctxt GateContext
	if contextJSON != "" {
		_ = json.Unmarshal([]byte(contextJSON), &ctxt)
	}
	ctxt.S4Audited = true
	ctxt.LastAuditChapter = chapterCount
	b, err := json.Marshal(ctxt)
	if err != nil {
		return contextJSON
	}
	return string(b)
}

// milestoneSummary 生成 G3b 里程碑闸的提示文案（中文，由后端直接下发给前端展示）。
func milestoneSummary(n, sinceLast int, crossed bool) string {
	if crossed {
		return fmt.Sprintf("已跨入新卷（累计 %d 章，距上次体检 %d 章）。建议在「卷边界复评」中做一次全卷签约体检，或继续写作。", n, sinceLast)
	}
	return fmt.Sprintf("已写 %d 章，距上次签约体检已过 %d 章（每 5 章触发一次）。建议进入签约体检与修复闭环，或继续写作。", n, sinceLast)
}

// enterAudit 置体检阶段 running 并异步执行 signability-audit。
// reason 决定进入的阶段：golden3 → S4（首检）；milestone / volume → S4b（卷边界复评）。
// 同时把触发原因与体检阶段写回 ContextJSON，供修复后复评回到同阶段。
func (s *Service) enterAudit(ctx context.Context, run *PipelineRun, reason string) {
	stage := StageS4
	if reason != "golden3" {
		stage = StageS4b
	}
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	ctxt.AuditReason = reason
	ctxt.AuditStage = stage
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S4 进入：序列化上下文失败", "err", merr)
		updated = []byte(run.ContextJSON)
	}
	if err := s.store.SetStageStatus(ctx, run.RunID, stage, StatusRunning, string(updated)); err != nil {
		s.logger.Error("S4 进入失败", "err", err)
		return
	}
	run.CurrentStage = stage
	run.ContextJSON = string(updated)
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: stage, Status: StatusRunning})
	go s.executeAudit(run.RunID, run.NovelID)
}

// enterFix 置 S5 running 并异步执行 signability-fix 修复循环。
func (s *Service) enterFix(ctx context.Context, run *PipelineRun) {
	if err := s.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning, run.ContextJSON); err != nil {
		s.logger.Error("S5 进入失败", "err", err)
		return
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS5, Status: StatusRunning})
	go s.executeFix(run.RunID, run.NovelID)
}

// executeAudit 异步执行签约体检：跑 signability-audit（只读）→ 解析 issue 清单 →
// 写回 ContextJSON.Audit → 置体检阶段 awaiting_gate 并广播 G4 闸门（options: continue/fix/abort）。
// 体检阶段（S4 首检 / S4b 卷边界复评）以进入时的 run.CurrentStage 为准，保证复评回到同阶段。
func (s *Service) executeAudit(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		s.logger.Error("S4 体检：加载流水线失败", "run_id", runID, "err", err)
		return
	}
	auditStage := run.CurrentStage // S4 或 S4b

	out, err := s.skillRunner(ctx, "signability-audit", SkillInput{
		NovelID:   novelID,
		Prompt:    signabilityPrompt(novelID),
		MaxTurns:  12,
		SessionID: "pipeline:" + runID + ":audit",
	})
	if err != nil {
		s.logger.Error("S4 体检执行失败", "run_id", runID, "err", err)
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: auditStage, Status: StatusRunning, Note: "签约体检失败：" + err.Error()})
		return
	}

	audit := parseAudit(out.Text)
	auditJSON, _ := json.Marshal(audit)

	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	ctxt.Audit = string(auditJSON)
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S4 体检：序列化上下文失败", "err", merr)
		updated = []byte(run.ContextJSON)
	}

	// 守卫：若异步执行期间用户已推进（如点了 fix / continue），不覆盖新状态。
	cur, gerr := s.store.GetByRunID(ctx, runID)
	if gerr == nil && (cur.CurrentStage != auditStage || cur.Status != StatusRunning) {
		s.logger.Info("S4 体检：状态已变更，放弃落闸", "stage", cur.CurrentStage, "status", cur.Status)
		return
	}

	// auto_run 自动裁决：仅对普通签约体检（S4/S4b）生效；完本终检（S6）必须人工确认 finish。
	// 通过 → continue 回 S3；不通过 → fix 进 S5，但受修复轮数上限约束，避免无限循环。
	if run.AutoRun && auditStage != StageS6 {
		if audit.Verdict == "pass" {
			if err := s.orchestrator.Advance(ctx, run, "continue"); err != nil {
				s.logger.Error("S4 体检：自动继续推进失败", "err", err)
			}
			return
		}
		if ctxt.FixRounds >= autoFixCap {
			// 超出自动修复上限：改交人工决策，避免无休止自动修复。
			if err := s.store.SetStageStatus(ctx, runID, auditStage, StatusAwaitingGate, string(updated)); err != nil {
				s.logger.Error("S4 体检：落库闸门状态失败", "err", err)
				return
			}
			s.emitGateDecision(ctx, runID, novelID, auditStage, audit)
			return
		}
		if err := s.orchestrator.Advance(ctx, run, "fix"); err != nil {
			s.logger.Error("S4 体检：自动进入修复闭环失败", "err", err)
		}
		return
	}

	if err := s.store.SetStageStatus(ctx, runID, auditStage, StatusAwaitingGate, string(updated)); err != nil {
		s.logger.Error("S4 体检：落库闸门状态失败", "err", err)
		return
	}
	s.emitGateDecision(ctx, runID, novelID, auditStage, audit)
}

// emitGateDecision 依据体检阶段广播对应的体检闸门：S4/S4b 用 G4（continue/fix/abort），
// 完本终检 S6 用 G终（finish/fix/abort），避免误把完本按钮当成「继续写作」。
func (s *Service) emitGateDecision(ctx context.Context, runID string, novelID int64, auditStage string, audit AuditData) {
	summary := audit.Summary
	if summary == "" {
		if auditStage == StageS6 {
			summary = "完本终检已完成，请确认是否完本或进入修复闭环。"
		} else {
			summary = "签约体检已完成，请确认是否继续或进入修复闭环。"
		}
	}
	gateID := GateG4
	options := []string{"continue", "fix", "abort"}
	if auditStage == StageS6 {
		gateID = GateGEnd
		options = []string{"finish", "fix", "abort"}
	}
	emitGate(ctx, GatePayload{
		RunID:   runID,
		NovelID: novelID,
		Stage:   auditStage,
		GateID:  gateID,
		Summary: summary,
		Options: options,
	})
}

// executeFix 异步执行 S5 修复：跑 signability-fix（按 issue 清单改稿 / 同步台账）→
// 修复完成后自动回到 executeAudit 复评（仍异步）。复评由 G4 闸门交回用户决策。
func (s *Service) executeFix(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		s.logger.Error("S5 修复：加载流水线失败", "run_id", runID, "err", err)
		return
	}

	out, err := s.skillRunner(ctx, "signability-fix", SkillInput{
		NovelID:   novelID,
		Prompt:    fixPrompt(novelID, run.ContextJSON),
		MaxTurns:  12,
		SessionID: "pipeline:" + runID + ":fix",
	})
	if err != nil {
		s.logger.Error("S5 修复执行失败", "run_id", runID, "err", err)
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS5, Status: StatusRunning, Note: "修复失败：" + err.Error()})
		return
	}
	_ = out

	// 守卫：若异步期间状态已变更（如用户中止），不自动复评。
	cur, gerr := s.store.GetByRunID(ctx, runID)
	if gerr == nil && (cur.CurrentStage != StageS5 || cur.Status != StatusRunning) {
		s.logger.Info("S5 修复：状态已变更，放弃复评", "stage", cur.CurrentStage, "status", cur.Status)
		return
	}

	// 修复完成 → 复位为原体检阶段（S4 / S4b / S6）并自动复评（仍异步）。
	// 复位到对应体检阶段 running 是为了让 executeAudit 的「状态未变更」守卫判定通过，
	// 否则守卫会因当前为 S5 而放弃落 G4 闸门，导致复评结果无家可归（见测试 TestServiceAuditFixLoopReAudit）。
	auditStage := StageS4
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	if ctxt.AuditStage != "" {
		auditStage = ctxt.AuditStage
	}
	ctxt.FixRounds++ // 递增修复轮数（供 auto_run 上限判断，避免无限自动修复）
	ctxtJSON, _ := json.Marshal(ctxt)
	if err := s.store.SetStageStatus(ctx, runID, auditStage, StatusRunning, string(ctxtJSON)); err != nil {
		s.logger.Error("S5 修复：复评前复位阶段失败", "err", err)
		return
	}
	s.executeAudit(runID, novelID)
}

// autoFixCap 是 auto_run 模式下「不通过→自动修复」的最大轮数上限，超过则交人工决策，避免无限循环。
const autoFixCap = 3

// ── S6 完本终检（PR5）────────────────────────────────

// enterFinalAudit 置完本终检阶段 running 并异步执行 signability-audit 全本体检。
// 复用 enterAudit 范式（reason="final"，stage=S6）→ executeFinalAudit。
func (s *Service) enterFinalAudit(ctx context.Context, run *PipelineRun) {
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	ctxt.AuditReason = "final"
	ctxt.AuditStage = StageS6
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S6 进入：序列化上下文失败", "err", merr)
		updated = []byte(run.ContextJSON)
	}
	if err := s.store.SetStageStatus(ctx, run.RunID, StageS6, StatusRunning, string(updated)); err != nil {
		s.logger.Error("S6 进入失败", "err", err)
		return
	}
	run.CurrentStage = StageS6
	run.ContextJSON = string(updated)
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS6, Status: StatusRunning})
	go s.executeFinalAudit(run.RunID, run.NovelID)
}

// executeFinalAudit 异步执行完本级终检：跑 signability-audit 全本 → 解析 → 落 ContextJSON.Audit →
// 广播 G终闸门（options: finish/fix/abort）。G终必须人工确认 finish，绝不自动完本。
func (s *Service) executeFinalAudit(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		s.logger.Error("S6 终检：加载流水线失败", "run_id", runID, "err", err)
		return
	}

	out, err := s.skillRunner(ctx, "signability-audit", SkillInput{
		NovelID:   novelID,
		Prompt:    finalAuditPrompt(novelID),
		MaxTurns:  16,
		SessionID: "pipeline:" + runID + ":final",
	})
	if err != nil {
		s.logger.Error("S6 终检执行失败", "run_id", runID, "err", err)
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS6, Status: StatusRunning, Note: "完本终检失败：" + err.Error()})
		return
	}

	audit := parseAudit(out.Text)
	auditJSON, _ := json.Marshal(audit)

	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	ctxt.Audit = string(auditJSON)
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		updated = []byte(run.ContextJSON)
	}

	// 守卫：若异步期间状态已变更（如用户中止），不覆盖新状态。
	cur, gerr := s.store.GetByRunID(ctx, runID)
	if gerr == nil && (cur.CurrentStage != StageS6 || cur.Status != StatusRunning) {
		s.logger.Info("S6 终检：状态已变更，放弃落闸", "stage", cur.CurrentStage, "status", cur.Status)
		return
	}

	// G终绝不自动裁决，必须人工确认 finish；fix 后的复评由 executeAudit 自动处理。
	if err := s.store.SetStageStatus(ctx, runID, StageS6, StatusAwaitingGate, string(updated)); err != nil {
		s.logger.Error("S6 终检：落库闸门状态失败", "err", err)
		return
	}
	s.emitGateDecision(ctx, runID, novelID, StageS6, audit)
}

// finalAuditPrompt 构造 S6 完本级终检的任务提示词。
func finalAuditPrompt(novelID int64) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 signability-audit 方法论，对全书做**完本级**终检："+
		"通读所有成稿章节，逐维打分并举证；最终回复末尾**必须**附一个 ```json 代码块，格式：{\"verdict\":\"pass|needs_fix\",\"score\":0,\"summary\":\"...\",\"issues\":[{\"id\":\"I1\",\"level\":\"P0|P1\",\"area\":\"维度\",\"desc\":\"...\"}]}。"+
		"存在任意 P0 致命伤 → verdict=needs_fix；无 P0 且 score≥30/35 → pass。", novelID)
}

// ── 入口判定与安全检查点（PR5）────────────────────────────────

// DetermineEntryStage 依据本书现状自动选择流水线入口：
//   - 无章节 → S1（新书，从孵化起步）；
//   - 有章节(>3)且无台账(无 volumes) → S0（导入/续写对齐入口）；
//   - 已就绪（有章节且有台账） → S3（直接汇入逐章写作循环）。
//
// 判定失败（如章节表暂不可查）保守返回 S1，不阻断启动。
func (s *Service) DetermineEntryStage(ctx context.Context, novelID int64) (string, error) {
	n, err := s.chapterCount(ctx, novelID)
	if err != nil {
		return StageS1, err
	}
	if n == 0 {
		return StageS1, nil
	}
	hasLedger, lerr := s.hasLedger(ctx, novelID)
	if lerr != nil {
		hasLedger = false // 查台账失败不阻断，保守按存量书处理
	}
	if n > 3 && !hasLedger {
		return StageS0, nil
	}
	return StageS3, nil
}

// defaultHasLedger 以是否存在 volumes 记录作为「已建卷/台账同步」的代理标志。
func (s *Service) defaultHasLedger(ctx context.Context, novelID int64) (bool, error) {
	if s.store == nil || s.store.db == nil {
		return false, nil
	}
	var c int64
	if err := s.store.db.WithContext(ctx).Table("volumes").Where("novel_id = ?", novelID).Count(&c).Error; err != nil {
		return false, err
	}
	return c > 0, nil
}

// setS3Running 把流水线置为 S3 逐章写作（running），供里程碑/普通闸在 auto_run 或继续时复用。
func (s *Service) setS3Running(ctx context.Context, run *PipelineRun) error {
	if err := s.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, run.ContextJSON); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS3, Status: StatusRunning})
	return nil
}

// pausedOrAborted 安全检查点：若流水线已被暂停或中止，返回 true（调用方应挂起不推进）。
// 重新从库读取最新状态，以捕获运行中的 ControlPipeline(pause/stop)。
func (s *Service) pausedOrAborted(ctx context.Context, run *PipelineRun) bool {
	if run.Status == StatusPaused || run.Status == StatusAborted {
		return true
	}
	cur, err := s.store.GetByRunID(ctx, run.RunID)
	if err != nil {
		return false
	}
	return cur.Status == StatusPaused || cur.Status == StatusAborted
}

// auditJSONRe 提取 signability-audit 输出中的 ```json ... ``` 结构化块。
var auditJSONRe = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// parseAudit 从 skill 输出文本解析结构化体检结果；失败则退化为单条 P0 issue。
func parseAudit(text string) AuditData {
	data := AuditData{Verdict: "needs_fix"}
	m := auditJSONRe.FindStringSubmatch(text)
	raw := text
	if len(m) > 1 {
		raw = m[1]
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return AuditData{
			Verdict: "needs_fix",
			Summary: text,
			Issues:  []AuditIssue{{ID: "I1", Level: "P0", Area: "总体", Desc: text}},
		}
	}
	if data.Verdict == "" {
		data.Verdict = "needs_fix"
	}
	if data.Issues == nil {
		data.Issues = []AuditIssue{}
	}
	return data
}

// signabilityPrompt 构造 S4 签约体检阶段的任务提示词。
func signabilityPrompt(novelID int64) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 signability-audit 方法论，对已成稿章节做签约级体检："+
		"用 get_chapter_list 列出章节，抽样通读前三章（黄金三章）+ 最新章 + 任意中段章，逐维打分并举证；"+
		"最终回复末尾**必须**附一个 ```json 代码块，格式：{\"verdict\":\"pass|needs_fix\",\"score\":0,\"summary\":\"...\",\"issues\":[{\"id\":\"I1\",\"level\":\"P0|P1\",\"area\":\"维度\",\"desc\":\"...\"}]}。"+
		"存在任意 P0 致命伤 → verdict=needs_fix；无 P0 且 score≥25/35 → pass。", novelID)
}

// fixPrompt 构造 S5 修复阶段的任务提示词，内嵌 S4 产出的 issue 清单 JSON。
func fixPrompt(novelID int64, contextJSON string) string {
	var ctxt GateContext
	if contextJSON != "" {
		_ = json.Unmarshal([]byte(contextJSON), &ctxt)
	}
	audit := ctxt.Audit
	if audit == "" {
		audit = "{}"
	}
	return fmt.Sprintf("本书 novel_id=%d。以下是 S4 签约体检产出的 issue 清单（JSON），请逐条修复：\n%s\n\n"+
		"按 signability-fix 方法论：P0 优先于 P1，用 edit/update_chapter_plan/create_volume/台账写工具修复，"+
		"修复后给出不超过 15 行的修复摘要。", novelID, audit)
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
	case "set_auto":
		// 运行中切换 auto_run：翻转 AutoRun 字段。开启后，自动化的阶段（S2/S4/S5）
		// 在闸门点自动裁决连跑；关闭则恢复为人工逐闸确认。
		if err := s.store.SetAutoRun(ctx, runID, !run.AutoRun); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: run.Status})
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
