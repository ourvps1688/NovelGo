package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 定向处方契约：L1 六类语义发现不再共用一句「二选一：改正文或更新台账」通用
// 模板，而是按 check 分流、逐条带 seg/字区间/可复制原句/定点动作。
// 判官评分侧 BuildPrescriptions（零锚点）输出必须逐字保持改造前口径。

func anchoredTestFixtures() ([]Finding, ChapterWordsTarget, string, []TensionPoint) {
	content := "智脑说「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」，陈烬没接话。后面又写「多的两百全在北沿那截绕上」，把数在心里过了一遍。"
	findings := []Finding{
		{ID: "L1-1", Check: "continuity", Desc: "智脑「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」数字自相矛盾"},
		{ID: "L1-2", Check: "task_card", Desc: "计划卡「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」的爽点分段未兑现"},
		{ID: "L1-3", Check: "foreshadow", Desc: "伏笔「比推算多两百步。六千七百步按你的步幅折，是五公里零三十」精简口径未落实"},
	}
	words := ChapterWordsTarget{Min: 1000, Max: 1500, Target: 1500, Source: "platform_profile", Label: "七猫 1000–1500"}
	return findings, words, content, BuildTensionCurve(content)
}

func TestAnchoredL1PrescriptionsArePerFindingAndActionable(t *testing.T) {
	findings, words, content, curve := anchoredTestFixtures()
	lines := RenderPrescriptions(BuildPrescriptionsAnchored(findings, curve, 1180, words, content))
	require.Len(t, lines, 3, "一条发现对应一条处方，保序、不去重、不串条")

	for _, line := range lines {
		assert.Contains(t, line, "原句「比推算多两百步", "位置必须给到可逐字复制的正文原句")
		assert.Contains(t, line, "字 ", "位置必须给到 rune 区间")
		assert.Contains(t, line, "search_replace", "动作必须能直接定点改")
		assert.NotContains(t, line, "二选一：把正文改成与台账一致", "不得再把纯正文问题导向改台账")
		assert.NotContains(t, line, "位置：本章正文 + ", "不得再给空位置")
	}
	assert.NotEqual(t, lines[0], lines[1], "不同 check 不得共用同一份通用模板")
	assert.NotEqual(t, lines[1], lines[2])
	assert.Contains(t, lines[0], "不要去改台账")
	assert.Contains(t, lines[1], "定点补写计划卡")
	assert.Contains(t, lines[2], "对齐计划卡口径")
}

func TestBuildPrescriptionsAnchoredWithoutContentMarksUnlocated(t *testing.T) {
	findings, words, _, curve := anchoredTestFixtures()
	// 判官评分门禁（零锚点路径）保持旧模板逐字不变，历史 golden 不受影响。
	legacy := RenderPrescriptions(BuildPrescriptions(findings, curve, 1180, words))
	for _, line := range legacy {
		assert.Contains(t, line, "二选一：把正文改成与台账一致", "判官侧旧模板不许被动到")
	}
	// 「去修复」定向路径即使读盘失败也如实标未定位，不退回误导性的旧模板。
	degraded := RenderPrescriptions(BuildPrescriptionsAnchored(findings, curve, 1180, words, ""))
	require.Len(t, degraded, 3)
	for _, line := range degraded {
		assert.Contains(t, line, "未定位（描述里没有能在正文逐字命中的原句")
		assert.Contains(t, line, "search_replace")
		assert.NotContains(t, line, "二选一：把正文改成与台账一致")
	}
}

func TestAnchoredPrescriptionKeepsL0PrescriptionsUnchanged(t *testing.T) {
	_, words, content, curve := anchoredTestFixtures()
	l0 := []Finding{
		{ID: "C1", Check: CheckTailHook, Desc: "章尾无钩"},
		{ID: "C2", Check: CheckAIFlavor, Desc: "套话"},
	}
	assert.Equal(t,
		RenderPrescriptions(BuildPrescriptions(l0, curve, 1180, words)),
		RenderPrescriptions(BuildPrescriptionsAnchored(l0, curve, 1180, words, content)),
		"L0 处方本来就有段级实测，锚点路径不得改动它")
}

func TestAnchoredPrescriptionMarksUnlocatedHonestly(t *testing.T) {
	f := Finding{ID: "L1-1", Check: "continuity", Desc: "纯描述里没有任何能命中正文的引号"}
	words := ChapterWordsTarget{Min: 1000, Max: 1500, Target: 1500}
	lines := RenderPrescriptions(BuildPrescriptionsAnchored(
		[]Finding{f}, nil, 1180, words, "本章正文完全 unrelated。"))
	require.Len(t, lines, 1)
	assert.Contains(t, lines[0], "未定位（描述里没有能在正文逐字命中的原句")
	assert.NotContains(t, lines[0], "原句「", "抽不到就如实说，绝不编一句原句")
}
