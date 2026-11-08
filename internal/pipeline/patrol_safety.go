package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/llm"
)

// ── 安全审核触犯内容定位（#101）────────────────────────────────────────────
//
// 一致性巡检把全书正文 + 台账快照整块拼进一次 LLM 调用；上游模型/网关一旦触发内容审核，
// 旧实现只回显一句通用拒绝语并写明「系统无法自动定位是哪一章命中，需由你定位」——
// 作者面对一本书完全不知道该改哪里。本模块把失败态升级为「可操作的定位结果」：
//
//  1. 语料单元化：patrol_corpus.go 的 buildPatrolCorpusWithUnits 在渲染语料的同时回收
//     最小可定位单元（一章正文 / 一条台账行），且 Content 与真正发出去的文本同源同截断；
//  2. 二分探测：用轻量直连调用（llmClient.GenerateText，非 skill 派发，不占用技能/熔断观测）
//     把单元集合反复对半发送，找出「单独发送即被拦」的片段，给出「哪张台账哪一行」+ 摘录；
//  3. 降级提示：探测不可用时改用本地高精度词面扫描，明确标注「仅提示、不等于确认命中」；
//  4. 诚实边界：探针只发送语料片段，不重放完整提示词/预筛块；组合触发、整体语境触发
//     会明确写进定位说明，绝不伪装成已确认命中。
//
// 全程只读、只诊断，绝不改写正文/台账，也绝不自动触发修复。

const (
	patrolSafetyProbeTimeout  = 60 * time.Second
	patrolSafetyLocateTimeout = 8 * time.Minute
	patrolSafetyMaxProbes     = 64
	patrolSafetyExcerptRunes  = 160 // 单条命中单元在报告中的摘录长度（rune）
	patrolSafetyRenderCap     = 12  // 每类结果最多逐条展示的单元数，超出给汇总
	// control 探针不含任何本书内容：若连它都被拦，说明是网关/账户级风控，定位无意义。
	patrolSafetyControlProbe = "请仅回复 OK。"
)

// patrolProbeState 是单次定位探针的三态结论。unknown 与 clean 必须区分：供应商断连、
// 限流、鉴权失败都不是「该片段安全」，误当 clean 会漏报真正命中的内容。
type patrolProbeState int

const (
	probeClean patrolProbeState = iota
	probeBlocked
	probeUnknown
)

// patrolSafetyProbe 发送一段语料文本并返回上游审核结论。生产实现走轻量直连调用，
// 测试注入桩可据此精确模拟命中/全清/被拦/不可用的各种上游行为。
type patrolSafetyProbe func(ctx context.Context, content string) patrolProbeState

// isLLMSafetyError 识别「以 error 形式返回」的上游内容审核拦截（与 isLLMRefusal 分工：
// 后者处理 err==nil 但 content 是拒绝语的情况）。APIError.Message 携带上游原始 body，
// 因此同时检查错误链文本与 APIError.Message，避免包装层把关键信息吞掉。
func isLLMSafetyError(err error) (bool, string) {
	if err == nil {
		return false, ""
	}
	msg := strings.ToLower(err.Error())
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) && apiErr != nil {
		msg += " " + strings.ToLower(apiErr.Message)
	}
	type sig struct{ needle, reason string }
	sigs := []sig{
		{"content_filter", "网关内容过滤拦截"},
		{"content filter", "网关内容过滤拦截"},
		{"content policy", "请求命中内容策略"},
		{"safety policy", "请求命中安全策略"},
		{"content moderation", "内容审核拦截"},
		{"moderation", "内容审核拦截"},
		{"your request has been flagged", "请求被内容审核标记"},
		{"sensitive content", "命中敏感内容"},
		{"unsafe content", "命中不安全内容"},
		{"policy violation", "违反内容策略"},
		{"violates our", "违反内容策略"},
		{"内容审核", "内容审核拦截"},
		{"安全审核", "安全审核拦截"},
		{"敏感内容", "命中敏感内容"},
		{"内容不合规", "内容不合规"},
		{"违规内容", "命中违规内容"},
		{"风险内容", "命中风险内容"},
		{"违反内容", "违反内容策略"},
	}
	for _, s := range sigs {
		if strings.Contains(msg, s.needle) {
			return true, s.reason
		}
	}
	// "high risk" 单独出现时歧义太大（例如「磁盘用量有 high risk of failure」这类与内容
	// 审核无关的业务错误），仅当它来自网关 4xx 错误体时才认定为审核拦截。
	if apiErr != nil && (apiErr.StatusCode == 400 || apiErr.StatusCode == 403 || apiErr.StatusCode == 451) &&
		strings.Contains(msg, "high risk") {
		return true, "请求被判定为高风险"
	}
	return false, ""
}

