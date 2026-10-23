package pipeline

// P0 WBS T2.4：流水线降级 golden 测试（Service 级，走 provider → buildBlocks → 构造器 全链）。
//
// 锁定降级契约（基线=testdata/p0_baseline_*.txt，改造前真实输出）：
//  ① voice/intent provider 为 nil → 五类提示词输出与改造前基线逐字一致；
//  ② voice 非空 → 只在尾部追加 Voice 块（Facts 块与前文不动）；
//  ③ intent 同理（且仅 rewrite 用途装配，audit/fix/patrol 不注）。

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/stretchr/testify/require"
)

// newBlocksTestService 构造只用于 buildBlocks 的 Service（无需真实 agent/llm）。
func newBlocksTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(newTestDB(t), nil, nil, nil, "", "", context.Background(), slog.Default())
}

// ① 未注入 voice/intent provider：五类提示词与改造前基线逐字一致（旧行为）。
func TestServiceBlocksNilProvidersMatchBaseline(t *testing.T) {
	svc := newBlocksTestService(t)
	svc.SetBibleProvider(func(context.Context, int64) (string, error) { return p0FactsMarker, nil })
	for _, tc := range p0PromptCases() {
		blocks := svc.buildBlocks(context.Background(), 1, tc.purpose)
		// voice/intent 未注入 → 只剩 Facts，与「仅 Facts」基线逐字一致。
		require.Equal(t, contextasm.ContextBlocks{Facts: p0FactsMarker}, blocks, "%s", tc.name)
		require.Equal(t, readP0Baseline(t, tc.name+"_facts"), tc.make(blocks), "%s", tc.name)
	}
	// provider 全未注入 → 与「空」基线逐字一致。
	svc2 := newBlocksTestService(t)
	for _, tc := range p0PromptCases() {
		blocks := svc2.buildBlocks(context.Background(), 1, tc.purpose)
		require.Equal(t, contextasm.ContextBlocks{}, blocks, "%s", tc.name)
		require.Equal(t, readP0Baseline(t, tc.name+"_empty"), tc.make(blocks), "%s", tc.name)
	}
}

// ② voice 非空 → 只在尾部追加 Voice 块（Facts 块与前文不动），引导语按 purpose（§0.6）。
func TestServiceBlocksVoiceAppendedTailOnly(t *testing.T) {
	const mind = "表达指纹：短句、克制、动词开头。"
	svc := newBlocksTestService(t)
	svc.SetBibleProvider(func(context.Context, int64) (string, error) { return p0FactsMarker, nil })
	svc.SetVoiceProvider(func(context.Context, int64) (string, error) { return mind, nil })

	for _, tc := range p0PromptCases() {
		blocks := svc.buildBlocks(context.Background(), 1, tc.purpose)
		require.Equal(t, mind, blocks.Voice, "%s", tc.name)
		got := tc.make(blocks)
		want := readP0Baseline(t, tc.name+"_facts") + voiceBlockTail(tc.purpose, mind)
		require.Equal(t, want, got, "voice 非空只在尾部追加 Voice 块：%s", tc.name)
	}
}

// ③ intent 非空 → 同理只在尾部追加 Intent 块；且仅 rewrite 用途装配（§0.5）。
func TestServiceBlocksIntentAppendedTailOnly(t *testing.T) {
	const intent = "卖点：凡人流修仙，稳健发育。"
	svc := newBlocksTestService(t)
	svc.SetBibleProvider(func(context.Context, int64) (string, error) { return p0FactsMarker, nil })
	svc.SetIntentProvider(func(context.Context, int64) (string, error) { return intent, nil })

	for _, tc := range p0PromptCases() {
		blocks := svc.buildBlocks(context.Background(), 1, tc.purpose)
		got := tc.make(blocks)
		if tc.purpose != contextasm.PurposeRewrite {
			require.Equal(t, contextasm.ContextBlocks{Facts: p0FactsMarker}, blocks, "%s 不装配 intent", tc.name)
			require.Equal(t, readP0Baseline(t, tc.name+"_facts"), got, "%s 不注 intent 块", tc.name)
			continue
		}
		want := readP0Baseline(t, tc.name+"_facts") +
			"\n\n【创作意图锚定】\n" + intent + "\n" +
			"以上是本书的创作意图与卖点定位，写作与校验以此为方向锚定；与之冲突时以意图为准调整执行细节，不得偏离卖点定位。"
		require.Equal(t, want, got, "intent 非空只在尾部追加 Intent 块：%s", tc.name)
	}
}

// provider 出错 → 该块降级不注入（其余块正常），提示词与未接该块逐字一致。
func TestServiceBlocksProviderErrorDegrades(t *testing.T) {
	svc := newBlocksTestService(t)
	svc.SetBibleProvider(func(context.Context, int64) (string, error) { return p0FactsMarker, nil })
	svc.SetVoiceProvider(func(context.Context, int64) (string, error) { return "", errors.New("persona 解析失败") })
	svc.SetIntentProvider(func(context.Context, int64) (string, error) { return "", errors.New("intent 读取失败") })

	for _, tc := range p0PromptCases() {
		blocks := svc.buildBlocks(context.Background(), 1, tc.purpose)
		require.Equal(t, contextasm.ContextBlocks{Facts: p0FactsMarker}, blocks, "%s", tc.name)
		require.Equal(t, readP0Baseline(t, tc.name+"_facts"), tc.make(blocks),
			"provider 出错降级后提示词与未接该块逐字一致：%s", tc.name)
	}
}

// voiceBlockTail 拼出 Voice 块的尾部追加部分（块格式 golden 与 contextasm 一致）。
func voiceBlockTail(purpose contextasm.Purpose, mind string) string {
	guide := "以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。"
	if purpose == contextasm.PurposeRewrite {
		guide = "以下创作心智是本书的文风基准。重写时保持该叙事习惯、表达指纹与创作决策启发式，" +
			"**不得因重写造成文风漂移**；解决既有问题优先于改写风格。"
	}
	return "\n\n【写作人格 · 文风锚定】\n\n" + guide + "\n\n" + mind
}
