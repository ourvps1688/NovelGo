package agentcfg

import (
	"strings"

	"github.com/novelgo/novelgo/internal/skill"
)

// PersonaRef 引用一个写作人格（创作心智），作为对话的"写作模式"开关。
//   - Kind == "skill"：Ref 为技能名（如 "X 的创作心智"），直接按名解析。
//   - Kind == "author"：Ref 为规范的 source_author（如 "刘慈欣"），解析到该作者的
//     mental 技能（优先聚合档，其次任意 mental 档）。
//
// persona 与 .enabled.json 的常驻技能启用正交：persona 是会话级 / 每轮注入的写作模式，
// 关闭即回普通模式，不写入 .enabled.json。
type PersonaRef struct {
	Kind string `json:"kind"` // "skill" | "author"
	Ref  string `json:"ref"`  // 技能名 或 规范 source_author
}

// PersonaMarker 是 persona 系统消息的标识头，用于已有会话每轮注入时检测去重
// （避免与"新会话常驻块"重复注入）。亦作为该块的视觉/语义边界。
const PersonaMarker = "【写作人格 · Persona】"

// PersonaHonestyNote 诚实边界声明（尊重 PRD WC-5）：创作心智推断自虚构文本，
// 不代表真实作者意图。构建 persona 引导段时必须追加。导出供测试与前端引用。
const PersonaHonestyNote = "注意：以下创作心智推断自虚构文本，仅供参考，不得作为真实作者意图的断言。"

// BuildPersonaSystemText 解析 persona 引用并构建注入 LLM 的引导段文本
// （标识头 + 引导说明 + 技能正文 + 诚实边界声明）。
// 返回空串表示无法解析（persona 为 nil、skillStore 为 nil 或引用不存在），调用方应跳过注入。
func BuildPersonaSystemText(persona *PersonaRef, skillStore *skill.Store, novelID int64) string {
	if persona == nil || skillStore == nil {
		return ""
	}
	content, _ := resolvePersonaContent(persona, skillStore, novelID)
	if content == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(PersonaMarker)
	b.WriteString("\n\n")
	b.WriteString("以下创作心智将指导你起草本轮新章节。请在保持故事世界一致性的前提下，以该作者的叙事习惯、表达指纹与创作决策启发式来写作，而不是简单模仿或套用公式。\n\n")
	b.WriteString(content)
	b.WriteString("\n\n")
	b.WriteString(PersonaHonestyNote)
	return b.String()
}

// BuildPersonaMindText 解析 persona 引用并构建「心智正文」（P0 WBS §0.3 的 provider 口径）：
// 技能正文 + 诚实边界声明。统一上下文装配器（internal/contextasm）以本返回值为
// Voice 块正文，块头（PersonaMarker/VoiceHeader）与 purpose 引导语由装配器渲染，
// 本函数不拼块头与引导语。
// 返回空串表示无法解析（persona 为 nil、skillStore 为 nil 或引用不存在），
// 调用方应降级为不注入该块。
func BuildPersonaMindText(persona *PersonaRef, skillStore *skill.Store, novelID int64) string {
	if persona == nil || skillStore == nil {
		return ""
	}
	content, _ := resolvePersonaContent(persona, skillStore, novelID)
	if content == "" {
		return ""
	}
	return content + "\n\n" + PersonaHonestyNote
}

// resolvePersonaContent 把 persona 引用解析到技能正文 + 展示名（技能名）。
//   - Kind=="skill"：Ref 为技能名，直接 Get。
//   - Kind=="author"：Ref 为规范 source_author，在该 novel 的 mental 技能中匹配；
//     优先取聚合档（aggregated），否则取首个匹配（调用方已保证 Ref 经归一）。
func resolvePersonaContent(persona *PersonaRef, skillStore *skill.Store, novelID int64) (string, string) {
	switch persona.Kind {
	case "skill":
		if sk, ok := skillStore.Get(novelID, persona.Ref); ok {
			return sk.Content, sk.Name
		}
		return "", ""
	case "author":
		var fallback *skill.SkillMeta
		for _, m := range skillStore.ListMeta(novelID) {
			if m.Kind != "mental" {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(m.SourceAuthor), strings.TrimSpace(persona.Ref)) {
				continue
			}
			if m.Aggregated {
				if sk, ok := skillStore.Get(novelID, m.Name); ok {
					return sk.Content, sk.Name
				}
			}
			if fallback == nil {
				fallback = &m
			}
		}
		if fallback != nil {
			if sk, ok := skillStore.Get(novelID, fallback.Name); ok {
				return sk.Content, sk.Name
			}
		}
		return "", ""
	default:
		return "", ""
	}
}
