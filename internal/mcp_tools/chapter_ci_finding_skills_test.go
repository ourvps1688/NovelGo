//go:build cgo

package mcp_tools

import (
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/stretchr/testify/require"
)

// #116 P1 写后自检定向：旧链路每章固定注入 4 个技法（实测占技能字符 66%），
// 与本章实际发现维度并不对应。这里只改注入集合，检查项/回执契约/执行体一律不动。

// #126 A：零发现不再回落全量 4 技法（实测每章固定 4837 rune、#116 定向注入
// 真实运行零样本），改为最小评审清单；首章仍有自检清单，L0 真有发现时定向照常生效。
func TestChapterCIFindingSkillsZeroFindingsMinimalSet(t *testing.T) {
	minimal := []string{"review"}
	require.Equal(t, minimal, chapterCIFindingSkills(nil),
		"新章/档案未落库/测试直调：零发现只挂最小集 review")
	require.Equal(t, minimal, chapterCIFindingSkills([]quality.Finding{}))
	require.Equal(t, minimal, chapterCIFindingSkills([]quality.Finding{{Check: "unknown_check"}}),
		"命中但不落映射仍是同一最小集，两路口径一致")
}

func TestChapterCIFindingSkillsRoutesByFinding(t *testing.T) {
	// 只有去 AI 味发现：注入 deai-flavor，不注入其余三个。
	got := chapterCIFindingSkills([]quality.Finding{{Check: quality.CheckAIFlavor}})
	require.Equal(t, []string{"deai-flavor"}, got)

	// 曲线/开篇类发现归入修订通稿：仍按单源顺序输出。
	got = chapterCIFindingSkills([]quality.Finding{
		{Check: quality.CheckOpeningSlope}, {Check: quality.CheckTailHook},
	})
	require.Equal(t, []string{"revision-pass"}, got)

	// L1 连续性/任务卡归入 review；潜台词类同时带 dialogue-subtext。
	got = chapterCIFindingSkills([]quality.Finding{{Check: "continuity"}, {Check: "task_card"}})
	require.Equal(t, []string{"review"}, got)
	got = chapterCIFindingSkills([]quality.Finding{{Check: "immersion_anchor"}})
	require.Equal(t, []string{"dialogue-subtext", "review"}, got)

	// 命中但不在映射表：最小集 review，不回退成全量（否则定向失效）。
	require.Equal(t, []string{"review"}, chapterCIFindingSkills([]quality.Finding{{Check: "unknown_check"}}))
}

func TestChapterCIFindingSkillsDeterministicUnion(t *testing.T) {
	findings := []quality.Finding{
		{Check: quality.CheckAIFlavor}, {Check: "immersion_anchor"},
		{Check: quality.CheckOpeningSlope},
	}
	got := chapterCIFindingSkills(findings)
	require.Equal(t, []string{"deai-flavor", "revision-pass", "dialogue-subtext", "review"}, got,
		"顺序恒按 chapterCISelfCheckSkills 单源，去重后确定性输出")
	require.Equal(t, got, chapterCIFindingSkills(findings), "同输入同输出")
}

func TestChapterCISelfCheckBlockDirectedByFindings(t *testing.T) {
	s := newCISkillStore(t)
	// #126 A：零发现块只剩最小评审清单。
	minimal := chapterCISelfCheckBlock(s, 1, 7)
	require.NotEmpty(t, minimal)
	require.Contains(t, minimal, "【写后自检·review】")
	for _, n := range []string{"deai-flavor", "revision-pass", "dialogue-subtext"} {
		require.NotContains(t, minimal, "【写后自检·"+n+"】")
	}

	// 多发现组合仍能触发全量 4 技法：#126 A 只改「零发现」分支，不删任何技法。
	all := chapterCISelfCheckBlock(s, 1, 7,
		quality.Finding{Check: quality.CheckAIFlavor},
		quality.Finding{Check: "immersion_anchor"},
		quality.Finding{Check: quality.CheckOpeningSlope})
	for _, n := range chapterCISelfCheckSkills {
		require.Contains(t, all, "【写后自检·"+n+"】")
	}
	require.Less(t, len([]rune(minimal)), len([]rune(all)),
		"零发现最小集必须真的省下写后自检字符，而不是只换个函数名")

	directed := chapterCISelfCheckBlock(s, 1, 7, quality.Finding{Check: quality.CheckAIFlavor})
	require.Contains(t, directed, "【写后自检·deai-flavor】")
	require.NotContains(t, directed, "【写后自检·revision-pass】")
	require.NotContains(t, directed, "【写后自检·dialogue-subtext】")
	require.NotContains(t, directed, "【写后自检·review】")

	// 定向块真实进入 L1 指令，且仍在回执契约之前、「只报告不改写」纪律不丢。
	instruction := buildChapterCIInstruction(s, 1, 7, "", quality.Finding{Check: quality.CheckAIFlavor})
	require.Contains(t, instruction, "【写后自检·deai-flavor】")
	require.NotContains(t, instruction, "【写后自检·review】")
	require.Less(t, strings.Index(instruction, "写后自检清单"), strings.Index(instruction, "回答第一段必须是"))
	require.Contains(t, instruction, "只报告不改写：不调用任何写工具")
}
