package pipeline

// R2b-7 §8.3-3 台账变更进修复日志测试（后台自动修复执行体下线后保留复验侧契约）：
//   - ledgerChangeNotes 前缀过滤；
//   - verifyPrompt 台账变更块：空=提示词与旧文逐字一致（降级零提示），
//     非空=追加「下游影响核对」块。

import (
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/stretchr/testify/require"
)

func TestLedgerChangeNotes(t *testing.T) {
	entries := []FixLogEntry{
		{ID: "I1", Note: "正文修复摘要"},
		{ID: "L:5", Note: "台账变更 characters:1 name: \"旧\"→\"新\"（source=auto）"},
		{ID: "V1", Note: "文风豁免项"},
		{ID: "L:6", Note: "台账变更 time_entries:2 title: \"旧\"→\"新\"（source=author）"},
	}
	got := ledgerChangeNotes(entries)
	require.Equal(t, []string{
		`台账变更 characters:1 name: "旧"→"新"（source=auto）`,
		`台账变更 time_entries:2 title: "旧"→"新"（source=author）`,
	}, got)
	require.Nil(t, ledgerChangeNotes(nil))
	require.Nil(t, ledgerChangeNotes([]FixLogEntry{{ID: "I1", Note: "仅 issue 条目"}}))
}

// TestVerifyPromptLedgerChangesBlock 台账变更块契约：空/nil 台账清单 → 提示词与
// 旧文逐字一致（降级零提示）；非空 → 逐条列变更并要求核对下游影响，位置在上下文块前。
func TestVerifyPromptLedgerChangesBlock(t *testing.T) {
	targets := []verifyTarget{{ID: "I1", Level: "P1", Area: "人设", Desc: "动机缺失", Chapters: []int64{3}}}
	blocks := contextasm.ContextBlocks{Facts: "FACTS-MARKER"}

	base := verifyPrompt(1, targets, nil, "", blocks)
	require.Equal(t, base, verifyPrompt(1, targets, []string{}, "", blocks),
		"空清单与 nil 逐字一致（降级零提示）")
	require.NotContains(t, base, "【本轮台账变更清单", "无台账变更不渲染该块")

	notes := []string{`台账变更 characters:1 name: "旧名"→"新名"（source=auto）`}
	got := verifyPrompt(1, targets, notes, "", blocks)
	require.Contains(t, got, "【本轮台账变更清单（修复动过台账）】")
	require.Contains(t, got, notes[0])
	require.Contains(t, got, "下游影响", "要求核对台账修改的下游影响")
	require.Less(t, len(base), len(got), "非空清单必追加块")
	// 块在上下文块（Facts）之前：前缀=旧文的契约主体，尾部仍是上下文块。
	require.Less(t, strings.Index(got, "【本轮台账变更清单"), strings.Index(got, "FACTS-MARKER"))
	require.Contains(t, got, "FACTS-MARKER", "上下文块尾部装配不受影响")
}
