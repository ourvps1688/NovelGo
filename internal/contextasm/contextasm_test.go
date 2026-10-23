package contextasm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// 本文件是 P0 WBS T1.2 装配器降级纪律单测：六类用例全部逐字断言（string equal，非 contains），
// 块文案以字面量 golden 锁定（文案单源被改动时此处必须同步失败）。
//
// ① 三 provider 全 nil → 空
// ② 单块非空只渲染该块
// ③ 多块顺序 Facts → Voice → Intent
// ④ 超预算截断 + 「（已截断）」标注
// ⑤ provider error → 该块空、其余正常
// ⑥ purpose 只影响 Voice 引导语（§0.6 表驱动）
// 另含纪律 2/4 的空串降级、非法 UTF-8 整块放弃、chat 路径 golden 锁定。

// —— golden 文案字面量（与 blocks.go / agentcfg.persona.go 逐字一致；勿引用常量拼装，否则锁不住文案）——

const (
	goldenFactsBlock = "【已冻结卷既定事实（系列圣经快照）】\n{body}\n" +
		"以上是已冻结（定稿）卷在最近一次冻结时固化的系列圣经，是本次校验/修复的**既定事实依据**：\n" +
		"与之冲突的一律改本卷及后续来适配它（前向修复），**不得**为消解冲突回改已冻结卷；" +
		"与之不冲突的部分以正文与台账工具读到的现状为准。"

	goldenGuideChat = "以下创作心智将指导你起草本轮新章节。请在保持故事世界一致性的前提下，" +
		"以该作者的叙事习惯、表达指纹与创作决策启发式来写作，而不是简单模仿或套用公式。"
	goldenGuideRewrite = "以下创作心智是本书的文风基准。重写时保持该叙事习惯、表达指纹与创作决策启发式，" +
		"**不得因重写造成文风漂移**；解决既有问题优先于改写风格。"
	goldenGuideReference = "以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。"

	goldenIntentBlock = "【创作意图锚定】\n{body}\n" +
		"以上是本书的创作意图与卖点定位，写作与校验以此为方向锚定；与之冲突时以意图为准调整执行细节，不得偏离卖点定位。"

	goldenHonestyNote = "注意：以下创作心智推断自虚构文本，仅供参考，不得作为真实作者意图的断言。"
)

// wantFacts / wantVoice / wantIntent 按 golden 模板拼出单块渲染期望值。
func wantFacts(body string) string {
	return strings.Replace(goldenFactsBlock, "{body}", body, 1)
}

func wantVoice(header, guide, body string) string {
	return header + "\n\n" + guide + "\n\n" + body
}

func wantIntent(body string) string {
	return strings.Replace(goldenIntentBlock, "{body}", body, 1)
}

// constProvider 返回固定文本的 provider。
func constProvider(text string) TextProvider {
	return func(context.Context, int64) (string, error) { return text, nil }
}

// errProvider 返回固定错误的 provider（error 链断言用）。
func errProvider(err error) TextProvider {
	return func(context.Context, int64) (string, error) { return "", err }
}

// panicProvider 证明该 provider 绝不应被调用（purpose 矩阵不越界取块）。
func panicProvider() TextProvider {
	return func(context.Context, int64) (string, error) { panic("provider 不应被调用") }
}

// —— ① 三 provider 全 nil → 空（提示词与未接任何块逐字一致）——

func TestBuild_AllNilProviders(t *testing.T) {
	a := NewAssembler(nil, nil, nil)
	for _, purpose := range []Purpose{PurposeChat, PurposeRewrite, PurposeAudit, PurposeFix, PurposePatrol} {
		blocks, err := a.Build(context.Background(), 1, purpose)
		require.NoError(t, err)
		require.Equal(t, ContextBlocks{}, blocks)
		require.Equal(t, "", blocks.Render(purpose))
	}
}

// —— ② 单块非空只渲染该块（其余块零输出，不带多余分隔）——

func TestRender_SingleNonEmptyBlock(t *testing.T) {
	t.Run("仅 Facts", func(t *testing.T) {
		blocks := ContextBlocks{Facts: "既定事实正文"}
		require.Equal(t, "\n\n"+wantFacts("既定事实正文"), blocks.Render(PurposeAudit))
	})
	t.Run("仅 Voice", func(t *testing.T) {
		blocks := ContextBlocks{Voice: "心智正文"}
		require.Equal(t, "\n\n"+wantVoice("【写作人格 · 文风锚定】", goldenGuideReference, "心智正文"),
			blocks.Render(PurposeAudit))
	})
	t.Run("仅 Intent", func(t *testing.T) {
		blocks := ContextBlocks{Intent: "意图正文"}
		require.Equal(t, "\n\n"+wantIntent("意图正文"), blocks.Render(PurposeRewrite))
	})
	t.Run("Build 单 provider 只出一块", func(t *testing.T) {
		blocks, err := NewAssembler(constProvider("F"), nil, nil).
			Build(context.Background(), 1, PurposeAudit)
		require.NoError(t, err)
		require.Equal(t, ContextBlocks{Facts: "F"}, blocks)
		require.Equal(t, "\n\n"+wantFacts("F"), blocks.Render(PurposeAudit))
	})
}

