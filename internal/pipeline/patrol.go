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
// 触发（WBS 建议「卷末 + 手动」而非实时，控成本）：
//   - 卷末自动：G_freeze 自动冻结成功（freezeVolumeWithNote）后跑一次；
//   - 手动：控制指令 "patrol"（ControlPipeline）。

// ── 分块 Map-Reduce 巡检配置（混合方案核心：单调用体量从「整本」降到「一块/一次归并」）──
//
// 旧设计把整本书（上限 20 万字符语料）一次性塞进一个 LLM 调用做全体分析，受限于单次调用延迟上限、
// 没有任何分解/续跑/部分沉淀机制，供应商稍慢即超时整轮作废。新设计：按连续章节切片，每块一次小调用
// （Map 步）只分析本切片并抽取跨引用锚点；最后一次性小调用（Reduce 步）汇总各分块结果做全书归并。
// 两步复用同一只读技能 consistency-patrol，仅由提示词区分「分块分析 / 归并」角色。每块小、快、可独立重试；
// 任一块失败不丢全局；归并失败但有分块结果时落库部分张力（verdict=needs_fix），杜绝「整轮高质量结论被丢弃」。

const (
	// patrolWindowSize 分块大小：每次分块调用分析的连续章节数（buildWindowCorpus 另有 patrolWindowCap 截断保护）。
	patrolWindowSize = 5
	// patrolChunkTimeout 单个分块（Map 步）LLM 调用的硬超时：小体量调用应远小于此；超时即标该块失败并继续其余分块。
	patrolChunkTimeout = 12 * time.Minute
	// patrolReduceTimeout 归并（Reduce 步）调用的硬超时。
	patrolReduceTimeout = 12 * time.Minute
	// patrolOverallTimeout 单次巡检整体预算：约束所有分块 + 归并的总耗时上限，避免无限拖延（结构上替代旧 patrolHardTimeout）。
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
// runConsistencyPatrol 执行一次全书一致性巡检（张力报告）：分块 Map-Reduce 编排。
//
// 旧设计把整本书（上限 20 万字符语料）一次性塞进一个 LLM 调用做全体分析，受限于单次调用延迟上限、
// 无分解/续跑/部分沉淀机制，供应商稍慢即超时整轮作废（verdict=error、issues=[]，高质量中间结论被丢弃）。
//
// 新设计（混合方案核心）：
//  1. MAP：按 patrolWindowSize 把章节切成连续切片，每块一次小调用（复用 consistency-patrol 技能，
//     提示词标「分块分析」）只分析本切片并抽取跨引用锚点；每块独立超时、互不牵连，失败仅记日志并继续。
//  2. REDUCE：最后一次性小调用（同样 consistency-patrol 技能，提示词标「归并」）汇总各分块结果做全书归并，
//     产出最终张力报告。
//  3. 部分沉淀：归并失败但分块已有结果时，落库部分张力报告（verdict=needs_fix），
//     杜绝「整轮高质量结论被丢弃」。
//
// 只报告不改写：结果绝不进 Audit/FixStates 等修复路由字段；失败只记日志。
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
	// 整体预算：约束所有分块 + 归并的总耗时上限（结构上替代旧单次 20min 硬超时）。
	runCtx, runCancel := context.WithTimeout(bg, patrolOverallTimeout)
	defer runCancel()

	blocks := s.buildBlocks(bg, novelID, contextasm.PurposePatrol)
	ledgers := s.dumpLedgers(bg, novelID)

	// 列章：失败仅记日志降级（退回归并步直接分析整本语料），绝不阻断巡检——
	// 沿用 buildPatrolCorpus 的容错契约（列章失败→空章节，不硬失败）。
	chapters, err := s.chapterStore.ListAllByNovel(bg, nil, novelID)
	if err != nil {
		s.logger.Warn("一致性巡检：列章失败，降级为整本单调用", "novel_id", novelID, "err", err)
		chapters = nil
	}
	total := len(chapters)
	emitPatrolProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "phase": "analyzing", "total": total, "done": 0,
	})

	// 向量预筛（Phase 2）：聚类跨章候选矛盾点，喂给归并步重点核对；失败/未就绪仅降级，不阻断巡检。
	prescreen := s.runPatrolPrescreen(runCtx, novelID, chapters)

	// ── MAP：逐切片小调用，每块独立超时、互不牵连 ──
	var allIssues []AuditIssue
	var allRefs []CrossRef
	windows := (total + patrolWindowSize - 1) / patrolWindowSize
	for wi := 0; wi < total; wi += patrolWindowSize {
		end := wi + patrolWindowSize
		if end > total {
			end = total
		}
		idx := wi/patrolWindowSize + 1
		window := chapters[wi:end]
		emitPatrolProgress(ctx, map[string]any{
			"novel_id": novelID, "run_id": runID, "phase": "analyzing", "total": total, "done": wi,
		})
		chunkCtx, chunkCancel := context.WithTimeout(runCtx, patrolChunkTimeout)
		windowCorpus := s.buildWindowCorpus(chunkCtx, novelID, runID, window)
		out, derr := s.dispatchSkill(chunkCtx, "consistency-patrol", SkillInput{
			NovelID:   novelID,
			Prompt:    chunkPrompt(novelID, idx, windows, windowCorpus) + blocks.Render(contextasm.PurposePatrol),
			MaxTurns:  6,
			SessionID: fmt.Sprintf("pipeline:patrol:%d:chunk:%d", novelID, idx),
			RunID:     runID,
		})
		chunkCancel()
		if derr != nil {
			s.logger.Warn("一致性巡检·分块失败（继续其余分块）", "window", idx, "err", derr)
			continue
		}
		if refused, reason := isLLMRefusal(out.Text); refused {
			s.logger.Warn("一致性巡检·分块被模型拒绝（跳过该块）", "window", idx, "reason", reason)
			continue
		}
		data, refs, perr := parseChunkOutput(out.Text)
		if perr != nil {
			s.logger.Warn("一致性巡检·分块解析失败（跳过该块）", "window", idx, "err", perr)
			continue
		}
		allIssues = append(allIssues, data.Issues...)
		allRefs = append(allRefs, refs...)
	}
	emitPatrolProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "phase": "synthesizing", "total": total, "done": total,
	})

	// ── REDUCE：汇总各分块结果做全书归并 ──
	redCtx, redCancel := context.WithTimeout(runCtx, patrolReduceTimeout)
	reducePrompt := synthesizePrompt(novelID, allIssues, allRefs) + formatPrescreen(prescreen)
	if len(chapters) == 0 {
		// 无分块语料（列章失败或本书暂无章节）：把整本语料注入归并步，使其退化为「整本单次分析」，
		// 保证仍有内容可读、结果与旧单调用等价（只报告不改写）。buildPatrolCorpus 已含台账快照。
		reducePrompt += "\n### 本书语料（退回整本注入）\n" + s.buildPatrolCorpus(redCtx, novelID, runID)
	} else {
		reducePrompt += "\n### 台账快照（角色/关系/地点/时间线/弧线/读者视角/不变量/任务卡）\n" + ledgers
	}
	reducePrompt += blocks.Render(contextasm.PurposePatrol)
	out, rerr := s.dispatchSkill(redCtx, "consistency-patrol", SkillInput{
		NovelID:   novelID,
		Prompt:    reducePrompt,
		MaxTurns:  8,
		SessionID: fmt.Sprintf("pipeline:patrol:%d:reduce", novelID),
		RunID:     runID,
	})
	redCancel()

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
			emitConsistencyPatrol(ctx, map[string]any{"novel_id": novelID, "run_id": runID})
			return
		} else {
			rerr = fmt.Errorf("归并报告写回失败: %w", werr)
		}
	}

	// 归并失败但有分块结果：落库部分张力报告（verdict=needs_fix），避免整轮高质量结论被丢弃。
	if len(allIssues) > 0 {
		why := "归并步异常"
		if rerr != nil {
			why = firstN(rerr.Error(), 80)
		}
		partial := AuditData{
			Verdict:       "needs_fix",
			Summary:       fmt.Sprintf("一致性巡检部分结果：归并步未完成（%s），以下为各分块已检出的 %d 处张力（尚未做全书归并去重，可能含重复/误报）。建议稍后重跑以生成完整报告。", why, len(allIssues)),
			SchemaVersion: AuditSchemaVersion,
			Issues:        allIssues,
		}
		if serr := s.storePatrolReport(bg, runID, partial); serr == nil {
			emitConsistencyPatrol(ctx, map[string]any{"novel_id": novelID, "run_id": runID})
			return
		}
	}

	// 全无产出：落地清晰失败态（沿用既有分类文案）。
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

