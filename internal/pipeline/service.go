package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/retcon"
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
	// stateSyncRunner 对单章执行后台状态同步（statesync 子 Agent）。由 app 层注入（依赖 agent.RunSubAgent）。
	// 为 nil 时 executeS0Align 跳过 statesync（仅做建卷），测试可注入 stub。
	stateSyncRunner func(ctx context.Context, novelID, chapterID int64) error
	// chapterStore 用于「回流 S3 重写回路」把 issue 里的章节阅读序号映射回真实 chapter_id。
	// 默认从 s.store.db 构造；测试可注入或用 nil（nil 时重写回路降级为手动交回）。
	chapterStore *chapter.Store
	// chapterLister 返回本书全部章节 ID（用于 S0 批量 statesync）。默认走 raw gorm 查询。
	chapterLister func(ctx context.Context, novelID int64) ([]int64, error)
	// volumeAssigner 为存量书建默认卷并把所有未分卷章节归入该卷（修复 G3b 卷边界判定）。默认走 raw gorm。
	volumeAssigner func(ctx context.Context, novelID int64) error
	// volumeFreezer 是 G_freeze 自动闸门（WBS 2.5）的执行体：把指定卷置冻结并提交
	// committed snapshot（含 2.4 冻结前台账审计）。由 app 层注入 internal/snapshot.Service.Freeze；
	// 测试可注入 stub。为 nil 时跳过自动冻结（仅告警），不影响收口流转。
	volumeFreezer func(ctx context.Context, novelID, volumeID int64) error
	// bibleProvider 返回「已冻结卷既定事实」文本块（internal/snapshot 快照渲染，WBS 2.3），
	// 注入 S4/S5/S6/章节重写提示词作为校验与前向修复依据。由 app 层注入
	// internal/snapshot.Service.EstablishedFactsText；nil 或出错时降级为不注入（旧行为）。
	bibleProvider func(ctx context.Context, novelID int64) (string, error)
	// voiceProvider 返回创作心智正文（PersonaHonestyNote 由 provider 端拼进正文，P0 WBS §0.7），
	// 注入重写/体检/修复/巡检提示词作为文风锚定块。由 app 层用
	// agentcfg.BuildPersonaSystemText 实现；nil 或出错时降级为不注入。
	voiceProvider func(ctx context.Context, novelID int64) (string, error)
	// chapterTextReader 返回单章正文（P2 漂移检测采样输入，WBS p2 §0.1）。
	// 由 app 层用 git.ReadFile(novelID, git.ChapterPath(id)) 装配；nil 时漂移检测
	// 整体跳过（降级零 issue，宁缺勿错）。
	chapterTextReader func(ctx context.Context, novelID, chapterID int64) (string, error)
	// voiceMetricsProvider 返回本书 Voice 台账基线指标（voice.MergeMetrics 装配，
	// P2 漂移检测基线）。nil / 出错 / 空基线时漂移检测整体跳过（不造默认基线）。
	voiceMetricsProvider func(ctx context.Context, novelID int64) (map[string]float64, error)
	// intentProvider 返回创作意图正文（卖点定位），rewrite 路径注入意图锚定块（P0 WBS §4）。
	// T4.1 定源后由 app 层注入；nil 或出错时降级为不注入。
	intentProvider func(ctx context.Context, novelID int64) (string, error)
	// retconGate 是 retcon 审批闸门（WBS 3.1，internal/retcon）：S5 交回的写保护/retcon
	// 候选沉淀为持久化候选案（状态机起点），供作者显式审批后受控回改冻结卷。
	// 未注入时跳过沉淀（降级为只暂停交回），不影响安全出口流转。
	retconGate *retcon.Service
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
		s.chapterLister = func(ctx context.Context, novelID int64) ([]int64, error) {
			var ids []int64
			if err := s.store.db.WithContext(ctx).Table("chapters").Where("novel_id = ?", novelID).Order("id ASC").Pluck("id", &ids).Error; err != nil {
				return nil, err
			}
			return ids, nil
		}
		s.volumeAssigner = s.defaultVolumeAssigner
		s.chapterStore = chapter.NewStore(s.store.db, s.logger)
		// stateSyncRunner 默认 nil：依赖 app 层注入（需要 agent.RunSubAgent）。
		// 生产环境 app/handler.go 会注入；测试可注入 stub 或保持 nil（executeS0Align 跳过 statesync）。
	}
	s.orchestrator = NewOrchestrator(s)
	return s
}

// SetStateSyncRunner 注入 statesync 子 Agent 执行器（由 app 层用 agent.RunSubAgent 实现）。
// 生产环境必须注入，否则 executeS0Align 会跳过 statesync 仅做建卷。
func (s *Service) SetStateSyncRunner(fn func(ctx context.Context, novelID, chapterID int64) error) {
	s.stateSyncRunner = fn
}

// SetVolumeFreezer 注入 G_freeze 自动冻结执行体（由 app 层用 internal/snapshot.Service.Freeze 实现，
// 冻结即同事务提交 committed snapshot）。未注入时自动冻结降级为跳过（告警），不影响流水线流转。
func (s *Service) SetVolumeFreezer(fn func(ctx context.Context, novelID, volumeID int64) error) {
	s.volumeFreezer = fn
}

// SetBibleProvider 注入「已冻结卷既定事实」文本提供者（由 app 层用
// internal/snapshot.Service.EstablishedFactsText 实现）。未注入/出错时降级为不注入。
func (s *Service) SetBibleProvider(fn func(ctx context.Context, novelID int64) (string, error)) {
	s.bibleProvider = fn
}

// SetVoiceProvider 注入「创作心智」文本提供者（由 app 层用
// agentcfg.BuildPersonaSystemText 实现；persona 未配置/解析失败返回空串即降级）。
// 未注入/出错时降级为不注入。
func (s *Service) SetVoiceProvider(fn func(ctx context.Context, novelID int64) (string, error)) {
	s.voiceProvider = fn
}

// SetChapterTextReader 注入单章正文读取体（P2 漂移检测，由 app 层用
// git.ReadFile+git.ChapterPath 装配）。未注入时漂移检测整体跳过（降级零 issue）。
func (s *Service) SetChapterTextReader(fn func(ctx context.Context, novelID, chapterID int64) (string, error)) {
	s.chapterTextReader = fn
}

// SetVoiceMetricsProvider 注入 Voice 基线指标提供者（P2 漂移检测，由 app 层用
// voice.Store.List + voice.MergeMetrics 装配）。未注入/出错/空基线时漂移检测跳过。
func (s *Service) SetVoiceMetricsProvider(fn func(ctx context.Context, novelID int64) (map[string]float64, error)) {
	s.voiceMetricsProvider = fn
}

// SetIntentProvider 注入「创作意图」文本提供者（T4.1 定源后由 app 层接入）。
// 未注入/出错时降级为不注入。
func (s *Service) SetIntentProvider(fn func(ctx context.Context, novelID int64) (string, error)) {
	s.intentProvider = fn
}

// SetRetconGate 注入 retcon 审批闸门（WBS 3.1，由 app 层构造 internal/retcon.Service）。
// 未注入时 S5 交回候选只暂停不沉淀（降级），闸门上线后由 app 统一注入。
func (s *Service) SetRetconGate(gate *retcon.Service) {
	s.retconGate = gate
}

// buildBlocks 按 purpose×块矩阵（P0 WBS §0.5）装配三块上下文（Facts/Voice/Intent）。
// provider 未注入 / 返回空 / 出错 → 对应块降级为空，提示词与未接该块时逐字一致（向后兼容）；
// 聚合错误只记日志不阻断。Facts 语义与旧 establishedFactsText 完全同构。
func (s *Service) buildBlocks(ctx context.Context, novelID int64, purpose contextasm.Purpose) contextasm.ContextBlocks {
	blocks, err := contextasm.NewAssembler(s.bibleProvider, s.voiceProvider, s.intentProvider).
		Build(ctx, novelID, purpose)
	if err != nil {
		s.logger.Warn("上下文装配：部分块降级为不注入", "novel_id", novelID, "err", err)
	}
	return blocks
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
		// 统一走 abortRun：内含同族联动终止（book-run ↔ 卷级 scoped sub-run，G1）。
		return s.abortRun(ctx, run)
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

	// 蓝图生成完成：先把卷结构概览（Blueprint）落库，再进入蓝图体检（A 方案）。
	if err := s.store.SetStageStatus(ctx, runID, StageS2, StatusRunning, string(updated)); err != nil {
		s.logger.Error("S2 蓝图：落库蓝图状态失败", "err", err)
		return
	}
	// 用含 Blueprint 的最新上下文进入蓝图体检，避免 enterBlueprintAudit 重新解析到旧上下文而丢失 Blueprint 字段。
	run.ContextJSON = string(updated)
	// 进入蓝图体检：异步跑 blueprint-audit，通过后落 G1 确认闸，不通过落 G1b 修复闸，
	// 修复闭环复用 S5 执行体（由 ctxt.AuditKind 分流）。
	s.enterBlueprintAudit(ctx, run)
}

// enterBlueprintAudit 在蓝图生成（executeBlueprint）完成后进入蓝图体检：置 AuditKind=blueprint、
// AuditStage=S2、清空 BlueprintAudit，置 S2 running 并异步跑 blueprint-audit（只读体检）。
// 体检结果写回 ctxt.BlueprintAudit，随后由 executeAudit 落 G1/G1b 闸门。
func (s *Service) enterBlueprintAudit(ctx context.Context, run *PipelineRun) {
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	ctxt.AuditKind = "blueprint"
	ctxt.AuditStage = StageS2
	ctxt.BlueprintAudit = ""
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("蓝图体检：序列化上下文失败", "err", merr)
		updated = []byte(run.ContextJSON)
	}
	if err := s.store.SetStageStatus(ctx, run.RunID, StageS2, StatusRunning, string(updated)); err != nil {
		s.logger.Error("蓝图体检：进入失败", "err", err)
		return
	}
	run.ContextJSON = string(updated)
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS2, Status: StatusRunning})
	go s.executeAudit(run.RunID, run.NovelID)
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
// 联动 book-run ↔ scoped sub-run 的同族终止（G1），避免一侧终止后另一侧永久等待/孤儿。
func (s *Service) abortRun(ctx context.Context, run *PipelineRun) error {
	if err := s.store.SetStatus(ctx, run.RunID, StatusAborted, run.ContextJSON); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: run.CurrentStage, Status: StatusAborted})
	s.abortRelatives(ctx, run)
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

// currentVolumeScope 返回「当前卷」——本书最新写就章节所属的卷 id。
// 未分卷书 / 最新章未归卷时返回 nil（走全书旧路径，向后兼容）。
func (s *Service) currentVolumeScope(ctx context.Context, novelID int64) *int64 {
	if s.store == nil || s.store.db == nil {
		return nil
	}
	type row struct{ VolumeID *int64 }
	var r row
	if err := s.store.db.WithContext(ctx).Table("chapters").
		Select("volume_id").
		Where("novel_id = ?", novelID).
		Order("id DESC").Limit(1).Scan(&r).Error; err != nil {
		s.logger.Warn("卷级作用域：查询当前卷失败", "err", err)
		return nil
	}
	return r.VolumeID
}

// completedVolumeScope 返回「刚完结的前一卷」——全局阅读序上最近一个非当前卷的有卷章节所属卷。
// 供卷边界复评（reason=volume，G_freeze 前置）定界：跨入新卷后对刚写完的一卷做全卷收尾体检
// （S4+S5），通过即冻结。排序口径与 defaultVolumeBoundary 一致（volumes.sort_order DESC、
// chapters.sort_order DESC、chapters.id DESC）；未分卷章节不参与（不是卷）。无前一卷时返回 nil。
func (s *Service) completedVolumeScope(ctx context.Context, novelID int64) *int64 {
	if s.store == nil || s.store.db == nil {
		return nil
	}
	cur := s.currentVolumeScope(ctx, novelID)
	type row struct{ VolumeID *int64 }
	var r row
	q := s.store.db.WithContext(ctx).
		Table("chapters").
		Select("chapters.volume_id").
		Joins("LEFT JOIN volumes ON volumes.id = chapters.volume_id").
		Where("chapters.novel_id = ? AND chapters.volume_id IS NOT NULL", novelID).
		Order("volumes.sort_order DESC, chapters.sort_order DESC, chapters.id DESC")
	if cur != nil {
		q = q.Where("chapters.volume_id <> ?", *cur)
	}
	if err := q.Limit(1).Scan(&r).Error; err != nil {
		s.logger.Warn("卷级作用域：查询前一卷失败", "err", err)
		return nil
	}
	return r.VolumeID
}

