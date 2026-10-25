package structure

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRenderBlockEmptyZeroPrompt 零告警 → 空串（与无该块逐字一致，降级零提示）。
func TestRenderBlockEmptyZeroPrompt(t *testing.T) {
	require.Equal(t, "", RenderBlock(nil))
	require.Equal(t, "", RenderBlock([]StructFinding{}))
}

// TestRenderBlockFormat 块头 + 每条一行：ID[检查项] 描述（time_entries:n）。
func TestRenderBlockFormat(t *testing.T) {
	got := RenderBlock([]StructFinding{
		{ID: "S1", Check: CheckForeshadowOverdue, Level: FindingLevelDefault,
			Desc: "伏笔「甲」预期第 12 章前回收，当前进度第 20 章仍待收，烂尾风险", Ref: "time_entries:3"},
		{ID: "S2", Check: CheckForeshadowUnsourced, Level: FindingLevelDefault,
			Desc: "伏笔「乙」已标记回收但无埋设章记录（收未埋=无源）", Ref: "time_entries:8"},
	})
	want := "【结构告警】长程结构索引层确定性检查（伏笔 lifecycle / 书写不变量 / 台账对账）产出的风险告警" +
		"（只报告不改写，供体检汇总裁决参照；请对告警点做定向核验）：\n" +
		"  - S1[foreshadow_overdue] 伏笔「甲」预期第 12 章前回收，当前进度第 20 章仍待收，烂尾风险（time_entries:3）\n" +
		"  - S2[foreshadow_unsourced] 伏笔「乙」已标记回收但无埋设章记录（收未埋=无源）（time_entries:8）"
	require.Equal(t, want, got)
}

// TestRenderBlockOneLineDesc 多行描述压成单行（不破版式），输出确定性。
func TestRenderBlockOneLineDesc(t *testing.T) {
	f := []StructFinding{{ID: "S1", Check: CheckForeshadowNoTarget,
		Desc: "伏笔「多行\n  描述」\t待收", Ref: "time_entries:1"}}
	a := RenderBlock(f)
	b := RenderBlock(f)
	require.Equal(t, a, b)
	require.NotContains(t, a, "\n  - S1[foreshadow_no_target] 伏笔「多行\n")
	require.Contains(t, a, "  - S1[foreshadow_no_target] 伏笔「多行 描述」 待收（time_entries:1）")
}
