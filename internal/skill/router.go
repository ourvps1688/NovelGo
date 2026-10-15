package skill

import "strings"

// RouteResult 是用户消息的技能路由结果。
// Auto 为命中的 auto 模式技能（应自动注入正文）；
// Manual 为命中的 manual 模式技能（如总调度 next，建议类注入）。
// always 模式技能已在会话开头常驻，不在此返回，避免重复注入稀释注意力。
type RouteResult struct {
	Auto   []SkillMeta
	Manual []SkillMeta
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

	for _, m := range all {
		if exclude[m.Name] {
			continue
		}
		if !skillMatches(m, msg) {
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
func skillMatches(m SkillMeta, msg string) bool {
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