// delegateVolumeAudit 把 book-run 的卷级体检委托给卷级 scoped sub-run（G1）：
// 复用或创建目标卷的 scoped run，并把 book-run 置 waiting（阶段保持 S3，收口后唤醒）。
// 目标卷不可定界（未分卷书 / 最新章未归卷）时返回 nil，调用方走 book-run 全书旧路径。
//
// 定界规则（G1 + 2.5）：
//   - golden3 / milestone（中途体检）→ 「当前卷」= 本书最新写就章节所属卷（S4 只扫在写的这卷）；
//   - volume（卷边界复评）→ 「刚完结的前一卷」：跨入新卷后对刚写完的一卷做全卷收尾体检，
//     过 S4+S5 收口即触发 G_freeze 自动冻结（见 finishVolumeScopedRun）。当前卷（新卷）只作背景。
func (s *Service) delegateVolumeAudit(ctx context.Context, bookRun *PipelineRun, stage, reason string) *PipelineRun {
	scope := s.currentVolumeScope(ctx, bookRun.NovelID)
	if reason == "volume" {
		if prev := s.completedVolumeScope(ctx, bookRun.NovelID); prev != nil {
			scope = prev
		}
	}
	if scope == nil {
		return nil
	}
	scoped, err := s.store.GetActiveVolumeRun(ctx, bookRun.NovelID, *scope)
	if err != nil {
		scoped = &PipelineRun{
			NovelID:      bookRun.NovelID,
			RunID:        uuid.NewString(),
			Mode:         bookRun.Mode,
			CurrentStage: stage,
			Status:       StatusRunning,
			AutoRun:      bookRun.AutoRun,
			VolumeID:     scope,
			ParentRunID:  bookRun.RunID,
		}
		if cerr := s.store.Create(ctx, scoped); cerr != nil {
			s.logger.Error("卷级 scoped run 创建失败", "novel_id", bookRun.NovelID, "volume_id", *scope, "err", cerr)
			return nil
		}
	}
	if werr := s.store.SetStatus(ctx, bookRun.RunID, StatusWaiting, bookRun.ContextJSON); werr != nil {
		s.logger.Error("book-run 置等待失败", "run_id", bookRun.RunID, "err", werr)
		return nil
	}
	return scoped
}

// wakeBookRun 唤醒等待中的 book-run 回 S3 写作。merge 非空时先把卷级体检结论并入
// book-run 上下文（S4Audited / Audit 等），再置 S3 running 并广播，供向导与阶段条同步。
func (s *Service) wakeBookRun(ctx context.Context, bookRun *PipelineRun, merge func(*GateContext)) {
	var ctxt GateContext
	if bookRun.ContextJSON != "" {
		_ = json.Unmarshal([]byte(bookRun.ContextJSON), &ctxt)
	}
	if merge != nil {
		merge(&ctxt)
	}
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("唤醒 book-run：序列化上下文失败", "err", merr)
		updated = []byte(bookRun.ContextJSON)
	}
	if err := s.store.SetStageStatus(ctx, bookRun.RunID, StageS3, StatusRunning, string(updated)); err != nil {
		s.logger.Error("唤醒 book-run 失败", "run_id", bookRun.RunID, "err", err)
		return
	}
	emitStage(ctx, StagePayload{RunID: bookRun.RunID, NovelID: bookRun.NovelID, Stage: StageS3, Status: StatusRunning})
}

// finishVolumeScopedRun 卷级 scoped sub-run 收口（G4 选 continue：体检通过 / 接受已知问题）：
// 先把体检结论回写父 book-run 并唤醒回 S3，再静默终结 scoped run（不广播终态事件，
// 避免前端阶段条被 finished 误隐）。先唤醒后终结，任一步崩溃都可自愈：
// 唤醒先行则 scoped 可重入收口；孤儿 waiting 由启动恢复自愈兜底。
func (s *Service) finishVolumeScopedRun(ctx context.Context, scoped *PipelineRun, scopedCtxtJSON string, chapterTotal int) error {
	bookRun, err := s.store.GetByRunID(ctx, scoped.ParentRunID)
	if err != nil {
		s.logger.Error("卷级收口：找不到父 book-run", "run_id", scoped.RunID, "parent", scoped.ParentRunID, "err", err)
		return err
	}
	var scopedCtxt GateContext
	if scopedCtxtJSON != "" {
		_ = json.Unmarshal([]byte(scopedCtxtJSON), &scopedCtxt)
	}
	s.wakeBookRun(ctx, bookRun, func(bc *GateContext) {
		// 体检结论回写 book-run：供 S3 里程碑判定（S4Audited/LastAuditChapter）
		// 与体检视图（Audit/DriftReport/RewriteBacklog）在 GetByNovel 切回 book-run 后仍可见。
		bc.S4Audited = true
		bc.LastAuditChapter = chapterTotal
		bc.Audit = scopedCtxt.Audit
		bc.AuditReason = scopedCtxt.AuditReason
		bc.AuditStage = scopedCtxt.AuditStage
		bc.DriftReport = scopedCtxt.DriftReport
		bc.RewriteBacklog = scopedCtxt.RewriteBacklog
		bc.RetconBacklog = scopedCtxt.RetconBacklog
	})
	// 静默终结 scoped run（终态不广播事件，见函数注释）。
	if err := s.store.SetStageStatus(ctx, scoped.RunID, scoped.CurrentStage, StatusFinished, scopedCtxtJSON); err != nil {
		s.logger.Error("卷级收口：scoped run 终结失败", "run_id", scoped.RunID, "err", err)
		return err
	}
	// G_freeze 自动闸门（WBS 2.5）：卷边界复评（AuditReason=volume）收口 = 该卷过 S4+S5，
	// 自动置冻结 + 提交 committed snapshot（一卷一卷定稿）。
	s.maybeFreezeOnHandoff(ctx, scoped, &scopedCtxt)
	return nil
}

// maybeFreezeOnHandoff 在卷边界复评收口（该卷过 S4+S5）时执行 G_freeze 自动冻结：
// 置冻结 + 同事务提交 committed snapshot。仅在 AuditReason=volume 且该卷已不是「当前卷」
// （写作已推进走）时触发——中途体检（golden3/milestone）收口不冻结（卷还在写），
// 也防定界 fallback 把仍在写的活跃卷误冻。冻结失败不阻断收口（卷保持可写）。
func (s *Service) maybeFreezeOnHandoff(ctx context.Context, scoped *PipelineRun, scopedCtxt *GateContext) {
	if scopedCtxt.AuditReason != "volume" || scoped.VolumeID == nil {
		return
	}
	volumeID := *scoped.VolumeID
	if cur := s.currentVolumeScope(ctx, scoped.NovelID); cur != nil && *cur == volumeID {
		return
	}
	s.freezeVolumeWithNote(ctx, scoped.NovelID, volumeID, scoped.ParentRunID, StageS3, StatusRunning)
}

// freezeRemainingVolumes 完本收口（S6 finish）时冻结本书全部尚未冻结的卷并提交快照：
// 边界复评冻结中间卷，末卷（含单卷书）在完本时收尾，补齐 WBS 2.2 验收「每卷有
// committed snapshot」。逐卷独立执行，单卷失败不阻断其余。
func (s *Service) freezeRemainingVolumes(ctx context.Context, novelID int64, runID string) {
	if s.volumeFreezer == nil || s.store == nil || s.store.db == nil {
		return
	}
	type row struct{ VolumeID int64 }
	var rows []row
	if err := s.store.db.WithContext(ctx).
		Table("chapters").
		Select("chapters.volume_id AS volume_id").
		Joins("LEFT JOIN volumes ON volumes.id = chapters.volume_id").
		Where("chapters.novel_id = ? AND chapters.volume_id IS NOT NULL", novelID).
		Group("chapters.volume_id").
		Order("MIN(volumes.sort_order)").
		Scan(&rows).Error; err != nil {
		s.logger.Warn("G_freeze 完本收口：查询卷列表失败", "novel_id", novelID, "err", err)
		return
	}
	volStore := volume.NewStore(s.store.db, s.logger)
	frozenNow, failed := 0, 0
	for _, r := range rows {
		v, err := volStore.GetByID(ctx, nil, novelID, r.VolumeID)
		if err != nil || v == nil || v.Frozen {
			continue // 已冻结 / 查不到：跳过（Freeze 幂等，这里只为少发噪声 note）
		}
		if err := s.volumeFreezer(ctx, novelID, r.VolumeID); err != nil {
			failed++
			s.logger.Error("G_freeze 完本收口：冻结失败", "novel_id", novelID, "volume_id", r.VolumeID, "err", err)
			continue
		}
		frozenNow++
	}
	if frozenNow == 0 && failed == 0 {
		return
	}
	note := fmt.Sprintf("G_freeze：完本收口，已冻结 %d 卷并提交快照", frozenNow)
	if failed > 0 {
		note += fmt.Sprintf("（%d 卷冻结失败，详见日志）", failed)
	}
	emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS6, Status: StatusFinished, Note: note})
}

// freezeVolumeWithNote 执行单卷 G_freeze 冻结（含 2.2 快照提交与 2.4 台账审计闸门），
// 并把结果以 note 广播到指定 run 的阶段事件（note 仅作展示，不改变阶段状态）。
// 冻结失败（如台账审计不过）只提示不抛错：卷保持可写，用户可修复台账后手动冻结
// （App.SetVolumeFrozen）或重跑卷边界复评。
func (s *Service) freezeVolumeWithNote(ctx context.Context, novelID, volumeID int64, runID, stage, status string) {
	label := fmt.Sprintf("volume_id=%d", volumeID)
	if v, err := volume.NewStore(s.store.db, s.logger).GetByID(ctx, nil, novelID, volumeID); err == nil && v != nil && v.Name != "" {
		label = fmt.Sprintf("《%s》", v.Name)
	}
	if s.volumeFreezer == nil {
		s.logger.Warn("G_freeze：未注入 volumeFreezer，跳过自动冻结", "novel_id", novelID, "volume_id", volumeID)
		return
	}
	if err := s.volumeFreezer(ctx, novelID, volumeID); err != nil {
		s.logger.Error("G_freeze：自动冻结失败", "novel_id", novelID, "volume_id", volumeID, "err", err)
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: stage, Status: status,
			Note: fmt.Sprintf("卷%s已过 S4+S5，但 G_freeze 自动冻结被拒：%s。卷保持可写，请修复台账后手动冻结或重跑卷边界复评", label, err.Error())})
		return
	}
	emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: stage, Status: status,
		Note: fmt.Sprintf("卷%s已过 S4+S5，G_freeze 自动冻结并提交卷快照", label)})
	// 卷末自动一致性巡检（WBS 3.3 建议「卷末 + 手动」而非实时，控成本）：本卷冻结定稿
	// 即「卷末」，后台跑一次全书张力报告（只读非阻断，失败只记日志），落 book-run 供
	// 巡检报告视图。runID 为空（无流水线归属）时跳过，可经控制指令 "patrol" 手动补跑。
	if runID != "" {
		go s.runConsistencyPatrol(ctx, runID, novelID)
	}
}

// abortRelatives 终止联动（G1）：scoped sub-run 终止 → 父 book-run 一并终止（「终止」语义
// 覆盖整条流水线）；book-run 终止 → 其活跃 scoped sub-run 一并终止，避免孤儿子 run。
func (s *Service) abortRelatives(ctx context.Context, run *PipelineRun) {
	if run.ParentRunID != "" {
		if parent, err := s.store.GetByRunID(ctx, run.ParentRunID); err == nil &&
			parent.Status != StatusFinished && parent.Status != StatusAborted {
			if aerr := s.store.SetStatus(ctx, parent.RunID, StatusAborted, parent.ContextJSON); aerr != nil {
				s.logger.Error("联动终止父 book-run 失败", "run_id", parent.RunID, "err", aerr)
			} else {
				emitStage(ctx, StagePayload{RunID: parent.RunID, NovelID: parent.NovelID,
					Stage: parent.CurrentStage, Status: StatusAborted, Note: "卷级体检已终止，整条流水线一并终止"})
			}
		}
	}
	if children, err := s.store.ListActiveScopedChildren(ctx, run.RunID); err == nil {
		for _, child := range children {
			if cerr := s.store.SetStatus(ctx, child.RunID, StatusAborted, child.ContextJSON); cerr != nil {
				s.logger.Error("联动终止卷级子 run 失败", "run_id", child.RunID, "err", cerr)
				continue
			}
			emitStage(ctx, StagePayload{RunID: child.RunID, NovelID: child.NovelID,
				Stage: child.CurrentStage, Status: StatusAborted, Note: "父流水线已终止，本卷体检一并终止"})
		}
	}
}

// enterAudit 置体检阶段 running 并异步执行 signability-audit。
// reason 决定进入的阶段：golden3 → S4（首检）；milestone / volume → S4b（卷边界复评）。
// 同时把触发原因与体检阶段写回 ContextJSON，供修复后复评回到同阶段。
//
// 卷级作用域（G1 + 2.5）：book-run 的 golden3/milestone/volume 体检委托给卷级 scoped
// sub-run 执行——golden3/milestone 扫「当前卷」（S4 只扫在写的这卷），volume（卷边界复评）
// 扫「刚完结的前一卷」（全卷收尾体检，过 S4+S5 收口即 G_freeze 冻结）；未分卷项目
// （目标卷不可定界）、已在 scoped run 上的复评、以及完本终检（reason=final，恒为全书）
// 走原路径，行为不变。
func (s *Service) enterAudit(ctx context.Context, run *PipelineRun, reason string) {
	stage := StageS4
	if reason != "golden3" {
		stage = StageS4b
	}
	target := run
	if run.ParentRunID == "" && reason != "final" {
		if scoped := s.delegateVolumeAudit(ctx, run, stage, reason); scoped != nil {
			target = scoped
		}
	}
	var ctxt GateContext
	if target.ContextJSON != "" {
		_ = json.Unmarshal([]byte(target.ContextJSON), &ctxt)
	}
	ctxt.AuditReason = reason
	ctxt.AuditStage = stage
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S4 进入：序列化上下文失败", "err", merr)
		updated = []byte(target.ContextJSON)
	}
	if err := s.store.SetStageStatus(ctx, target.RunID, stage, StatusRunning, string(updated)); err != nil {
		s.logger.Error("S4 进入失败", "err", err)
		// 委托已把 book-run 置 waiting：派发失败必须唤醒回写作，避免永久等待。
		if target != run {
			s.wakeBookRun(ctx, run, nil)
		}
		return
	}
	target.CurrentStage = stage
	target.ContextJSON = string(updated)
	emitStage(ctx, StagePayload{RunID: target.RunID, NovelID: target.NovelID, Stage: stage, Status: StatusRunning})
	go s.executeAudit(target.RunID, target.NovelID)
}

