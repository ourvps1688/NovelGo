package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
)

// Orchestrator 是流水线唯一的阶段流转源（A 期状态机）。
//
// 设计动机：PR1–PR4 阶段流转逻辑散落在两处——`service.go` 的 `ResumePipeline`
// 硬编码了 S3 里程碑 / S4 / S5 分支，`orchestrator_c.go` 的 `NextStage` 只兜底
// S1→S2 线性路径。PR5 把两者收敛到 `Orchestrator.Advance`，`service.go` 的
// `ResumePipeline` 退化为薄壳（仅参数校验 + 记录决策 + 委托 Advance），彻底消除双源。
//
// 状态机是确定性的：每个阶段的"下一步"只取决于当前阶段 + 用户决策 + 少量可替换的
// 计数函数（章数 / 卷边界 / 台账），不依赖 LLM 记忆步骤。后台的签约体检 / 修复 /
// 蓝图仍走异步 `skillRunner`（见 service.go 的 enterAudit/enterFix/enterFinalAudit），
// 但"何时触发、触发后落哪个闸、闸后去哪"全部在此集中裁决。
type Orchestrator struct {
	svc *Service
}

// NewOrchestrator 构造编排器，持有 *Service 以调用其异步执行原语与仓储。
func NewOrchestrator(svc *Service) *Orchestrator {
	return &Orchestrator{svc: svc}
}

// Advance 按当前阶段与决策推进流水线，是唯一的"下一步去哪"裁决点。
//   - decision=abort：终止流水线。
//   - 安全检查点：运行中若已被暂停/中止，挂起不推进（供 S5 每轮前 / S3 章间）。
func (o *Orchestrator) Advance(ctx context.Context, run *PipelineRun, decision string) error {
	if run.Status == StatusFinished || run.Status == StatusAborted {
		return fmt.Errorf("流水线已 %s，无法推进", run.Status)
	}
	if decision == "abort" {
		return o.svc.abortRun(ctx, run)
	}
	// 安全检查点：若运行中已被暂停/中止，挂起不推进。
	if o.svc.pausedOrAborted(ctx, run) {
		return nil
	}
	// 全量对齐：后台异步执行（建卷+statesync+全本体检），不改变当前阶段。
	if decision == "align_full" {
		o.svc.dispatchExecutor(taskKindFullAlign, run.RunID, run.NovelID, func() { go o.svc.runFullAlign(ctx, run) })
		return nil
	}

	switch run.CurrentStage {
	case StageS0:
		return o.advanceS0(ctx, run, decision)
	case StageS1:
		return o.advanceS1(ctx, run, decision)
	case StageS2:
		return o.advanceS2(ctx, run, decision)
	case StageS3:
		return o.advanceS3(ctx, run, decision)
	case StageS4, StageS4b:
		return o.advanceS4(ctx, run, decision)
	case StageS5:
		return o.advanceS5(ctx, run, decision)
	case StageS6:
		return o.advanceS6(ctx, run, decision)
	default:
		return fmt.Errorf("未知阶段 %q", run.CurrentStage)
	}
}

// advanceS0 处理导入/续写对齐入口：把存量书汇入公共循环 S3 逐章写作，并异步触发最小对齐
// （建默认卷 + 批量 statesync 回填台账）。对齐在后台进行，用户可立即续写，完成后置 ImportAligned=true。
func (o *Orchestrator) advanceS0(ctx context.Context, run *PipelineRun, _ string) error {
	if err := o.svc.setS3Running(ctx, run); err != nil {
		return err
	}
	o.svc.dispatchExecutor(taskKindS0Align, run.RunID, run.NovelID, func() { go o.svc.executeS0Align(ctx, run) })
	return nil
}