// —— ③ 多块顺序固定 Facts → Voice → Intent（硬事实在前、软约束在后）——

func TestRender_MultiBlockOrder(t *testing.T) {
	blocks := ContextBlocks{Facts: "F", Voice: "V", Intent: "I"}
	got := blocks.Render(PurposeRewrite)
	want := "\n\n" + wantFacts("F") +
		"\n\n" + wantVoice("【写作人格 · 文风锚定】", goldenGuideRewrite, "V") +
		"\n\n" + wantIntent("I")
	require.Equal(t, want, got)
}

// —— ④ 超预算截断 + 「（已截断）」标注（截断保 rune 边界）——

func TestFitBudget_Truncate(t *testing.T) {
	t.Run("超限按字节截断加标注", func(t *testing.T) {
		require.Equal(t, "abcd（已截断）", fitBudget("abcdef", 4))
	})
	t.Run("未超限原样返回不加标注", func(t *testing.T) {
		require.Equal(t, "abc", fitBudget("abc", 3))
		require.Equal(t, "abc", fitBudget("abc", 10))
	})
	t.Run("budget<=0 视为不限额", func(t *testing.T) {
		require.Equal(t, "abcdef", fitBudget("abcdef", 0))
		require.Equal(t, "abcdef", fitBudget("abcdef", -1))
	})
	t.Run("截断点回退到 rune 边界", func(t *testing.T) {
		// 「你」占 3 字节，budget=5 落在「好」中间 → 回退到「你」之后。
		require.Equal(t, "你（已截断）", fitBudget("你好世界", 5))
		require.True(t, utf8.ValidString(fitBudget("你好世界", 5)))
	})
	t.Run("标注计入结果尾部", func(t *testing.T) {
		require.Equal(t, "ab（已截断）", fitBudget("abc", 2))
		require.Equal(t, 2+len("（已截断）"), len(fitBudget("abc", 2)))
	})
}

func TestBuild_BudgetTruncation(t *testing.T) {
	t.Run("Voice 超 8KB 截断加标注", func(t *testing.T) {
		over := strings.Repeat("a", VoiceBudgetBytes+10)
		blocks, err := NewAssembler(nil, constProvider(over), nil).
			Build(context.Background(), 1, PurposeAudit)
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("a", VoiceBudgetBytes)+"（已截断）", blocks.Voice)
	})
	t.Run("Intent 超 8KB 截断加标注", func(t *testing.T) {
		over := strings.Repeat("心", IntentBudgetBytes) // 3×8192 字节，必超限
		blocks, err := NewAssembler(nil, nil, constProvider(over)).
			Build(context.Background(), 1, PurposeRewrite)
		require.NoError(t, err)
		want := strings.Repeat("心", IntentBudgetBytes/3) + "（已截断）"
		require.Equal(t, want, blocks.Intent)
		require.True(t, utf8.ValidString(blocks.Intent))
	})
	t.Run("未超限不加标注", func(t *testing.T) {
		blocks, err := NewAssembler(nil, constProvider("短正文"), constProvider("短意图")).
			Build(context.Background(), 1, PurposeRewrite)
		require.NoError(t, err)
		require.Equal(t, "短正文", blocks.Voice)
		require.Equal(t, "短意图", blocks.Intent)
	})
	t.Run("Facts 预算不在装配器执行（现状不变）", func(t *testing.T) {
		big := strings.Repeat("史", FactsBudgetBytes) // 3×24576 字节，远超 24KB
		blocks, err := NewAssembler(constProvider(big), nil, nil).
			Build(context.Background(), 1, PurposeAudit)
		require.NoError(t, err)
		require.Equal(t, big, blocks.Facts)
	})
}

// —— ⑤ provider error → 该块空、其余正常（error 聚合返回，只用于记日志）——

func TestBuild_ProviderError(t *testing.T) {
	t.Run("单块出错不影响其余块", func(t *testing.T) {
		sentinel := errors.New("voice provider down")
		blocks, err := NewAssembler(constProvider("F"), errProvider(sentinel), constProvider("I")).
			Build(context.Background(), 1, PurposeRewrite)
		require.Error(t, err)
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, ContextBlocks{Facts: "F", Intent: "I"}, blocks)
	})
	t.Run("多块出错错误聚合", func(t *testing.T) {
		errF := errors.New("facts down")
		errI := errors.New("intent down")
		blocks, err := NewAssembler(errProvider(errF), nil, errProvider(errI)).
			Build(context.Background(), 1, PurposeRewrite)
		require.Error(t, err)
		require.ErrorIs(t, err, errF)
		require.ErrorIs(t, err, errI)
		require.Equal(t, ContextBlocks{}, blocks)
	})
	t.Run("出错块降级后渲染与未接该块逐字一致", func(t *testing.T) {
		blocks, err := NewAssembler(nil, errProvider(errors.New("down")), nil).
			Build(context.Background(), 1, PurposeAudit)
		require.Error(t, err)
		require.Equal(t, "", blocks.Render(PurposeAudit))
	})
}