// enterFix 置 S5 running 并异步执行 signability-fix 修复循环。
func (s *Service) enterFix(ctx context.Context, run *PipelineRun) {
	// 新一轮修复开始：清零批次连续失败计数（上一轮的失败不应累计到新一轮）。
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	if ctxt.FixBatchFailures > 0 {
		ctxt.FixBatchFailures = 0
		if updated, merr := json.Marshal(ctxt); merr == nil {
			run.ContextJSON = string(updated)
		}
	}
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
	auditStage := run.CurrentStage // S4 / S4b / S6 / S2（蓝图体检）
	s.setDispatching(ctx, runID, true)

	// 提前解析上下文，按 AuditKind 选择体检技能与提示词（蓝图走 blueprint-audit，其余走 signability-audit）。
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	auditSkill := "signability-audit"
	auditPrompt := signabilityPrompt(novelID, s.volumeScopeText(ctx, novelID, run.VolumeID),
		s.buildBlocks(ctx, novelID, contextasm.PurposeAudit))
	if ctxt.AuditKind == "blueprint" {
		auditSkill = "blueprint-audit"
		auditPrompt = blueprintAuditPrompt(novelID)
	}

	out, err := s.skillRunner(ctx, auditSkill, SkillInput{
		NovelID: novelID,
		Prompt:  auditPrompt,
		// 只读体检需通读/抽样全书章节与台账（长篇动辄 70+ 章），12 轮在长篇上
		// 会在读完全书前耗尽（「已达最大轮数」）。放宽到 24 轮。
		MaxTurns:  24,
		SessionID: "pipeline:" + runID + ":audit",
	})
	if err != nil {
		s.logger.Error("S4 体检执行失败", "run_id", runID, "err", err)
		s.setDispatching(ctx, runID, false)
		// 失败落地为 paused：恢复时按需重派发，避免状态停在 running 无人执行。
		if serr := s.store.SetStageStatus(ctx, runID, auditStage, StatusPaused, run.ContextJSON); serr != nil {
			s.logger.Error("S4 体检：失败落暂停失败", "err", serr)
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: auditStage, Status: StatusPaused,
			Note: "签约体检失败：" + err.Error() + "；已暂停，点击恢复将重新体检"})
		return
	}

	audit := parseFreshAudit(out.Text)
	// P2 文风漂移（WBS p2 §0.3）：确定性检测的 V 系 issue 追加到清单尾部（warning 级，
	// 经 bodyIssuesOf/retconItemsOf 自然收口进既有修复/重写路由）。verdict 不翻转——
	// drift-only 保持 LLM verdict，不触发收口修复循环；蓝图体检不参与文风检测。
	if ctxt.AuditKind != "blueprint" {
		audit.Issues = mergeDriftIssues(audit.Issues, s.runStyleDriftCheck(ctx, novelID, run.VolumeID))
	}
	auditJSON, _ := json.Marshal(audit)

	// ctxt 已在函数顶部解析；按 AuditKind 把体检结果写回对应字段（蓝图 → BlueprintAudit，签约 → Audit）。
	// 签约类体检同步重算「回流 S3 重写回路」的正文级问题清单（每次体检反映最新结果）；
	// 蓝图体检不产生正文级问题，置空避免污染。
	if ctxt.AuditKind == "blueprint" {
		ctxt.BlueprintAudit = string(auditJSON)
		ctxt.RewriteBacklog = nil
		ctxt.RetconBacklog = nil
	} else {
		ctxt.Audit = string(auditJSON)
		ctxt.RewriteBacklog = bodyIssuesOf(audit)
		ctxt.RetconBacklog = retconItemsOf(audit)
	}
	// 新一轮体检结果落地即视为全新问题清单：清除上一轮的修复断点（FixStates）。
	// 否则复评产出的 issue 会与旧断点 ID 撞车（LLM 惯性编号 I1..In），executeFix 会把
	// 全部问题误判为「已修复」而静默跳过修复直接复评——表现为提示「修复完成，正在自动
	// 复评」，实际上一个问题都没修（auto_run 下还会空转到 FixRounds 上限）。
	ctxt.FixStates = nil
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S4 体检：序列化上下文失败", "err", merr)
		updated = []byte(run.ContextJSON)
	}

	// 守卫：若异步执行期间用户已推进（如点了 fix / continue），不覆盖新状态。
	// 无论守卫结果如何，本执行体即将退出，先清除执行中标记。
	s.setDispatching(ctx, runID, false)
	cur, gerr := s.store.GetByRunID(ctx, runID)
	if gerr == nil && (cur.CurrentStage != auditStage || cur.Status != StatusRunning) {
		s.logger.Info("S4 体检：状态已变更，放弃落闸", "stage", cur.CurrentStage, "status", cur.Status)
		return
	}

	// auto_run 自动裁决：仅对普通签约体检（S4/S4b）生效；完本终检（S6）必须人工确认 finish。
	// 通过 → continue 回 S3；不通过 → fix 进 S5，但受修复轮数上限约束，避免无限循环。
	if run.AutoRun && auditStage != StageS6 && ctxt.AuditKind != "blueprint" {
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
	// 蓝图体检走独立闸门：通过 → G1 确认（continue/modify/abort）；不通过 → G1b 修复闸（fix/continue/abort）。
	// 蓝图体检必须人工确认（与 G1 原语义一致），不走 auto_run 自动修复。
	var bc GateContext
	if run, rerr := s.store.GetByRunID(ctx, runID); rerr == nil && run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &bc)
	}
	if bc.AuditKind == "blueprint" {
		summary := audit.Summary
		if summary == "" {
			if audit.Verdict == "pass" {
				summary = "蓝图体检通过，请确认是否进入写作或重生成。"
			} else {
				summary = "蓝图体检发现问题，建议进入修复闭环后再确认。"
			}
		}
		// G3：S5 修复后的蓝图巡检结论随闸门带给用户（非阻断，仅供参考）。
		if bc.DriftReport != "" {
			summary += "；" + bc.DriftReport
		}
		if audit.Verdict == "pass" {
			emitGate(ctx, GatePayload{RunID: runID, NovelID: novelID, Stage: auditStage, GateID: GateG1,
				Summary: summary, Options: []string{"continue", "modify", "abort"}})
		} else {
			emitGate(ctx, GatePayload{RunID: runID, NovelID: novelID, Stage: auditStage, GateID: GateG1b,
				Summary: summary, Options: []string{"fix", "continue", "abort"}})
		}
		return
	}

	summary := audit.Summary
	if summary == "" {
		if auditStage == StageS6 {
			summary = "完本终检已完成，请确认是否完本或进入修复闭环。"
		} else {
			summary = "签约体检已完成，请确认是否继续或进入修复闭环。"
		}
	}
	// G3：S5 修复后的蓝图巡检结论随闸门带给用户（非阻断，仅供参考）。
	if bc.DriftReport != "" {
		summary += "；" + bc.DriftReport
	}
	// 正文级问题已回流 S3 重写回路：在闸门 summary 显式提示用户哪些章节需回到 S3 重写，
	// 闭合修复链路（否则这些问题既不在 S5 修复范围内，也无出口，修复链不完整）。
	if len(bc.RewriteBacklog) > 0 {
		summary += "；" + rewriteBacklogSummary(bc.RewriteBacklog)
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
// fixBatchP0 / fixBatchP1 是分批修复的每批问题数：P0 单条常涉及整章改写/重排，独占一批；
// P1 每批两条。保证每条问题都有充足的对话轮数预算，避免一轮对话吃下全部问题导致
// MaxTurns 耗尽、整轮修复失败。
const (
	fixBatchP0 = 1
	fixBatchP1 = 2
	// fixBatchFailureCap 是同一运行内修复批次连续失败的上限：达到后不再「暂停→恢复→
	// 原样重派」地无限循环，改发闸门交用户裁决（继续=回复评重新评估 / 终止）。
	// 批次成功完成即清零计数。
	fixBatchFailureCap = 3
	// fixProviderIdleTrip 是批次派发前的供应商熔断阈值：近 10 分钟 LLM 流式空闲超时
	// 达到该次数说明 Provider 基本不可用（正常修复期间接近 0），此时不派发 agent
	// 白烧 18 轮墙钟时间，直接按批次失败快速落入计数/闸门通道。
	fixProviderIdleTrip = 10
)

// autoRewriteCap 是自动章节重写回路的单次运行最大轮数：达到上限后不再自动重写，
// 改为交回用户手动处理（避免「重写→复评→再重写」无限循环）。每轮 finishFixHandoffRewrite
// 自动重写后 +1，复评若仍报出同一批正文问题会再次进入重写，最多 autoRewriteCap 轮。
const autoRewriteCap = 2

// planFixBatches 把待修问题按优先级分批（保持原有顺序）：P0 每批 fixBatchP0 条，
// P1 每批 fixBatchP1 条。
func planFixBatches(issues []AuditIssue) [][]AuditIssue {
	batches := make([][]AuditIssue, 0, len(issues))
	var cur []AuditIssue
	flush := func() {
		if len(cur) > 0 {
			batches = append(batches, cur)
			cur = nil
		}
	}
	for _, it := range issues {
		if it.Level == "P0" {
			flush()
			batches = append(batches, []AuditIssue{it})
			continue
		}
		cur = append(cur, it)
		if len(cur) >= fixBatchP1 {
			flush()
		}
	}
	flush()
	return batches
}

// setDispatching 更新 GateContext.Dispatching 标记（执行体派发/退出时调用），
// 供 resume 区分「执行体仍存活（软暂停，会在批次/阶段边界感知状态变化）」与
// 「无人执行（需重新派发）」。
func (s *Service) setDispatching(ctx context.Context, runID string, v bool) {
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		return
	}
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	if ctxt.Dispatching == v {
		return
	}
	ctxt.Dispatching = v
	if updated, merr := json.Marshal(ctxt); merr == nil {
		if err := s.store.SetStatus(ctx, runID, run.Status, string(updated)); err != nil {
			s.logger.Warn("更新执行中标记失败（非阻断）", "run_id", runID, "err", err)
		}
	}
}

