package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/llm"
)

// ── 全书一致性巡检（WBS 3.3「张力报告」）────────────────────────────────
//
// 系列长期连载只允许「前向适配」时设定会随卷数慢慢失真（每一处局部修补都合理，
// 合起来渐行渐远）。本巡检是累计漂移的兜底：全书视角对照「已冻结卷既定事实」与
// 当前台账/正文，产出**建议性张力报告**——只报告不改写、绝不进 S5 自动修复路由，
// 改不改由作者定（retcon 候选也只走 3.1 人工审批闸门）。
//
// 触发（WBS 建议「卷末 + 手动」而非实时）：
//   - 卷末自动：G_freeze 自动冻结成功（freezeVolumeWithNote）后跑一次；
//   - 手动：控制指令 "patrol"（ControlPipeline）。

// ── 单次归并巡检配置 ────────────────────────────────────────────────
//
// 历史教训（勿回退）：早期「整本书一次性塞进一个 LLM 调用」与「分块 Map-Reduce（每块一次小调用 +
// 一次归并）」都跑不出来——根因不是算法而是**供应商太慢 / 半死连接**：思考模型单调用要 >12 分钟
// 才吐完，而分块方案把 1 次调用放大成 3~N 次串行慢调用，总时长被供应商延迟复利放大成 35 分钟级，
// 且归并步在 12 分钟预算内吐不完 → 只落残缺报告（verdict=needs_fix 但 summary 写着「建议重跑」），
// 用户体感即「一直检测中 / 卡死」。
//
// 现行方案（单次归并 + 全书语料内联 + 预筛聚焦）：
//  1. 后端批量预取全书语料（章正文 + 台账快照，本地读取秒级）拼成【本书语料】注入提示词，
//     LLM 一次性通读分析——替代「让 agent 逐章 read」（旧设计 50+ 次工具往返、易卡死），这是唯一完整证据源；
//     【2026-11-03 收紧】该供应商对大上下文极慢（思考模型单调用 >45min 仍吐不完），且无法换更快模型，
//     故语料预算收敛为 patrolChapterCap=2000 / patrolLedgerCap=20000 / patrolCorpusCap=120000（上下文约原 40%），
//     并约束模型「输出从简、只交 JSON」，再以 patrolSynthesizeMaxTokens 硬顶输出 token（压思维块膨胀，
//     实测思维块可空转 20 万+字符拖满超时）——在 45min 预算内稳定收口，详见下方常量注释。
//  2. 本地向量预筛（Phase 2，免费、严格限时）把「跨章、语义相近、数值/事实可能分歧」的陈述聚成候选矛盾簇，
//     仅作归并步的聚焦提示（非证据必需，超时/失败仅降级）；
//  3. 仅**一次** consistency-patrol 调用，提示词内联【本书语料】+ 预筛候选簇，明确禁止 read/get_*/search 工具；
//  4. 单次调用给足超时预算（patrolSynthesizeTimeout）；失败走 classifyPatrolFailure 落清晰失败态，绝不静默卡死。

const (
	// patrolSynthesizeTimeout 单次归并（全书核对）调用的硬超时：证据已内联【本书语料】
	// （章正文+台账快照，经 2026-11-03 收紧：patrolChapterCap=2000 / patrolLedgerCap=20000 /
	// patrolCorpusCap=120000，单调用上下文压到约原 40%）+ 预筛候选簇，并约束模型「输出从简、
	// 只交 JSON 不写长篇分析」以削减思维模型冗长输出；45min 预算对收紧后的体量绰绰有余。
	// 超时即清晰失败态（classifyPatrolFailure），绝不静默卡死。
	patrolSynthesizeTimeout = 45 * time.Minute
	// patrolSynthesizeMaxTokens 归并调用的输出 token 硬上限（MiMo 映射 max_completion_tokens=
	// 思考+回答总量）。实测该供应商思考模型的思维块不限时可膨胀到 20 万+字符（≈10 万 token），
	// 以约 50 token/s 的速度把单调用拖满 45min 仍收不了口——语料收紧后思维量仍不按比例缩，
	// 必须用输出上限硬性压思维预算：报告 JSON 仅 2~4k token，48k 上限给足精简思考空间，
	// 同时把最坏生成时长钉在 ~16min。触顶截断时 parseFreshAudit 降级落清晰失败态，绝不静默。
	patrolSynthesizeMaxTokens = 48_000
	// patrolOverallTimeout 单次巡检整体预算（含预筛本地计算 + 单次归并调用），兜底防无限拖延。
	patrolOverallTimeout = 60 * time.Minute
)

