package pipeline

// P0 WBS T5.2：主观 A/B 验收（P0 唯一的「人感受」验收）。
//
// 同一章节样稿、同一 chapterRewritePrompt 任务文本、同一模型与系统提示，
// 唯一变量是 Voice 块开/关：
//
//	A 臂（voice 开）= ContextBlocks{Voice: 心智} → 提示词尾部含【写作人格 · 文风锚定】块
//	B 臂（voice 关）= ContextBlocks{} → 提示词与 P0 前逐字一致（golden 已锁）
//
// 对比两臂真实产出：①文风锚定语句生效、叙事习惯可见保持；
// ②关 voice 时与 P0 前行为无差别（提示词侧由 golden 逐字保证，产出侧无指纹佐证）。
// 无 key 时 Skip（CI 默认跳过）；A/B 结论（3 行内）记入 WBS §T5.2。
//
// 说明：真实 S3 路径经 agent loop 用 read/edit 工具执行；本测试是其「重写生成步」的
// 单发模拟——正文内联（插在任务文本与尾部块之间）、系统提示为 chapter-rewrite 方法论的
// 去工具化提炼，块仍尾接提示词末尾——两臂除 Voice 块外逐字相同。

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/stretchr/testify/require"
)

// abMind 合成创作心智：给定可机检的叙事习惯指纹（锚定语句），用于 A/B 归因。
const abMind = `叙事习惯（重写必须逐条保持）：
1. 短句白描：单句一般不超过 12 字，多用句号收句。
2. 心理活动一律写成「他心想：……」，不得用「他想」「心中暗想」「心道」「暗忖」等其他写法。
3. 禁用比喻：不得出现「仿佛」「犹如」「宛如」「就像」及任何「像……一样/似的」句式。
4. 对话极简：只用「『话』+某某说」，说前不加神态、动作、语气修饰。
5. 每段以一个具体物件或动作收束，不发议论、不抒情。`

// abSampleChapter 与 skill_audit_e2e_test.go 样稿同源（弱开场、含心理活动节拍），
// 便于重写时把心理活动显式成文（指纹「他心想」有落点）。
const abSampleChapter = `第一章 废脉少年

林墨站在测灵石前，周围同门窃窃私语。石台寂然不动，没有半点灵光透出。

"又是废脉。"执事长老摇头，"三年了，一丝灵气都引不动。"

林墨攥紧拳头。他记得昨夜梦中，一道古老烙印在胸口发烫，却怎么也想不起细节。远处，天骄弟子乘坐飞舟掠过，留下一路哄笑。

他低头看着掌心——那里，有一道几乎看不见的灰色纹路，正缓缓游动。`

// abFingerprintVoice 是 A 臂锚定生效的机检指纹（心智第 2 条强制句式）。
const abFingerprintVoice = "他心想"

func loadP0EvalLLMConfig(t *testing.T) *llm.UserLLMConfig {
	t.Helper()
	if key := os.Getenv("NOVELGO_EVAL_KEY"); key != "" {
		return &llm.UserLLMConfig{Providers: []llm.Provider{{Name: "deepseek", APIKey: key}}}
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		return &llm.UserLLMConfig{Providers: []llm.Provider{{Name: "deepseek", APIKey: key}}}
	}
	cfg, err := llm.LoadUserConfig(config.LLMConfigPath())
	if err != nil {
		t.Skipf("无法读取 dev LLM 配置（%v），跳过 voice A/B 真实验收", err)
	}
	return cfg
}

// abMetrics 双臂产出的可机检文风指标（供结论引用，不作硬断言）。
type abMetrics struct {
	sentences int
	avgLen    float64 // 平均句长（字/句）
	similes   int     // 比喻词出现次数（仿佛/犹如/宛如/就像/似的）
	finger    int     // 指纹「他心想」次数
}

func measureAB(out string) abMetrics {
	m := abMetrics{finger: strings.Count(out, abFingerprintVoice)}
	for _, w := range []string{"仿佛", "犹如", "宛如", "就像", "似的"} {
		m.similes += strings.Count(out, w)
	}
	total := 0
	for _, seg := range strings.FieldsFunc(out, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '!' || r == '?'
	}) {
		if n := utf8.RuneCountInString(strings.TrimSpace(seg)); n > 0 {
			m.sentences++
			total += n
		}
	}
	if m.sentences > 0 {
		m.avgLen = float64(total) / float64(m.sentences)
	}
	return m
}