// ── 分块 Map-Reduce 辅助：提示词构造与分块输出解析 ──

// CrossRef 是分块分析（Map 步）抽取的「待全书尺度核对」事实锚点（consistency-patrol 分块提示词产出）。
type CrossRef struct {
	Kind       string  `json:"kind"`        // timeline | number | entity | claim | name
	Text       string  `json:"text"`        // 原样摘录该事实所在的一句（含章内定位）
	ChapterIDs []int64 `json:"chapter_ids"` // 涉及的章节全局阅读序号
}

// chunkPrompt 构造分块（Map 步）的任务提示词：只分析传入切片、产出 intra_issues + cross_refs。
func chunkPrompt(novelID int64, idx, total int, windowCorpus string) string {
	return fmt.Sprintf("本书 novel_id=%d。你正在执行一致性巡检的分块分析（第 %d/%d 块）。请**只分析下方【本书语料·切片】中的这几章**，不要假设能看到全书其他章节。\n"+
		"产出两件事：①本切片内的张力（intra_issues，沿用全书巡检的张力契约：target_scope/dimension/direction/fix_type + 具体证据）；②需要全书尺度核对的跨引用事实锚点（cross_refs：本切片里任何后续章节可能冲突/呼应的数值、时间/时长、年龄、名字/称呼、实体关系、承诺/伏笔、设定常量；每条给 kind、原样文本、涉及章节全局阅读序号 chapter_ids）。\n"+
		"只报告不改写。末尾**必须**附 ```json 代码块：{\"verdict\":\"pass|needs_fix\",\"summary\":\"...\",\"schema_version\":2,\"issues\":[...],\"cross_refs\":[{\"kind\":\"timeline|number|entity|claim|name\",\"text\":\"...\",\"chapter_ids\":[...]}]}。",
		novelID, idx, total) + windowCorpus
}