// newPatrolSafetyProbe 构造生产探针。未配置轻量直连模型（llmClient/provider/model 任一
// 缺失，例如测试桩或降级部署）时返回常量 unknown，由定位器直接走本地词面提示，不空转。
func (s *Service) newPatrolSafetyProbe() patrolSafetyProbe {
	if s.llmClient == nil || s.defaultProv == "" || s.defaultModel == "" {
		return func(context.Context, string) patrolProbeState { return probeUnknown }
	}
	return func(ctx context.Context, content string) patrolProbeState {
		pctx, cancel := context.WithTimeout(ctx, patrolSafetyProbeTimeout)
		defer cancel()
		text, err := s.llmClient.GenerateText(pctx, s.defaultProv, []map[string]any{
			{"role": "user", "content": content},
		}, s.defaultModel)
		if err != nil {
			if blocked, _ := isLLMSafetyError(err); blocked {
				return probeBlocked
			}
			return probeUnknown // 断连/限流/鉴权：不可判定，绝不当作安全
		}
		if refused, _ := isLLMRefusal(text); refused {
			return probeBlocked
		}
		return probeClean
	}
}

// probePatrolUnitsContent 把一组单元拼成探针文本。章节单元带章标题、台账行带定位标签
// （尽量还原巡检语料形态）；#104 的请求侧单元（提示词 / 工具返回）按原文直接重放，
// 不加任何前缀——上游看到的正是这段文本，多一个标题都可能改变审核结论。
func probePatrolUnitsContent(units []patrolUnit) string {
	var sb strings.Builder
	for _, u := range units {
		switch u.Kind {
		case "chapter":
			fmt.Fprintf(&sb, "### %s\n%s\n", u.Label, u.Content)
		case "request":
			sb.WriteString(u.Content)
			sb.WriteString("\n")
		default:
			fmt.Fprintf(&sb, "%s：%s\n", u.Label, u.Content)
		}
	}
	return sb.String()
}

// safetyLocateResult 是一次定位的结论。Hits 为已确认（单独发送即被拦）的片段；
// Unconfirmed 为探测不可判定的片段；Hints 为本地词面提示（仅降级用）；Note 记录
// 边界条件（控制探针被拦 / 预算耗尽 / 组合触发 / 探测不可用等）。
type safetyLocateResult struct {
	Hits        []patrolUnit
	Unconfirmed []patrolUnit
	Hints       []patrolUnit
	Note        string
}

