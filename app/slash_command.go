package app

import (
	"fmt"
	"strings"

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
		// always 已在会话开头注入，/ 触发时只给出提示
		content := "用户通过 /" + sk.Name + " 提醒你注意常驻技能「" + sk.Name + "」，其完整内容已在本次对话开头注入，请严格遵循。"
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
func (a *App) buildAutoSkillInject(novelID int64, message string) string {
	if a.skill == nil {
		return ""
	}
	all := a.skill.ListMeta(novelID)
	exclude := explicitSkillNames(message, all)
	route := skill.RouteUserMessage(all, message, 1, 1, exclude)

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
	return strings.TrimSpace(b.String())
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