func (s *Service) executeFix(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		s.logger.Error("S5 修复：加载流水线失败", "run_id", runID, "err", err)
		return
	}
	s.setDispatching(ctx, runID, true)

	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}

	// 广播修复清单：让用户看到本轮在修复哪些问题（含涉及元素 area）。
	// issues 来自体检结果（蓝图走 BlueprintAudit，签约走 Audit）；解析失败退化为单条总体项。
	auditJSON := ctxt.Audit
	if ctxt.AuditKind == "blueprint" {
		auditJSON = ctxt.BlueprintAudit
	}
	auditData := parseAudit(auditJSON)

	// 陈旧 run 守卫（契约单源，见 contract.go）：体检结果按 issue 契约判定陈旧——
	// ① 字段嗅探（契约字段组缺失，旧版数据或 LLM 未按契约输出）；② schema_version 版本门
	// （显式低于当前契约版本，字段碰巧齐全也不可信）。任一命中即拒绝派发、交回用户重跑体检，
	// 而非默默当 ledger 跑、把轮数吃光或带盲区派发。蓝图体检只签核心字段契约
	// （blueprint-audit，无 fix_type/作用域字段），整体豁免。阈值 >=2 避免与解析失败的
	// 单条兜底 issue 误判。
	if ctxt.AuditKind != "blueprint" && len(auditData.Issues) >= 2 {
		if reason := auditStaleReason(auditData); reason != "" {
			s.logger.Warn("S5 修复：检测到陈旧体检，拒绝派发",
				"run_id", runID, "issues", len(auditData.Issues), "reason", reason)
			s.setDispatching(ctx, runID, false)
			emitFixDone(ctx, map[string]any{
				"run_id":      runID,
				"novel_id":    novelID,
				"ok":          false,
				"stale_audit": true,
				"error":       reason,
			})
			if cur, gerr := s.store.GetByRunID(ctx, runID); gerr == nil &&
				cur.CurrentStage == StageS5 && cur.Status == StatusRunning {
				if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusPaused, cur.ContextJSON); serr != nil {
					s.logger.Error("S5 修复：陈旧体检落暂停失败", "err", serr)
				}
				emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS5,
					Status: StatusPaused,
					Note:   reason})
			}
			return
		}
	}

	// 断点续修：跳过已修复/已跳过/已路由重写（rewrite）问题（此前批次完成并落库的），只修剩余部分。
	// 注意：旧断点与新体检清单 ID 撞车的问题由 executeAudit 落新体检结果时重置
	// FixStates 解决（新一轮问题就是新问题），这里「全部命中断点」只可能是修复
	// 完成后、复评前的中断恢复，直接进入复评是正确语义。
	if ctxt.FixStates == nil {
		ctxt.FixStates = map[string]string{}
	}
	fixedIDs := make([]string, 0, len(ctxt.FixStates))
	isFixed := make(map[string]bool, len(ctxt.FixStates))
	isResolved := make(map[string]bool, len(ctxt.FixStates)) // fixed/skipped/rewrite 均不再进修复批次
	for id, st := range ctxt.FixStates {
		switch st {
		case "fixed":
			isFixed[id] = true
			fixedIDs = append(fixedIDs, id)
			isResolved[id] = true
		case "skipped", "rewrite", "retcon":
			isResolved[id] = true
		}
	}

	// 冻结卷写保护/retcon 候选（1.3/1.4）：target_scope=frozen_volume 或 direction=
	// retcon_candidate 的问题一律不派发自动修复/自动重写（冻结卷正文与既定设定不得被
	// 自动回改），标记 FixStates=retcon 并沉淀到 RetconBacklog，交回人工裁决。
	retconBacklog := ctxt.RetconBacklog
	if retconBacklog == nil {
		retconBacklog = []RewriteItem{}
	}
	for _, it := range auditData.Issues {
		if isResolved[it.ID] || !writeProtected(it.TargetScope, it.Direction) {
			continue
		}
		retconBacklog = upsertRewriteItem(retconBacklog, RewriteItem{
			ID: it.ID, Level: it.Level, Area: it.Area, Desc: it.Desc,
			Chapters:    parseChapterRefs(it.Area, it.Desc),
			TargetScope: it.TargetScope, Dimension: it.Dimension, Direction: it.Direction,
		})
		ctxt.FixStates[it.ID] = "retcon"
		isResolved[it.ID] = true
	}
	ctxt.RetconBacklog = retconBacklog

	// 析出正文级（fix_type=body）问题：S5 不尝试 edit 修补这类「必重写」项，
	// 否则会对修不动的问题反复重试（空转）或上报 skipped 永久丢弃（修复链断裂）。
	// 改为显式路由到 S3 章节重写回路——标记 FixStates=rewrite 并沉淀到 RewriteBacklog。
	// （命中写保护的正文级问题已在上方路由进 RetconBacklog，不会落入本清单。）
	// V 系文风漂移项（isStyleExempt）也在此收口进 RewriteBacklog（「自动进路由」=进
	// 既有容器供作者查看，零新路由），但下游派发一律豁免——与下方 pending 过滤成对，
	// 保证每个 issue 必进「retcon / rewrite / pending / resolved」四分区之一，无死区。
	backlog := ctxt.RewriteBacklog
	if backlog == nil {
		backlog = []RewriteItem{}
	}
	for _, it := range auditData.Issues {
		if isResolved[it.ID] || (it.FixType != FixTypeBody && !isStyleExempt(it.ID)) {
			continue
		}
		backlog = upsertRewriteItem(backlog, RewriteItem{
			ID:       it.ID,
			Level:    it.Level,
			Area:     it.Area,
			Desc:     it.Desc,
			Chapters: parseChapterRefs(it.Area, it.Desc),
		})
		ctxt.FixStates[it.ID] = "rewrite"
	}
	ctxt.RewriteBacklog = backlog
	// 正文级路由落库：标记 rewrite + 沉淀重写清单（即便没有台账级问题待修也需持久化，供 G4 展示）。
	if updated, merr := json.Marshal(ctxt); merr == nil {
		if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusRunning, string(updated)); serr != nil {
			s.logger.Error("S5 修复：落库正文级重写标记失败", "err", serr)
		}
	}
	if len(backlog) > 0 {
		s.emitRewriteBacklog(ctx, runID, novelID, backlog)
	}

	pending := make([]AuditIssue, 0, len(auditData.Issues))
	for _, it := range auditData.Issues {
		// V 系文风漂移项豁免自动修复派发（只报告不改写，WBS §0.3）：已随上方
		// 析出循环进 RewriteBacklog 供作者裁决，绝不进修复批次。
		if !isResolved[it.ID] && it.FixType != FixTypeBody && !isStyleExempt(it.ID) {
			pending = append(pending, it)
		}
	}
	if len(pending) == 0 {
		// 无台账级问题待修。三种语义必须区分，否则会谎报「修复完成」后立刻复评，
		// 而复评必复报同一批问题，形成「完成但零修复」死循环：
		//  (a) 存在回流 S3 重写的清单 → 这些问题 S5 改不动，明确交回用户重写，不再复评；
		//  (b) 存在写保护/retcon 候选清单 → 冻结卷不得自动回改，交回人工裁决，不再复评；
		//  (c) 既无待修也无上述清单（全部此前已 fixed/skipped）→ 才进复评做最终验证。
		if len(backlog) > 0 {
			s.finishFixHandoffRewrite(ctx, runID, novelID, ctxt)
			return
		}
		if len(retconBacklog) > 0 {
			s.handoffRetconToUser(ctx, runID, novelID, ctxt)
			return
		}
		s.finishFixAndReaudit(ctx, runID, novelID, ctxt)
		return
	}
	batches := planFixBatches(pending)

	for i, batch := range batches {
		// 批次间守卫：用户暂停/中止后在边界停下（软暂停语义），恢复时从剩余问题继续。
		if cur, gerr := s.store.GetByRunID(ctx, runID); gerr == nil &&
			(cur.CurrentStage != StageS5 || cur.Status != StatusRunning) {
			s.logger.Info("S5 修复：状态已变更，批次边界停止", "batch", i+1, "stage", cur.CurrentStage, "status", cur.Status)
			s.setDispatching(ctx, runID, false)
			emitFixDone(ctx, map[string]any{"run_id": runID, "novel_id": novelID,
				"ok": false, "error": "修复已被暂停或中止，等待恢复"})
			return
		}

		batchIDs := make(map[string]bool, len(batch))
		for _, it := range batch {
			batchIDs[it.ID] = true
		}
		batchIssues := make([]FixIssuePayload, 0, len(batch))
		for _, it := range batch {
			batchIssues = append(batchIssues, FixIssuePayload{
				ID: it.ID, Level: it.Level, Area: it.Area, Desc: it.Desc, Status: "fixing",
				TargetScope: it.TargetScope, Dimension: it.Dimension, Direction: it.Direction,
			})
		}
		// 全量问题清单（含逐项状态）：前端在修复清单里给每个问题标注
		// 待修复 / 正在修复 / 已修复，用户不用猜 LLM 做到哪了。
		allIssues := make([]FixIssuePayload, 0, len(auditData.Issues))
		for _, it := range auditData.Issues {
			p := FixIssuePayload{ID: it.ID, Level: it.Level, Area: it.Area, Desc: it.Desc,
				TargetScope: it.TargetScope, Dimension: it.Dimension, Direction: it.Direction}
			switch {
			case isFixed[it.ID]:
				p.Status = "fixed"
			case ctxt.FixStates[it.ID] == "skipped":
				p.Status = "skipped"
			case ctxt.FixStates[it.ID] == "rewrite":
				p.Status = "rewrite"
			case ctxt.FixStates[it.ID] == "retcon":
				p.Status = "retcon"
			case batchIDs[it.ID]:
				p.Status = "fixing"
			default:
				p.Status = "pending"
			}
			allIssues = append(allIssues, p)
		}
		emitFixStart(ctx, map[string]any{
			"run_id":      runID,
			"novel_id":    novelID,
			"issues":      batchIssues,
			"all_issues":  allIssues,
			"summary":     auditData.Summary,
			"batch_index": i + 1,
			"batch_total": len(batches),
			"done_ids":    fixedIDs,
		})

		// 每批独立会话：避免历史消息堆积挤占后续批次的轮数预算。
		fixSkill := "signability-fix"
		fixPromptText := fixPrompt(novelID, batch, auditData.Summary,
			s.buildBlocks(ctx, novelID, contextasm.PurposeFix))
		if ctxt.AuditKind == "blueprint" {
			fixSkill = "blueprint-fix"
			fixPromptText = blueprintFixPrompt(novelID, batch, auditData.Summary)
		}
		// 供应商熔断：近 10 分钟流式空闲超时达到阈值说明 Provider 基本不可用
		// （正常修复期间接近 0），此时不派发 agent 白烧 18 轮墙钟时间（每轮都可能
		// 白等 3 分钟被看门狗取消），直接按批次失败快速落入计数/闸门通道。
		var out SkillOutput
		var batchErr error
		if n := llm.RecentIdleTimeouts(10 * time.Minute); n >= fixProviderIdleTrip {
			batchErr = fmt.Errorf("供应商熔断：近 10 分钟 LLM 流式空闲超时 %d 次，暂不派发修复 agent", n)
		} else {
			out, batchErr = s.skillRunner(ctx, fixSkill, SkillInput{
				NovelID:   novelID,
				Prompt:    fixPromptText,
				MaxTurns:  18,
				SessionID: "pipeline:" + runID + ":fix:" + uuid.NewString(),
			})
		}
		if batchErr != nil {
			s.logger.Error("S5 修复执行失败", "run_id", runID, "batch", i+1, "err", batchErr)
			s.setDispatching(ctx, runID, false)
			// 透出供应商健康度：若近期频发流式空闲超时，提示用户这是 Provider 挂起而非代码问题，
			// 避免误判为「修复功能坏了」。idle 超时由 llm 层空闲看门狗记录。
			idleCount := llm.RecentIdleTimeouts(15 * time.Minute)
			idleNote := ""
			if idleCount > 0 {
				idleNote = fmt.Sprintf("；近 15 分钟 LLM 流式空闲超时 %d 次（疑似供应商挂起，建议检查 Provider 或稍后重跑）", idleCount)
			}
			// 失败计数：连续失败达上限不再「暂停→恢复→原样重派」无限循环，
			// 改发闸门交用户裁决（继续=回复评重新评估剩余问题 / 终止）。
			ctxt.FixBatchFailures++
			ctxtUpdated, merr := json.Marshal(ctxt)
			if merr != nil {
				ctxtUpdated = []byte(run.ContextJSON)
			}
			emitFixDone(ctx, map[string]any{"run_id": runID, "novel_id": novelID, "ok": false,
				"error":                  batchErr.Error() + idleNote,
				"batch_index":            i + 1,
				"batch_total":            len(batches),
				"provider_idle_timeouts": idleCount})
			if cur, gerr := s.store.GetByRunID(ctx, runID); gerr == nil &&
				cur.CurrentStage == StageS5 && cur.Status == StatusRunning {
				if ctxt.FixBatchFailures >= fixBatchFailureCap {
					// 达到失败上限：转人工裁决，等待闸门决策而非盲目重试。
					if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusAwaitingGate, string(ctxtUpdated)); serr != nil {
						s.logger.Error("S5 修复：失败转闸门落库失败", "err", serr)
					}
					emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS5,
						Status: StatusAwaitingGate,
						Note: fmt.Sprintf("修复批次已连续失败 %d 次（最近一次：第 %d/%d 批），已转人工裁决",
							ctxt.FixBatchFailures, i+1, len(batches))})
					emitGate(ctx, GatePayload{RunID: runID, NovelID: novelID, Stage: StageS5, GateID: GateG4,
						Summary: fmt.Sprintf("修复批次已连续失败 %d 次（最近一次：第 %d/%d 批：%s%s）。已修复 %d 项保留。"+
							"「继续」= 回到体检复评重新评估剩余问题（可拆分/降级难修项后再决定）；「终止」= 结束本流水线。",
							ctxt.FixBatchFailures, i+1, len(batches), batchErr.Error(), idleNote, len(fixedIDs)),
						Options: []string{"continue", "abort"}})
				} else {
					// 失败落地为 paused（保留已完成批次的断点），恢复时从剩余问题继续。
					if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusPaused, string(ctxtUpdated)); serr != nil {
						s.logger.Error("S5 修复：失败落暂停失败", "err", serr)
					}
					emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS5,
						Status: StatusPaused,
						Note: fmt.Sprintf("第 %d/%d 批修复失败（累计 %d/%d 次）：%s；已暂停，点击恢复将从剩余问题继续（已修复 %d 项保留）%s",
							i+1, len(batches), ctxt.FixBatchFailures, fixBatchFailureCap, batchErr.Error(), len(fixedIDs), idleNote)})
				}
			}
			return
		}
		// 本批完成：解析修复回执并落库断点（仍 S5/running），供失败恢复与重启续修。
		fixedNow := applyFixReceipt(&ctxt, batch, out.Text, s.logger)
		fixedIDs = append(fixedIDs, fixedNow...)
		ctxt.FixBatchFailures = 0 // 批次成功即清零连续失败计数
		if ctxtJSON, merr := json.Marshal(ctxt); merr == nil {
			if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusRunning, string(ctxtJSON)); serr != nil {
				s.logger.Error("S5 修复：批次断点落库失败", "err", serr)
			}
		}
	}
	s.finishFixAndReaudit(ctx, runID, novelID, ctxt)
}

