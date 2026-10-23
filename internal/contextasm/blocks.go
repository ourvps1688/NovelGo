package contextasm

import "strings"

// —— 块文案单源（WBS §6）：块头/引导语/尾注文案只允许存在于本文件一处，
// pipeline/agentcfg 不得自带副本。——

const (
	// FactsHeader 「已冻结卷既定事实」块头（原文案自 internal/pipeline.bibleBlock 一字不动迁入）。
	FactsHeader = "【已冻结卷既定事实（系列圣经快照）】"
	// FactsTail Facts 块尾注（原文案一字不动迁入）。
	FactsTail = "以上是已冻结（定稿）卷在最近一次冻结时固化的系列圣经，是本次校验/修复的**既定事实依据**：\n" +
		"与之冲突的一律改本卷及后续来适配它（前向修复），**不得**为消解冲突回改已冻结卷；" +
		"与之不冲突的部分以正文与台账工具读到的现状为准。"

	// VoiceHeader 非 chat 用途的文风锚定块头（WBS §0.3）。
	VoiceHeader = "【写作人格 · 文风锚定】"
	// PersonaMarker chat 路径块头。chat 每轮注入去重靠该前缀匹配，文案与位置禁改。
	// 注意：internal/agentcfg.PersonaMarker 现为同文案副本，T3.1 起应改为引用本常量（文案单源）。
	PersonaMarker = "【写作人格 · Persona】"

	// IntentHeader / IntentTail 创作意图锚定块（WBS §0.3）。
	IntentHeader = "【创作意图锚定】"
	IntentTail   = "以上是本书的创作意图与卖点定位，写作与校验以此为方向锚定；与之冲突时以意图为准调整执行细节，不得偏离卖点定位。"

	// GuideChat chat 用途引导语。与 agentcfg.BuildPersonaSystemText 现行引导语逐字一致（golden 锁定，禁改）。
	GuideChat = "以下创作心智将指导你起草本轮新章节。请在保持故事世界一致性的前提下，" +
		"以该作者的叙事习惯、表达指纹与创作决策启发式来写作，而不是简单模仿或套用公式。"
	// GuideRewrite rewrite 用途引导语（WBS §0.6）。
	GuideRewrite = "以下创作心智是本书的文风基准。重写时保持该叙事习惯、表达指纹与创作决策启发式，" +
		"**不得因重写造成文风漂移**；解决既有问题优先于改写风格。"
	// GuideReference audit/fix/patrol 用途引导语（WBS §0.6，仅作参照不扣分）。
	GuideReference = "以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。"
)

// guideFor 返回 purpose 对应的 Voice 引导语（WBS §0.6 表驱动）。
// 未知 purpose 复用参照语（最保守：只看不扣分）。
func guideFor(purpose Purpose) string {
	switch purpose {
	case PurposeChat:
		return GuideChat
	case PurposeRewrite:
		return GuideRewrite
	default:
		return GuideReference
	}
}

// RenderFacts 渲染「已冻结卷既定事实」块（不含前导空行，空串返回空）。
// 文案与格式自 pipeline.bibleBlock 一字不动迁入（文案单源）。
func RenderFacts(text string) string {
	if text == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(FactsHeader)
	sb.WriteString("\n")
	sb.WriteString(text)
	sb.WriteString("\n")
	sb.WriteString(FactsTail)
	return sb.String()
}

// RenderVoice 渲染文风锚定块（不含前导空行，空串返回空）：
// 块头 + 引导语（purpose 相关，§0.6）+ 心智正文。
//   - chat 用途块头为 PersonaMarker（去重/持久化语义零变化，禁改），其余为 VoiceHeader；
//   - mindText 为 provider 返回的心智正文，应已含 PersonaHonestyNote 诚实边界声明
//     （由 provider 端保证，装配器不再重复拼）；
//   - 本函数纯格式化、不截断预算——chat 路径经此渲染须与 BuildPersonaSystemText 逐字一致。
func RenderVoice(purpose Purpose, mindText string) string {
	if mindText == "" {
		return ""
	}
	header := VoiceHeader
	if purpose == PurposeChat {
		header = PersonaMarker
	}
	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n\n")
	sb.WriteString(guideFor(purpose))
	sb.WriteString("\n\n")
	sb.WriteString(mindText)
	return sb.String()
}

// RenderIntent 渲染创作意图锚定块（不含前导空行，空串返回空）。
func RenderIntent(text string) string {
	if text == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(IntentHeader)
	sb.WriteString("\n")
	sb.WriteString(text)
	sb.WriteString("\n")
	sb.WriteString(IntentTail)
	return sb.String()
}

// Render 按固定顺序 Facts → Voice → Intent 渲染非空块（硬事实在前、软约束在后）。
// 每块以 "\n\n" 前缀追加到提示词尾部（bibleBlock 范式）；全部为空返回空串，
// 提示词与未接任何块时逐字一致。purpose 仅决定 Voice 引导语（§0.6）；
// 取块范围由 Assembler.Build 的 purpose×块矩阵（§0.5）决定。
func (b ContextBlocks) Render(purpose Purpose) string {
	var sb strings.Builder
	for _, block := range []string{RenderFacts(b.Facts), RenderVoice(purpose, b.Voice), RenderIntent(b.Intent)} {
		if block == "" {
			continue
		}
		sb.WriteString("\n\n")
		sb.WriteString(block)
	}
	return sb.String()
}