// localizeSafetyTriggers 用二分探测找出语料中触发上游审核的单元。
// 复杂度：命中 k 处时探针数约 O(k·log n)，受 patrolSafetyMaxProbes 与总超时双重硬帽约束，
// 绝不因一本书有几百条台账而放大成几百次上游调用。
func localizeSafetyTriggers(ctx context.Context, units []patrolUnit, probe patrolSafetyProbe) safetyLocateResult {
	res := safetyLocateResult{}
	if len(units) == 0 {
		res.Note = "本轮巡检未拼入任何章节/台账语料，拦截可能来自提示词本身或上游策略。"
		return res
	}
	if probe == nil {
		res.Note = "定位探针不可用，已改用本地词面提示。"
		res.Hints = scanLocalSafetyHints(units)
		return res
	}
	locCtx, cancel := context.WithTimeout(ctx, patrolSafetyLocateTimeout)
	defer cancel()

	probes, exhausted := 0, false
	call := func(content string) patrolProbeState {
		if locCtx.Err() != nil || probes >= patrolSafetyMaxProbes {
			exhausted = true
			return probeUnknown
		}
		probes++
		return probe(locCtx, content)
	}

	// 控制探针先行：被拦 = 网关/账户级风控，unknown = 轻量直连不可用，两者都不必再细分。
	switch call(patrolSafetyControlProbe) {
	case probeBlocked:
		res.Note = "上游对不含本书内容的控制探针同样拦截（疑似网关或账户级风控），本轮无法定位到具体片段；请稍后重试或更换供应商。"
		res.Hints = scanLocalSafetyHints(units)
		return res
	case probeUnknown:
		res.Note = "当前无法向模型发起定位探针（未配置轻量直连模型或供应商不可达），已改用本地词面提示。"
		res.Hints = scanLocalSafetyHints(units)
		return res
	}

	// walk 返回该子集内是否找到了命中/待确认项；blocked 父集两半皆 clean 视为组合触发。
	var walk func([]patrolUnit) bool
	walk = func(sub []patrolUnit) bool {
		if len(sub) == 0 || exhausted || locCtx.Err() != nil {
			return false
		}
		state := call(probePatrolUnitsContent(sub))
		if state == probeClean {
			return false
		}
		if len(sub) == 1 {
			if state == probeBlocked {
				res.Hits = append(res.Hits, sub[0])
			} else {
				res.Unconfirmed = append(res.Unconfirmed, sub[0])
			}
			return true
		}
		mid := len(sub) / 2
		left := walk(sub[:mid])
		right := walk(sub[mid:])
		if state == probeBlocked && !left && !right {
			res.Note = appendLocateNote(res.Note, fmt.Sprintf(
				"区间「%s」～「%s」拆开后单独测试均未触发，合并后才被拦（组合触发），请重点核查该区间内容。",
				sub[0].Label, sub[len(sub)-1].Label))
			return true
		}
		return left || right
	}
	walk(units)

	if exhausted {
		res.Note = appendLocateNote(res.Note, "定位预算或时间已用尽，未验证的片段仍可能含触犯内容；建议优先核查下列命中项与词面提示。")
		res.Hints = scanLocalSafetyHints(units)
	}
	if len(res.Hits) == 0 && len(res.Unconfirmed) == 0 && res.Note == "" {
		res.Note = "逐段探测未发现单独触发审核的片段：本次拦截更可能由多段内容组合、请求整体语境/体量或网关侧规则触发；" +
			"建议优先排查供应商/网关策略与请求规模（更换模型或供应商、分批检测、缩短台账块），不必盲目逐段改稿。"
		res.Hints = scanLocalSafetyHints(units)
	}
	res.Note = appendLocateNote(res.Note,
		"探针仅重放本次请求内的章节/台账/提示词/工具返回片段，未重放完整 system prompt 与工具调用结构；结论为定位辅助，最终以你对该处内容的核查为准。")
	return res
}

func appendLocateNote(note, extra string) string {
	if note == "" {
		return extra
	}
	return note + " " + extra
}

// patrolSafetyHintTerms 是本地降级提示用的高精度词面。仅在无法探测或探测无命中时
// 给出「可能需核查」的片段，绝不作为已确认命中；刻意保持小规模以压低误报。
var patrolSafetyHintTerms = []string{
	"血腥", "虐杀", "肢解", "碎尸", "性侵", "猥亵", "淫秽", "制毒", "自残", "凌迟",
}

// scanLocalSafetyHints 扫描语料单元中命中的高精度词面，返回去重后的单元列表。
func scanLocalSafetyHints(units []patrolUnit) []patrolUnit {
	var out []patrolUnit
	for _, u := range units {
		for _, term := range patrolSafetyHintTerms {
			if strings.Contains(u.Content, term) {
				out = append(out, u)
				break
			}
		}
	}
	return out
}