// finishFixAndReaudit 是 S5 修复全部批次完成后的收尾：递增修复轮数、复位到原体检
// 阶段并自动复评（仍异步）。「修复到位」最终以复评（G4/G终 闸门）通过为准。
func (s *Service) finishFixAndReaudit(ctx context.Context, runID string, novelID int64, ctxt GateContext) {
	// 守卫：若异步期间状态已变更（如用户中止），不自动复评。
	cur, gerr := s.store.GetByRunID(ctx, runID)
	if gerr == nil && (cur.CurrentStage != StageS5 || cur.Status != StatusRunning) {
		s.logger.Info("S5 修复：状态已变更，放弃复评", "stage", cur.CurrentStage, "status", cur.Status)
		s.setDispatching(ctx, runID, false)
		return
	}

	// 广播修复完成：前端收尾活动流，提示进入复评。
	emitFixDone(ctx, map[string]any{"run_id": runID, "novel_id": novelID, "ok": true})

	// G3 蓝图漂移巡检：S5 修复可能改动了设计台账（卷纲/弧线/时间线），已写章节可能与当前蓝图
	// 产生新的不一致。修复完成后触发一次「轻量蓝图一致性巡检」（只读、非阻断），结论存入
	// ctxt.DriftReport，由复评闸门 summary 带给用户。无已写章节（纯蓝图阶段）时跳过。
	if n, cerr := s.chapterCount(ctx, novelID); cerr == nil && n > 0 {
		ctxt.DriftReport = s.runBlueprintDriftPatrol(ctx, runID, novelID)
	}

	// 复位到对应体检阶段 running 是为了让 executeAudit 的「状态未变更」守卫判定通过，
	// 否则守卫会因当前为 S5 而放弃落 G4 闸门，导致复评结果无家可归（见测试 TestServiceAuditFixLoopReAudit）。
	auditStage := ctxt.AuditStage
	if auditStage == "" {
		auditStage = StageS4
	}
	ctxt.FixRounds++ // 递增修复轮数（供 auto_run 上限判断，避免无限自动修复）
	ctxtJSON, _ := json.Marshal(ctxt)
	if err := s.store.SetStageStatus(ctx, runID, auditStage, StatusRunning, string(ctxtJSON)); err != nil {
		s.logger.Error("S5 修复：复评前复位阶段失败", "err", err)
		s.setDispatching(ctx, runID, false)
		return
	}
	s.setDispatching(ctx, runID, false)
	s.executeAudit(runID, novelID)
}

// finishFixHandoffRewrite 处理「本轮无台账级问题可修、但存在正文级重写清单」的情况：
// 这些问题必须回 S3 重写章节才能解决，S5 用补丁改不动。为闭合修复链，本函数默认**自动
// 派发章节重写 agent** 把被标记章节整章重写（用户选择全自动模式），重写后自动回到 S4 复评；
// 复评若仍报出同一批正文问题会再次进入本函数，由 autoRewriteCap 上限收敛（达到上限改交人工）。
//
// 三种「交回用户」的出口（明确暂停 + 广播 rewrite_handoff，前端展示「已转交章节重写」而非「修复完成」）：
//  0. 重写清单里没有任何可派发项（全为 V 系文风漂移豁免/写保护兜底项）——只报告不改写，
//     交回作者裁决（note「文风漂移项待作者裁决」），绝不进自动重写循环；
//  1. 重写轮数已达 autoRewriteCap —— 自动重写多次仍无效，交回人工决策；
//  2. 重写清单涉及章节无法在本书定位（chapters 为空或缺章）——无法自动重写，只能手动。
//
// 其余情况一律走自动重写 + 复评闭环，绝不谎报「修复完成」后立刻复评形成死循环。
func (s *Service) finishFixHandoffRewrite(ctx context.Context, runID string, novelID int64, ctxt GateContext) {
	// 守卫：若异步期间状态已变更（如用户中止），不重复处理。
	if cur, gerr := s.store.GetByRunID(ctx, runID); gerr == nil &&
		(cur.CurrentStage != StageS5 || cur.Status != StatusRunning) {
		s.setDispatching(ctx, runID, false)
		return
	}

	// 出口 0（P2 T2.2 派发豁免）：可派发项=非写保护且非 V 系文风漂移（isStyleExempt）。
	// V 系「只报告不改写」（WBS §0.3）：进 RewriteBacklog 供作者查看，但绝不派发
	// 自动章节重写。全豁免（清单里没有任何可派发项）→ 手动交接收口，绝不进
	// 「重写→复评」循环空转（风险 G2 防死循环）。
	dispatchable := make([]RewriteItem, 0, len(ctxt.RewriteBacklog))
	for _, item := range ctxt.RewriteBacklog {
		if writeProtected(item.TargetScope, item.Direction) || isStyleExempt(item.ID) {
			continue
		}
		dispatchable = append(dispatchable, item)
	}
	if len(dispatchable) == 0 {
		n := len(ctxt.RewriteBacklog)
		s.handoffRewriteWithMsg(ctx, runID, novelID, ctxt, fmt.Sprintf(
			"%d 个文风漂移项（V 系）只报告不改写，已豁免自动章节重写，文风漂移项待作者裁决："+
				"请在写作页酌情润色相关章节或保持原样后，再重新进入修复/复评",
			n,
		))
		return
	}

	// 出口 1：重写轮数已达上限 → 交回人工。
	if ctxt.RewriteRounds >= autoRewriteCap {
		s.handoffRewriteToUser(ctx, runID, novelID, ctxt)
		return
	}

	// 把重写清单涉及章节的阅读序号映射回真实 chapter_id；剔除无法定位的章节。
	// 只解析可派发项（写保护项与 V 系豁免项不派发，无需定位）。
	chaptersByReading, err := s.resolveRewriteChapters(ctx, novelID, dispatchable)
	if err != nil {
		s.logger.Warn("S5 重写：章节定位失败，降级交回用户", "run_id", runID, "err", err)
		s.handoffRewriteToUser(ctx, runID, novelID, ctxt)
		return
	}
	if len(chaptersByReading) == 0 {
		// 出口 2：清单未指向任何可定位章节（如无 chapters 标注或章节已删）→ 手动重写。
		s.handoffRewriteToUser(ctx, runID, novelID, ctxt)
		return
	}

	// 自动重写：逐章派发章节重写 agent（每章独立会话，充足轮数预算）。
	// 派发范围=可派发项（上方已过滤写保护与 V 系豁免；正常路径下 bodyIssuesOf 已把
	// 写保护项分流进 RetconBacklog，此处兜底旧数据）。
	s.setDispatching(ctx, runID, true)
	total := 0
	for _, item := range dispatchable {
		total += len(chaptersByReading[item.ID])
	}
	index := 0
	for _, item := range dispatchable {
		chaps := chaptersByReading[item.ID]
		for _, ch := range chaps {
			index++
			emitRewriteProgress(ctx, map[string]any{
				"run_id": runID, "novel_id": novelID, "phase": "starting",
				"index": index, "total": total, "chapter_id": ch.ID, "reading_number": ch.ReadingNumber,
			})
			if rerr := s.executeChapterRewrite(ctx, runID, novelID, item, ch); rerr != nil {
				s.logger.Error("S5 重写：章节重写失败", "run_id", runID, "chapter_id", ch.ID, "err", rerr)
				emitRewriteProgress(ctx, map[string]any{
					"run_id": runID, "novel_id": novelID, "phase": "failed",
					"index": index, "total": total, "chapter_id": ch.ID, "reading_number": ch.ReadingNumber, "error": rerr.Error(),
				})
				// 单章失败不阻断其余章节；失败章节仍留在重写清单，由复评与重试上限约束。
				continue
			}
			emitRewriteProgress(ctx, map[string]any{
				"run_id": runID, "novel_id": novelID, "phase": "done",
				"index": index, "total": total, "chapter_id": ch.ID, "reading_number": ch.ReadingNumber,
			})
		}
	}

	// 重写轮数 +1，持久化后自动回到 S4 复评（复评会重算重写清单：解决则出清单，仍无效则再进本函数）。
	ctxt.RewriteRounds++
	ctxtJSON, merr := json.Marshal(ctxt)
	if merr == nil {
		// 保持 S5/running，使下方复评前的守卫通过；复评函数会自行复位到体检阶段。
		if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusRunning, string(ctxtJSON)); serr != nil {
			s.logger.Error("S5 重写：落库重写轮数失败", "err", serr)
		}
	}
	s.setDispatching(ctx, runID, false)
	s.logger.Info("S5 重写：自动重写完成，回到复评", "run_id", runID, "round", ctxt.RewriteRounds, "total", total)
	s.executeAudit(runID, novelID)
}

// handoffRewriteToUser 把正文级重写清单交回用户手动处理：明确暂停当前阶段、广播带
// rewrite_handoff 标记的 fix_done（前端展示「已转交章节重写」而非「修复完成」），绝不谎报完成。
func (s *Service) handoffRewriteToUser(ctx context.Context, runID string, novelID int64, ctxt GateContext) {
	n := len(ctxt.RewriteBacklog)
	var msg string
	if ctxt.RewriteRounds >= autoRewriteCap {
		msg = fmt.Sprintf(
			"自动章节重写已尝试 %d 轮仍未解决；%d 个正文级问题转交你手动在写作页重写相关章节后，再重新进入修复/复评",
			autoRewriteCap, n,
		)
	} else {
		msg = fmt.Sprintf(
			"本轮无台账级问题可修；%d 个正文级问题已转交 S3 章节重写回路，请在写作页重写相关章节后重新进入修复/复评",
			n,
		)
	}
	s.handoffRewriteWithMsg(ctx, runID, novelID, ctxt, msg)
}

// handoffRewriteWithMsg 是 rewrite_handoff 交回的统一出口（暂停 + 广播 + 前端 note），
// 消息由调用方按语义定制（自动重写超限/章节不可定位/文风漂移全豁免）。
func (s *Service) handoffRewriteWithMsg(ctx context.Context, runID string, novelID int64, ctxt GateContext, msg string) {
	n := len(ctxt.RewriteBacklog)
	emitFixDone(ctx, map[string]any{
		"run_id":          runID,
		"novel_id":        novelID,
		"ok":              true,
		"rewrite_handoff": true,
		"rewrite_count":   n,
		"rewrite_rounds":  ctxt.RewriteRounds,
		"error":           msg,
	})
	if cur, gerr := s.store.GetByRunID(ctx, runID); gerr == nil &&
		cur.CurrentStage == StageS5 && cur.Status == StatusRunning {
		if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusPaused, cur.ContextJSON); serr != nil {
			s.logger.Error("S5 重写：交回落暂停失败", "err", serr)
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS5,
			Status: StatusPaused, Note: msg})
	}
	s.setDispatching(ctx, runID, false)
}

// handoffRetconToUser 把命中冻结卷写保护/retcon 候选的问题交回人工裁决：明确暂停当前
// 阶段、广播带 retcon_handoff 标记的 fix_done（前端展示「已转交人工裁决」而非「修复完成」）。
// 冻结卷正文与既定设定不得被自动回改（1.3），retcon 候选须作者显式拍板（retcon 审批闸门，
// WBS 3.1）；候选同时沉淀进 retcon_cases（状态机起点），供审批闸门逐项裁决。
// 本函数是安全出口，绝不带盲区派发、也绝不谎报完成。
func (s *Service) handoffRetconToUser(ctx context.Context, runID string, novelID int64, ctxt GateContext) {
	n := len(ctxt.RetconBacklog)
	s.persistRetconCandidates(ctx, novelID, ctxt.RetconBacklog)
	msg := fmt.Sprintf(
		"%d 项问题命中冻结卷写保护/retcon 候选（target_scope=frozen_volume 或 direction=retcon_candidate），"+
			"已拦截自动修复与自动重写，转交人工裁决：请优先前向适配（改本卷及后续适配已定稿前卷）；"+
			"确需回改前卷的，请经 retcon 审批闸门逐项审批后受控处理（批准后留痕编辑、可回滚）",
		n,
	)
	emitFixDone(ctx, map[string]any{
		"run_id":         runID,
		"novel_id":       novelID,
		"ok":             true,
		"retcon_handoff": true,
		"retcon_count":   n,
		"error":          msg,
	})
	if cur, gerr := s.store.GetByRunID(ctx, runID); gerr == nil &&
		cur.CurrentStage == StageS5 && cur.Status == StatusRunning {
		if serr := s.store.SetStageStatus(ctx, runID, StageS5, StatusPaused, cur.ContextJSON); serr != nil {
			s.logger.Error("S5 写保护：retcon 交回落暂停失败", "err", serr)
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS5,
			Status: StatusPaused, Note: msg})
	}
	s.setDispatching(ctx, runID, false)
}

// persistRetconCandidates 把 RetconBacklog 逐项沉淀为持久化 retcon 候选案（WBS 3.1
// 状态机起点，origin=pipeline）。同一条 issue 每轮体检都会重算进 RetconBacklog，
// 由 retcon.CreateCandidate 按（issueID, description）对未终态案件去重，重复沉淀幂等。
// 目标卷不做机械推断（issue 三枚举只标 frozen_volume 未指明哪一卷），留作者审批时在
// 界面确认；沉淀失败只记日志，不影响安全出口本身。
func (s *Service) persistRetconCandidates(ctx context.Context, novelID int64, items []RewriteItem) {
	if s.retconGate == nil || len(items) == 0 {
		return
	}
	for _, it := range items {
		in := retcon.CreateInput{
			NovelID:     novelID,
			Origin:      retcon.OriginPipeline,
			IssueID:     it.ID,
			TargetScope: it.TargetScope,
			Dimension:   it.Dimension,
			Direction:   it.Direction,
			Description: it.Desc,
		}
		// 文风漂移（V 系）立案带 style（P2 T4.2）：Rollback 只还原 voice 行，
		// 事实层零触碰；其余（I/D/T 系）缺省 fact，全量回滚路由不变。
		if isStyleExempt(it.ID) {
			in.Kind = retcon.KindStyle
		}
		if _, err := s.retconGate.CreateCandidate(ctx, in); err != nil {
			s.logger.Warn("retcon 闸门：候选沉淀失败", "novel_id", novelID, "issue_id", it.ID, "err", err)
		}
	}
}

