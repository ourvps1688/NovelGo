package pipeline

// detect.go — R5-3「检测任务独立入口」的检测-only 执行核心
// （pipeline-core-redesign-proposal.md §10.4/§10.5b，WBS r5-3 §0.1/§1 T1.1）。
//
// 定位：把「体检本体」从流水线裁决中剥离为可复用核心——跑体检 skill → 解析结构化
// 结果 → 合并确定性文风漂移（V 系）→ 落工单/报告。核心零 auto-advance、零修复派发、
// 零闸门广播（检测-only 语义）：
//   - 流水线内的裁决（auto_run 自动推进 / awaiting_gate + G4·G终广播）由
//     executeAudit / executeFinalAudit 调用本核心后承接，行为零变化（既有 pipeline
//     测试零回退）；
//   - 检测任务（RunDetectionTask，T1.2）直接消费核心结果，按 scope 落正确结果容器
//     后收口——「只体检第 X 卷 / 只查结构」皆走本核心，与流水线阶段解耦。
//
// 提示词与 issue 契约不在此层拼装：signabilityPrompt / finalAuditPrompt /
// blueprintAuditPrompt 与 auditFormatLine 契约片段照旧单源（service.go / contract.go），
// 本核心只负责「跑 → 解析 → 合并 → 落」——与 executeAudit 共享同一提示词/契约单源
// （§10.5b「S4 体检本体→检测任务，契约/三层输入/goldenN 零改动」）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/volume"
)

// detectLanding 是检测结果的落法策略。
type detectLanding int

const (
	// detectLandReportOnly 只写报告字段（GateContext.Audit），不动工单/修复断点/清单——
	// S6 完本终检的现状落法（流水线行为零变化，勿改；其发现项经 S5 路由沉淀）。
	detectLandReportOnly detectLanding = iota
	// detectLandNewRound 按「新一轮问题清单」落法：issue 工单整批替换（ReplaceIssues，
	// R5-1「新体检=新问题清单」）+ 修复断点/日志/人工工单清零 + RewriteBacklog/
	// RetconBacklog 重算——S4/S4b/蓝图体检与检测任务共用。
	detectLandNewRound
)

// detectRequest 描述一次检测-only 体检的执行参数。
type detectRequest struct {
	runID     string
	novelID   int64
	skill     string // 体检技能（signability-audit / blueprint-audit）
	prompt    string // 任务提示词（提示词单源装配，本层不拼装）
	sessionID string // 派发会话 ID（多轮会话落库）
	maxTurns  int    // agent loop 轮数上限
	runRef    string // SkillInput.RunID（R0 观测归属；空=非 run 任务）
	volumeID  *int64 // 文风漂移作用域（nil=全书；蓝图体检不参与漂移检测）
	kind      string // AuditKind（"blueprint" → 蓝图落法；其余 → 签约落法）
	landing   detectLanding
	label     string // 日志前缀（S4 体检 / S6 终检 / 检测任务），定位用
}

// detectOutcome 是检测-only 核心的产物。
type detectOutcome struct {
	Audit   AuditData   // 结构化体检结果（已合并 V 系漂移）
	Context GateContext // 已写回体检结果的上下文（未持久化，由调用方落库）
	Updated []byte      // 序列化后的 ContextJSON（marshal 失败回退原值）
}

