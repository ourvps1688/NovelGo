package agentcfg

import (
	"strings"

	"github.com/novelgo/novelgo/internal/skill"
)

// BuildSkillCatalog 将 auto 模式的 skill 元数据格式化为 LLM 可用的技能目录。
// manual 和 always 模式的 skill 不出现在目录中。
// 按 novel > user > builtin 分组输出，每组只列出 name 和 description。
func BuildSkillCatalog(meta []skill.SkillMeta) string {
	if len(meta) == 0 {
		return ""
	}

	novel := filterBySource(meta, "novel")
	user := filterBySource(meta, "user")
	builtin := filterBySource(meta, "builtin")

	var b strings.Builder
	b.WriteString("<available_skills>\n")

	if len(novel) > 0 {
		b.WriteString("## 小说专属技能\n")
		for _, s := range novel {
			b.WriteString("- ")
			b.WriteString(s.Name)
			if s.Description != "" {
				b.WriteString(": ")
				b.WriteString(s.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(user) > 0 {
		b.WriteString("## 用户技能\n")
		for _, s := range user {
			b.WriteString("- ")
			b.WriteString(s.Name)
			if s.Description != "" {
				b.WriteString(": ")
				b.WriteString(s.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if len(builtin) > 0 {
		b.WriteString("## 内置技能（只读）\n")
		for _, s := range builtin {
			b.WriteString("- ")
			b.WriteString(s.Name)
			if s.Description != "" {
				b.WriteString(": ")
				b.WriteString(s.Description)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("---\n")
	b.WriteString("使用 read 工具加载技能完整内容：\n")
	b.WriteString("- skills/<name>.md（小说技能）\n")
	b.WriteString("- ~/.novelgo/skills/<name>.md（用户技能）\n")
	b.WriteString("- /builtin/skills/<name>.md（内置技能，只读）\n")
	b.WriteString("使用 edit 工具创建或修改技能。内置技能不可编辑。\n")
	b.WriteString("</available_skills>")

	return b.String()
}

// BuildAlwaysSkillsContent 拼合所有 always 模式的 skill 正文，作为常驻系统消息注入。
// 不包含 YAML frontmatter，只拼接 body 内容。
// 除 frontmatter mode=always 的技能外，还会合并小说"勾选启用"的技能
// （用户在聊天输入区 + 菜单里按书勾选，存于小说技能目录 .enabled.json）——
// 勾选即临时 always，取消勾选即恢复原 mode 行为。
func BuildAlwaysSkillsContent(meta []skill.SkillMeta, store *skill.Store, novelID int64) string {
	if store == nil {
		return ""
	}
	always := filterByMode(meta, skill.ModeAlways)
	enabled := filterByNames(meta, store.EnabledNames(novelID), always)
	resident := make([]skill.SkillMeta, 0, len(always)+len(enabled))
	resident = append(resident, always...)
	resident = append(resident, enabled...)
	if len(resident) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("【常驻技能】\n")
	b.WriteString("以下技能在本次对话中始终生效：\n\n")

	for _, m := range resident {
		sk, ok := store.Get(novelID, m.Name)
		if !ok {
			continue
		}
		b.WriteString("--- ")
		b.WriteString(sk.Name)
		b.WriteString(" ---\n")
		b.WriteString(sk.Content)
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String())
}

// excludeNames 从 meta 中剔除 names 里出现的技能（用于目录去重）。
func excludeNames(meta []skill.SkillMeta, names []string) []skill.SkillMeta {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	var result []skill.SkillMeta
	for _, s := range meta {
		if !drop[s.Name] {
			result = append(result, s)
		}
	}
	return result
}

func filterBySource(meta []skill.SkillMeta, source string) []skill.SkillMeta {
	var result []skill.SkillMeta
	for _, s := range meta {
		if s.Source == source {
			result = append(result, s)
		}
	}
	return result
}

func filterByMode(meta []skill.SkillMeta, mode string) []skill.SkillMeta {
	var result []skill.SkillMeta
	for _, s := range meta {
		if s.Mode == mode {
			result = append(result, s)
		}
	}
	return result
}

// filterByNames 按 names 过滤 meta，跳过 already 集合中已有的名字。
// 用于把勾选启用的技能（任意 mode）并入常驻注入清单；清单中已不存在的
// 名字（技能被删除）自动忽略。
func filterByNames(meta []skill.SkillMeta, names []string, already []skill.SkillMeta) []skill.SkillMeta {
	if len(names) == 0 {
		return nil
	}
	skip := make(map[string]bool, len(already))
	for _, s := range already {
		skip[s.Name] = true
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var result []skill.SkillMeta
	for _, s := range meta {
		if want[s.Name] && !skip[s.Name] {
			result = append(result, s)
		}
	}
	return result
}