// advanceS1 孵化完成 → 进入 S2 并异步执行蓝图，最终落 G1 闸门（awaiting_gate）。
func (o *Orchestrator) advanceS1(ctx context.Context, run *PipelineRun, _ string) error {
	if err := o.svc.store.SetStageStatus(ctx, run.RunID, StageS2, StatusRunning, run.ContextJSON); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS2, Status: StatusRunning})
	o.svc.dispatchExecutor(taskKindBlueprint, run.RunID, run.NovelID, func() { go o.svc.executeBlueprint(run.RunID, run.NovelID) })
	return nil
}

// advanceS2 蓝图宿主阶段：统一处理蓝图「生成完成 → 体检 → 修复」的闸门决策。
//   - fix → 进入 S5 蓝图修复（蓝图修复技能由 ctxt.AuditKind 驱动，复用 S5 执行体）
//   - continue → 进入 S3 写作（从 G1 通过 或 G1b 接受已知问题，均进入写作）
//   - modify → 重新执行蓝图生成 + 复体检
//   - abort 由 Advance 顶部统一处理，不在此分支
func (o *Orchestrator) advanceS2(ctx context.Context, run *PipelineRun, decision string) error {
	switch decision {
	case "fix":
		o.svc.enterFix(ctx, run)
		return nil
	case "continue":
		return o.svc.setS3Running(ctx, run)
	case "modify":
		// 重新生成蓝图：回到 S2 running 并异步重新执行蓝图技能 + 复体检。
		if err := o.svc.store.SetStageStatus(ctx, run.RunID, StageS2, StatusRunning, run.ContextJSON); err != nil {
			return err
		}
		emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS2, Status: StatusRunning})
		o.svc.dispatchExecutor(taskKindBlueprint, run.RunID, run.NovelID, func() { go o.svc.executeBlueprint(run.RunID, run.NovelID) })
		return nil
	default:
		// 未知决策回落到写作，避免卡死。
		return o.svc.setS3Running(ctx, run)
	}
}

// advanceS3 逐章写作阶段：处理完本终检入口、黄金三章首检与非阻塞进度提示。
//
// R5-4 G3b 弹闸退役（WBS 契约 0.2）：里程碑与普通逐章节奏不再落 awaiting_gate、
// 不发 G3b 闸门事件，改非阻塞 stage note——S3 写作循环持续 running 不被检测打断，
// 是否复检由用户经「进入体检/复检」显式触发（audit_now 分支保留，自动检测节奏红线不动）。
func (o *Orchestrator) advanceS3(ctx context.Context, run *PipelineRun, decision string) error {
	// 用户主动发起完本终检。
	if decision == "finish_book" {
		o.svc.enterFinalAudit(ctx, run)
		return nil
	}
	// 用户显式选择进入体检（复检入口），直接进体检（卷边界复检走 S4b）。
	// 触发原因按成因区分：刚跨入新卷 → volume（卷边界复评，收口即 G_freeze 冻结前一卷）；
	// 每 5 章里程碑 → milestone（中途复检，不冻结）。
	if decision == "audit_now" {
		reason := "milestone"
		if o.svc.crossedVolumeBoundary(ctx, run.NovelID) {
			reason = "volume"
		}
		o.svc.enterAudit(ctx, run, reason)
		return nil
	}
	// 闸门响应态：旧版 G3b 弹闸遗留的 awaiting_gate run（升级兼容），继续即回写作循环。
	if run.Status == StatusAwaitingGate {
		return o.svc.setS3Running(ctx, run)
	}
	// 首检：写完黄金三章且尚未做过签约体检 → 自动进入 S4 签约体检。
	if o.svc.reachedGoldenThree(ctx, run.NovelID) && !o.svc.auditPassed(ctx, run.RunID) {
		o.svc.enterAudit(ctx, run, "golden3")
		return nil
	}
	// 进度提示（非阻塞）：里程碑/卷边界给复检建议 note，普通节奏给「已完成第 N 章」note；
	// 均不落闸、不发闸门事件，S3 持续 running（写作循环改纯按钮节奏）。
	n, _ := o.svc.chapterCount(ctx, run.NovelID)
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	sinceLast := n - ctxt.LastAuditChapter
	crossed := o.svc.crossedVolumeBoundary(ctx, run.NovelID)
	note := fmt.Sprintf("已完成第 %d 章。继续写作下一章；累计写满 5 章或进入新卷时可进入体检复检。", n)
	if n >= 3 && (sinceLast >= 5 || crossed) {
		note = milestoneSummary(n, sinceLast, crossed)
	}
	if err := o.svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, run.ContextJSON); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS3, Status: StatusRunning, Note: note})
	return nil
}