func TestRewriteVoiceABE2E(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// —— 确定性部分：双臂提示词组装（先锁死唯一变量是 Voice 块，再烧 LLM 调用）——
	item := RewriteItem{ID: "I2", Level: "P1", Area: "节奏", Desc: "开场拖沓、缺乏钩子——保留原有情节与信息点的前提下压缩开场、提前抛出金手指暗示"}
	ch := chapter.Chapter{ID: 9, ReadingNumber: 1, Title: "第一章 废脉少年"}
	taskBase := chapterRewritePrompt(1, item, ch, contextasm.ContextBlocks{})
	bodyBlock := "\n\n【待重写章节正文】\n" + abSampleChapter +
		"\n\n【输出要求】本环境没有任何工具可调用，禁止输出任何工具调用；" +
		"请直接输出重写后的完整章节正文，只含叙事本身，不含章节号/标题/章末标记，不含任何解释、前后缀或代码块。"
	promptOff := taskBase + bodyBlock
	promptOn := taskBase + bodyBlock + contextasm.ContextBlocks{Voice: abMind}.Render(contextasm.PurposeRewrite)

	require.NotContains(t, promptOff, contextasm.VoiceHeader, "voice 关提示词不得含文风锚定块（=P0 前行为）")
	require.NotContains(t, promptOff, abFingerprintVoice, "voice 关提示词不得泄漏指纹句式")
	require.Equal(t, promptOff+contextasm.ContextBlocks{Voice: abMind}.Render(contextasm.PurposeRewrite),
		promptOn, "双臂唯一差异应为 Voice 块（尾部追加）")
	require.Contains(t, promptOn, contextasm.VoiceHeader, "voice 开提示词须含文风锚定块头")
	require.Contains(t, promptOn, contextasm.GuideRewrite, "voice 开提示词须含 rewrite 引导语")
	require.Contains(t, promptOn, abFingerprintVoice, "锚定语句（指纹句式）须随心智进入提示词")

	// —— 真实 LLM 部分（无 key Skip）——
	cfg := loadP0EvalLLMConfig(t)
	hasKey := false
	for _, p := range cfg.Providers {
		if p.APIKey != "" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		t.Skip("LLM 配置中无可用 key，跳过 voice A/B 真实验收（设置 NOVELGO_EVAL_KEY 或配置 dev key 可运行）")
	}

	providers := llm.Merge(llm.Builtin, cfg)
	llmClient := llm.NewClient(providers, logger)

	// 选模型：优先 reasoner → chat → pro，跳过 flash/lite（轻量模型丢细节）。
	var pn, mid string
	var allIDs []string
	for name, p := range providers {
		if len(p.Models) == 0 {
			continue
		}
		heavy := func(id string) bool {
			low := strings.ToLower(id)
			return !strings.Contains(low, "flash") && !strings.Contains(low, "lite")
		}
		pick := ""
		for _, prefer := range []string{"reasoner", "chat", "pro"} {
			for _, m := range p.Models {
				if strings.Contains(strings.ToLower(m.ID), prefer) && heavy(m.ID) {
					pick = m.ID
					break
				}
			}
			if pick != "" {
				break
			}
		}
		if pick == "" {
			for _, m := range p.Models {
				if heavy(m.ID) {
					pick = m.ID
					break
				}
			}
		}
		if pick == "" {
			pick = p.Models[0].ID
		}
		for _, m := range p.Models {
			allIDs = append(allIDs, m.ID)
		}
		pn, mid = name, pick
		break
	}
	require.NotEmpty(t, mid, "未找到可用模型")
	t.Logf("voice A/B 使用 provider=%s model=%s（可选：%v）", pn, mid, allIDs)

	// 系统提示 = chapter-rewrite 方法论的去工具化提炼（两臂共享，非 A/B 变量）。
	sysPrompt := "你是一名资深网文作者，负责按签约体检意见对章节做整章重写。" +
		"重写要真正解决问题（立人设/补爽点/修节奏/兑现伏笔），而不是润色表面；" +
		"保持与前后章节衔接、与既有设定一致。" +
		"正文格式：无破折号「——」、无序号段落分隔符、无 AI 味硬伤。" +
		"本环境没有任何工具可调用，禁止输出工具调用；请直接输出重写后的完整章节正文。"

	gen := func(prompt string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
		defer cancel()
		out, err := llmClient.GenerateText(ctx, pn, []map[string]any{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": prompt},
		}, mid)
		require.NoError(t, err, "voice A/B LLM 调用失败")
		require.NotEmpty(t, strings.TrimSpace(out), "LLM 返回空产出")
		return out
	}

	outOn := gen(promptOn)
	outOff := gen(promptOff)

	mOn, mOff := measureAB(outOn), measureAB(outOff)
	t.Logf("===== A 臂（voice 开）产出（%d 字，%d 句，均句长 %.1f 字，比喻 %d 次，指纹「%s」%d 次）=====\n%s",
		utf8.RuneCountInString(outOn), mOn.sentences, mOn.avgLen, mOn.similes, abFingerprintVoice, mOn.finger, outOn)
	t.Logf("===== B 臂（voice 关）产出（%d 字，%d 句，均句长 %.1f 字，比喻 %d 次，指纹「%s」%d 次）=====\n%s",
		utf8.RuneCountInString(outOff), mOff.sentences, mOff.avgLen, mOff.similes, abFingerprintVoice, mOff.finger, outOff)
	t.Logf("A/B 指标对照：均句长 %.1f vs %.1f；比喻 %d vs %d；指纹 %d vs %d",
		mOn.avgLen, mOff.avgLen, mOn.similes, mOff.similes, mOn.finger, mOff.finger)

	// ① 文风锚定语句生效：心智强制的指纹句式只在 A 臂出现。
	require.Contains(t, outOn, abFingerprintVoice,
		"voice 开产出须使用心智锚定的指纹句式「%s」（锚定语句生效）", abFingerprintVoice)
	// ② 关 voice 与 P0 前无差别：产出侧无 A 臂指纹（提示词侧逐字一致由 golden 保证）。
	require.NotContains(t, outOff, abFingerprintVoice,
		"voice 关产出不得出现 A 臂指纹句式（关 voice=旧行为）")
}