// resolveRewriteChapters 把重写清单里每个 issue 涉及的章节阅读序号，映射回本书真实章节
// （chapter_id + 标题 + 阅读序号），供章节重写 agent 精确定位。返回 issueID → 章节列表；
// 无法定位的章节（序号越界 / 章节已删）被丢弃；chaptersStore 为 nil 时返回空（降级手动）。
func (s *Service) resolveRewriteChapters(ctx context.Context, novelID int64, items []RewriteItem) (map[string][]chapter.Chapter, error) {
	if s.chapterStore == nil {
		return map[string][]chapter.Chapter{}, nil
	}
	chapters, err := s.chapterStore.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, err
	}
	// 阅读序号（1 起）→ 章节。
	byReading := make(map[int64]chapter.Chapter, len(chapters))
	for _, ch := range chapters {
		byReading[int64(ch.ReadingNumber)] = ch
	}
	out := make(map[string][]chapter.Chapter, len(items))
	for _, item := range items {
		if len(item.Chapters) == 0 {
			continue
		}
		var resolved []chapter.Chapter
		for _, rn := range item.Chapters {
			if ch, ok := byReading[rn]; ok {
				resolved = append(resolved, ch)
			} else {
				s.logger.Warn("S5 重写：章节阅读序号无法定位，跳过", "novel_id", novelID, "item", item.ID, "reading_number", rn)
			}
		}
		if len(resolved) > 0 {
			out[item.ID] = resolved
		}
	}
	return out, nil
}

// executeChapterRewrite 对单章派发章节重写 agent（chapter-rewrite 技能），按 body issue 整章重写。
func (s *Service) executeChapterRewrite(ctx context.Context, runID string, novelID int64, item RewriteItem, ch chapter.Chapter) error {
	prompt := chapterRewritePrompt(novelID, item, ch,
		s.buildBlocks(ctx, novelID, contextasm.PurposeRewrite))
	_, err := s.skillRunner(ctx, "chapter-rewrite", SkillInput{
		NovelID:   novelID,
		Prompt:    prompt,
		MaxTurns:  18,
		SessionID: "pipeline:" + runID + ":rewrite:" + uuid.NewString(),
	})
	if err != nil {
		return fmt.Errorf("章节重写（chapter_id=%d）失败: %w", ch.ID, err)
	}
	return nil
}

// chapterRewritePrompt 构造单章重写的任务提示：注入章节定位信息与要解决的正文级 issue。
// blocks 按 Facts→Voice→Intent 顺序尾部装配（P0 §0.5 rewrite 三块全注）；
// 冻结快照非空时重写须在既定事实上展开，不得写回冻结卷。
func chapterRewritePrompt(novelID int64, item RewriteItem, ch chapter.Chapter, blocks contextasm.ContextBlocks) string {
	return fmt.Sprintf(
		"本书 novel_id=%d。请整章重写以下章节以解决正文级签约体检问题：\n"+
			"- 第 %d 章（chapter_id=%d，标题：%s）— 问题 %s[%s] %s：%s\n\n"+
			"要求：用 edit 工具传 full_replace=true 把该章**整章正文**替换为重写后的版本，真正解决上述问题"+
			"（如立人设 / 补爽点 / 修节奏 / 兑现伏笔），保持与前后章节及台账（角色/弧线/时间线/读者认知）一致；"+
			"正文格式遵守规范：无破折号「——」、无序号段落分隔符、无 AI 味硬伤，正文只含叙事不含章节号/标题/章末标记。",
		novelID, ch.ReadingNumber, ch.ID, ch.Title, item.ID, item.Level, item.Area, item.Desc,
	) + blocks.Render(contextasm.PurposeRewrite)
}

// runBlueprintDriftPatrol 执行一次 G3 轻量蓝图一致性巡检（只读、非阻断）：
// 跑 blueprint-drift-check 技能比对「当前卷纲」与「已写章节」，返回人类可读结论；
// 巡检失败只记日志并返回空串（不落库、不阻断复评主流程）。
func (s *Service) runBlueprintDriftPatrol(ctx context.Context, runID string, novelID int64) string {
	out, err := s.skillRunner(ctx, "blueprint-drift-check", SkillInput{
		NovelID:   novelID,
		Prompt:    driftPatrolPrompt(novelID),
		MaxTurns:  8,
		SessionID: "pipeline:" + runID + ":drift",
	})
	if err != nil {
		s.logger.Warn("G3 蓝图巡检执行失败（非阻断，跳过）", "run_id", runID, "err", err)
		return ""
	}
	audit := parseFreshAudit(out.Text)
	if audit.Verdict == "pass" || len(audit.Issues) == 0 {
		return "蓝图巡检：未发现与当前卷纲冲突的章节。"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "蓝图巡检：%d 章与当前卷纲冲突，建议重读：", len(audit.Issues))
	for i, iss := range audit.Issues {
		if i >= 5 {
			sb.WriteString("等。")
			break
		}
		if i > 0 {
			sb.WriteString("；")
		}
		fmt.Fprintf(&sb, "%s %s", iss.Area, iss.Desc)
	}
	return sb.String()
}

// driftPatrolPrompt 构造 G3 蓝图漂移巡检的任务提示词（只读、轻量、非评分）。
func driftPatrolPrompt(novelID int64) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 blueprint-drift-check 方法论做一次**轻量蓝图一致性巡检**："+
		"逐卷 read 当前卷纲（volumes/id_{卷id}.md），对照已写章节的细纲与关键正文段落，"+
		"只标出与当前卷纲冲突的章节（剧情矛盾/卷纲承诺未兑现/顺序错位），不评分、不修稿；"+
		"若没有任何已写章节，直接输出空冲突清单。最终回复末尾**必须**附一个 ```json 代码块，格式："+
		auditFormatLine(false, issueSampleDrift)+"。"+
		"无冲突 → verdict=pass；有冲突 → needs_fix。", novelID)
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
	s.setDispatching(ctx, runID, true)

	out, err := s.skillRunner(ctx, "signability-audit", SkillInput{
		NovelID:   novelID,
		Prompt:    finalAuditPrompt(novelID, s.buildBlocks(ctx, novelID, contextasm.PurposeAudit)),
		MaxTurns:  16,
		SessionID: "pipeline:" + runID + ":final",
	})
	if err != nil {
		s.logger.Error("S6 终检执行失败", "run_id", runID, "err", err)
		s.setDispatching(ctx, runID, false)
		if serr := s.store.SetStageStatus(ctx, runID, StageS6, StatusPaused, run.ContextJSON); serr != nil {
			s.logger.Error("S6 终检：失败落暂停失败", "err", serr)
		}
		emitStage(ctx, StagePayload{RunID: runID, NovelID: novelID, Stage: StageS6, Status: StatusPaused,
			Note: "完本终检失败：" + err.Error() + "；已暂停，点击恢复将重新终检"})
		return
	}

	audit := parseFreshAudit(out.Text)
	// P2 文风漂移：终检扫全书（scope=nil），V 系追加尾部；verdict 不翻转（G终人工裁决）。
	audit.Issues = mergeDriftIssues(audit.Issues, s.runStyleDriftCheck(ctx, novelID, nil))
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
	// 无论守卫结果如何，本执行体即将退出，先清除执行中标记。
	s.setDispatching(ctx, runID, false)
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

// finalAuditPrompt 构造 S6 完本级终检的任务提示词。issue JSON 契约片段由 contract.go 单源生成（全量契约）。
// blocks 尾部装配上下文块（P0 §0.5 audit=Facts+Voice，完本审同体检口径）。
func finalAuditPrompt(novelID int64, blocks contextasm.ContextBlocks) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 signability-audit 方法论，对全书做**完本级**终检："+
		"**逐章通读**所有成稿章节（不得抽样跳章），逐维打分并举证；最终回复末尾**必须**附一个 ```json 代码块，格式："+
		auditFormatLine(true, issueSampleDefault)+"——"+issueFieldRules()+
		"文风漂移项（如有）走 V 系契约："+auditFormatLine(true, issueSampleStyle)+
		"（id 从 V1 起编、level 恒 P1、不计入评分与 verdict）；"+
		"存在任意 P0 致命伤 → verdict=needs_fix；无 P0 且 score≥34/40 → pass。", novelID) + blocks.Render(contextasm.PurposeAudit)
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

// ── S0 存量对齐子流程（PR5 缺口补全）────────────────────────────────
//
// 存量书（有章节无台账/卷）导入流水线时经 S0 入口，需做最小对齐才能保证续写质量：
//  1. 建默认卷并把所有 volume_id 为 NULL 的章节归入该卷（修复 G3b 卷边界判定）；
//  2. 批量 statesync 回填角色/地点/弧线/时间线/读者认知台账（否则续写 AI 看不到已有设定）；
//  3. 标记 ImportAligned=true。
// 全部为「非阻断」：任一步失败仅记日志，不回滚已完成的步骤。

// defaultVolumeAssigner 为存量书建一个默认卷「正文」并把所有未分卷章节归入该卷。
// 目的：修复 G3b 卷边界判定（NULL volume_id 会让卷边界分支永不命中）；不破坏已有分卷。
func (s *Service) defaultVolumeAssigner(ctx context.Context, novelID int64) error {
	if s.store == nil || s.store.db == nil {
		return nil
	}
	db := s.store.db.WithContext(ctx)
	var vcount int64
	if err := db.Table("volumes").Where("novel_id = ?", novelID).Count(&vcount).Error; err != nil {
		return err
	}
	var volumeID int64
	if err := db.Table("volumes").Where("novel_id = ?", novelID).Order("sort_order DESC").Limit(1).Pluck("id", &volumeID).Error; err != nil {
		return err
	}
	if vcount == 0 {
		if err := db.Table("volumes").Create(map[string]any{
			"novel_id":   novelID,
			"name":       "正文",
			"sort_order": 0,
		}).Error; err != nil {
			return err
		}
		if err := db.Table("volumes").Where("novel_id = ?", novelID).Order("id DESC").Limit(1).Pluck("id", &volumeID).Error; err != nil {
			return err
		}
	}
	if volumeID > 0 {
		if err := db.Table("chapters").Where("novel_id = ? AND volume_id IS NULL", novelID).Update("volume_id", volumeID).Error; err != nil {
			return err
		}
	}
	return nil
}

// executeS0Align 在后台为存量书做最小对齐：建默认卷挂 volume_id + 批量 statesync 回填台账。
// 非阻塞（由 advanceS0 以 goroutine 调用），完成后置 ImportAligned=true。
func (s *Service) executeS0Align(ctx context.Context, run *PipelineRun) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("S0 对齐：panic 恢复", "panic", r)
		}
	}()
	novelID := run.NovelID
	if s.volumeAssigner != nil {
		if err := s.volumeAssigner(ctx, novelID); err != nil {
			s.logger.Warn("S0 对齐：建卷失败（非阻断）", "err", err, "novelID", novelID)
		}
	}
	ids := []int64{}
	if s.chapterLister != nil {
		if lst, err := s.chapterLister(ctx, novelID); err == nil {
			ids = lst
		} else {
			s.logger.Warn("S0 对齐：列举章节失败（非阻断）", "err", err)
		}
	}
	for _, cid := range ids {
		if s.pausedOrAborted(ctx, run) {
			s.logger.Info("S0 对齐：被暂停/中止，停止", "novelID", novelID)
			return
		}
		if s.stateSyncRunner != nil {
			if err := s.stateSyncRunner(ctx, novelID, cid); err != nil {
				s.logger.Warn("S0 对齐：单章 statesync 失败（非阻断）", "chapterID", cid, "err", err)
			}
		}
	}
	s.markImportAligned(ctx, run)
}

func (s *Service) isImportAligned(ctx context.Context, run *PipelineRun) bool {
	cur, err := s.store.GetByRunID(ctx, run.RunID)
	if err != nil {
		return false
	}
	var ctxt GateContext
	if cur.ContextJSON != "" {
		_ = json.Unmarshal([]byte(cur.ContextJSON), &ctxt)
	}
	return ctxt.ImportAligned
}

func (s *Service) markImportAligned(ctx context.Context, run *PipelineRun) {
	cur, err := s.store.GetByRunID(ctx, run.RunID)
	if err != nil {
		return
	}
	var ctxt GateContext
	if cur.ContextJSON != "" {
		_ = json.Unmarshal([]byte(cur.ContextJSON), &ctxt)
	}
	ctxt.ImportAligned = true
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn("S0 对齐：序列化上下文失败", "err", merr)
		return
	}
	if err := s.store.SetStageStatus(ctx, run.RunID, cur.CurrentStage, cur.Status, string(updated)); err != nil {
		s.logger.Warn("S0 对齐：写回 ImportAligned 失败（非阻断）", "err", err)
	}
}

// runFullAlign 「全量对齐」按钮触发：确保对齐完成（建卷+statesync）+ 跑全本现状体检，
// 结果存 context.StockAudit 并广播 pipeline:stock_audit 事件通知前端刷新。
// 设计为后台异步（由 ControlPipeline 以 goroutine 调用），不阻塞控制指令返回。
func (s *Service) runFullAlign(ctx context.Context, run *PipelineRun) {
	bg := s.appCtx
	novelID := run.NovelID
	if !s.isImportAligned(bg, run) {
		s.executeS0Align(bg, run)
	}
	data, err := s.runStockAudit(bg, novelID)
	if err != nil {
		s.logger.Error("全量对齐：体检失败", "err", err, "novelID", novelID)
		emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: novelID, Stage: run.CurrentStage, Status: run.Status, Note: "全量体检失败：" + err.Error()})
		return
	}
	if err := s.storeStockAudit(bg, run, data); err != nil {
		s.logger.Warn("全量对齐：写回体检结果失败", "err", err)
	}
	emitStockAudit(ctx, map[string]any{
		"novel_id": novelID,
		"run_id":   run.RunID,
	})
}

