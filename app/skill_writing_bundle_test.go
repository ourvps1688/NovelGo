package app

import (
	"context"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/skill"
	"github.com/stretchr/testify/require"
)

// TestBuildAutoSkillInject_WritingBundle 锁定 #108 的核心修复：写章意图命中总调度
// next 时，必须把技法技能正文一并注入（而非只给一段「建议启用」提醒）。
func TestBuildAutoSkillInject_WritingBundle(t *testing.T) {
	a := setupTestApp(t)
	inject, names := a.buildAutoSkillInjectWithNames(context.Background(), 0, "开始创作下一章", "", "")
	require.NotEmpty(t, inject)
	require.Contains(t, names, "next")
	for _, name := range writingBundleSkills {
		require.Contains(t, names, name, "写章技法包应包含 %s", name)
		require.Contains(t, inject, "【写章技法·"+name+"】")
	}
	require.Contains(t, inject, "必须逐条落实", "技法包必须声明为硬约束，不是可选建议")
	require.NotContains(t, inject, "---\r\nname:", "注入正文不得残留 YAML frontmatter")
	require.NotContains(t, inject, "---\nname:", "注入正文不得残留 YAML frontmatter")
	require.LessOrEqual(t, len([]rune(inject)), 16000, "注入总量应有界，避免稀释上下文")
}

// TestBuildAutoSkillInject_NoBundleForNonWriting 非写章消息只注入命中的单个技能，
// 不叠加技法包；无关消息仍保持零注入。
func TestBuildAutoSkillInject_NoBundleForNonWriting(t *testing.T) {
	a := setupTestApp(t)
	inject, names := a.buildAutoSkillInjectWithNames(context.Background(), 0, "帮我设计一下力量体系", "", "")
	require.Equal(t, []string{"worldbuilding-design"}, names)
	require.NotContains(t, inject, "【写章技法·")

	empty, emptyNames := a.buildAutoSkillInjectWithNames(context.Background(), 0, "今天天气真好", "", "")
	require.Empty(t, empty)
	require.Empty(t, emptyNames)
}

// TestWritingCueBoundary 锁定「动笔」触发与「动笔前」不误触发的边界（canary 同款）。
func TestWritingCueBoundary(t *testing.T) {
	a := setupTestApp(t)
	for _, msg := range []string{"动笔", "继续", "next", "开始创作下一章", "写下一章"} {
		_, names := a.buildAutoSkillInjectWithNames(context.Background(), 0, msg, "", "")
		require.Contains(t, names, "next", "%q 应命中写章总调度", msg)
	}
	_, names := a.buildAutoSkillInjectWithNames(
		context.Background(), 0,
		"动笔前我想先把整本书的线索和坑都拢一遍，免得后面穿帮", "", "",
	)
	require.NotContains(t, names, "next", "「动笔前」不应被写章 cue 抢走")

	// 纯确定性路由层同样守住边界（不依赖 LLM 兜底）。
	all := a.skill.ListMeta(0)
	r := skill.RouteUserMessage(all, "动笔", 1, 1, nil)
	require.Len(t, r.Manual, 1)
	require.Equal(t, "next", r.Manual[0].Name)
	r = skill.RouteUserMessage(all, "动笔前我想先把整本书的线索和坑都拢一遍", 1, 1, nil)
	for _, m := range append(append([]skill.SkillMeta{}, r.Auto...), r.Manual...) {
		require.NotEqual(t, "next", m.Name)
	}
}

// TestSkillBodyCRLF 内置技能是 CRLF：Body 必须剥掉 YAML 头，且正文不为空。
func TestSkillBodyCRLF(t *testing.T) {
	raw := "---\r\nname: x\r\nmode: auto\r\n---\r\n# 标题\r\n正文第一段\r\n"
	body := skill.Body(raw)
	require.Equal(t, "# 标题\r\n正文第一段", strings.TrimSpace(body))
	require.NotContains(t, body, "name: x")
	require.Equal(t, "无头正文", skill.Body("无头正文"))
	require.Equal(t, "正文", skill.Body("---\nname: x\n---\n正文"))    // LF 同样剥离
	require.Contains(t, skill.Body("---\nname: x\n正文"), "name: x") // 未闭合保守返回原文
}