// runDetection 是检测-only 执行核心：跑体检 skill → 解析 → 合并 drift → 落工单/报告。
// ctxt 为调用方已解析的运行上下文，检测结果按 req.landing 写回 *ctxt；返回序列化后的
// 上下文 JSON（origContextJSON 为 marshal 失败时的回退原值，逐字同旧文）。
//
// 派发失败原样返回 err，由调用方按自身失败通道落地（S4/S6 各自落 paused 有各自文案；
// 检测任务走静默降级）；核心自身零 auto-advance/fix/gate、零阶段状态写入——跑完即返回，
// run 状态不被推进（WBS T1.1 验收①）。
func (s *Service) runDetection(ctx context.Context, req detectRequest, ctxt *GateContext, origContextJSON string) (detectOutcome, error) {
	out, err := s.dispatchSkill(ctx, req.skill, SkillInput{
		NovelID:   req.novelID,
		Prompt:    req.prompt,
		MaxTurns:  req.maxTurns,
		SessionID: req.sessionID,
		RunID:     req.runRef,
	})
	if err != nil {
		return detectOutcome{}, err
	}
	audit := parseFreshAudit(out.Text)
	// P2 文风漂移（WBS p2 §0.3）：确定性检测的 V 系 issue 追加到清单尾部（warning 级，
	// 经 bodyIssuesOf/retconItemsOf 自然收口进既有修复/重写路由）。verdict 不翻转——
	// drift-only 保持 LLM verdict，不触发收口修复循环；蓝图体检不参与文风检测。
	if req.kind != "blueprint" {
		audit.Issues = mergeDriftIssues(audit.Issues, s.runStyleDriftCheck(ctx, req.novelID, req.volumeID))
	}
	auditJSON, _ := json.Marshal(audit)

	// 按 AuditKind 把体检结果写回对应字段（蓝图 → BlueprintAudit，签约 → Audit）。
	// 签约类体检同步重算「回流 S3 重写回路」的正文级问题清单（每次体检反映最新结果）；
	// 蓝图体检不产生正文级问题，置空避免污染。
	if req.kind == "blueprint" {
		ctxt.BlueprintAudit = string(auditJSON)
		if req.landing == detectLandNewRound {
			ctxt.RewriteBacklog = nil
			ctxt.RetconBacklog = nil
		}
	} else {
		ctxt.Audit = string(auditJSON)
		if req.landing == detectLandNewRound {
			ctxt.RewriteBacklog = bodyIssuesOf(audit)
			ctxt.RetconBacklog = retconItemsOf(audit)
		}
	}
	if req.landing == detectLandNewRound {
		// 新一轮体检结果落地即视为全新问题清单：清除上一轮的修复断点（FixStates）。
		// 否则复评产出的 issue 会与旧断点 ID 撞车（LLM 惯性编号 I1..In），S5 路由会把
		// 全部问题误判为「已修复」而静默跳过工单沉淀——表现为「检测完毕」却零发现项。
		// issue 工单表（R1）同语义整批替换：新体检=新问题清单，跨轮同号不共用断点。
		if terr := s.store.ReplaceIssues(ctx, req.novelID, req.runID, ctxt.FixRounds, audit.Issues); terr != nil {
			s.logger.Warn(req.label+"：issue 工单落表失败（非阻断）", "run_id", req.runID, "err", terr)
		} else {
			// §9.4 命中率度量：L2 告警产生（曝光口径，见 alert_metrics.go 单源注释）。
			s.recordFeatureUsageN(ctx, featureAlertRaisedL2, len(audit.Issues))
			// R5-1「RewriteBacklog 读写改走工单」：落表成功后重写清单以工单表投影为准
			// （工单面板驳回/复验收口即时反映到清单）；bodyIssuesOf 内存清单降级为
			// 落表失败时的回退（上方赋值保留）。
			if req.kind != "blueprint" {
				ctxt.RewriteBacklog = s.rewriteBacklogOf(ctx, req.novelID, req.runID, ctxt.RewriteBacklog)
			}
		}
		// 修复日志（R1b）同轮语义：随断点一并重置，新清单=新修复日志。
		// 转人工工单（R1c）随断点一并重置：新一轮体检=新一轮问题清单。
		ctxt.FixStates = nil
		ctxt.FixLog = nil
		ctxt.ManualBacklog = nil
	}
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		s.logger.Warn(req.label+"：序列化上下文失败", "err", merr)
		updated = []byte(origContextJSON)
	}
	return detectOutcome{Audit: audit, Context: *ctxt, Updated: updated}, nil
}

// ── 统一检测任务入口 RunDetectionTask（R5-3 T1.2，WBS §0.1/§0.2）────────────
//
// 检测任务 = 「scoped 检测 run + 队列派发执行体」：卷/书/结构检测建/复用一个检测
// 容器 run（ModeDetect，出生即终态）作结果容器（工单 keyed by runID 依赖它），
// 经 R3 任务队列派发检测-only 执行体；章节检测直接走注入的 chapterCIRunner
// （RunChapterCIForChapter，写质量档案，无 run 依赖）。「独立性」= 任意时刻可
// 单独启动、不依赖流水线阶段（无活跃流水线也能跑），而非脱离 run 存储。

// 检测任务 kind（scope → 执行体/结果容器映射见 WBS §0.2 表）。
const (
	DetectionKindChapter   = "chapter"   // 章节检测：chapter-ci → chapter_quality（质量档案）
	DetectionKindVolume    = "volume"    // 卷体检：signability-audit → 发现项工单（卷级检测容器）
	DetectionKindBook      = "book"      // 书体检：signability-audit → 发现项工单（全书检测容器）
	DetectionKindStructure = "structure" // 巡检·结构：consistency-patrol → PatrolReport（张力报告）
)