// —— ⑥ purpose 只影响 Voice 引导语（§0.6 表驱动；块头 chat 用 PersonaMarker）——

func TestRenderVoice_PurposeOnlyAffectsGuide(t *testing.T) {
	const mind = "心智正文"
	cases := []struct {
		purpose Purpose
		want    string
	}{
		{PurposeChat, wantVoice("【写作人格 · Persona】", goldenGuideChat, mind)},
		{PurposeRewrite, wantVoice("【写作人格 · 文风锚定】", goldenGuideRewrite, mind)},
		{PurposeAudit, wantVoice("【写作人格 · 文风锚定】", goldenGuideReference, mind)},
		{PurposeFix, wantVoice("【写作人格 · 文风锚定】", goldenGuideReference, mind)},
		{PurposePatrol, wantVoice("【写作人格 · 文风锚定】", goldenGuideReference, mind)},
		{Purpose("unknown"), wantVoice("【写作人格 · 文风锚定】", goldenGuideReference, mind)}, // 未知 purpose 同 audit
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, RenderVoice(tc.purpose, mind), "purpose=%s", tc.purpose)
	}
}

// —— §0.5 purpose×块矩阵：chat 只取 Voice；rewrite 三块全取；audit/fix/patrol/未知取 Facts+Voice ——

func TestBuild_PurposeBlockMatrix(t *testing.T) {
	cases := []struct {
		purpose Purpose
		want    ContextBlocks
	}{
		{PurposeChat, ContextBlocks{Voice: "V"}},
		{PurposeRewrite, ContextBlocks{Facts: "F", Voice: "V", Intent: "I"}},
		{PurposeAudit, ContextBlocks{Facts: "F", Voice: "V"}},
		{PurposeFix, ContextBlocks{Facts: "F", Voice: "V"}},
		{PurposePatrol, ContextBlocks{Facts: "F", Voice: "V"}},
		{Purpose("unknown"), ContextBlocks{Facts: "F", Voice: "V"}},
	}
	for _, tc := range cases {
		// chat 不得触碰 Facts/Intent provider（panicProvider 兜底证明）。
		facts, intent := TextProvider(constProvider("F")), TextProvider(constProvider("I"))
		if tc.purpose == PurposeChat {
			facts, intent = panicProvider(), panicProvider()
		}
		blocks, err := NewAssembler(facts, constProvider("V"), intent).
			Build(context.Background(), 1, tc.purpose)
		require.NoError(t, err)
		require.Equal(t, tc.want, blocks, "purpose=%s", tc.purpose)
	}
}

// —— 纪律 2：provider 返回空串 → 该块降级为空，无 error ——

func TestBuild_EmptyStringDegrades(t *testing.T) {
	blocks, err := NewAssembler(constProvider(""), constProvider("V"), constProvider("")).
		Build(context.Background(), 1, PurposeRewrite)
	require.NoError(t, err)
	require.Equal(t, ContextBlocks{Voice: "V"}, blocks)
}

// —— 纪律 4：非法 UTF-8 整块放弃（宁缺勿错），其余块正常 ——

func TestBuild_InvalidUTF8AbandonsBlock(t *testing.T) {
	bad := "前缀\xff后缀"
	require.False(t, utf8.ValidString(bad))
	blocks, err := NewAssembler(constProvider("F"), constProvider(bad), constProvider("I")).
		Build(context.Background(), 1, PurposeRewrite)
	require.Error(t, err)
	require.Equal(t, ContextBlocks{Facts: "F", Intent: "I"}, blocks)
}

// —— chat 路径 golden 锁定：RenderVoice(chat) 与 agentcfg.BuildPersonaSystemText 拼装格式逐字一致
//（PersonaMarker + "\n\n" + 引导语 + "\n\n" + 心智正文 + "\n\n" + 诚实边界声明；PersonaHonestyNote 由 provider 端拼进正文）——

func TestRenderVoice_ChatGolden(t *testing.T) {
	const content = "技能正文"
	mind := content + "\n\n" + goldenHonestyNote
	want := "【写作人格 · Persona】\n\n" + goldenGuideChat + "\n\n" + content + "\n\n" + goldenHonestyNote
	require.Equal(t, want, RenderVoice(PurposeChat, mind))
}

// —— 全空 Render 返回空串（提示词与未接任何块逐字一致）——

func TestRender_AllEmpty(t *testing.T) {
	require.Equal(t, "", ContextBlocks{}.Render(PurposeRewrite))
}
