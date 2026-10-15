package agentcfg

import (
	"strconv"
	"strings"

	"github.com/novelgo/novelgo/internal/skill"
)

// MaxAlwaysSkillsResident 是常驻（always + 勾选启用）技能注入 system 消息的数量硬上限。
// 超过此数的技能被截断为按需（auto）触发，避免长驻正文稀释主 Agent 上下文注意力、
// 造成"规则越多执行越差"的退化。显式 mode=always 的技能优先保留，其次才轮到勾选启用的。
const MaxAlwaysSkillsResident = 3

// BuildSkillCatalog 将 auto 模式的 skill 元数据格式化为 LLM 可用的技能目录。
// manual 和 always 模式的 skill 不出现在目录中。
// 按 novel > user > builtin 分组；每个来源块内再按 category 子分组（category 字段
// 此前仅被解析却未使用，现真正参与目录结构，便于 LLM 区分语义相邻的技法）。
func BuildSkillCatalog(meta []skill.SkillMeta) string {
	if len(meta) == 0 {
		return ""
	}

	novel := filterBySource(meta, "novel")
	user := filterBySource(meta, "user")
	builtin := filterBySource(meta, "builtin")

	var b strings.Builder
	b.WriteString("<available_skills>\n")

	writeSourceGroup(&b, "小说专属技能", novel)
	writeSourceGroup(&b, "用户技能", user)
	writeSourceGroup(&b, "内置技能（只读）", builtin)

	b.WriteString("---\n")
	b.WriteString("路由指引（消歧）：\n")
	b.WriteString("- 宏观层（story-blueprint 蓝图 / opening-chapters 前三章 / worldbuilding-design 设定体系 / character-design 人设 / signability-audit 签约体检）先于微观层（scene-beats 场景 / pacing-control 节奏 / plot-hook 钩子 / dialogue-subtext 对白 / revision-pass 打磨 / reader-immersion 代入 / deai-flavor 去味 / brainstorm-composer 发散）。\n")
	b.WriteString("- 场景内部张力/节拍用 scene-beats；节奏快慢用 pacing-control；二者不混。\n")
	b.WriteString("- 只改某类具体技法时点名加载对应技能；综合请求先对齐宏观层再下沉，勿一次堆砌多个微观技法。\n")
	b.WriteString("\n使用 read 工具加载技能完整内容：\n")
	b.WriteString("- skills/<name>.md（小说技能）\n")
	b.WriteString("- ~/.novelgo/skills/<name>.md（用户技能）\n")
	b.WriteString("- /builtin/skills/<name>.md（内置技能，只读）\n")
	b.WriteString("使用 edit 工具创建或修改技能。内置技能不可编辑。\n")
	b.WriteString("</available_skills>")

	return b.String()
}

// writeSourceGroup 将一个来源的技能按 category 子分组写入 builder。
// category 按首次出现顺序稳定输出；无 category 的技能归入「其他」组并置于末尾。
func writeSourceGroup(b *strings.Builder, title string, skills []skill.SkillMeta) {
	if len(skills) == 0 {
		return
	}
	b.WriteString("## ")
	b.WriteString(title)
	b.WriteString("\n")

	groups := groupByCategoryOrdered(skills)
	for _, g := range groups {
		label := g.name
		if label == "" {
			label = "其他"
		}
		b.WriteString("### ")
		b.WriteString(label)
		b.WriteString("\n")
		for _, s := range g.items {
			b.WriteString("- ")
			b.WriteString(s.Name)
			if s.Description != "" {
				b.WriteString(": ")
				b.WriteString(s.Description)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
}

// categoryGroup 是同一 category 下的技能集合。
type categoryGroup struct {
	name  string
	items []skill.SkillMeta
}

// groupByCategoryOrdered 按 category 分组并保持稳定顺序（首次出现优先），
// 无 category 的技能统一归入 name=="" 的组，调用方将其渲染为「其他」并置于末尾。
func groupByCategoryOrdered(skills []skill.SkillMeta) []categoryGroup {
	order := make([]string, 0)
	byCat := make(map[string][]skill.SkillMeta)
	for _, s := range skills {
		cat := s.Category
		if _, ok := byCat[cat]; !ok {
			order = append(order, cat)
		}
		byCat[cat] = append(byCat[cat], s)
	}
	result := make([]categoryGroup, 0, len(order))
	for _, cat := range order {
		result = append(result, categoryGroup{name: cat, items: byCat[cat]})
	}
	return result
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
	full := make([]skill.SkillMeta, 0, len(always)+len(enabled))
	full = append(full, always...)
	full = append(full, enabled...)

	// 常驻上限保护：最多 MaxAlwaysSkillsResident 个，优先保留显式 always，再补勾选启用的；
	// 超出部分截断为按需（auto）触发，并生成警告提示被丢弃的技能名（不阻塞流程）。
	var warning string
	resident := full
	if len(full) > MaxAlwaysSkillsResident {
		kept := make([]skill.SkillMeta, 0, MaxAlwaysSkillsResident)
		// 显式 always 优先，但连 always 本身也超上限时同样截断（保前 N 个）。
		alwaysKeep := len(always)
		if alwaysKeep > MaxAlwaysSkillsResident {
			alwaysKeep = MaxAlwaysSkillsResident
		}
		kept = append(kept, always[:alwaysKeep]...)
		if room := MaxAlwaysSkillsResident - len(kept); room > 0 && len(enabled) > 0 {
			end := room
			if end > len(enabled) {
				end = len(enabled)
			}
			kept = append(kept, enabled[:end]...)
		}
		dropped := make([]string, 0, len(full)-len(kept))
		keptNames := make(map[string]bool, len(kept))
		for _, m := range kept {
			keptNames[m.Name] = true
		}
		for _, m := range full {
			if !keptNames[m.Name] {
				dropped = append(dropped, m.Name)
			}
		}
		resident = kept
		warning = "⚠️ 常驻技能超过 " + strconv.Itoa(MaxAlwaysSkillsResident) +
			" 个上限，已仅常驻最核心的 " + strconv.Itoa(MaxAlwaysSkillsResident) +
			" 个；其余（" + strings.Join(dropped, "、") +
			"）改为按需（auto）触发，避免长驻正文稀释上下文注意力。如需常驻请优先保留最核心的技能。\n"
	}

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

	if warning != "" {
		b.WriteString(warning)
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
