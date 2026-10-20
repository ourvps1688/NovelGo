package pipeline

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/skill"
)

// agentTypeBlueprint 是蓝图规划 Agent 在 agent.Run 中的 AgentType 字符串。
const agentTypeBlueprint = "blueprint"

// agentTypeSignability / agentTypeFix 分别是签约体检 / 修复执行 Agent 的 AgentType 字符串。
// agentTypeBlueprintFix 是蓝图修复 Agent 的 AgentType 字符串（限权：只改卷纲 / 补卷）。
const (
	agentTypeSignability  = "signability"
	agentTypeFix          = "fix"
	agentTypeBlueprintFix = "blueprintfix"
)

// skillAgentType 按 skill 名返回其在 agent.Run 中使用的 AgentType。
// 蓝图规划走 BlueprintAgent（可建卷），签约体检走 SignabilityAgent（只读），
// 修复执行走 FixAgent（可改稿 + 同步台账）；未知 skill 默认 BlueprintAgent。
func skillAgentType(name string) agentcfg.AgentType {
	switch name {
	case "signability-audit":
		return agentcfg.SignabilityAgent
	case "signability-fix":
		return agentcfg.FixAgent
	case "blueprint-audit":
		return agentcfg.SignabilityAgent // 复用只读白名单
	case "blueprint-fix":
		return agentcfg.BlueprintFixAgent // 限权：只改卷纲 / 补卷
	case "blueprint-drift-check":
		return agentcfg.SignabilityAgent // 复用只读白名单（G3 蓝图漂移巡检）
	case "story-blueprint":
		return agentcfg.BlueprintAgent
	default:
		return agentcfg.BlueprintAgent
	}
}

// skillAgentTypeString 返回 skillAgentType 对应的 agent.Run AgentType 字符串。
func skillAgentTypeString(name string) string {
	switch name {
	case "signability-audit":
		return agentTypeSignability
	case "signability-fix":
		return agentTypeFix
	case "blueprint-audit":
		return agentTypeSignability // 复用签约体检 AgentType 字符串（同属只读体检）
	case "blueprint-fix":
		return agentTypeBlueprintFix
	case "blueprint-drift-check":
		return agentTypeSignability // 复用只读体检 AgentType（G3 蓝图漂移巡检）
	case "story-blueprint":
		return agentTypeBlueprint
	default:
		return agentTypeBlueprint
	}
}

// SkillInput 是 RunSkill 的输入：编排给该 skill 的具体任务 + 轮数上限 + 落库 session。
type SkillInput struct {
	NovelID   int64
	Prompt    string
	Whitelist []string // 预留：RunSkill 内部统一使用 agentcfg.BlueprintAgent 白名单
	MaxTurns  int
	// SessionID 用于多轮会话落库（选项 a）。为空时内部生成，便于审计与回放。
	SessionID string
}

// SkillOutput 是 RunSkill 的产物文本与结构化产物。
type SkillOutput struct {
	Text      string
	Artifacts map[string]string
}

// RunSkill 把 builtin skill 正文作 system prompt，跑 internal/agent.Run 的多轮 agent loop 并返回产物。
//
// 与 PR1 的单轮 GenerateText 骨架不同，这里走完整 agent 循环：挂载 blueprint 白名单工具
// （create_volume / read / list_volumes / edit-卷纲），由 AgentType="blueprint" 驱动，
// 多轮内可调用 create_volume 真正建卷落库。对外签名在 PR1 基础上把 llmClient 前置为
// *agent.Agent（多轮循环需要 Agent 的 registry/db/工具执行能力），其余语义保持不变。
//
// 会话消息按 SessionID 落库到 session.messages（独立 pipeline session），可追溯、可回放。
func RunSkill(ctx context.Context, ag *agent.Agent, skillStore *skill.Store, llmClient *llm.Client, providerName, modelID, name string, input SkillInput) (SkillOutput, error) {
	if ag == nil {
		return SkillOutput{}, fmt.Errorf("skill %q 执行失败: agent 未初始化", name)
	}
	sk, ok := skillStore.Get(0, name)
	if !ok {
		return SkillOutput{}, fmt.Errorf("skill %q 未找到", name)
	}

	at := skillAgentType(name)
	sysPrompt := agentcfg.AgentIdentity(at) + "\n\n# 本次任务方法论\n" + sk.Content

	msgs := []map[string]any{
		{"role": "system", "content": sysPrompt},
		{"role": "user", "content": input.Prompt},
	}

	model, ok := llmClient.ProviderModel(providerName, modelID)
	if !ok {
		return SkillOutput{}, fmt.Errorf("模型未找到: %s/%s", providerName, modelID)
	}

	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = "pipeline:" + uuid.NewString()
	}

	opts := agent.RunOptions{
		TurnID:       1,
		SessionID:    sessionID,
		NovelID:      input.NovelID,
		Messages:     msgs,
		AllowedTools: agentcfg.Allowlist(at),
		Model:        model,
		ProviderName: providerName,
		AgentType:    skillAgentTypeString(name),
		MaxTurns:     input.MaxTurns,
		// pipeline 会话事件走独立前缀（pipeline-agent:1），前端流水线视图监听它展示
		// S5 修复等阶段的活动流；主聊天仍用默认 "agent:"，互不干扰。
		EventPrefix: "pipeline-agent:",
	}
	if opts.MaxTurns <= 0 {
		opts.MaxTurns = 12
	}

	result, err := ag.Run(ctx, opts)
	if err != nil {
		return SkillOutput{}, fmt.Errorf("skill %q 执行失败: %w", name, err)
	}
	return SkillOutput{Text: result.FinalText, Artifacts: map[string]string{}}, nil
}
