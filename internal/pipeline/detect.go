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
