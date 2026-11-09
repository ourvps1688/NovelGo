package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/activity"
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
		content := "用户启用了技能「" + sk.Name + "」。请根据该技能的定义和用户需求进行工作。\n\n---\n" + sk.Content + "\n---"
		return "<system-reminder>\n" + content + "\n</system-reminder>", sk.Name
	}
}

// writingBundleSkills 是写章总调度命中时一并注入的技法技能（确定性单源）。
// 取 next 阶段 2 里最直接影响本章正文的三项；节奏模板/前情回顾由 chat.go 独立
// 通道注入，不在此重复。数量受单技能/整块双封顶约束，避免稀释上下文注意力。
var writingBundleSkills = []string{"reader-immersion", "scene-beats", "plot-hook"}

const (
	writingBundleSkillCap = 1500 // 单技能注入上限（rune）
	// #111 P1：整块帽按 6×1500 同步扩容。固定 3 技法已占 4273 rune，旧 5000 帽
	// 只剩 727，任何条件技能（最短 1273）都会被 break，条件化将永远不生效。
	// 单技能帽保持 1500 不变；异常时仍可单条开关回退到固定 3 个（≤5000）。
	writingBundleBlockCap = 9000

	// #111 P1：chat 意图注入预算。auto 放宽到 3 但受同类去重与单轮总 rune 帽约束，
	// 避免「多命中 = 多塞正文」把注意力稀释掉；新帽先按账本观测 7 天再定值。
	chatMaxAutoSkills      = 3
	chatMaxManualSkills    = 1
	chatAutoInjectRuneCap  = 12000
	chatSkillDefaultBudget = 4000
)

// capSkillRunes 按 manifest 声明的 budget 截断技能正文，返回实际注入 rune 与是否节选。
// budget 非正时退回缺省值；如实标注由调用方负责（与写章技法包同口径）。
func capSkillRunes(content string, budget, fallback int) ([]rune, bool) {
	if budget <= 0 {
		budget = fallback
	}
	runes := []rune(content)
	if len(runes) > budget {
		return runes[:budget], true
	}
	return runes, false
}

// buildAutoSkillInject 依据用户消息的意图匹配，自动构建应注入的技能正文。
// 仅对 auto 技能注入完整内容（确定性预路由，让 LLM 不再靠猜目录）；
// 对 manual 技能（如总调度 next）注入为「建议类」reminder，触发其执行；
// always 技能已常驻，跳过。#111 P1 起 auto ≤3 / manual ≤1，并受同类去重与
// 单轮总 rune 帽约束，防止过度注入稀释注意力。
// 命中写章总调度 next 时额外注入写章技法包（固定 3 个 + 按章况命中的条件项）。
func (a *App) buildAutoSkillInject(ctx context.Context, novelID int64, message, providerName, modelID string) string {
	inject, _ := a.buildAutoSkillInjectWithNames(ctx, novelID, message, providerName, modelID)
	return inject
}

