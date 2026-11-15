package mcp_tools

// 显式章节 L1 推理预算接线契约。
//
// 背景：agent.RunOptions.MaxTokens 与 agent.Run 的 callOpts.MaxTokens 接线早已存在
// （巡检/复验在用），但 RunSubAgent 构造 subOpts 时没透传、SubAgentRequest 也没有
// 该字段，导致显式 L1 回落到模型 MaxOutputTokens（MiMo 为 128k）——完全无帽，
// 单条思维链可膨胀到几十分钟。本次补齐三段接线，只给显式 L1 设帽。
//
// #362「回执前置 + 小帽 + 截断抢救」：首轮 12k 小帽配合回执前置逼思维收敛；
// 调用成功但拿不到回执时，回落 48k 完整预算复跑一次（预算层截断抢救）。
// 派发失败/超时不重试，其余链路零波及。帽值数据依据见 chapter_ci.go 常量注释。

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

// TestChapterCIL1BudgetCoversObservedThinking 两级预算边界：
//   - 首轮小帽必须明显小于历史最大成功思维量，才是一次真正的「逼收敛」试验；
//   - 回落预算必须完整覆盖历史最大成功样本，否则试验失败时本版 L1 会停在 pending；
//   - 两者都必须真的小于模型无帽回落值，否则等于没设帽。
func TestChapterCIL1BudgetCoversObservedThinking(t *testing.T) {
	const largestObservedSuccess = 36_150 // 2026-11-15 21:43 chapterci 成功回执的 output token
	const modelMaxOutput = 128_000        // mimo-v2.6-flash MaxOutputTokens（无帽回落值）

	assert.Less(t, chapterCIMaxTokens, largestObservedSuccess,
		"首轮小帽必须小于历史最大成功思维量，才是真正的收敛试验")
	assert.Greater(t, chapterCIFallbackMaxTokens, largestObservedSuccess,
		"回落预算必须覆盖历史最大成功样本，否则回执会在生成之前被截断")
	assert.Less(t, chapterCIFallbackMaxTokens, modelMaxOutput,
		"回落预算也必须比模型无帽回落值小，否则等于没设")
	assert.Less(t, chapterCIMaxTokens, chapterCIFallbackMaxTokens,
		"两级预算必须严格递增，回落才有意义")
	assert.Equal(t, 12_000, chapterCIMaxTokens, "首轮小帽，改动需重跑数据核算")
	assert.Equal(t, 48_000, chapterCIFallbackMaxTokens, "与 patrolSynthesizeMaxTokens 同档")

	ci := readL1BudgetSource(t, "chapter_ci.go")
	assert.Contains(t, ci, "const chapterCIMaxTokens = 12_000")
	assert.Contains(t, ci, "const chapterCIFallbackMaxTokens = 48_000")
	assert.Contains(t, ci, "36,150", "数据依据必须留在注释里，避免拍脑袋调帽")
	assert.Contains(t, ci, "req.MaxTokens = chapterCIFallbackMaxTokens",
		"预算层截断抢救接线不得退役")
	assert.Contains(t, ci, "MaxTokens:   chapterCIMaxTokens", "首轮必须带小帽")
	assert.Contains(t, ci, "ctx.Err() == nil", "ctx 已取消时不得复跑")
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
