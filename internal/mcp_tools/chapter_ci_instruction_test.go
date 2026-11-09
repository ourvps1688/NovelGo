//go:build cgo

package mcp_tools

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/skill"
	"github.com/stretchr/testify/require"
)

func newCISkillStore(t *testing.T) *skill.Store {
	t.Helper()
	s, err := skill.NewStore(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		filepath.Join(t.TempDir(), "skills"),
	)
	require.NoError(t, err)
	return s
}

// TestChapterCISelfCheckBlock_NilStore #111 P1：store 缺失/技能正文为空时整块为空串，
// 保证「非空才注入、空则逐字不变」的降级纪律。
func TestChapterCISelfCheckBlock_NilStore(t *testing.T) {
	require.Empty(t, chapterCISelfCheckBlock(nil, 1, 7))
}

// TestBuildChapterCIInstruction_EmptySelfCheckVerbatim #111 P1：无技能 store 时，
// 新增自检块完全不出现，检查项/情绪曲线/回执契约的顺序与文案逐字保持 P0 口径。
func TestBuildChapterCIInstruction_EmptySelfCheckVerbatim(t *testing.T) {
	got := buildChapterCIInstruction(nil, 1, 7, "")
	require.NotContains(t, got, "写后自检清单")
	require.NotContains(t, got, "【写后自检·")
	require.NotContains(t, got, "本次任务方法论", "无 store 时章节方法论块也必须缺席")
	require.Contains(t, got, "章节（chapter_id=7）正文刚刚修改并落盘")
	require.Contains(t, got, "只报告不改写：不调用任何写工具")
	require.Contains(t, got, "最终回复末尾必须附")
	require.Less(t, strings.Index(got, "检查项："), strings.Index(got, "情绪曲线标注"))
	require.Less(t, strings.Index(got, "情绪曲线标注"), strings.Index(got, "最终回复末尾必须附"))
}

// TestChapterCISelfCheckBlock_NonEmpty #111 P1：三个写后自检技法按单源顺序注入、
// 受单帽截断，并真实进入 L1 指令（回执契约之前）。
func TestChapterCISelfCheckBlock_NonEmpty(t *testing.T) {
	s := newCISkillStore(t)
	block := chapterCISelfCheckBlock(s, 1, 7)
	require.NotEmpty(t, block)
	for _, n := range chapterCISelfCheckSkills {
		require.Contains(t, block, "【写后自检·"+n+"】")
	}
	require.Contains(t, block, "只报告不改写")
	require.LessOrEqual(t, len([]rune(block)), 3*chapterCISelfCheckSkillCap+300)

	got := buildChapterCIInstruction(s, 1, 7, "")
	require.Contains(t, got, "写后自检清单")
	require.Less(t, strings.Index(got, "写后自检清单"), strings.Index(got, "最终回复末尾必须附"))
	require.Contains(t, got, "只报告不改写：不调用任何写工具", "回执契约与「只报告」纪律不得被自检块冲掉")
}