// buildAutoSkillInjectWithNames 与 buildAutoSkillInject 同义，额外返回本轮实际注入的
// 技能名（含写章技法包），供 chat:started 事件回传前端做可见徽标。
func (a *App) buildAutoSkillInjectWithNames(ctx context.Context, novelID int64, message, providerName, modelID string) (string, []string) {
	if a.skill == nil {
		return "", nil
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

	route := skill.RouteUserMessageSmart(ctx, all, message, chatMaxAutoSkills, chatMaxManualSkills, exclude, classifier)

	var b strings.Builder
	names := make([]string, 0, 4)
	// #111 P1：同类去重 + 单轮总 rune 帽（auto/manual 正文合并计算；技法包另按自身双封顶）。
	injectedRunes := 0
	seenCategory := make(map[string]bool)
	for _, m := range route.Auto {
		if m.Category != "" && seenCategory[m.Category] {
			continue
		}
		sk, ok := a.skill.Get(novelID, m.Name)
		if !ok {
			continue
		}
		runes, truncated := capSkillRunes(sk.Content, m.Budget, chatSkillDefaultBudget)
		if injectedRunes+len(runes) > chatAutoInjectRuneCap {
			break
		}
		injectedRunes += len(runes)
		if m.Category != "" {
			seenCategory[m.Category] = true
		}
		b.WriteString("<system-reminder>\n")
		fmt.Fprintf(&b, "用户消息触发了技能「%s」的自动加载（意图匹配）。请依据该技能定义与用户需求工作。\n\n---\n%s\n---\n", m.Name, string(runes))
		if truncated {
			b.WriteString("（技能正文节选，完整以技能原文为准）\n")
		}
		b.WriteString("</system-reminder>\n")
		names = append(names, m.Name)
		// #111 P0 统一账本：chat 意图通道每次正文注入记一条（只记标识与字符数）。
		activity.RecordSkillInvocation(ctx, activity.SkillInvocation{
			Skill: m.Name, Channel: "chat_intent", Mount: "chat_intent",
			NovelID: novelID, Runes: len(runes),
		})
	}
	writingTurn := false
	for _, m := range route.Manual {
		sk, ok := a.skill.Get(novelID, m.Name)
		if !ok {
			continue
		}
		// 总调度（next）必须注入完整正文：其阶段编排是写章 bundle 的上游依赖，
		// 不按 budget 截断，只计入单轮总帽（与 #108 行为逐字一致）。
		runes := []rune(sk.Content)
		if injectedRunes+len(runes) > chatAutoInjectRuneCap {
			break
		}
		injectedRunes += len(runes)
		intent := skill.FirstIntent(m)
		b.WriteString("<system-reminder>\n")
		fmt.Fprintf(&b, "检测到用户意图匹配技能「%s」（%s）。请启用该总调度/技能，按其对用户需求开展工作：\n\n---\n%s\n---\n", m.Name, intent, sk.Content)
		b.WriteString("</system-reminder>\n")
		names = append(names, m.Name)
		activity.RecordSkillInvocation(ctx, activity.SkillInvocation{
			Skill: m.Name, Channel: "chat_intent", Mount: "chat_intent",
			NovelID: novelID, Runes: len(runes),
		})
		if m.Name == "next" {
			writingTurn = true
		}
	}
	// #111 P1：internalOnly 命中只回一行指引，不注入正文（消掉 signability-fix 反向错投）。
	for _, m := range route.Redirects {
		fmt.Fprintf(&b, "<system-reminder>\n用户消息命中内部工作流技能「%s」。该类技能由检测任务/发现项工单的流水线调用，聊天窗口不注入其正文，以免污染输出格式。请在「检测任务 / 工单面板」完成对应操作；若要继续常规创作，直接描述写作需求即可。\n</system-reminder>\n", m.Name)
		activity.RecordSkillInvocation(ctx, activity.SkillInvocation{
			Skill: m.Name, Channel: "chat_redirect", Mount: "chat_redirect",
			NovelID: novelID,
		})
	}
	if writingTurn {
		if block, bundle := a.buildWritingSkillBundle(ctx, novelID, message, names); block != "" {
			b.WriteString(block)
			names = append(names, bundle...)
		}
	}
	if b.Len() > 0 {
		a.logger.Debug("意图预路由命中", "source", string(route.Source), "skills", strings.Join(names, ","))
	}
	return strings.TrimSpace(b.String()), names
}

// buildWritingSkillBundle 把写章技法正文按确定性顺序注入（去重、双封顶、如实标注节选）。
// 明确声明为「本轮必须落实的硬约束」，避免模型把总调度提醒读成可选建议后跳过编排。
func (a *App) buildWritingSkillBundle(ctx context.Context, novelID int64, message string, already []string) (string, []string) {
	seen := make(map[string]bool, len(already))
	for _, n := range already {
		seen[n] = true
	}
	// #111 P1：固定 3 个兜底 + 按章况命中的条件化技法。章况探测失败只退回固定 3 个，
	// 不新增 DB 强依赖；所有追加项仍逐个过单技能/整块双封顶。
	candidates := append([]string{}, writingBundleSkills...)
	candidates = append(candidates, conditionWritingSkillNames(a.detectWritingChapterContext(ctx, novelID, message))...)
	var b strings.Builder
	var names []string
	total := 0
	for _, name := range candidates {
		if seen[name] {
			continue
		}
		sk, ok := a.skill.Get(novelID, name)
		if !ok || strings.TrimSpace(sk.Content) == "" {
			continue
		}
		runes := []rune(sk.Content)
		truncated := false
		if len(runes) > writingBundleSkillCap {
			runes = runes[:writingBundleSkillCap]
			truncated = true
		}
		if total+len(runes) > writingBundleBlockCap {
			break
		}
		if len(names) > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "【写章技法·%s】\n%s", name, string(runes))
		if truncated {
			b.WriteString("\n（技法节选，完整以技能原文为准）")
		}
		total += len(runes)
		names = append(names, name)
		seen[name] = true
		// #111 P0 统一账本：写章技法包按实际注入的字符数逐技能记账（截断后的 runes）。
		activity.RecordSkillInvocation(ctx, activity.SkillInvocation{
			Skill: name, Channel: "writing_bundle", Mount: "writing_bundle",
			NovelID: novelID, Runes: len(runes),
		})
	}
	if len(names) == 0 {
		return "", nil
	}
	return "\n<system-reminder>\n写下一章时，以下技法是本轮**必须逐条落实**的硬约束（不是可选建议）；按列出顺序对照本章正文应用，不得跳过：\n\n" + b.String() + "\n</system-reminder>\n", names
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
// 例外：消息整体就是某个技能名时（如用户按 next.md 声明直接发「next」），那是触发词
// 而非「在句子里点名技能」；若照常排除，该技能会被自己排除而永不触发。
func explicitSkillNames(message string, all []skill.SkillMeta) map[string]bool {
	msg := strings.ToLower(message)
	trimmed := strings.TrimSpace(msg)
	set := make(map[string]bool)
	for _, m := range all {
		if m.Name == "" {
			continue
		}
		name := strings.ToLower(m.Name)
		if trimmed == name {
			continue
		}
		if strings.Contains(msg, name) {
			set[m.Name] = true
		}
	}
	return set
}
