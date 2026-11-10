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

// agentTypeSignability 是签约体检 Agent 的 AgentType 字符串。
// 后台自动修复执行体已整体下线：修复 / 蓝图修复 / 章节重写三类专用
// AgentType 字符串随 4 个退役技能一并移除。
// agentTypePatrol 是一致性巡检 Agent 的 AgentType 字符串（WBS 3.3：张力报告，只读）。
// agentTypeChapterCI 是章节 CI 检查员（R2 L1）的 AgentType 字符串（只读，只报告不改写）。
const (
	agentTypeSignability = "signability"
	agentTypePatrol      = "patrol"
	agentTypeChapterCI   = "chapterci"
)

// skillAgentType 按 skill 名返回其在 agent.Run 中使用的 AgentType。
// 蓝图规划走 BlueprintAgent（可建卷），签约体检走 SignabilityAgent（只读）；
// 修复类专用 Agent 随后台自动修复执行体下线，未知 skill 默认 BlueprintAgent。
func skillAgentType(name string) agentcfg.AgentType {
	switch name {
	case "signability-audit":
		return agentcfg.SignabilityAgent
	case "signability-verify":
		return agentcfg.SignabilityAgent // 增量复验只读（R1c）：复用只读体检白名单
	case "blueprint-audit":
		return agentcfg.SignabilityAgent // 复用只读白名单
	case "consistency-patrol":
		return agentcfg.ConsistencyPatrolAgent // 全书张力报告：只读、只报告不改写（WBS 3.3）
	case "chapter-ci":
		return agentcfg.ChapterCIAgent // 章节 CI（R2 L1）：只读增量质检、只报告不改写
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
	case "signability-verify":
		return agentTypeSignability // 增量复验（R1c）：同属只读体检 AgentType
	case "blueprint-audit":
		return agentTypeSignability // 复用签约体检 AgentType 字符串（同属只读体检）
	case "consistency-patrol":
		return agentTypePatrol // 一致性巡检（张力报告，只读）
	case "chapter-ci":
		return agentTypeChapterCI // 章节 CI（R2 L1）：只读增量质检
	case "story-blueprint":
		return agentTypeBlueprint
	default:
		return agentTypeBlueprint
	}
}

// SkillInput 是 RunSkill 的输入：编排给该 skill 的具体任务 + 轮数上限 + 落库 session。
type SkillInput struct {
	NovelID int64
	Prompt  string
	// Whitelist 非 nil 时覆盖默认工具白名单：传空集 = 工具零暴露（不给模型任何工具
	// schema，调用结构性不可能）。signability-verify 等「证据已内联」的调用传空集，
	// 单轮必出回执；nil 保持默认（skillAgentType 对应白名单）。
	Whitelist []string
	MaxTurns  int
	// MaxTokens 单次 LLM 调用输出 token 上限（0 = 模型默认）。巡检等单调用重任务用它
	// 压住思考模型的思维块膨胀，保证按时收口（详见 agent.RunOptions.MaxTokens）。
	MaxTokens int
	// SessionID 用于多轮会话落库（选项 a）。为空时内部生成，便于审计与回放。
	SessionID string
	// RunID 是本次派发归属的流水线 run（R0 观测用：按 run 聚合 S5 修复轮数等）。
	// 非 run 任务（如后台巡检）留空，不影响任何执行行为。
	RunID string
}

// SkillOutput 是 RunSkill 的产物文本与结构化产物。
type SkillOutput struct {
	Text      string
	Artifacts map[string]string
	// Turns 是 agent loop 实耗轮数（R0 观测用）；失败路径也回填已消耗轮数。
	Turns int
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

	allowed := agentcfg.Allowlist(at)
	if input.Whitelist != nil {
		allowed = make(map[string]bool, len(input.Whitelist))
		for _, t := range input.Whitelist {
			allowed[t] = true
		}
	}

	opts := agent.RunOptions{
		TurnID:       1,
		SessionID:    sessionID,
		NovelID:      input.NovelID,
		Messages:     msgs,
		AllowedTools: allowed,
		Model:        model,
		ProviderName: providerName,
		AgentType:    skillAgentTypeString(name),
		MaxTurns:     input.MaxTurns,
		MaxTokens:    input.MaxTokens,
		// pipeline 会话事件走独立前缀（pipeline-agent:1），前端流水线视图监听它展示
		// S5 修复等阶段的活动流；主聊天仍用默认 "agent:"，互不干扰。
		EventPrefix: "pipeline-agent:",
	}
	if opts.MaxTurns <= 0 {
		opts.MaxTurns = 12
	}

	result, err := ag.Run(ctx, opts)
	if err != nil {
		// 失败也回填已消耗轮数（R0 观测：失败前烧掉多少轮是瓶颈归因的关键数据）；
		// 调用方按既有纪律只看 err，零行为变化。
		return SkillOutput{Turns: result.TurnCount}, fmt.Errorf("skill %q 执行失败: %w", name, err)
	}
	return SkillOutput{Text: result.FinalText, Artifacts: map[string]string{}, Turns: result.TurnCount}, nil
}