// patrolPrompt 构造全书一致性巡检（张力报告）的任务提示词：只读、只报告不改写。
// blocks 尾部装配上下文块（巡检基准线，WBS 2.3；P0 §0.5 patrol=Facts+Voice）。
// issue JSON 契约片段由 contract.go 单源生成（全量契约：张力需标
// target_scope/dimension/direction 供报告视图展示建议方向）。
func patrolPrompt(novelID int64, blocks contextasm.ContextBlocks) string {
	return fmt.Sprintf("本书 novel_id=%d。请基于 consistency-patrol 方法论做一次**全书一致性巡检（张力报告）**："+
		"用 get_chapter_list 列出全部章节，对照「已冻结卷既定事实」与当前台账（角色/关系/地点/时间线/弧线/读者认知）通读全书，"+
		"只报**张力**（前向适配累计出来的漂移）：设定失真、承诺未兑现、读者认知被悄悄改写、时间线漂移、retcon 衔接裂缝；"+
		"每条张力按落点标 target_scope、按四维标 dimension、给建议 direction（默认 forward_fix；仅硬矛盾且无法前向消解才 retcon_candidate）"+
		"与 fix_type（正文↔台账对账冲突**不预设谁对**：可能台账记错该修台账（ledger），也可能正文记错该修正文（body），按证据判方向）；"+
		"每条必须给具体证据，不报无据臆测，不报单章文笔/节奏类质量问题。"+
		"这是**只报告不改写**的兜底巡检：你不改任何数据，verdict 只是给作者的参考信号（有张力 → needs_fix，无 → pass），"+
		"不会触发任何自动修复，改不改由作者定。"+
		"最终回复末尾**必须**附一个 ```json 代码块，格式："+auditFormatLine(true, issueSamplePatrol)+"——"+issueFieldRules(),
		novelID) + blocks.Render(contextasm.PurposePatrol)
}