func (s *Service) storeStockAudit(ctx context.Context, run *PipelineRun, data AuditData) error {
	cur, err := s.store.GetByRunID(ctx, run.RunID)
	if err != nil {
		return err
	}
	var ctxt GateContext
	if cur.ContextJSON != "" {
		_ = json.Unmarshal([]byte(cur.ContextJSON), &ctxt)
	}
	auditJSON, _ := json.Marshal(data)
	ctxt.StockAudit = string(auditJSON)
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		return merr
	}
	return s.store.SetStageStatus(ctx, run.RunID, cur.CurrentStage, cur.Status, string(updated))
}

func (s *Service) runStockAudit(ctx context.Context, novelID int64) (AuditData, error) {
	out, err := s.skillRunner(ctx, "signability-audit", SkillInput{
		NovelID:   novelID,
		Prompt:    stockAuditPrompt(novelID),
		MaxTurns:  14,
		SessionID: "pipeline:stock:" + strconv.FormatInt(novelID, 10),
	})
	if err != nil {
		return AuditData{Verdict: "needs_fix"}, err
	}
	return parseFreshAudit(out.Text), nil
}

// stockAuditPrompt 构造「全量对齐」全本现状体检的任务提示词。issue JSON 契约片段由 contract.go 单源生成（全量契约）。
func stockAuditPrompt(novelID int64) string {
	return fmt.Sprintf("本书 novel_id=%d。请对全书做**签约级现状体检**：用 get_chapter_list 列出全部章节，**逐章通读所有已落库章节**（不得抽样跳章），"+
		"从签约编辑视角评估：开篇钩子与过签力、人设记忆点、节奏与爽点密度、世界观/设定自洽、完本潜力、题材契合与数据预期等维度，逐维打分并举证；"+
		"最终回复末尾**必须**附一个 ```json 代码块，格式："+
		auditFormatLine(true, issueSampleDefault)+"——"+issueFieldRules()+
		"存在任意 P0 致命伤 → verdict=needs_fix；无 P0 且 score≥29/40 → pass。", novelID)
}

// auditJSONRe 提取 signability-audit 输出中的 ```json ... ``` 结构化块。
var auditJSONRe = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// parseAudit 从 skill 输出文本解析结构化体检结果；失败则退化为单条 P0 issue。
// fixReceipt 是修复 agent 在输出末尾上报的修复回执契约（由 fixPrompt/blueprintFixPrompt 要求）。
type fixReceipt struct {
	Fixed   []string `json:"fixed"`
	Skipped []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"skipped"`
}

// applyFixReceipt 解析修复输出回执并把本批问题的断点状态写入 ctxt.FixStates：
// 回执 fixed → "fixed"；skipped → "skipped"（记录原因，不再重试）；未提及 → 不落断点
// （后续轮次重试）。输出缺少回执 JSON 时退化为旧行为（整批 "fixed"），保证兼容不卡死
// 流水线。返回本批新标记为 fixed 的 id 列表（供前端 done_ids 累积）。
func applyFixReceipt(ctxt *GateContext, batch []AuditIssue, outText string, log *slog.Logger) []string {
	fixedSet, skipReasons, found := parseFixReceipt(outText)
	if !found {
		log.Warn("S5 修复：本批修复输出缺少回执 JSON，按整批已修复处理（兼容回退）",
			"batch_size", len(batch))
	}
	if ctxt.FixStates == nil {
		ctxt.FixStates = map[string]string{}
	}
	fixedNow := make([]string, 0, len(batch))
	for _, it := range batch {
		switch {
		case !found || fixedSet[it.ID]:
			ctxt.FixStates[it.ID] = "fixed"
			fixedNow = append(fixedNow, it.ID)
		case skipReasons[it.ID] != "":
			ctxt.FixStates[it.ID] = "skipped"
			log.Info("S5 修复：agent 上报无法修复，标记跳过",
				"issue", it.ID, "reason", skipReasons[it.ID])
		default:
			// 回执未提及：不落断点，后续轮次重试。
		}
	}
	return fixedNow
}

// parseFixReceipt 从修复 agent 输出解析修复回执：{"fixed":["I1"],"skipped":[{"id":"I2","reason":"..."}]}。
// 回执可能在 ```json 围栏或纯文本中，且 skipped 含嵌套对象——从每个 '{' 起用 json.Decoder
// 流式解码（容忍尾部文本），取最后一个合法且非空的回执。found=false 表示输出中没有回执，
// 调用方退化为「整批已修复」的旧行为。
func parseFixReceipt(text string) (fixed map[string]bool, skipReasons map[string]string, found bool) {
	for i := strings.LastIndex(text, "{"); i >= 0; i = strings.LastIndex(text[:i], "{") {
		var r fixReceipt
		if err := json.NewDecoder(strings.NewReader(text[i:])).Decode(&r); err != nil {
			continue
		}
		if len(r.Fixed) == 0 && len(r.Skipped) == 0 {
			continue
		}
		fixed = make(map[string]bool, len(r.Fixed))
		for _, id := range r.Fixed {
			fixed[id] = true
		}
		skipReasons = make(map[string]string, len(r.Skipped))
		for _, sk := range r.Skipped {
			if sk.ID != "" {
				skipReasons[sk.ID] = sk.Reason
			}
		}
		return fixed, skipReasons, true
	}
	return nil, nil, false
}

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

// parseFreshAudit 解析 skill 刚产出的体检文本并补标 schema_version（见 contract.go）：
// LLM 未 echo schema_version 时按当前契约版本补标——产出方是当前代码的提示词，语义即当前
// 契约版本，供后续 executeFix 版本门判定新旧。已落库历史 JSON 的再解析必须走 parseAudit
// （不补标），否则旧数据会被误标为当前版本、版本门失效。
func parseFreshAudit(text string) AuditData {
	a := parseAudit(text)
	if a.SchemaVersion == 0 {
		a.SchemaVersion = AuditSchemaVersion
	}
	return a
}

// writeProtected 判断问题是否命中冻结卷写保护/retcon 候选（1.3/1.4）：
// target_scope=frozen_volume（落点在已冻结定稿卷，改它=破坏定稿）或
// direction=retcon_candidate（硬矛盾需回改前卷，须作者审批）。这类问题一律
// 不派发自动修复、不派发自动章节重写，交回人工裁决。
func writeProtected(targetScope, direction string) bool {
	return targetScope == TargetScopeFrozenVolume || direction == DirectionRetconCandidate
}

// bodyIssuesOf 从体检结果中析出可自动重写的正文级（fix_type=body）问题，构造回流 S3
// 重写回路的清单。命中写保护（writeProtected）的正文级问题不进本清单（冻结卷章节
// 不得被自动重写），改由 retconItemsOf 析出到 RetconBacklog。
func bodyIssuesOf(audit AuditData) []RewriteItem {
	items := make([]RewriteItem, 0, len(audit.Issues))
	for _, it := range audit.Issues {
		if it.FixType != FixTypeBody || writeProtected(it.TargetScope, it.Direction) {
			continue
		}
		items = append(items, RewriteItem{
			ID:          it.ID,
			Level:       it.Level,
			Area:        it.Area,
			Desc:        it.Desc,
			Chapters:    parseChapterRefs(it.Area, it.Desc),
			TargetScope: it.TargetScope,
			Dimension:   it.Dimension,
			Direction:   it.Direction,
		})
	}
	return items
}

// retconItemsOf 从体检结果中析出命中冻结卷写保护/retcon 候选的全部问题（不分 fix_type），
// 构造 RetconBacklog（交回人工裁决清单）。与 bodyIssuesOf 互补：两者并集=全部 body 问题
// ∪ 全部写保护问题，互不相交。
func retconItemsOf(audit AuditData) []RewriteItem {
	items := make([]RewriteItem, 0, len(audit.Issues))
	for _, it := range audit.Issues {
		if !writeProtected(it.TargetScope, it.Direction) {
			continue
		}
		items = append(items, RewriteItem{
			ID:          it.ID,
			Level:       it.Level,
			Area:        it.Area,
			Desc:        it.Desc,
			Chapters:    parseChapterRefs(it.Area, it.Desc),
			TargetScope: it.TargetScope,
			Dimension:   it.Dimension,
			Direction:   it.Direction,
		})
	}
	return items
}

// upsertRewriteItem 按 ID 去重写入/更新重写清单项（复评后同 ID 问题刷新内容）。
func upsertRewriteItem(list []RewriteItem, item RewriteItem) []RewriteItem {
	for i, it := range list {
		if it.ID == item.ID {
			list[i] = item
			return list
		}
	}
	return append(list, item)
}

// parseChapterRefs 尽力从 area/desc 文本解析涉及的章节号，供 S3 写作回路定位待重写章节。
// 识别「第 N 章」「第 N-M 章」区间与「#N」两种写法；解析失败或越界（≤0）忽略。
var (
	chapterRangeRe = regexp.MustCompile(`第\s*(\d+)\s*(?:[-~到至]\s*(\d+))?\s*章`)
	chapterHashRe  = regexp.MustCompile(`#\s*(\d+)`)
)

func parseChapterRefs(area, desc string) []int64 {
	text := area + " " + desc
	seen := map[int64]bool{}
	var refs []int64
	add := func(n int64) {
		if n > 0 && !seen[n] {
			seen[n] = true
			refs = append(refs, n)
		}
	}
	for _, m := range chapterRangeRe.FindAllStringSubmatch(text, -1) {
		if a, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			add(a)
			if m[2] != "" {
				if b, err := strconv.ParseInt(m[2], 10, 64); err == nil && b >= a {
					for n := a + 1; n <= b; n++ {
						add(n)
					}
				}
			}
		}
	}
	for _, m := range chapterHashRe.FindAllStringSubmatch(text, -1) {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			add(n)
		}
	}
	return refs
}

// rewriteBacklogSummary 生成 G4 闸门 summary 的正文级重写提示文案。
func rewriteBacklogSummary(items []RewriteItem) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "另有 %d 项正文级问题已回流 S3 重写回路（需在写作中重写对应章节）", len(items))
	for i, it := range items {
		if i >= 3 {
			sb.WriteString("等")
			break
		}
		if i > 0 {
			sb.WriteString("；")
		}
		sb.WriteString(it.Area + "：" + it.Desc)
	}
	return sb.String()
}

// emitRewriteBacklog 广播「正文级问题回流 S3 重写回路」事件，通知前端刷新并展示重写清单。
func (s *Service) emitRewriteBacklog(ctx context.Context, runID string, novelID int64, items []RewriteItem) {
	emitRewriteBacklog(ctx, map[string]any{"run_id": runID, "novel_id": novelID, "items": items})
}

// volumeScopeText 生成 S4 体检提示词的卷级作用域说明（G1：S4 只扫当前卷）。
// volumeID 为空（book-run 全书路径）返回空串，提示词与旧行为逐字一致。
func (s *Service) volumeScopeText(ctx context.Context, novelID int64, volumeID *int64) string {
	if volumeID == nil {
		return ""
	}
	label := fmt.Sprintf("volume_id=%d", *volumeID)
	if v, err := volume.NewStore(s.store.db, s.logger).GetByID(ctx, nil, novelID, *volumeID); err == nil && v != nil && v.Name != "" {
		label = fmt.Sprintf("《%s》（volume_id=%d）", v.Name, *volumeID)
	}
	return fmt.Sprintf("本次体检作用域=%s（卷级 scoped 体检）：**只审属于该卷的章节**"+
		"（以 get_chapter_list 返回的 volume_id 归属为准，只审 volume_id=%d 的章节），"+
		"其它卷的章节仅作背景参照，不逐章审读、不评分不举证，也不要在 issues 里报其它卷自身的问题；"+
		"若本卷与前卷（已定稿）设定有冲突，在 issues 中照常报告，但修复方向应为「改本卷适配前卷」而非改动前卷；",
		label, *volumeID)
}

// signabilityPrompt 构造 S4 签约体检阶段的任务提示词。scopeText 非空时为卷级作用域说明（以「；」结尾）。
// blocks 尾部装配上下文块（S4 依据前卷快照校验，WBS 2.3；P0 §0.5 audit=Facts+Voice）。
// issue JSON 契约片段由 contract.go 单源生成（全量契约）。
func signabilityPrompt(novelID int64, scopeText string, blocks contextasm.ContextBlocks) string {
	return fmt.Sprintf("本书 novel_id=%d。%s请基于 signability-audit 方法论，对已成稿章节做签约级体检："+
		"用 get_chapter_list 列出章节，**逐章通读全部已成稿章节**（黄金三章逐字精读，其余分批评完，不得抽样跳章），逐维打分并举证；"+
		"最终回复末尾**必须**附一个 ```json 代码块，格式："+
		auditFormatLine(true, issueSampleDefault)+"——"+issueFieldRules()+
		"文风漂移项（如有）走 V 系契约："+auditFormatLine(true, issueSampleStyle)+
		"（id 从 V1 起编、level 恒 P1、不计入评分与 verdict）；"+
		"存在任意 P0 致命伤 → verdict=needs_fix；无 P0 且 score≥29/40 → pass。", novelID, scopeText) + blocks.Render(contextasm.PurposeAudit)
}

