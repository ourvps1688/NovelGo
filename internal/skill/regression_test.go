package skill

import (
	"io"
	"log/slog"
	"testing"
)

// TestCuePrecisionRegression 是零成本的确定性回归闸门，不调用 LLM、不需要 key，
// 可在 CI 默认 Go 环境（含 internal/skill 包）常跑。它守护三类不变量：
//
//  1. 负面样本（明显无关的消息）绝不能在确定性 cue 层触发任何技能 ——
//     否则说明某条 cue 过宽，会把无关消息误注入技能。
//  2. cue 精度 canary：「动笔前…」曾因 next 的 cue「动笔」过宽被误中（应属 collect），
//     必须不再路由到 next（理想情况也不命中任何技能）。
//  3. 若干显式含 cue 的消息必须正确路由 —— 防止 cues 被误删或改名导致路由失效。
//
// 此外对原 LLM 基准的模糊消息集跑一遍，仅 INFO 记录任何确定性命中，
// 便于发现「cue 过宽」的新苗头，但不硬性失败以免误伤合法精确匹配。
func TestCuePrecisionRegression(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := NewStore(logger, "")
	if err != nil {
		t.Fatalf("NewStore 初始化失败: %v", err)
	}
	all := store.ListMeta(0)
	if len(all) == 0 {
		t.Fatal("store 未加载任何内置 skill，无法执行回归")
	}

	// 1) 负面样本必须不触发任何技能（硬性失败）
	negatives := []string{
		"你好",
		"今天心情不错",
		"帮我查下明天上海的天气预报",
		"谢谢你的帮忙",
		"这软件怎么导出文件",
		"我想写点东西",
	}
	for _, msg := range negatives {
		res := RouteUserMessage(all, msg, 1, 1, nil)
		if len(res.Auto) > 0 || len(res.Manual) > 0 {
			t.Errorf("负面样本被确定性 cue 误触发（应不命中）：%q -> auto=%v manual=%v",
				msg, skillNames(res.Auto), skillNames(res.Manual))
		}
	}

	// 2) cue 精度 canary：「动笔前…」绝不应路由到 next
	canary := "动笔前我想先把整本书的线索和坑都拢一遍，免得后面穿帮"
	cr := RouteUserMessage(all, canary, 1, 1, nil)
	canaryHits := append(append([]SkillMeta{}, cr.Auto...), cr.Manual...)
	for _, m := range canaryHits {
		if m.Name == "next" {
			t.Errorf("canary 失败：「动笔前」被 next 的 cue 误中（应去 collect 或不命中）")
		}
	}
	if len(canaryHits) > 0 {
		t.Logf("INFO canary 命中（非 next，可接受）：%q -> %v", canary, skillNames(canaryHits))
	}

	// 3) 显式 cue 必须正确路由（硬失败，兼防 cues 被误删）
	positives := []struct {
		msg  string
		want string
	}{
		{"设定一下修炼体系", "worldbuilding-design"},
		{"帮我设计下人物关系", "character-design"},
		{"这章结尾留个伏笔", "plot-hook"},
		{"这两章节奏慢得不行", "pacing-control"},
		{"帮我改一下文", "revision-pass"},
		{"我想起个书名", "book-pitch"},
		{"按之前的走向继续写下一章", "next"},
		// 三个「签约智能」技能：确保它们的 cue 也纳入确定性回归闸门
		{"帮我评估签约潜力", "signability-audit"},
		{"写个全书卷纲", "story-blueprint"},
		{"设定一下修炼体系", "worldbuilding-design"},
	}
	for _, p := range positives {
		res := RouteUserMessage(all, p.msg, 1, 1, nil)
		got := ""
		if len(res.Auto) > 0 {
			got = res.Auto[0].Name
		} else if len(res.Manual) > 0 {
			got = res.Manual[0].Name
		}
		if got != p.want {
			t.Errorf("确定性 cue 路由错误：%q -> 得到 %q，期望 %q", p.msg, got, p.want)
		}
	}

	// 4) 模糊消息集：仅 INFO 记录确定性命中，不硬性失败
	fuzzy := []string{
		"我想搭一套自洽的力量强弱逻辑和进阶路线，前后别矛盾",
		"两个国家打仗，我想让双方的兵力对比和科技水平都更有说服力",
		"这个新对手我想让他每次登场都透着危险感，但做事动机要合理可信",
		"主角失忆了，我想后面慢慢揭开他不为人知的过去",
		"整本书从第一页到结局的大结构该怎么搭才不散，我怕写到后面收不回来",
		"头几章怎么写才能让人忍不住一直读下去，别一上来就劝退",
		"新书刚发，前两章没人看，是不是开场太温了",
		"我写的这段打斗总觉得平，缺乏让人紧张起来的张力",
		"这两章读着有点发闷，推进不够利落，读者容易划走",
		"高潮那段我想让它一口气冲到底，别在中途泄气",
		"每一节的末尾我想留个让人惦记下文的东西，勾着往下翻",
		"两个人说话总觉得太直白，话里没有藏着别的意思",
		"长辈训话那段，我想让每句话都带刺又不直接说破",
		"描写总像在旁边报幕，读者没法跟着主角一同经历那些时刻",
		"这段读起来很假，像是按公式凑出来的，没有活人该有的语感",
		"请把这段调得像是真人随手写的，别那么工整",
		"初稿写完了，我想整体过一遍把不顺的地方理平",
		"我想知道这类题材在平台上受不受欢迎，有没有商业前景",
		"脑子空白，一点主意都冒不出来",
		"写完这章你帮我看看水平怎么样，挑挑毛病",
		"按之前定好的走向，把后面的内容接着往下写",
		"我想给这部书想一句让人忍不住点开去看的话",
		canary,
	}
	for _, msg := range fuzzy {
		res := RouteUserMessage(all, msg, 1, 1, nil)
		if len(res.Auto) > 0 || len(res.Manual) > 0 {
			t.Logf("INFO 模糊消息触发确定性 cue（可能 cue 过宽，请复核）：%q -> %v",
				msg, skillNames(append(append([]SkillMeta{}, res.Auto...), res.Manual...)))
		}
	}
}

func skillNames(ms []SkillMeta) []string {
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Name)
	}
	return names
}