// runConsistencyPatrol 执行一次全书一致性巡检（张力报告）：跑 consistency-patrol
// 技能 → 解析结构化张力清单 → 写入 context.PatrolReport → 广播
// pipeline:consistency_patrol 事件通知前端刷新巡检报告视图。
//
// 只报告不改写：结果绝不进 Audit/FixStates 等修复路由字段；失败只记日志
// （不落库、不广播、不阻断）——兜底巡检不给流水线添堵。
//
// 编排（单次归并 + 预筛证据内联，见文件头注释）：
//  1. 本地向量预筛 → 跨章候选矛盾簇（免费，失败仅降级）；
//  2. 一次 consistency-patrol 调用，提示词内联「台账快照 + 预筛候选簇」作为全部证据；
//  3. 解析 JSON → 落库报告 + 广播；失败/拒绝 → 清晰失败态，绝不静默卡死。
func (s *Service) runConsistencyPatrol(ctx context.Context, runID string, novelID int64) {
	bg := s.appCtx
	defer s.endDetectByRunID(runID)
	// B5：仅当本巡逻是「巡检·结构」检测任务容器时，套一层带超时+取消的派生 ctx（登记 cancel 供
	// CancelDetectionTask 触发）；流水线侧巡检沿用 appCtx，行为零变化。
	if run, rerr := s.store.GetByRunID(s.appCtx, runID); rerr == nil && run != nil && run.Mode == detectModeOf(DetectionKindStructure) {
		dctx := s.beginDetectCtx(DetectionKindStructure, novelID, nil)
		defer s.endDetectCtx(DetectionKindStructure, novelID, nil)
		bg = dctx
	}
	// 整体预算：约束预筛本地计算 + 单次归并调用的总耗时上限（结构上替代旧单次/分块硬超时）。
	runCtx, runCancel := context.WithTimeout(bg, patrolOverallTimeout)
	defer runCancel()

	blocks := s.buildBlocks(bg, novelID, contextasm.PurposePatrol)

	// 列章：失败仅记日志降级（预筛退化为无候选、语料退化为无章正文，归并步仍用台账证据），绝不阻断巡检。
	chapters, err := s.chapterStore.ListAllByNovel(bg, nil, novelID)
	if err != nil {
		s.logger.Warn("一致性巡检：列章失败，预筛/语料退化为无章正文", "novel_id", novelID, "err", err)
		chapters = nil
	}
	total := len(chapters)
	emitPatrolProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "phase": "prescreen", "total": total, "done": 0,
	})

	// 向量预筛（Phase 2，本地免费）：聚类跨章候选矛盾点，仅作归并步的聚焦提示；失败/未就绪/超时仅降级，不阻断。
	// 关键：预筛须严格限时——EmbedBatch 走本地 ONNX，异常时可能同步阻塞，故套短超时 + goroutine 兜底
	// （见 clusterPrescreenCands），绝不允许预筛卡死拖垮整轮巡检。
	prescreenCtx, prescreenCancel := context.WithTimeout(runCtx, 90*time.Second)
	defer prescreenCancel()
	prescreen := s.runPatrolPrescreen(prescreenCtx, novelID, chapters)
	emitPatrolProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "phase": "reading", "total": total, "done": 0,
	})

	// 全书语料（章正文 + 台账快照）后端批量预取、本地读取秒级完成，拼成【本书语料】注入提示词——
	// 本巡检唯一完整证据源，替代「让 agent 逐章 read」（旧设计 50+ 次工具往返、易卡死）。
	// 语料缺失（无章正文）时归并步仍可用台账快照分析；buildPatrolCorpus 自带容错降级。
	corpus := s.buildPatrolCorpus(runCtx, novelID, runID)
	emitPatrolProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "phase": "synthesizing", "total": total, "done": total,
	})

	// ── 单次归并：提示词内联【本书语料】（全书正文+台账）+ 预筛候选簇 作为全部证据 ──
	synCtx, synCancel := context.WithTimeout(runCtx, patrolSynthesizeTimeout)
	reducePrompt := synthesizePrompt(novelID, prescreen) + corpus + blocks.Render(contextasm.PurposePatrol)
	out, rerr := s.dispatchSkill(synCtx, "consistency-patrol", SkillInput{
		NovelID:   novelID,
		Prompt:    reducePrompt,
		MaxTurns:  6,
		MaxTokens: patrolSynthesizeMaxTokens,
		SessionID: fmt.Sprintf("pipeline:patrol:%d:synthesize", novelID),
		RunID:     runID,
	})
	synCancel()

	if rerr == nil {
		if refused, reason := isLLMRefusal(out.Text); refused {
			// 归并步被模型拒绝（安全审核拦截）：复用既有拒绝失败态处理——回显原始拒绝语，
			// 保留「哪段内容被判高风险」这一唯一可定位线索（原实现「原始返回已忽略」已修复）。
			s.storeRefusalFailure(bg, runID, out.Text, reason)
			emitConsistencyPatrol(ctx, map[string]any{"novel_id": novelID, "run_id": runID})
			return
		}
		data := parseFreshAudit(out.Text)
		if werr := s.storePatrolReport(bg, runID, data); werr == nil {
			// 张力项同步沉淀为发现项工单（复用工单闭环：确认/去修复/复验/驳回）。
			// 按 run 整批替换（ReplaceIssues 契约：新体检=新问题清单）；失败只告警不阻断——
			// 报告本体已落库，工单缺失可由后续体检/回填补齐。失败/拒绝路径不落工单（避免清掉旧清单）。
			if terr := s.store.ReplaceIssues(bg, novelID, runID, 0, data.Issues); terr != nil {
				s.logger.Warn("一致性巡检：张力项落工单失败（非阻断）", "run_id", runID, "err", terr)
			}
			emitConsistencyPatrol(ctx, map[string]any{"novel_id": novelID, "run_id": runID})
			return
		} else {
			rerr = fmt.Errorf("巡检报告写回失败: %w", werr)
		}
	}

	// 单次调用失败：落地清晰失败态（沿用既有分类文案），绝不静默卡死或丢成空报告。
	verdict, summary := s.classifyPatrolFailure(rerr)
	if serr := s.storePatrolReport(bg, runID, AuditData{Verdict: verdict, Summary: summary, Issues: []AuditIssue{}}); serr != nil {
		s.logger.Warn("一致性巡检：写回失败态失败", "err", serr)
	}
	emitConsistencyPatrol(ctx, map[string]any{"novel_id": novelID, "run_id": runID})
}

