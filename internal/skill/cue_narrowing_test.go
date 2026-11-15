package skill

import (
	"io"
	"log/slog"
	"testing"
)

// TestCueNarrowingRepairRounds —— #128 最小 cue 收窄的静态契约。
//
// 背景：项目红线「所有修复都走 chat」使用户必然把 L0/L1 发现项原文贴进消息，
// 而「动笔 / 标签 / 润色」等检测器术语恰与创作技能 cue 同名，导致每轮按处方
// 改稿都被灌整套方法论（03:10 回放：24/32 命中、chat 侧 153,917r）。
// 本测试只锁确定性 cue 分支，不调用 LLM 兜底；正向用例防止「一刀切删 cue」。
func TestCueNarrowingRepairRounds(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := NewStore(logger, "")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	all := store.ListMeta(0)
	if len(all) == 0 {
		t.Fatal("store 未加载内置技能，无法执行回归")
	}

	repairRounds := []struct {
		name string
		msg  string
		want string // 不该命中的技能
	}{
		{
			name: "next 不被修复轮的「再动笔」抢走",
			msg: "请按以下章节体检发现处理第6章，由你我在聊天中协作改稿。" +
				"请先与我确认是判定器阈值冲突还是改动不足，并从「豁免 / 调整阈值 / 换思路」" +
				"中选一条再动笔，不要重复整章推翻重写。【本章目标发现项】- l0|ai_flavor|#0",
			want: "next",
		},
		{
			name: "book-pitch 不被「对话标签单一」抢走",
			msg: "一、字数核验：实测约 3155。三、具体写作问题：1）对话标签单一——" +
				"「陈烬说」全章出现约十五次，建议替换为动作 beats 或省略主语",
			want: "book-pitch",
		},
		{
			name: "deai-flavor 不被极小润色抢走",
			msg: "请对第 6 章做一处极小润色，并且必须实际调用 search_replace 工具把改动写入正文落盘：" +
				"只改这一处，不要整章重写",
			want: "deai-flavor",
		},
	}
	for _, c := range repairRounds {
		t.Run(c.name, func(t *testing.T) {
			res := RouteUserMessage(all, c.msg, 3, 1, nil)
			hits := append(append([]SkillMeta{}, res.Auto...), res.Manual...)
			for _, m := range hits {
				if m.Name == c.want {
					t.Errorf("修复轮误命中 %q：%q", c.want, c.msg)
				}
			}
		})
	}

	// 正向：收窄不得把合法意图一起删掉（与 regression_test.go 的 positives 同纪律）。
	positives := []struct {
		msg  string
		want string
	}{
		{"继续写下一章", "next"},
		{"动笔写新章", "next"},
		{"我想起个书名", "book-pitch"},
		{"帮我选标签", "book-pitch"},
		{"标签选什么比较好", "book-pitch"},
		{"这段有AI味，帮我去掉", "deai-flavor"},
		{"润色声线", "deai-flavor"},
	}
	for _, p := range positives {
		res := RouteUserMessage(all, p.msg, 3, 1, nil)
		found := false
		for _, m := range append(append([]SkillMeta{}, res.Auto...), res.Manual...) {
			if m.Name == p.want {
				found = true
			}
		}
		if !found {
			t.Errorf("正向 cue 被误删：%q 应命中 %q，实际 auto=%v manual=%v",
				p.msg, p.want, skillNames(res.Auto), skillNames(res.Manual))
		}
	}
}
