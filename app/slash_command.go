package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/skill"
)

// ListSlashCommandsInput 是 ListSlashCommands 的入参。
type ListSlashCommandsInput struct {
	NovelID int64 `json:"novel_id"`
}

// SlashCommand 是斜杠菜单的统一条目，数据源来自 skill store，Type 直接对应 skill mode。
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`          // "auto" | "manual" | "always"（对应 skill mode）
	UseWhenHint string `json:"use_when_hint"` // 何时使用提示（来自技能 useWhen 首条 intent），为空表示未声明
}

// ListSlashCommands 返回所有 skill 对应的斜杠菜单条目。
func (a *App) ListSlashCommands(input ListSlashCommandsInput) []SlashCommand {
	if a.skill == nil {
		return nil
	}
	meta := a.skill.ListMeta(input.NovelID)
	result := make([]SlashCommand, 0, len(meta))
	for _, m := range meta {
		result = append(result, SlashCommand{
			Name:        m.Name,
			Description: m.Description,
			Type:        m.Mode,
			UseWhenHint: skill.FirstIntent(m),
		})
	}
	return result
}

// resolveSlashCommand 根据名称查找 skill 并返回注入内容（空串表示未命中）。
func (a *App) resolveSlashCommand(novelID int64, name string) (inject string, logName string) {
	if a.skill == nil {
		return "", ""
	}
	sk, ok := a.skill.Get(novelID, name)
	if !ok {
		return "", ""
	}

	switch sk.Mode {
	case "always":
		// always 已在会话开头注入，/ 触发时只给出提示。
		// 注入可能缺位（超常驻上限被截断 / 本会话早于设置生效），措辞不把「已注入」说死。
		content := "用户通过 /" + sk.Name + " 提醒你注意常驻技能「" + sk.Name + "」，其正文应在本次对话开头注入；若开头未见其正文（超出常驻上限被截断或设置晚于本会话），请用 read 加载完整内容后严格遵循。"
		return "<system-reminder>\n" + content + "\n</system-reminder>", sk.Name
	case "manual":
		content := "用户启用了快捷指令「" + sk.Name + "」。请根据该指令的内容和用户需求进行工作。\n\n---\n" + sk.Content + "\n---"
		return "<system-reminder>\n" + content + "\n</system-reminder>", sk.Name
	default: // auto
		content := "用户启用了技能「" + sk.Name + "」。请根据该技能的定义和用户需求进行工作。\n\n---\n" + sk.RawContent + "\n---"
		return "<system-reminder>\n" + content + "\n</system-reminder>", sk.Name
	}
}

// buildAutoSkillInject 依据用户消息的意图匹配，自动构建应注入的技能正文。
// 仅对 auto 技能注入完整内容（确定性预路由，让 LLM 不再靠猜目录）；
// 对 manual 技能（如总调度 next）注入为「建议类」reminder，触发其执行；
// always 技能已常驻，跳过。每轮最多各注入 1 条，防止过度注入稀释注意力。
//
// 路由采用「确定性 cue 优先 + LLM 兜底」混合策略：cue 命中零开销直接注入；
// cue 未命中且消息非寒暄时，用 providerName/modelID 指定的模型做一次轻量意图分类，
// 把模糊意图映射到最合适的技能（避免误注入）。无 LLM/未指定模型时退化为纯确定性路由。
func (a *App) buildAutoSkillInject(ctx context.Context, novelID int64, message, providerName, modelID string) string {
	if a.skill == nil {
		return ""
	}
	all := a.skill.ListMeta(novelID)
	exclude := explicitSkillNames(message, all)

	var classifier skill.Classifier
	if a.llmClient != nil && providerName != "" && modelID != "" {
		pn, mid := providerName, modelID
		classifier = func(ctx context.Context, msg string, cands []skill.IntentCandidate) (string, float64) {
			return a.classifyIntent(ctx, msg, cands, pn, mid)
		}
	}

	route := skill.RouteUserMessageSmart(ctx, all, message, 1, 1, exclude, classifier)

	var b strings.Builder
	for _, m := range route.Auto {
		sk, ok := a.skill.Get(novelID, m.Name)
		if !ok {
			continue
		}
		b.WriteString("<system-reminder>\n")
		fmt.Fprintf(&b, "用户消息触发了技能「%s」的自动加载（意图匹配）。请依据该技能定义与用户需求工作。\n\n---\n%s\n---\n", m.Name, sk.RawContent)
		b.WriteString("</system-reminder>\n")
	}
	for _, m := range route.Manual {
		sk, ok := a.skill.Get(novelID, m.Name)
		if !ok {
			continue
		}
		intent := skill.FirstIntent(m)
		b.WriteString("<system-reminder>\n")
		fmt.Fprintf(&b, "检测到用户意图匹配技能「%s」（%s）。请启用该总调度/技能，按其对用户需求开展工作：\n\n---\n%s\n---\n", m.Name, intent, sk.RawContent)
		b.WriteString("</system-reminder>\n")
	}
	if b.Len() > 0 {
		names := make([]string, 0, len(route.Auto)+len(route.Manual))
		for _, m := range route.Auto {
			names = append(names, m.Name)
		}
		for _, m := range route.Manual {
			names = append(names, m.Name)
		}
		a.logger.Debug("意图预路由命中", "source", string(route.Source), "skills", strings.Join(names, ","))
	}
	return strings.TrimSpace(b.String())
}

// classifyMinConfidence 是分类器输出置信度低于此值时判定为无匹配（与 skill 包阈值保持一致）。
const classifyMinConfidence = 0.6

// classifyIntent 用 LLM 对模糊用户消息做轻量意图分类，返回最匹配技能名与置信度。
// 仅在确定性 cue 匹配无命中时由路由兜底调用；失败时降级为 ("", 0) 不注入，绝不阻塞主流程。
func (a *App) classifyIntent(ctx context.Context, msg string, cands []skill.IntentCandidate, providerName, modelID string) (string, float64) {
	if a.llmClient == nil {
		return "", 0
	}
	pn, mid := providerName, modelID
	if pn == "" || mid == "" {
		pn, mid = a.defaultClassifyPM()
	}
	if pn == "" {
		return "", 0
	}

	// 短超时，避免兜底分类拖慢主对话轮次
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	const sys = "你是一个小说创作助手的技能路由器。根据用户消息，从候选技能中选出最相关的一个；若都不相关则输出 none。只输出 JSON，不要任何解释文字。"
	var ub strings.Builder
	ub.WriteString("候选技能（name | 意图 | 示例触发词）：\n")
	for _, c := range cands {
		fmt.Fprintf(&ub, "- %s | %s | %s\n", c.Name, c.Intent, strings.Join(c.Cues, "、"))
	}
	ub.WriteString("用户消息：\n")
	ub.WriteString(msg)
	ub.WriteString("\n\n请输出 JSON：{\"skill\":\"<候选中的 name 或 none>\",\"confidence\":<0到1之间的数，越高越相关>}")

	out, err := a.llmClient.GenerateText(cctx, pn, []map[string]any{
		{"role": "system", "content": sys},
		{"role": "user", "content": ub.String()},
	}, mid)
	if err != nil {
		a.logger.Warn("意图兜底分类调用 LLM 失败，降级为不注入", "err", err)
		return "", 0
	}
	return parseClassifierJSON(out, cands)
}

// defaultClassifyPM 在调用方未显式给出 provider/model 时，取首个可用 provider 及其首个模型。
func (a *App) defaultClassifyPM() (string, string) {
	if a.llmClient == nil {
		return "", ""
	}
	for name, p := range a.llmClient.Providers() {
		if len(p.Models) > 0 {
			return name, p.Models[0].ID
		}
	}
	return "", ""
}

// parseClassifierJSON 从分类器输出中解析技能名与置信度，并校验其为候选之一。
func parseClassifierJSON(out string, cands []skill.IntentCandidate) (string, float64) {
	out = strings.TrimSpace(out)
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end <= start {
		return "", 0
	}
	var r struct {
		Skill      string  `json:"skill"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &r); err != nil {
		return "", 0
	}
	if r.Skill == "" || strings.EqualFold(r.Skill, "none") {
		return "", 0
	}
	for _, c := range cands {
		if c.Name == r.Skill {
			conf := r.Confidence
			if conf <= 0 {
				conf = classifyMinConfidence // 模型未给置信度时给保守默认，刚好达到注入门槛
			}
			return c.Name, conf
		}
	}
	return "", 0
}

// explicitSkillNames 返回消息中已显式出现的技能名集合，避免与 / 命令或口头提及重复注入。
func explicitSkillNames(message string, all []skill.SkillMeta) map[string]bool {
	msg := strings.ToLower(message)
	set := make(map[string]bool)
	for _, m := range all {
		if m.Name != "" && strings.Contains(msg, strings.ToLower(m.Name)) {
			set[m.Name] = true
		}
	}
	return set
}