// storeRefusalFailure 把模型拒绝/安全审核拦截落地为清晰的 verdict=error 失败态，并回显模型/
// 网关的原始拒绝语（这是系统能拿到的、最接近「哪段内容被判高风险」的信号），避免污染界面为
// 假张力报告（verdict=needs_fix 的垃圾项）。只报告不改写：绝不进修复路由。
func (s *Service) storeRefusalFailure(ctx context.Context, runID, text, reason string) {
	detail := refusalExcerpt(text, 800)
	summary := "一致性巡检未能完成：模型拒绝了本次请求（" + reason + "）。\n" +
		"触发来源：发送给模型的章节/台账内容触发了上游模型或网关的安全审核。" +
		"由于巡检会拼入本书内容，系统无法自动定位是哪一章命中，需由你定位。\n"
	if detail != "" {
		summary += "\n原始拒绝语（模型/网关原话，已脱敏截断）：\n" + detail +
			"\n若其中点名了具体章节或内容（如「第 N 章的某描写」），请对该处做局部脱敏/改写后重跑；" +
			"若只是通用拦截（未点名），可换内容审核更宽松的模型/供应商，或分块巡检以隔离命中章节。\n"
	}
	summary += "建议：①更换内容审核更宽松的模型或供应商；②确认当前模型支持长上下文（巡检会拼入本书内容）；" +
		"③如确为内容命中，可先对该处做局部脱敏后再巡检。本次未写入张力清单。"
	if serr := s.storePatrolReport(ctx, runID, AuditData{
		Verdict: "error",
		Summary: summary,
		Issues:  []AuditIssue{},
	}); serr != nil {
		s.logger.Warn("一致性巡检：写回拒绝失败态失败", "err", serr)
	}
}

// classifyPatrolFailure 把巡检失败 error 归约为面向作者的清晰失败态（verdict + summary）。
// 重点区分「供应商无响应 / 流式中断」与「其他错误」：前者是供应商问题（非设计缺陷），
// 给出明确可操作的提示，避免作者误以为巡检本身有 bug；后者沿用通用文案。
func (s *Service) classifyPatrolFailure(err error) (string, string) {
	var apiErr *llm.APIError
	isIdle := errors.As(err, &apiErr) && apiErr != nil && apiErr.Kind == "idle_timeout"
	if errors.Is(err, context.DeadlineExceeded) || isIdle {
		reason := "LLM 供应商在预期时间内未返回完整结果（疑似无响应或流式连接中断）"
		if isIdle {
			reason = "LLM 供应商空闲超时（连接已建立但持续未返回数据，疑似半死连接 / 吐字节后掉线）"
		}
		summary := "一致性巡检未能完成：" + reason + "\n" +
			"说明：这通常是 LLM 供应商问题，不是巡检设计缺陷——单次调用未能在限定时间内跑完，" +
			"可能因为供应商无响应、流式连接中断（吐出部分内容后掉线），或模型对该体量请求处理过慢。\n" +
			"建议：①更换更稳定的模型或供应商；②确认当前模型支持长上下文；③稍后重试。本次未写入张力清单。"
		return "error", summary
	}
	reason := firstN(err.Error(), 200)
	summary := "一致性巡检未能完成：" + reason + "\n" +
		"可能原因：LLM 供应商长时间无响应（连接已建立但未吐出数据）、请求超时，或进程被中断。\n" +
		"建议：稍后重试；若持续失败，可更换模型/供应商，或确认该模型支持本书体量所需的长上下文。"
	return "error", summary
}

