package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/skill"
)

// TestSignabilityAuditE2E 是「能签」层的端到端集成验证：
//  1. 确定性 cue 路由——"帮我评估签约潜力" 必须零 LLM 开销直接命中 signability-audit；
//  2. 真实 deepseek 调用——把 signability-audit 技能正文作为系统提示，作用于一章样稿，
//     断言返回结构化签约体检（覆盖多个评分维度），证明该技能真正产出可用结果，而非泛泛而谈。
//
// 无 key 时 Skip（CI 默认跳过，不污染流水线；本地 dev 已配 key 时可真实运行）。
func TestSignabilityAuditE2E(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	store, err := skill.NewStore(logger, config.UserSkillsDir())
	if err != nil {
		t.Fatalf("skill store 初始化失败: %v", err)
	}
	all := store.ListMeta(0)
	if len(all) == 0 {
		t.Fatal("store 未加载任何内置 skill，无法执行 E2E")
	}

	// 1) 确定性 cue 路由（零 LLM 开销）
	det := skill.RouteUserMessage(all, "帮我评估签约潜力", 1, 1, nil)
	detName := ""
	if len(det.Manual) > 0 {
		detName = det.Manual[0].Name
	}
	if detName != "signability-audit" {
		t.Fatalf("确定性 cue 路由失败：\"帮我评估签约潜力\" 期望命中 signability-audit，实际 %q", detName)
	}
	t.Logf("OK 确定性 cue 路由：\"帮我评估签约潜力\" -> %s (source=cue, 零 LLM 开销)", detName)

	// 2) 真实 LLM 体检
	cfg := loadEvalLLMConfig(t)
	var hasKey bool
	for _, p := range cfg.Providers {
		if p.APIKey != "" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		t.Skip("LLM 配置中无可用 key，跳过真实 signability-audit 体检（设置 NOVELGO_EVAL_KEY 或配置 dev key 可运行）")
	}

	providers := llm.Merge(llm.Builtin, cfg)
	llmClient := llm.NewClient(providers, logger)

	// 选一个能力较强的模型做体检：优先 reasoner，其次 chat，避免 flash/lite 轻量模型丢细节
	var pn, mid string
	for name, p := range providers {
		if len(p.Models) == 0 {
			continue
		}
		pick := ""
		for _, m := range p.Models {
			if strings.Contains(strings.ToLower(m.ID), "reasoner") {
				pick = m.ID
				break
			}
		}
		if pick == "" {
			for _, m := range p.Models {
				if strings.Contains(strings.ToLower(m.ID), "chat") {
					pick = m.ID
					break
				}
			}
		}
		if pick == "" {
			pick = p.Models[0].ID
		}
		pn, mid = name, pick
		break
	}
	t.Logf("使用 provider=%s model=%s 进行真实签约体检", pn, mid)

	sk, ok := store.Get(0, "signability-audit")
	if !ok {
		t.Fatal("未找到 signability-audit 技能")
	}

	// 一段刻意带明显短板（开场温、无钩子）的样稿，用来检验体检是否真能揪出致命伤
	sampleChapter := `第一章 废脉少年

林墨站在测灵石前，周围同门窃窃私语。石台寂然不动，没有半点灵光透出。

"又是废脉。"执事长老摇头，"三年了，一丝灵气都引不动。"

林墨攥紧拳头。他记得昨夜梦中，一道古老烙印在胸口发烫，却怎么也想不起细节。远处，天骄弟子乘坐飞舟掠过，留下一路哄笑。

他低头看着掌心——那里，有一道几乎看不见的灰色纹路，正缓缓游动。`

	sysPrompt := "你是一名资深网文平台编辑，请严格依据以下《签约级体检》方法论，对用户提供的小说章节做签约潜力评估。\n\n" + sk.Content
	userPrompt := "请按方法论的评分维度，对下面这一章做体检，逐维打分（1-5）并给出证据与致命伤。\n\n【待评估章节】\n" + sampleChapter

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := llmClient.GenerateText(ctx, pn, []map[string]any{
		{"role": "system", "content": sysPrompt},
		{"role": "user", "content": userPrompt},
	}, mid)
	if err != nil {
		if isLLMInfraError(err) {
			// 供应商侧/账户类错误（欠费/鉴权/限流/5xx/网络）属基础设施问题，不应阻断 CI。
			t.Skipf("signability-audit LLM 调用失败（供应商侧/基础设施错误，非代码回归，跳过）: %v", err)
		}
		t.Fatalf("signability-audit LLM 调用失败: %v", err)
	}

	t.Logf("===== signability-audit 真实输出（provider=%s model=%s）=====\n%s", pn, mid, out)

	// 断言：覆盖多个评分维度 —— 证明技能真正驱动了结构化体检，而非泛泛而谈
	dims := []string{"开篇过签力", "题材契合", "人设卖点", "节奏", "爽点", "设定自洽", "长线牵引", "合规", "致命伤"}
	hits := 0
	for _, d := range dims {
		if strings.Contains(out, d) {
			hits++
		}
	}
	if hits < 4 {
		t.Errorf("signability-audit 输出未体现结构化体检（命中维度 %d/%d，期望>=4）：\n%s", hits, len(dims), out)
	}
	if len([]rune(out)) < 200 {
		t.Errorf("signability-audit 输出过短（%d 字），疑似未真正套用方法论", len([]rune(out)))
	}
	t.Logf("OK 签约体检结构化输出：命中维度 %d/%d，长度 %d 字", hits, len(dims), len([]rune(out)))
}
