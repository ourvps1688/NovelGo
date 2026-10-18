package pipeline

import (
	"context"
	"fmt"

	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/skill"
)

// SkillInput 是 RunSkill 的输入：编排给该 skill 的具体任务 + 工具白名单 + 轮数上限。
type SkillInput struct {
	NovelID   int64
	Prompt    string
	Whitelist []string
	MaxTurns  int
}

// SkillOutput 是 RunSkill 的产物文本与结构化产物。
type SkillOutput struct {
	Text      string
	Artifacts map[string]string
}

// RunSkill 把 builtin skill 正文作 system prompt，跑一次性 agent run 并返回产物。
//
// 注意：PR1 实现为「加载 + 构造 system prompt + 单次 LLM 生成」的先行骨架，
// 与 app/skill_audit_e2e_test.go 的范式一致，可确定性触发并回收产物。
// PR2/PR3 将把内部从单次 GenerateText 升级为 internal/agent.Run 的多轮 agent loop
// （挂载 input.Whitelist 工具、设置 AgentType/ParentTurnID），对外接口保持不变。
func RunSkill(ctx context.Context, skillStore *skill.Store, llmClient *llm.Client, providerName, modelID, name string, input SkillInput) (SkillOutput, error) {
	sk, ok := skillStore.Get(0, name)
	if !ok {
		return SkillOutput{}, fmt.Errorf("skill %q 未找到", name)
	}
	sysPrompt := "你是一名资深网文创作助手，请严格依据以下方法论执行任务。\n\n" + sk.Content
	msgs := []map[string]any{
		{"role": "system", "content": sysPrompt},
		{"role": "user", "content": input.Prompt},
	}
	out, err := llmClient.GenerateText(ctx, providerName, msgs, modelID)
	if err != nil {
		return SkillOutput{}, fmt.Errorf("skill %q 执行失败: %w", name, err)
	}
	return SkillOutput{Text: out, Artifacts: map[string]string{}}, nil
}