// storePatrolReport 把张力报告写入 run 的 ContextJSON.PatrolReport（只读展示用，
// 不进任何修复路由）。仿 storeStockAudit：写前重读 run，保留当前 stage/status。
// 同步打落库时间戳 PatrolReportAt（T1.3）：GetPatrolReport 双源合并按时间戳最新者胜。
func (s *Service) storePatrolReport(ctx context.Context, runID string, data AuditData) error {
	cur, err := s.store.GetByRunID(ctx, runID)
	if err != nil {
		return err
	}
	var ctxt GateContext
	if cur.ContextJSON != "" {
		_ = json.Unmarshal([]byte(cur.ContextJSON), &ctxt)
	}
	reportJSON, _ := json.Marshal(data)
	now := time.Now()
	ctxt.PatrolReport = string(reportJSON)
	ctxt.PatrolReportAt = &now
	updated, merr := json.Marshal(ctxt)
	if merr != nil {
		return merr
	}
	return s.store.SetStageStatus(ctx, cur.RunID, cur.CurrentStage, cur.Status, string(updated))
}

// refusalExcerpt 取拒绝语的用户可读摘要：限长 + 折叠多余空白（含换行）。
// out.Text 是模型/网关对本次请求的回应（不是用户小说正文），按用户诉求保留原话要点，
// 仅防止超长灌入张力报告 DB 字段；返回空串表示无可摘录内容。
func refusalExcerpt(text string, maxRunes int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	collapsed := strings.Join(strings.Fields(text), " ")
	runes := []rune(collapsed)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return collapsed
	}
	return string(runes[:maxRunes]) + "…"
}

// ── 单次归并提示词构造 ──

// synthesizePrompt 构造全书归并（单次 consistency-patrol 调用）的任务提示词：
// 把本地向量预筛得到的跨章候选矛盾簇（formatPrescreen）作为聚焦证据内联进提示词，
// 并显式声明「全部证据已内联、不得调用 read/get_*/search 工具」，避免 agent 多轮工具抓取放大延迟。
// 必须显式要求末尾附审计 JSON（与 patrolPrompt 同款契约），否则 parseFreshAudit 抽不到结构化结果。
func synthesizePrompt(novelID int64, prescreen PrescreenReport) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "本书 novel_id=%d。请基于 consistency-patrol 方法论做一次**全书一致性巡检（张力报告）**："+
		"对照下方【本书语料】（已批量取好的全书正文 + 台账快照）与【预筛候选矛盾点】，把前向适配累计出来的漂移（张力）标出来，供作者裁决。\n", novelID)
	sb.WriteString("**全部证据已内联在下方（【本书语料】含全书正文与台账快照 + 预筛候选簇），你无需、也不得调用任何 read / get_* / search 工具去补读正文**——直接基于内联证据分析并输出报告；调用工具只会拖慢并可能触发超时。\n")
	sb.WriteString("**输出从简（关键，决定能否在预算内跑完）**：末尾的 ```json 代码块是必交产物；JSON 之前最多用 2-3 句话点明总体结论，**不要撰写长篇逐条分析论证**——每条张力在 JSON 的 evidence 字段用一句话点明证据即可，无需展开推理过程。不要重复罗列已在 JSON 中的内容。\n")
	sb.WriteString("张力类型：设定失真 / 承诺未兑现 / 读者认知被悄悄改写 / 时间线漂移 / retcon 衔接裂缝；" +
		"每条必须给具体证据（哪章/哪条既定事实），不报无据臆测、不报单章文笔/节奏类质量问题。\n")
	sb.WriteString("方向建议：每条标 target_scope / dimension / direction（默认 forward_fix；仅硬矛盾且无法前向消解才 retcon_candidate）/ fix_type（正文↔台账对账冲突不预设谁对，按证据判方向）。\n")
	sb.WriteString("这是**只报告不改写**的兜底巡检：你不改任何数据，verdict 只是给作者的参考信号（有张力→needs_fix，无→pass），" +
		"不会触发任何自动修复，改不改由作者定。\n")
	// 预筛候选矛盾簇（无 embedder/无候选时为空串，不影响归并）。
	sb.WriteString(formatPrescreen(prescreen))
	sb.WriteString("\n最终回复末尾**必须**附一个 ```json 代码块，格式：" + auditFormatLine(true, issueSamplePatrol) + "——" + issueFieldRules())
	return sb.String()
}
