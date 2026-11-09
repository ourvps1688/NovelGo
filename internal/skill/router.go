package skill

import (
	"context"
	"strings"
)

// RouteResult 是用户消息的技能路由结果。
// Auto 为命中的 auto 模式技能（应自动注入正文）；
// Manual 为命中的 manual 模式技能（如总调度 next，建议类注入）。
// always 模式技能已在会话开头常驻，不在此返回，避免重复注入稀释注意力。
// Source 记录本轮命中来源（cue=确定性线索 / llm=LLM 兜底 / none=未命中），便于上层可观测。
type RouteResult struct {
	Auto   []SkillMeta
	Manual []SkillMeta
	// Redirects 是命中但属于 internalOnly 的技能：只回一行「请到检测/工单面板」指引，
	// 绝不注入正文（内部技能带 JSON 回执契约或流水线阶段口径，投给 chat 主 Agent 会污染输出）。
	Redirects []SkillMeta
	Source    MatchSource
}

// RouteUserMessage 根据用户消息做确定性意图匹配，返回应注入的技能。
// 匹配规则：小写化消息后，对任一 useWhen 条目的任一 cue 做子串匹配（cue 为空跳过）。
// 不调用 LLM、零额外开销；仅依赖各技能 frontmatter 声明的 useWhen 线索。
//
// exclude 用于排除用户已在消息中显式点名的技能（避免与 / 命令或口头提及重复注入）。
// maxAuto / maxManual 控制每轮注入上限，防止过度注入（呼应 P2 常驻上限的同一原则）。
func RouteUserMessage(
	all []SkillMeta,
	message string,
	maxAuto, maxManual int,
	exclude map[string]bool,
) RouteResult {
	msg := strings.ToLower(strings.TrimSpace(message))
	res := RouteResult{}
	if msg == "" {
		return res
	}

	seenAuto := make(map[string]bool)
	seenManual := make(map[string]bool)
	seenRedirect := make(map[string]bool)

	for _, m := range all {
		if exclude[m.Name] {
			continue
		}
		if !skillMatches(m, msg) {
			continue
		}
		// #111 P1：internalOnly 前置过滤。chat 侧 cue 命中不注入正文、只登记指引，
		// 流水线挂载不受影响（该字段在契约里只约束 chat 路由与 catalog 可见性）。
		if m.InternalOnly {
			if !seenRedirect[m.Name] {
				seenRedirect[m.Name] = true
				res.Redirects = append(res.Redirects, m)
			}
			continue
		}
		switch m.Mode {
		case ModeAuto:
			if !seenAuto[m.Name] && len(res.Auto) < maxAuto {
				seenAuto[m.Name] = true
				res.Auto = append(res.Auto, m)
			}
		case ModeManual:
			if !seenManual[m.Name] && len(res.Manual) < maxManual {
				seenManual[m.Name] = true
				res.Manual = append(res.Manual, m)
			}
		}
	}
	return res
}

// skillMatches 判断技能 useWhen 线索是否命中消息（任一 cue 子串出现即命中）。
// notWhen 是负向边界：消息命中任一排除词时整体不路由，防止宽泛 cue 误伤
// （如「动笔」命中但「动笔前我想先拢一遍线索」不该被写章总调度抢走）。
func skillMatches(m SkillMeta, msg string) bool {
	for _, ex := range m.NotWhen {
		if ex != "" && strings.Contains(msg, strings.ToLower(ex)) {
			return false
		}
	}
	for _, e := range m.UseWhen {
		for _, c := range e.Cues {
			if c == "" {
				continue
			}
			if strings.Contains(msg, strings.ToLower(c)) {
				return true
			}
		}
	}
	return false
}

// FirstIntent 返回技能 useWhen 的首条意图描述，用于提示文案；无声明时返回空串。
func FirstIntent(m SkillMeta) string {
	if len(m.UseWhen) > 0 {
		return m.UseWhen[0].Intent
	}
	return ""
}

// MatchSource 标记本轮路由命中来源。
type MatchSource string

const (
	SourceCue  MatchSource = "cue"  // 确定性 cue 子串命中
	SourceLLM  MatchSource = "llm"  // LLM 兜底分类命中
	SourceNone MatchSource = "none" // 未命中
)

// IntentCandidate 是兜底分类器可用的候选意图。
type IntentCandidate struct {
	Name   string   // 技能名
	Intent string   // 人类可读意图描述
	Cues   []string // 该技能的确定性线索词（作为分类上下文）
}

// Classifier 是模糊意图兜底分类器（可选，由上层注入 LLM 实现）。
// 仅当确定性 cue 匹配无命中时被调用；返回最匹配技能名与置信度 [0,1]，无匹配返回 ("", 0)。
type Classifier func(ctx context.Context, msg string, candidates []IntentCandidate) (name string, confidence float64)

// llmConfidenceThreshold 是 LLM 兜底分类的最低置信度，低于此值视为无匹配，避免误注入。
const llmConfidenceThreshold = 0.6

// RouteUserMessageSmart 在确定性 cue 匹配之上叠加 LLM 兜底分类。
// 流程：
//  1. 先跑确定性匹配；有命中直接返回（Source=cue），零额外开销；
//  2. 无命中且 classifier 非空时，用候选意图列表调用分类器；命中且置信度达标则注入（Source=llm）；
//  3. 都没有则 Source=none、返回空结果。
//
// 原 RouteUserMessage 保留为纯确定性核心（供单测）；Smart 在其之上叠加兜底。
func RouteUserMessageSmart(
	ctx context.Context,
	all []SkillMeta,
	message string,
	maxAuto, maxManual int,
	exclude map[string]bool,
	classifier Classifier,
) RouteResult {
	res := RouteUserMessage(all, message, maxAuto, maxManual, exclude)
	// Redirects 也算确定性命中：用户明确在找内部工作流时，不再让 LLM 兜底硬塞
	// 一个不相关的 chat 技能（避免「修复」被解释成某个创作技法）。
	if len(res.Auto) > 0 || len(res.Manual) > 0 || len(res.Redirects) > 0 {
		res.Source = SourceCue
		return res
	}
	res.Source = SourceNone
	if classifier == nil {
		return res
	}
	// 模糊/过短消息（纯寒暄、单字）跳过 LLM 兜底，省成本与延迟
	if len([]rune(strings.TrimSpace(message))) < 6 {
		return res
	}
	cands := make([]IntentCandidate, 0, len(all))
	for _, m := range all {
		if exclude[m.Name] || m.InternalOnly || len(m.UseWhen) == 0 {
			continue
		}
		var cues []string
		for _, e := range m.UseWhen {
			cues = append(cues, e.Cues...)
		}
		cands = append(cands, IntentCandidate{
			Name:   m.Name,
			Intent: m.UseWhen[0].Intent,
			Cues:   cues,
		})
	}
	if len(cands) == 0 {
		return res
	}
	name, conf := classifier(ctx, message, cands)
	if name == "" || conf < llmConfidenceThreshold {
		return res
	}
	for _, m := range all {
		if m.Name != name || m.InternalOnly {
			continue
		}
		res.Source = SourceLLM
		switch m.Mode {
		case ModeAuto:
			res.Auto = append(res.Auto, m)
		case ModeManual:
			res.Manual = append(res.Manual, m)
		}
		return res
	}
	return res
}
