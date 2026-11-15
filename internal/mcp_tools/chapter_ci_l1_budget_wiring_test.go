package mcp_tools

// 显式章节 L1 推理预算接线契约。
//
// 背景：agent.RunOptions.MaxTokens 与 agent.Run 的 callOpts.MaxTokens 接线早已存在
// （巡检/复验在用），但 RunSubAgent 构造 subOpts 时没透传、SubAgentRequest 也没有
// 该字段，导致显式 L1 回落到模型 MaxOutputTokens（MiMo 为 128k）——完全无帽，
// 单条思维链可膨胀到几十分钟。本次补齐三段接线，只给显式 L1 设帽。
//
// 帽值的数据依据与「不得调小」的边界见 chapterCIMaxTokens 注释：历史成功收口样本
// 最大 output 36,150 token，回执在回复末尾，帽 < 36,150 会把回执在生成之前截断。

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readL1BudgetSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取 L1 预算接线源文件 %s", rel)
	// Windows CRLF 归一后再断言（项目既有静态断言纪律）。
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// TestChapterCIL1BudgetWiring 三段接线必须同时成立，缺一段即回到无帽回落。
func TestChapterCIL1BudgetWiring(t *testing.T) {
	base := readL1BudgetSource(t, "base.go")
	agent := readL1BudgetSource(t, "../agent/agent.go")
	ci := readL1BudgetSource(t, "chapter_ci.go")

	assert.Contains(t, base, "MaxTokens int", "SubAgentRequest 必须暴露可选输出预算字段")
	assert.Contains(t, agent, "MaxTokens:       req.MaxTokens", "RunSubAgent 必须把请求预算透传进 subOpts")
	assert.Contains(t, ci, "MaxTokens:   chapterCIMaxTokens", "显式 L1 执行体必须设置章节专用帽")
	assert.Contains(t, agent, "if opts.MaxTokens > 0 {", "agent.Run → callOpts.MaxTokens 接线不得退役")
	assert.Contains(t, agent, "callOpts.MaxTokens = &mt")

	stream := readL1BudgetSource(t, "../llm/stream.go")
	assert.Contains(t, stream, "maxTokens := 64000")
	assert.Contains(t, stream, "opts.MaxTokens != nil")
	mimo := readL1BudgetSource(t, "../llm/hooks_mimo.go")
	assert.Contains(t, mimo, `payload["max_completion_tokens"] = mt`,
		"MiMo 思考+回答共享预算的映射不得退役")
}

// TestChapterCIL1BudgetCoversObservedThinking 帽值必须覆盖全部历史成功样本，
// 防止后人以「提速」为由调小，把 16m31s 的成功变成回执被截断的失败。
func TestChapterCIL1BudgetCoversObservedThinking(t *testing.T) {
	const largestObservedSuccess = 36_150 // 2026-11-15 21:43 chapterci 成功回执的 output token
	const modelMaxOutput = 128_000        // mimo-v2.6-flash MaxOutputTokens（无帽回落值）

	assert.Greater(t, chapterCIMaxTokens, largestObservedSuccess,
		"帽必须大于历史最大成功 thinking 量，否则回执会在末尾被截掉")
	assert.Less(t, chapterCIMaxTokens, modelMaxOutput,
		"帽必须真的比模型无帽回落值小，否则等于没设")
	assert.Equal(t, 48_000, chapterCIMaxTokens, "与 patrolSynthesizeMaxTokens 同档，改动需重跑数据核算")

	ci := readL1BudgetSource(t, "chapter_ci.go")
	assert.Contains(t, ci, "const chapterCIMaxTokens = 48_000")
	assert.Contains(t, ci, "36,150", "数据依据必须留在注释里，避免拍脑袋调帽")
}

// TestChapterCIL1BudgetScopeIsExplicitDetectOnly 主 chat / 写章 / 前台 run_subagent /
// 编辑反馈 / 巡检 / 复验都不吃这个帽：其余构造点不得出现 MaxTokens。
func TestChapterCIL1BudgetScopeIsExplicitDetectOnly(t *testing.T) {
	for _, rel := range []string{"feedback_tools.go", "subagent_tools.go"} {
		src := readL1BudgetSource(t, rel)
		assert.NotContains(t, src, "MaxTokens", "%s 不得被章节 L1 预算波及", rel)
	}
	// 后台 RunOptions 只负责模型来源，不设全局帽（否则会波及所有子 Agent）。
	bg := readL1BudgetSource(t, "../../app/background_agent.go")
	assert.NotContains(t, bg, "MaxTokens", "backgroundRunOptions 不得设全局帽")

	// A 方案纪律不受影响：写后钩子仍零后台 LLM，L1 仍只由显式入口触发。
	rw := readL1BudgetSource(t, "rw_tools.go")
	assert.NotContains(t, rw, "RunChapterCIForChapter(")
	assert.Contains(t, rw, "ScheduleChapterCI(ctx, tc, ch, proposed)")
}
