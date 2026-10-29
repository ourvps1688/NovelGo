package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/skill"
)

// fallbackCase 是一条用于测量 LLM 兜底命中率的样例消息。
//   - expect: 期望路由到的技能名（文件名 stem）；negative=true 时表示期望“不命中任何技能”。
//   - 正样例刻意避开所有技能的确定性 cue 子串，以便真正走 LLM 兜底路径。
type fallbackCase struct {
	msg      string
	expect   string
	negative bool
}

// fallbackDataset 是人工编纂的模糊消息集，每个都对应一个明确意图但用口语化、不含 cue 的方式表达。
// 覆盖全部 17 个内置技能 + 若干负面（不应命中）样本。
var fallbackDataset = []fallbackCase{
	// worldbuilding-design
	{msg: "我想搭一套自洽的力量强弱逻辑和进阶路线，前后别矛盾", expect: "worldbuilding-design"},
	{msg: "两个国家打仗，我想让双方的兵力对比和科技水平都更有说服力", expect: "worldbuilding-design"},
	// character-design
	{msg: "这个新对手我想让他每次登场都透着危险感，但做事动机要合理可信", expect: "character-design"},
	{msg: "主角失忆了，我想后面慢慢揭开他不为人知的过去", expect: "character-design"},
	// story-blueprint
	{msg: "整本书从第一页到结局的大结构该怎么搭才不散，我怕写到后面收不回来", expect: "story-blueprint"},
	// opening-chapters
	{msg: "头几章怎么写才能让人忍不住一直读下去，别一上来就劝退", expect: "opening-chapters"},
	{msg: "新书刚发，前两章没人看，是不是开场太温了", expect: "opening-chapters"},
	// scene-beats
	{msg: "我写的这段打斗总觉得平，缺乏让人紧张起来的张力", expect: "scene-beats"},
	// pacing-control
	{msg: "这两章读着有点发闷，推进不够利落，读者容易划走", expect: "pacing-control"},
	{msg: "高潮那段我想让它一口气冲到底，别在中途泄气", expect: "pacing-control"},
	// plot-hook
	{msg: "每一节的末尾我想留个让人惦记下文的东西，勾着往下翻", expect: "plot-hook"},
	// dialogue-subtext
	{msg: "两个人说话总觉得太直白，话里没有藏着别的意思", expect: "dialogue-subtext"},
	{msg: "长辈训话那段，我想让每句话都带刺又不直接说破", expect: "dialogue-subtext"},
	// reader-immersion
	{msg: "描写总像在旁边报幕，读者没法跟着主角一同经历那些时刻", expect: "reader-immersion"},
	// deai-flavor
	{msg: "这段读起来很假，像是按公式凑出来的，没有活人该有的语感", expect: "deai-flavor"},
	{msg: "请把这段调得像是真人随手写的，别那么工整", expect: "deai-flavor"},
	// revision-pass
	{msg: "初稿写完了，我想整体过一遍把不顺的地方理平", expect: "revision-pass"},
	// signability-audit
	{msg: "我想知道这类题材在平台上受不受欢迎，有没有商业前景", expect: "signability-audit"},
	// brainstorm-composer
	{msg: "脑子空白，一点主意都冒不出来", expect: "brainstorm-composer"},
	// review
	{msg: "写完这章你帮我看看水平怎么样，挑挑毛病", expect: "review"},
	// next (manual)
	{msg: "按之前定好的走向，把后面的内容接着往下写", expect: "next"},
	// book-pitch (manual)
	{msg: "我想给这部书想一句让人忍不住点开去看的话", expect: "book-pitch"},
	// collect (manual)
	{msg: "动笔前我想先把整本书的线索和坑都拢一遍，免得后面穿帮", expect: "collect"},

	// ---- 负面样本：期望不命中任何技能 ----
	{msg: "你好", expect: "", negative: true},
	{msg: "今天心情不错", expect: "", negative: true},
	{msg: "帮我查下明天上海的天气预报", expect: "", negative: true},
	{msg: "谢谢你的帮忙", expect: "", negative: true},
	{msg: "这软件怎么导出文件", expect: "", negative: true},
	{msg: "我想写点东西", expect: "", negative: true},
}

// fallbackRow 是单条测量结果的展示结构。
type fallbackRow struct {
	msg      string
	expect   string
	routed   string
	source   string
	cueHit   bool
	correct  bool
	negative bool
}

