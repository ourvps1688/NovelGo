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
