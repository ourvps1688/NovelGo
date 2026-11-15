package mcp_tools

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/skill"
)

// TestBuildFeedbackInstruction_CapsMethodRunes —— #128 防御性补帽：
// 用户自定义同名 editor-feedback 超长正文不得全文灌入零工具推理子 Agent 的指令。
func TestBuildFeedbackInstruction_CapsMethodRunes(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("法", feedbackMethodRuneCap+200) + "_TAIL_SENTINEL"
	fm := "---\nname: editor-feedback\nmode: manual\ninternalOnly: true\nbudget: 4000\n---\n" + long
	require.NoError(t, os.WriteFile(filepath.Join(dir, "editor-feedback.md"), []byte(fm), 0o644))
	store, err := skill.NewStore(slog.New(slog.NewTextHandler(io.Discard, nil)), dir)
	require.NoError(t, err)

	got := buildFeedbackInstruction(store, &quality.EditorFeedback{
		ID: 7, NovelID: 1, WorkTitle: "测试书", Platform: "番茄", Verdict: "故事代入感较弱",
	})
	require.Contains(t, got, "# 本次任务方法论")
	require.NotContains(t, got, "_TAIL_SENTINEL", "超长技能正文必须被截断")
	require.Contains(t, got, "技能正文节选")
	require.Contains(t, got, "编辑原话（唯一依据")
	require.Less(t, strings.Index(got, "# 本次任务方法论"),
		strings.Index(got, "最终回复末尾必须附"), "方法论块必须仍在回执契约之前")
	require.LessOrEqual(t, len([]rune(got)), feedbackMethodRuneCap+800,
		"指令整体须受帽约束（800 为编辑原话 + 回执契约等固定块的余量）")
}

// TestBuildFeedbackInstruction_BuiltinBodyUnchanged 锁内置长度（3446r）在帽内、
// 行为逐字不变：不出现节选标注、方法论块与回执契约顺序不变。
func TestBuildFeedbackInstruction_BuiltinBodyUnchanged(t *testing.T) {
	store, err := skill.NewStore(slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir())
	require.NoError(t, err)
	got := buildFeedbackInstruction(store, &quality.EditorFeedback{
		ID: 8, NovelID: 1, WorkTitle: "测试书", Platform: "番茄", Verdict: "开篇不足",
	})
	require.Contains(t, got, "# 本次任务方法论")
	require.NotContains(t, got, "技能正文节选")
	require.Less(t, strings.Index(got, "# 本次任务方法论"),
		strings.Index(got, "最终回复末尾必须附"))
}