// TestSkillFallbackHitRate 用真实 LLM（deepseek）测量兜底分类命中率。
// key 读取优先级：环境变量 NOVELGO_EVAL_KEY / DEEPSEEK_API_KEY → dev 端加密配置
// (~/.novelgo/llm_config.enc)。二者皆无时 t.Skip（CI 默认跳过，不污染流水线）。
//
// 命中率门槛：整体 <90% 或负面样本被误触发时，本测试以 t.Errorf 失败，
// 使「命中率回归」能在夜跑工作流中阻断，形成回归闸门的一部分。
func TestSkillFallbackHitRate(t *testing.T) {
	cfg := loadEvalLLMConfig(t)

	var hasKey bool
	for _, p := range cfg.Providers {
		if p.APIKey != "" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		t.Skip("LLM 配置中无可用 key，跳过真实兜底命中率测试（设置 NOVELGO_EVAL_KEY 或配置 dev key 可运行）")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	providers := llm.Merge(llm.Builtin, cfg)
	llmClient := llm.NewClient(providers, logger)
	store, err := skill.NewStore(logger, config.UserSkillsDir())
	if err != nil {
		t.Fatalf("skill store 初始化失败: %v", err)
	}

	// 选取带 key 的 provider 与其首个模型（优先 flash/lite/mini 等轻量模型提速）。
	var pn, mid string
	for _, p := range cfg.Providers {
		if p.APIKey == "" {
			continue
		}
		pn = p.Name
		if prov, ok := providers[pn]; ok {
			pick := ""
			for _, m := range prov.Models {
				low := strings.ToLower(m.ID)
				if strings.Contains(low, "flash") || strings.Contains(low, "lite") || strings.Contains(low, "mini") {
					pick = m.ID
					break
				}
			}
			if pick == "" && len(prov.Models) > 0 {
				pick = prov.Models[0].ID
			}
			mid = pick
		}
		break
	}
	t.Logf("使用 provider=%s model=%s 进行真实兜底分类", pn, mid)

	// 探针：先用一次最小调用探明 LLM 可达性。供应商侧错误（欠费/鉴权/限流/5xx/网络）
	// 属基础设施问题，不应阻断 CI —— 直接 Skip；API 可达时再跑完整命中率测量
	// （命中率门槛等断言仍保留，能抓住真正的路由/分类回归）。
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	_, probeErr := llmClient.GenerateText(probeCtx, pn, []map[string]any{
		{"role": "user", "content": "ping"},
	}, mid)
	probeCancel()
	if probeErr != nil {
		if isLLMInfraError(probeErr) {
			t.Skipf("LLM 供应商侧不可达（%v），跳过真实兜底命中率测试（非代码回归，属基础设施/账户问题）", probeErr)
		}
		t.Fatalf("LLM 探针调用失败（疑似配置/代码问题）: %v", probeErr)
	}

	a := &App{llmClient: llmClient, skill: store, logger: logger}
	classifier := func(ctx context.Context, m string, cands []skill.IntentCandidate) (string, float64) {
		return a.classifyIntent(ctx, m, cands, pn, mid)
	}

	all := store.ListMeta(0)
	// 校验数据集中期望的技能名真实存在。
	nameSet := make(map[string]bool)
	for _, m := range all {
		nameSet[m.Name] = true
	}
	for _, c := range fallbackDataset {
		if !c.negative && !nameSet[c.expect] {
			t.Fatalf("数据集中期望技能 %q 在 store 中不存在", c.expect)
		}
	}

	type row = fallbackRow
	var rows []row
	total, correct, llmTotal, llmCorrect, cueTotal, cueCorrect, negTotal, negCorrect := 0, 0, 0, 0, 0, 0, 0, 0

	for _, c := range fallbackDataset {
		exclude := map[string]bool{}
		det := skill.RouteUserMessage(all, c.msg, 1, 1, exclude)
		cueHit := len(det.Auto) > 0 || len(det.Manual) > 0

		r := skill.RouteUserMessageSmart(context.Background(), all, c.msg, 1, 1, exclude, classifier)
		routed := ""
		if len(r.Auto) > 0 {
			routed = r.Auto[0].Name
		} else if len(r.Manual) > 0 {
			routed = r.Manual[0].Name
		}

		ok := routed == c.expect
		rows = append(rows, row{c.msg, c.expect, routed, string(r.Source), cueHit, ok, c.negative})

		total++
		if ok {
			correct++
		}
		if c.negative {
			negTotal++
			if ok {
				negCorrect++
			}
		}
		if cueHit {
			cueTotal++
			if ok {
				cueCorrect++
			}
		} else {
			llmTotal++
			if ok {
				llmCorrect++
			}
		}
		time.Sleep(120 * time.Millisecond) // 轻量限速，避免触发 provider 限流
	}

	// 输出明细表
	fmt.Println("\n================ LLM 兜底命中率测量 ================")
	fmt.Printf("%-46s | %-22s | %-22s | %-5s | %-4s\n", "消息", "期望", "路由到", "来源", "对错")
	fmt.Println(strings.Repeat("-", 110))
	for _, rw := range rows {
		exp := rw.expect
		if rw.negative {
			exp = "(不命中)"
		}
		mark := "OK"
		if !rw.correct {
			mark = "XX"
		}
		fmt.Printf("%-46s | %-22s | %-22s | %-5s | %-4s\n",
			truncate(rw.msg, 22), truncate(exp, 10), truncate(rw.routed, 10), rw.source, mark)
	}
	fmt.Println(strings.Repeat("=", 110))
	fmt.Printf("整体命中率:        %d/%d = %.1f%%\n", correct, total, pct(correct, total))
	fmt.Printf("  - 确定性 cue 命中子集: %d/%d = %.1f%% （未走 LLM）\n", cueCorrect, cueTotal, pct(cueCorrect, cueTotal))
	fmt.Printf("  - LLM 兜底子集:       %d/%d = %.1f%% （本测试核心指标）\n", llmCorrect, llmTotal, pct(llmCorrect, llmTotal))
	fmt.Printf("负面样本（不应命中）误触发率: %d/%d = %.1f%%\n", negTotal-negCorrect, negTotal, pct(negTotal-negCorrect, negTotal))

	// 写报告文件（未跟踪，便于复跑对比）
	writeFallbackReport(t, rows, total, correct, cueTotal, cueCorrect, llmTotal, llmCorrect, negTotal, negCorrect, pn, mid)

	// ---- 回归闸门：命中率门槛 ----
	// 负面样本一旦被误触发，属于「cue 过宽 / 置信门槛失效」，必须硬性失败。
	if negTotal-negCorrect > 0 {
		t.Errorf("负面样本被误触发 %d/%d（期望 0），兜底分类存在误注入风险", negTotal-negCorrect, negTotal)
	}
	// 整体命中率低于 90% 视为回归（历史基线 ~96%），夜跑工作流将因此变红。
	if total > 0 && pct(correct, total) < 90 {
		t.Errorf("整体命中率 %.1f%% 低于阈值 90%%（correct=%d total=%d），疑似路由/分类回归", pct(correct, total), correct, total)
	}
}

// loadEvalLLMConfig 解析用于兜底命中率测量的 LLM 配置。
// 优先读取环境变量（CI 注入 secret），其次读取 dev 端加密配置；都无则 Skip。
func loadEvalLLMConfig(t *testing.T) *llm.UserLLMConfig {
	if key := os.Getenv("NOVELGO_EVAL_KEY"); key != "" {
		return &llm.UserLLMConfig{Providers: []llm.Provider{{Name: "deepseek", APIKey: key}}}
	}
	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		return &llm.UserLLMConfig{Providers: []llm.Provider{{Name: "deepseek", APIKey: key}}}
	}
	cfg, err := llm.LoadUserConfig(config.LLMConfigPath())
	if err != nil {
		t.Skipf("无法读取 dev LLM 配置（%v），跳过真实兜底命中率测试", err)
	}
	return cfg
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func writeFallbackReport(t *testing.T, rows []fallbackRow, total, correct, cueTotal, cueCorrect, llmTotal, llmCorrect, negTotal, negCorrect int, pn, mid string) {
	var b strings.Builder
	fmt.Fprintf(&b, "# LLM 兜底分类命中率报告\n\n")
	fmt.Fprintf(&b, "- 生成时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "- Provider/Model: `%s` / `%s`\n", pn, mid)
	fmt.Fprintf(&b, "- 数据集规模: %d 条（正样例 %d，负面 %d）\n\n", total, total-negTotal, negTotal)
	fmt.Fprintf(&b, "## 汇总\n\n")
	fmt.Fprintf(&b, "| 指标 | 命中 | 总数 | 命中率 |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| 整体 | %d | %d | %.1f%% |\n", correct, total, pct(correct, total))
	fmt.Fprintf(&b, "| 确定性 cue（未走 LLM） | %d | %d | %.1f%% |\n", cueCorrect, cueTotal, pct(cueCorrect, cueTotal))
	fmt.Fprintf(&b, "| **LLM 兜底子集** | %d | %d | **%.1f%%** |\n", llmCorrect, llmTotal, pct(llmCorrect, llmTotal))
	fmt.Fprintf(&b, "| 负面误触发 | %d | %d | %.1f%% |\n", negTotal-negCorrect, negTotal, pct(negTotal-negCorrect, negTotal))
	fmt.Fprintf(&b, "\n## 明细\n\n")
	fmt.Fprintf(&b, "| 消息 | 期望 | 路由到 | 来源 | 结果 |\n|---|---|---|---|---|\n")
	for _, rw := range rows {
		exp := rw.expect
		if rw.negative {
			exp = "(不命中)"
		}
		res := "✅"
		if !rw.correct {
			res = "❌"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", rw.msg, exp, rw.routed, rw.source, res)
	}
	// 写到仓库根目录（与 go.mod 同级），避免 go test 的工作目录（包目录 app/）
	// 与 CI upload-artifact 的相对路径（仓库根）不一致导致报告找不到。
	path := fallbackReportPath()
	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		t.Logf("写报告失败: %v", err)
	} else {
		t.Logf("报告已写入 %s", path)
	}
}

// fallbackReportPath 从本测试文件位置向上查找 go.mod，返回仓库根目录下的报告路径。
// 这样无论 go test 的工作目录是包目录还是仓库根，报告都落到可预期的位置。
func fallbackReportPath() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		dir := filepath.Dir(file)
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return filepath.Join(dir, "skill-llm-fallback-report.md")
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "skill-llm-fallback-report.md"
}
