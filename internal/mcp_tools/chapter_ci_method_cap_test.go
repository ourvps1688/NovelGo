//go:build cgo

package mcp_tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestChapterCIMethodCapContract #126 B：chapter-ci 方法论正文受 1200 rune 单帽
// 截断、如实标注节选、统一 LF，且统一账本 Runes 记实际注入长度而非技能全文。
// 回执契约与「只报告不改写」纪律不得因截断丢失。
func TestChapterCIMethodCapContract(t *testing.T) {
	invs := skillInvocations(t)
	s := newCISkillStore(t)
	sk, ok := s.Get(0, "chapter-ci")
	require.True(t, ok, "内置 chapter-ci 技能必须存在")
	require.Greater(t, len([]rune(sk.Content)), chapterCIMethodCap,
		"内置正文须超过单帽，才能真正验证截断（旧实现全文注入 2612 rune）")

	got := buildChapterCIInstruction(s, 1, 7, "")
	require.Contains(t, got, "# 本次任务方法论")
	require.Contains(t, got, "（章节方法论节选，完整以技能原文为准）")

	const header = "# 本次任务方法论\n"
	start := strings.Index(got, header) + len(header)
	end := strings.Index(got[start:], "\n（章节方法论节选")
	require.Positive(t, end, "节选标注必须紧跟方法论正文")
	method := got[start : start+end]
	require.LessOrEqual(t, len([]rune(method)), chapterCIMethodCap,
		"方法论正文不得超过单帽")
	require.NotContains(t, method, "\r", "注入的技能正文须统一为 LF（#108 洁净化）")

	// 截断不得冲掉 P0 纪律与回执契约，顺序亦不变。
	require.Contains(t, got, "只报告不改写：不调用任何写工具")
	require.Contains(t, got, "回答第一段必须是")
	require.Less(t, strings.Index(got, "# 本次任务方法论"), strings.Index(got, "回答第一段必须是"))

	idx := -1
	for i := range *invs {
		if (*invs)[i].Skill == "chapter-ci" {
			idx = i
			break
		}
	}
	require.GreaterOrEqual(t, idx, 0, "chapter-ci 注入须记一条具体技能账")
	require.Equal(t, chapterCIMethodCap, (*invs)[idx].Runes,
		"账本 Runes 必须与实际注入的截断后长度一致，不再虚报全文字符数")
	require.Equal(t, "chapter_ci", (*invs)[idx].Channel)
	require.Equal(t, int64(7), (*invs)[idx].ChapterID)
}