// detectContainerStage 是检测容器 run 的 CurrentStage 标记（非流水线阶段，
// 只作容器身份标识；容器出生即 StatusFinished，阶段永不推进）。
const detectContainerStage = "detect"

// DetectionTaskResult 是 RunDetectionTask 的启动回执：检测异步执行，回执只反映
// 「是否成功派发」；结果落容器后由检测状态查询/结果面板消费（WBS T1.3）。
type DetectionTaskResult struct {
	Kind    string `json:"kind"`
	NovelID int64  `json:"novel_id"`
	ScopeID *int64 `json:"scope_id,omitempty"`
	// RunID 是结果容器（检测 run）的 RunID：卷/书体检工单 keyed by runID、
	// 巡检张力报告落此 run。章节检测无容器（直写质量档案），为空。
	RunID string `json:"run_id,omitempty"`
	// Dispatched=true 执行体已派发（队列或 legacy 直跑）；false=降级未执行
	// （如未注入章节检测执行器）——降级零提示零报错，回执如实标记。
	Dispatched bool `json:"dispatched"`
}

// RunDetectionTask 统一检测任务入口（契约 0.1/0.2）：按 kind 分流——
//   - chapter → 注入的 chapterCIRunner（RunChapterCIForChapter，L1 检测落质量档案）；
//   - volume/book → ensureDetectRun 建/复用检测容器 + dispatchExecutor 派发
//     检测-only 体检执行体（executeDetectAudit）；
//   - structure → ensureDetectRun + dispatchExecutor 派发 runConsistencyPatrol
//     （复用 taskKindPatrol，张力报告只读落 PatrolReport）。
//
// 队列不可用时 dispatchExecutor 降级 legacy 直跑（逐字一致）；同 kind+run 在排队/
// 执行中时由队列 dedup 合并（幂等重入，验收④）。
func (s *Service) RunDetectionTask(ctx context.Context, novelID int64, kind string, scopeID *int64) (*DetectionTaskResult, error) {
	switch kind {
	case DetectionKindChapter:
		if scopeID == nil || *scopeID <= 0 {
			return nil, fmt.Errorf("检测任务：章节检测需要 chapterID")
		}
		return s.dispatchChapterDetect(novelID, *scopeID), nil
	case DetectionKindVolume:
		if scopeID == nil || *scopeID <= 0 {
			return nil, fmt.Errorf("检测任务：卷体检需要 volumeID")
		}
		// 卷归属校验：防误传 volumeID 落出野容器、跑空检测。
		if v, err := volume.NewStore(s.store.db, s.logger).GetByID(ctx, nil, novelID, *scopeID); err != nil || v == nil {
			return nil, fmt.Errorf("检测任务：卷不存在 volume_id=%d", *scopeID)
		}
		return s.dispatchScopedDetect(ctx, novelID, kind, scopeID)
	case DetectionKindBook:
		return s.dispatchScopedDetect(ctx, novelID, kind, nil)
	case DetectionKindStructure:
		return s.dispatchScopedDetect(ctx, novelID, kind, nil)
	default:
		return nil, fmt.Errorf("检测任务：未知 kind %q", kind)
	}
}

// dispatchChapterDetect 章节检测分流：经注入的 chapterCIRunner 跑 L1 增量检查
// （落质量档案），异步执行不阻塞调用方。未注入执行器时降级跳过（零提示零报错，
// 回执 Dispatched=false 如实标记）。
func (s *Service) dispatchChapterDetect(novelID, chapterID int64) *DetectionTaskResult {
	res := &DetectionTaskResult{Kind: DetectionKindChapter, NovelID: novelID, ScopeID: &chapterID}
	if s.chapterCIRunner == nil {
		s.logger.Warn("检测任务：未注入章节检测执行器，章节检测降级跳过", "novel_id", novelID, "chapter_id", chapterID)
		return res
	}
	runner := s.chapterCIRunner
	go func() {
		if err := runner(s.appCtx, novelID, chapterID); err != nil {
			s.logger.Warn("检测任务：章节检测执行失败（非阻断）", "novel_id", novelID, "chapter_id", chapterID, "err", err)
		}
	}()
	res.Dispatched = true
	return res
}