// render 把定位结论渲染为可直接放进巡检报告 Summary 的多行文本（前端按
// whitespace-pre-wrap 渲染）。每类结果设条数帽，避免把整本书台账灌进 DB 字段。
func (r safetyLocateResult) render() string {
	var sb strings.Builder
	writeUnits := func(title, tail string, units []patrolUnit, withExcerpt bool) {
		if len(units) == 0 {
			return
		}
		sb.WriteString("\n" + title + "\n")
		show := units
		if len(show) > patrolSafetyRenderCap {
			show = show[:patrolSafetyRenderCap]
		}
		for i, u := range show {
			fmt.Fprintf(&sb, "%d. %s", i+1, u.Label)
			if withExcerpt {
				fmt.Fprintf(&sb, "\n   摘录：%s", safetyUnitExcerpt(u.Content))
			}
			sb.WriteString("\n")
		}
		if len(units) > len(show) {
			fmt.Fprintf(&sb, "…另有 %d 条未逐条展示。\n", len(units)-len(show))
		}
		sb.WriteString(tail)
	}
	if len(r.Hits) > 0 {
		writeUnits("【触犯内容自动定位】以下片段单独发送给模型时同样被拦，请优先修改/脱敏后重跑：",
			"", r.Hits, true)
	}
	writeUnits("【未能确认的片段】探测结果不稳定，以下片段仍需人工核查：", "", r.Unconfirmed, false)
	writeUnits("【词面提示】以下片段含常见高风险词面（仅本地启发式提示，不等于确认命中）：",
		"", r.Hints, true)
	if r.Note != "" {
		sb.WriteString("\n定位说明：" + r.Note + "\n")
	}
	return sb.String()
}

// safetyUnitExcerpt 给命中片段配一段可读摘录。长片段采用「首窗 + 中略 + 尾窗」：
// 触犯内容常落在片段中后段（例如一大块台账的最后一行），只截开头会让作者看不到
// 真正要改的字句，定位结果等于白给（#104 实测）。
func safetyUnitExcerpt(content string) string {
	collapsed := strings.Join(strings.Fields(content), " ")
	if collapsed == "" {
		return "（空内容）"
	}
	runes := []rune(collapsed)
	if len(runes) <= patrolSafetyExcerptRunes {
		return collapsed
	}
	headN := patrolSafetyExcerptRunes * 2 / 3
	tailN := patrolSafetyExcerptRunes - headN
	return string(runes[:headN]) + " …（中略）… " + string(runes[len(runes)-tailN:])
}

// safetyProbe 返回本次定位使用的探针。生产走轻量直连调用；测试可经
// Service.safetyProbeFn 注入假上游，精确模拟命中/全清/被拦/不可用，不必发真实请求。
func (s *Service) safetyProbe() patrolSafetyProbe {
	if s == nil {
		return func(context.Context, string) patrolProbeState { return probeUnknown }
	}
	if s.safetyProbeFn != nil {
		return s.safetyProbeFn()
	}
	return s.newPatrolSafetyProbe()
}

// locatePatrolSafety 是 Service 侧接线：先广播 locating 进度（前端显示「正在定位…」），
// 再在巡检总预算 ctx 内执行定位。runCtx 受 patrolOverallTimeout 约束，定位本身另有
// patrolSafetyLocateTimeout 硬帽，绝不拖垮整轮巡检。
func (s *Service) locatePatrolSafety(ctx context.Context, runCtx context.Context, novelID int64, runID string, units []patrolUnit) safetyLocateResult {
	emitPatrolProgress(ctx, map[string]any{
		"novel_id": novelID, "run_id": runID, "phase": "locating",
	})
	loc := localizeSafetyTriggers(runCtx, units, s.safetyProbe())
	s.logger.Info("一致性巡检：安全审核触犯内容定位完成",
		"run_id", runID, "units", len(units), "hits", len(loc.Hits),
		"unconfirmed", len(loc.Unconfirmed), "hints", len(loc.Hints))
	return loc
}