// advanceS4 签约体检阶段（S4 首检 / S4b 卷边界复评）：
//   - fix → 进入 S5 修复闭环；
//   - continue → 标记已体检，回到 S3 继续写作。
//
// 注：S6 完本终检的复评走 advanceS6，不在此处理（S6 的 continue 不存在，改用 finish）。
func (o *Orchestrator) advanceS4(ctx context.Context, run *PipelineRun, decision string) error {
	if decision == "fix" {
		o.svc.enterFix(ctx, run)
		return nil
	}
	// continue（通过或强制继续）：标记已体检，回到 S3 写作（不再自动弹体检）。
	n, _ := o.svc.chapterCount(ctx, run.NovelID)
	updated := o.svc.markAuditedJSON(run.ContextJSON, n)
	// 卷级 scoped sub-run 收口（G1）：体检结论回写父 book-run 并唤醒回 S3 写作。
	if run.ParentRunID != "" {
		return o.svc.finishVolumeScopedRun(ctx, run, updated, n)
	}
	if err := o.svc.store.SetStageStatus(ctx, run.RunID, StageS3, StatusRunning, updated); err != nil {
		return err
	}
	emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS3, Status: StatusRunning})
	return nil
}

// advanceS5 修复闭环：由异步循环（executeFix→executeAudit）自动回到 S4 复评；
// 此处仅作安全兜底，按上下文记录的体检阶段回到同阶段（S4b 卷边界复评 / S4 首检 / S6 终检）。
// 复评原因优先沿用 ctxt.AuditReason（触发原因在 enterAudit 时记录，S5 回路全程保留），
// 避免把 milestone 复检误标成 volume（卷边界）而误触 G_freeze；旧数据无记录时才按阶段推导。
func (o *Orchestrator) advanceS5(ctx context.Context, run *PipelineRun, _ string) error {
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	reason := ctxt.AuditReason
	if reason == "" {
		reason = "milestone"
		switch ctxt.AuditStage {
		case StageS6:
			reason = "final"
		}
	}
	o.svc.enterAudit(ctx, run, reason)
	return nil
}

// advanceS6 完本终检阶段（G终闸）：
//   - finish → 置 Status=finished，完本收口；
//   - fix → 进入 S5 修复闭环，修完复评通过再回 S6；
//   - abort 已在 Advance 顶部统一处理。
func (o *Orchestrator) advanceS6(ctx context.Context, run *PipelineRun, decision string) error {
	switch decision {
	case "finish":
		if err := o.svc.store.SetStatus(ctx, run.RunID, StatusFinished, run.ContextJSON); err != nil {
			return err
		}
		emitStage(ctx, StagePayload{RunID: run.RunID, NovelID: run.NovelID, Stage: StageS6, Status: StatusFinished})
		// G_freeze 完本收尾（WBS 2.5）：末卷（含单卷书）没有「跨入新卷」时刻，
		// 在完本收口统一冻结本书尚未冻结的卷并提交快照。
		o.svc.freezeRemainingVolumes(ctx, run.NovelID, run.RunID)
		return nil
	case "fix":
		o.svc.enterFix(ctx, run)
		return nil
	default:
		return fmt.Errorf("终检阶段不支持决策 %q", decision)
	}
}