// dispatchScopedDetect 卷/书/结构检测分流：建/复用检测容器 run 后派发执行体
// （structure 复用 taskKindPatrol，卷/书走 taskKindDetect 检测-only 皮）。
func (s *Service) dispatchScopedDetect(ctx context.Context, novelID int64, kind string, scopeID *int64) (*DetectionTaskResult, error) {
	run, err := s.ensureDetectRun(ctx, novelID, scopeID)
	if err != nil {
		return nil, err
	}
	res := &DetectionTaskResult{Kind: kind, NovelID: novelID, ScopeID: scopeID, RunID: run.RunID, Dispatched: true}
	if kind == DetectionKindStructure {
		s.dispatchExecutor(taskKindPatrol, run.RunID, novelID,
			func() { go s.runConsistencyPatrol(s.appCtx, run.RunID, novelID) })
		return res, nil
	}
	s.dispatchExecutor(taskKindDetect, run.RunID, novelID,
		func() { go s.executeDetectAudit(run.RunID, novelID) })
	return res, nil
}

// ensureDetectRun 建/复用检测容器 run（WBS T1.2「scoped 检测 run」语义）：
// 同 novel+scope 复用最新容器（新检测=同一容器换新一轮结果，工单整批替换语义
// 自然成立）；无则新建（ModeDetect、出生即 StatusFinished）。
func (s *Service) ensureDetectRun(ctx context.Context, novelID int64, volumeID *int64) (*PipelineRun, error) {
	existing, err := s.store.GetDetectRun(ctx, novelID, volumeID)
	if err == nil && existing != nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("检测任务：查询检测容器失败: %w", err)
	}
	run := &PipelineRun{
		NovelID:      novelID,
		RunID:        uuid.NewString(),
		Mode:         ModeDetect,
		CurrentStage: detectContainerStage,
		Status:       StatusFinished,
		VolumeID:     volumeID,
	}
	if cerr := s.store.Create(ctx, run); cerr != nil {
		return nil, fmt.Errorf("检测任务：创建检测容器失败: %w", cerr)
	}
	return run, nil
}

// executeDetectAudit 是检测任务（卷体检/书体检）的执行体（WBS §0.2「executeAudit
// 检测-only 皮」）：signability-audit 提示词装配与 S4 逐字同源（契约/三层输入/
// goldenN 零改动，focused=false 编辑部通读），跑检测-only 核心（detectLandNewRound
// 新一轮清单落法）后把结果落检测容器并广播完成——零 auto-advance、零闸门广播、
// 零阶段推进（检测-only 语义）；失败静默降级（零提示零报错，不改检测口径）。
func (s *Service) executeDetectAudit(runID string, novelID int64) {
	ctx := s.appCtx
	run, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		s.logger.Warn("检测任务：加载检测容器失败（非阻断）", "run_id", runID, "err", err)
		return
	}
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	prompt := signabilityPrompt(novelID, auditInput{
		scope:       s.volumeScopeText(ctx, novelID, run.VolumeID),
		ledger:      s.qualityLedgerBlock(ctx, novelID),
		structBlock: s.structureBlock(ctx, novelID),
		focused:     false,
	}, s.buildBlocks(ctx, novelID, contextasm.PurposeAudit))
	outcome, derr := s.runDetection(ctx, detectRequest{
		runID:     runID,
		novelID:   novelID,
		skill:     "signability-audit",
		prompt:    prompt,
		sessionID: "detect:" + runID,
		maxTurns:  24,
		runRef:    runID,
		volumeID:  run.VolumeID,
		kind:      "",
		landing:   detectLandNewRound,
		label:     "检测任务",
	}, &ctxt, run.ContextJSON)
	if derr != nil {
		s.logger.Warn("检测任务：体检执行失败（非阻断，静默降级）", "run_id", runID, "err", derr)
		return
	}
	// 落库保 stage/status：检测容器出生即终态，结果写回不得改其状态
	// （仿 storePatrolReport；进行态可见走 R0 观测与任务队列）。
	if serr := s.store.SetStageStatus(ctx, runID, run.CurrentStage, run.Status, string(outcome.Updated)); serr != nil {
		s.logger.Warn("检测任务：结果落库失败", "run_id", runID, "err", serr)
	}
	detectKind := DetectionKindVolume
	if run.VolumeID == nil {
		detectKind = DetectionKindBook
	}
	emitDetectDone(ctx, map[string]any{
		"novel_id":    novelID,
		"run_id":      runID,
		"kind":        detectKind,
		"verdict":     outcome.Audit.Verdict,
		"issue_count": len(outcome.Audit.Issues),
	})
}