// blueprintAuditPrompt 构造 S2 蓝图体检阶段的任务提示词。issue JSON 契约片段由 contract.go
// 单源生成（核心契约：蓝图体检不涉 fix_type/卷作用域字段）。
func blueprintAuditPrompt(novelID int64) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 blueprint-audit 方法论，对本书**蓝图（卷纲结构）**做签约级体检："+
		"用 list_volumes / get_chapter_list / get_story_arcs / get_characters / get_reader_perspective 读取蓝图与已落设定，"+
		"逐维打分并举证；最终回复末尾**必须**附一个 ```json 代码块，格式："+
		auditFormatLine(false, issueSampleDefault)+"。"+
		"存在任意 P0 致命伤 → verdict=needs_fix；无 P0 且 score≥24/30 → pass。", novelID)
}

// blueprintFixPrompt 构造 S2 蓝图修复阶段的任务提示词，内嵌蓝图体检产出的 issue 清单 JSON。
func blueprintFixPrompt(novelID int64, issues []AuditIssue, summary string) string {
	data := AuditData{Verdict: "needs_fix", Summary: summary, Issues: issues}
	b, _ := json.Marshal(data)
	return fmt.Sprintf("本书 novel_id=%d。以下是蓝图体检本轮待修复的 issue 清单（JSON，共 %d 条，均为未修复项），请逐条修复卷纲：\n%s\n\n"+
		"按 blueprint-fix 方法论：P0 优先于 P1，用 edit（仅 volumes/id_{id}.md 卷纲）/create_volume 修复，"+
		"每条问题修复到位后再处理下一条，全部修复后给出不超过 15 行的修复摘要。"+
		"最终回复末尾**必须**附一个 ```json 代码块上报修复回执，格式："+
		fixReceiptFormatLine()+"——"+
		"fixed 列出已修复到位的 issue id；skipped 列出确实无法修复的及原因；未列入 fixed 的会被视为未修复并在后续轮次重试，请如实上报，不要把没修的标成已修。"+
		"（注：蓝图级问题均为卷纲/设定层，不涉及正文章节重写，无需考虑 fix_type=body 的回流。）",
		novelID, len(issues), b)
}

// fixPrompt 构造一批待修问题的修复提示词：只传本批问题子集（均为未修复项），
// 保证 agent 在有限轮数内聚焦逐条修复到位。blocks 尾部装配上下文块
// （S5 前向修复以快照为唯一依据，WBS 2.3；P0 §0.5 fix=Facts+Voice）。
func fixPrompt(novelID int64, issues []AuditIssue, summary string, blocks contextasm.ContextBlocks) string {
	data := AuditData{Verdict: "needs_fix", Summary: summary, Issues: issues}
	b, _ := json.Marshal(data)
	return fmt.Sprintf("本书 novel_id=%d。以下是本轮待修复的 issue 清单（JSON，共 %d 条，均为未修复的**台账/卷纲级（fix_type=ledger）**项）。"+
		"注意：**正文级（fix_type=body）问题已由流水线路由到 S3 章节重写回路，不会出现在本批清单中，你无需处理，也不要自作主张去重写整章**。"+
		"**冻结卷写保护（必须遵守）**：本批均为前向修复项（direction=forward_fix）——跨卷冲突一律改本卷及后续适配已定稿前卷，**不得改动已冻结卷（frozen 卷）的正文与既定设定**；"+
		"命中 retcon 候选（direction=retcon_candidate）的问题已被流水线拦截转人工审批，不会出现在本批，你不要代为回改前卷。"+
		"**修前评估护栏（必须遵守）**：动手修每条 issue 前，先估算它需要触碰的台账条目数与章节数；"+
		"若预计超过 5 条台账 entry 或 2 章正文（如「整池悬念治理」「批量伏笔改挂」这类工程级问题），**不要硬修**，"+
		"直接把该 issue 放入回执的 skipped 并注明 reason=\"scope exceeds single-batch budget（建议拆分为原子 issue 或转人工/章节重写）\"，然后继续下一条。"+
		"宁可如实上报跳过，也不要磨到对话轮数耗尽导致整批失败、已做工作全部作废。"+
		"请仅对以下 ledger 级问题逐条修复：\n%s\n\n"+
		"按 signability-fix 方法论：P0 优先于 P1，用 edit/update_chapter_plan/create_volume/台账写工具修复，"+
		"每条问题修复到位后再处理下一条，全部修复后给出不超过 15 行的修复摘要。"+
		"最终回复末尾**必须**附一个 ```json 代码块上报修复回执，格式："+
		fixReceiptFormatLine()+"——"+
		"fixed 列出已修复到位的 issue id；skipped 列出确实无法修复的及原因（如属正文级问题需章节重写，台账修复无法覆盖）；"+
		"未列入 fixed 的会被视为未修复并在后续轮次重试，请如实上报，不要把没修的标成已修。",
		novelID, len(issues), b) + blocks.Render(contextasm.PurposeFix)
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
		// 同步广播 stage 事件：侧边栏阶段条只订阅 stage 事件，不广播会导致
		// 停止/暂停后阶段条仍显示旧阶段旧状态（与工作区向导不一致）。
		emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: run.CurrentStage,
			Status: StatusPaused, Note: "已手动暂停，点击继续可从当前阶段恢复"})
	case "resume":
		if run.Status != StatusPaused {
			return fmt.Errorf("当前状态 %s 无法恢复", run.Status)
		}
		if err := s.store.SetStatus(ctx, runID, StatusRunning, run.ContextJSON); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: StatusRunning})
		emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: run.CurrentStage,
			Status: StatusRunning, Note: "已恢复运行"})
		// 按需重新派发执行体：修复/体检失败落暂停的运行、以及启动恢复机制暂停的中断运行，
		// 都在此按当前阶段重派发（体检/修复是幂等重入的），否则状态回到 running 却无人执行。
		// 执行体仍存活（Dispatching=true，软暂停）时不重派发，等待其在批次/阶段边界继续。
		s.redispatchIfNeeded(ctx, run)
	case "stop":
		if err := s.store.SetStatus(ctx, runID, StatusAborted, run.ContextJSON); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: StatusAborted})
		// stop 必须广播 stage 事件：否则阶段条停留在旧阶段（如「修复闭环」），
		// 而工作区向导重新拉取后已切到其它 run，两侧信息不一致。
		emitStage(ctx, StagePayload{RunID: runID, NovelID: run.NovelID, Stage: run.CurrentStage,
			Status: StatusAborted, Note: "流水线已终止"})
		// 联动终止同族 run（book-run ↔ 卷级 scoped sub-run），避免孤儿子 run / 永久等待。
		s.abortRelatives(ctx, run)
	case "set_auto":
		// 运行中切换 auto_run：翻转 AutoRun 字段。开启后，自动化的阶段（S2/S4/S5）
		// 在闸门点自动裁决连跑；关闭则恢复为人工逐闸确认。
		if err := s.store.SetAutoRun(ctx, runID, !run.AutoRun); err != nil {
			return err
		}
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: run.Status})
	case "align_full":
		// 全量对齐：后台异步执行（建卷+statesync+全本体检），不阻塞控制指令返回。
		go s.runFullAlign(ctx, run)
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: run.Status})
	case "patrol":
		// 一致性巡检（WBS 3.3 张力报告）：后台异步执行，只读非阻断、不改写、不改变当前阶段。
		go s.runConsistencyPatrol(ctx, runID, run.NovelID)
		emitControlAck(ctx, ControlAckPayload{RunID: runID, Cmd: cmd, Status: run.Status})
	default:
		return fmt.Errorf("未知控制命令 %q", cmd)
	}
	return nil
}

// RecoverInterruptedRuns 应用启动时调用：扫描 status=running 的「僵尸运行」。
// 流水线的执行体是内存 goroutine，随进程退出而消亡，但 DB 状态仍停留在 running——
// 重启后用户会看到「修复中/体检中」却永远没有进展（也不报错）。此处统一置为 paused，
// 并在 ContextJSON 打上 interrupted 标记；用户点「恢复」时按阶段重新派发执行体。
// 已打过标记的运行跳过，保证幂等（OnStartup / RetryStartup 可能多次调用）。
func (s *Service) RecoverInterruptedRuns() {
	ctx := s.appCtx
	runs, err := s.store.ListByStatus(ctx, StatusRunning)
	if err != nil {
		s.logger.Error("启动恢复：扫描运行中流水线失败", "err", err)
		return
	}
	for _, run := range runs {
		var ctxt GateContext
		if run.ContextJSON != "" {
			_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
		}
		if ctxt.Interrupted {
			continue
		}
		ctxt.Interrupted = true
		ctxt.Dispatching = false // 进程已重启，旧执行体必然已消亡，清除失真标记
		updated, merr := json.Marshal(ctxt)
		if merr != nil {
			s.logger.Error("启动恢复：序列化上下文失败", "run_id", run.RunID, "err", merr)
			continue
		}
		if err := s.store.SetStatus(ctx, run.RunID, StatusPaused, string(updated)); err != nil {
			s.logger.Error("启动恢复：置暂停失败", "run_id", run.RunID, "err", err)
			continue
		}
		s.logger.Info("启动恢复：上次进程遗留的运行中流水线已暂停，恢复后将重新执行当前阶段",
			"run_id", run.RunID, "stage", run.CurrentStage)
		emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: run.CurrentStage,
			Status: StatusPaused, Note: "应用重启导致流程中断，已暂停；点击恢复将从当前阶段重新执行"})
	}
	// 自愈（G1）：waiting 的 book-run 若其卷级 scoped sub-run 已全部收口（崩溃发生在
	// 收口与唤醒之间），直接唤醒回 S3 写作并置 S4Audited，避免永久等待或唤醒后重复首检。
	if waiting, werr := s.store.ListByStatus(ctx, StatusWaiting); werr == nil {
		for _, run := range waiting {
			if run.ParentRunID != "" {
				continue
			}
			if active, aerr := s.store.HasActiveScopedChild(ctx, run.RunID); aerr == nil && active {
				continue
			}
			s.wakeBookRun(ctx, run, func(bc *GateContext) { bc.S4Audited = true })
			s.logger.Info("启动恢复：waiting book-run 的卷级子 run 已收口，已唤醒回写作", "run_id", run.RunID)
		}
	}
}

// redispatchInterrupted 对带 interrupted 标记的已暂停运行，在恢复（resume）时按当前阶段
// 重新派发执行体，并清除标记。体检（executeAudit）与修复（executeFix）均为幂等重入设计：
// 它们从 DB 重新加载状态、末尾有「状态未变更」守卫，重复派发不会产生双写。
// redispatchIfNeeded 在恢复（resume）后按需重新派发执行体。执行体是否存活以
// GateContext.Dispatching 为准：标记为真说明 goroutine 仍在运行（软暂停，会在
// 批次/阶段边界感知状态变化并继续），无需派发；否则按当前阶段重新派发
// （体检/修复均为幂等重入设计）。应用重启遗留的中断标记（Interrupted）在此一并清除。
func (s *Service) redispatchIfNeeded(ctx context.Context, run *PipelineRun) {
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	if ctxt.Dispatching {
		s.logger.Info("恢复：执行体仍在运行（软暂停），等待其在边界继续", "run_id", run.RunID, "stage", run.CurrentStage)
		return
	}
	ctxt.Interrupted = false
	if updated, merr := json.Marshal(ctxt); merr == nil {
		if err := s.store.SetStatus(ctx, run.RunID, StatusRunning, string(updated)); err != nil {
			s.logger.Warn("恢复：清除中断标记失败（非阻断）", "run_id", run.RunID, "err", err)
		}
	}
	switch run.CurrentStage {
	case StageS4, StageS4b:
		s.logger.Info("恢复：重新派发签约体检", "run_id", run.RunID, "stage", run.CurrentStage)
		go s.executeAudit(run.RunID, run.NovelID)
	case StageS5:
		s.logger.Info("恢复：重新派发 S5 修复（从剩余问题继续）", "run_id", run.RunID)
		go s.executeFix(run.RunID, run.NovelID)
	case StageS6:
		s.logger.Info("恢复：重新派发完本终检", "run_id", run.RunID)
		go s.executeFinalAudit(run.RunID, run.NovelID)
	case StageS2:
		if ctxt.AuditKind == "blueprint" {
			s.logger.Info("恢复：重新派发蓝图体检", "run_id", run.RunID)
			go s.executeAudit(run.RunID, run.NovelID)
		}
	default:
		// S0/S1/S2(生成)/S3：由用户交互或既有入口驱动（写作/蓝图生成由前端操作触发），无需自动派发。
		s.logger.Info("恢复：当前阶段由交互驱动，不自动派发执行体", "run_id", run.RunID, "stage", run.CurrentStage)
		// 显式告知用户：恢复成功但本阶段不会自动执行，避免界面停留在
		// 「正在启动 agent」的假象（S2 蓝图生成/S3 写作需在对应面板交互推进）。
		emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: run.CurrentStage,
			Status: StatusRunning,
			Note:   "已恢复：当前阶段由你在界面中交互推进（如蓝图对话/逐章写作），流水线不会自动执行"})
	}
}

// 可替换的事件发射器，便于测试注入无操作实现。
var (
	emitStage             = EmitStage
	emitGate              = EmitGate
	emitControlAck        = EmitControlAck
	emitStockAudit        = EmitStockAudit
	emitConsistencyPatrol = EmitConsistencyPatrol
	emitFixStart          = EmitFixStart
	emitFixDone           = EmitFixDone
	emitRewriteBacklog    = EmitRewriteBacklog
	emitRewriteProgress   = EmitRewriteProgress
)

// TimeNow 暴露 time.Now，便于测试时统一；服务内部不依赖外部时钟。
func TimeNow() time.Time { return time.Now() }