// synthesizePrompt 构造归并（Reduce 步）的任务提示词：把各分块结果汇总为 JSON 锚点块，交 LLM 全书归并。
// 必须显式要求末尾附审计 JSON（与 patrolPrompt 同款契约），否则 parseFreshAudit 抽不到结构化结果。
func synthesizePrompt(novelID int64, issues []AuditIssue, refs []CrossRef) string {
	agg := struct {
		Issues    []AuditIssue `json:"issues"`
		CrossRefs []CrossRef   `json:"cross_refs"`
	}{Issues: issues, CrossRefs: refs}
	raw, _ := json.Marshal(agg)
	return fmt.Sprintf("本书 novel_id=%d。你正在执行一致性巡检的归并（Reduce 步）。下方【分块结果汇总】是各分块已检出的张力与待全局核对的跨引用锚点，请在全书尺度上对照台账与既定事实做归并，产出最终张力报告（只报告不改写，verdict 只是给作者的参考信号）。\n"+
		"【分块结果汇总】\n```json\n%s\n```\n", novelID, string(raw)) +
		"最终回复末尾**必须**附一个 ```json 代码块，格式：" + auditFormatLine(true, issueSamplePatrol) + "——" + issueFieldRules()
}

// parseChunkOutput 解析分块（Map 步）输出：复用 extractAuditJSON 抽取 JSON 块，拆出本切片张力
// issues 与跨引用锚点 cross_refs。解析失败返回 error（调用方跳过该块，不阻断整体）。
func parseChunkOutput(text string) (AuditData, []CrossRef, error) {
	raw := extractAuditJSON(text)
	if raw == "" {
		return AuditData{}, nil, fmt.Errorf("分块输出未含合法 JSON")
	}
	var cr struct {
		AuditData
		CrossRefs []CrossRef `json:"cross_refs"`
	}
	if err := json.Unmarshal([]byte(raw), &cr); err != nil {
		return AuditData{}, nil, fmt.Errorf("分块 JSON 解析失败: %w", err)
	}
	if cr.Issues == nil {
		cr.Issues = []AuditIssue{}
	}
	if cr.CrossRefs == nil {
		cr.CrossRefs = []CrossRef{}
	}
	if cr.SchemaVersion == 0 {
		cr.SchemaVersion = AuditSchemaVersion
	}
	return cr.AuditData, cr.CrossRefs, nil
}