// TestWritingSkillConditions_HitAndUnknown #111 P1：条件命中按表顺序确定性输出、
// 去重；章况探测全失败（零值）时一律不命中，退回固定 3 技法。
func TestWritingSkillConditions_HitAndUnknown(t *testing.T) {
	c := writingChapterContext{
		nextReadingNumber: 2,
		rhythmRequired:    true,
		taskCardText:      "本章新卷开启，出现新地图与新势力，并有大量对白戏",
		message:           "卡文了，没思路",
	}
	got := conditionWritingSkillNames(c)
	require.Equal(t, []string{
		"opening-chapters", "worldbuilding-design", "dialogue-subtext",
		"pacing-control", "brainstorm-composer",
	}, got)
	seen := map[string]bool{}
	for _, n := range got {
		require.False(t, seen[n], "条件技能不得重复 %s", n)
		seen[n] = true
	}
	require.Empty(t, conditionWritingSkillNames(writingChapterContext{}), "章况全未知时不得命中任何条件")
	require.Empty(t, conditionWritingSkillNames(writingChapterContext{nextReadingNumber: 9}), "非黄金三章不命中开篇")
}

// TestBuildWritingSkillBundle_ConditionsAndCaps #111 P1：条件化扩容必须真实生效
// （旧 5000 整块帽会把第 4 个技能全部 break），同时守住单技能/整块双封顶与零叠加。
func TestBuildWritingSkillBundle_ConditionsAndCaps(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	const msg = "卡文了，没思路，开始创作下一章"
	block, names := a.buildWritingSkillBundle(ctx, 0, msg, nil)
	require.NotEmpty(t, block)
	for _, n := range writingBundleSkills {
		require.Contains(t, names, n, "固定兜底技法 %s 不得被条件项挤掉", n)
	}
	// 测试库 0 章 → 下一章阅读序号 1 → opening 命中；消息含「卡文/没思路」→ brainstorm 命中。
	require.Contains(t, names, "opening-chapters")
	require.Contains(t, names, "brainstorm-composer")
	require.Greater(t, len(names), 3, "条件化扩容必须真实生效，不能被整块帽全 break")
	require.LessOrEqual(t, len(names), 6, "验收口径 3~6 个技法")
	require.LessOrEqual(t, len([]rune(block)), writingBundleBlockCap+300, "整块注入须有界（含包裹文案余量）")

	// 零叠加：already 覆盖全部候选时，一块都不再注入。
	candidates := append([]string{}, writingBundleSkills...)
	candidates = append(candidates, conditionWritingSkillNames(a.detectWritingChapterContext(ctx, 0, msg))...)
	emptyBlock, emptyNames := a.buildWritingSkillBundle(ctx, 0, msg, candidates)
	require.Empty(t, emptyBlock)
	require.Empty(t, emptyNames)
}

// TestCapSkillRunes manifest budget 真正被装配器消费：超帽截断并如实标注，
// 未声明 budget 时退回缺省值。
func TestCapSkillRunes(t *testing.T) {
	runes, truncated := capSkillRunes("一二三四五", 3, 4000)
	require.Equal(t, "一二三", string(runes))
	require.True(t, truncated)
	runes2, truncated2 := capSkillRunes("短", 0, 4000)
	require.Equal(t, "短", string(runes2))
	require.False(t, truncated2)
}

// TestBuildAutoSkillInject_InternalOnlyRedirect #111 P1：chat 侧不再注入
// signability-fix 正文，只回一行指引导语（验收：反向错投 = 0）。
func TestBuildAutoSkillInject_InternalOnlyRedirect(t *testing.T) {
	a := setupTestApp(t)
	inject, names := a.buildAutoSkillInjectWithNames(context.Background(), 0, "帮我修复一下签约体检的问题", "", "")
	require.NotContains(t, names, "signability-fix", "internalOnly 技能不得计入本轮注入技能徽标")
	require.Contains(t, inject, "内部工作流技能")
	require.Contains(t, inject, "signability-fix", "指引里要指名命中的内部技能")
	require.Contains(t, inject, "工单面板")
	require.NotContains(t, inject, "【写章技法", "内部工作流消息不得误触发写章技法包")
}
