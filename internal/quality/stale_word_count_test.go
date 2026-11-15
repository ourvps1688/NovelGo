package quality

// #130 契约：重新判分注入判官的验收处方不得再带上一轮正文的历史字数。
// 真实样本取自 2026-11-15 16:51 第 7 章 source=rescore 事件：正文已 3135 字，
// 但 L1 超时「保留上一轮 L1」，pacing 描述仍写着首写时的 1879 字。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const staleL1WordCountDesc = "权威字数 1879（word_count），低于平台卡单章 3000–4000 字下限约 1100+ 字（题材偏好「单章不少于 3200 字」亦未达）。" +
	"第一轮五十场仅一行带过、同组其余场次与台下反应未铺开，章内容量被压缩；" +
	"修复时按 3000–4000 口径补齐，勿按其它数字估算。"

func staleWordsTarget() ChapterWordsTarget {
	return ChapterWordsTarget{
		Min: 3000, Max: 4000, Target: 3500,
		Source: "platform_card", Label: "平台约束卡 3000–4000 字",
	}
}

func TestReconcileStaleWordCountUsesCurrentText(t *testing.T) {
	got := reconcileStaleWordCountDesc(staleL1WordCountDesc, 3135, staleWordsTarget())
	assert.NotContains(t, got, "1879", "上一轮正文的历史字数不得残留")
	assert.NotContains(t, got, "1100+", "历史差额不得残留")
	assert.NotContains(t, got, "3200", "历史偏好数字不得残留")
	assert.Contains(t, got, "3135 字（word_count）", "必须改按当前落盘正文现场判定")
	assert.Contains(t, got, "已落在 平台约束卡区间 3000–4000 字区间内")
	assert.Contains(t, got, "历史字数已过期，不得作为验收数字")
	assert.Contains(t, got, "第一轮五十场仅一行带过", "与字数无关的真实 pacing 内容逐字保留")
	assert.Contains(t, got, "章内容量被压缩")
}

func TestReconcileStaleWordCountShortfallIsComputedFromCurrentText(t *testing.T) {
	got := reconcileStaleWordCountDesc(staleL1WordCountDesc, 1879, staleWordsTarget())
	assert.Contains(t, got, "1879 字（word_count）", "当前正文确实不足时如实写现场字数")
	assert.Contains(t, got, "尚差 1121 字", "差额按当前现场与区间下限现算，不照抄历史约数")
	assert.NotContains(t, got, "权威字数 1879", "不得再以「权威字数」口吻复述历史数字")
	assert.Contains(t, got, "第一轮五十场仅一行带过")
}

func TestReconcileStaleWordCountPreservesFindingFieldsAndSourceSlice(t *testing.T) {
	original := []Finding{
		{
			ID: "L1-1", Check: "pacing", Level: "P1", Desc: staleL1WordCountDesc,
			LedgerRefs: []string{"preference_id:135", "preference_id:136"}, Status: "pending",
		},
		{
			ID: "C1", Check: CheckAIFlavor,
			Desc:   "AI 味套话 [universal-adverb]（命中：带着一丝）（所在句：她带着一丝笑意。）",
			Status: "pending",
		},
	}
	got := ReconcileStaleWordCountFindings(original, 3135, staleWordsTarget())
	require.Len(t, got, 2)
	for i := range original {
		assert.Equal(t, original[i].ID, got[i].ID)
		assert.Equal(t, original[i].Check, got[i].Check)
		assert.Equal(t, original[i].Level, got[i].Level)
		assert.Equal(t, original[i].Status, got[i].Status)
		assert.Equal(t, original[i].LedgerRefs, got[i].LedgerRefs)
	}
	assert.Equal(t, original[1].Desc, got[1].Desc, "不含历史字数陈述的去 AI 味描述必须逐字保持")
	assert.Equal(t, staleL1WordCountDesc, original[0].Desc, "调用方 slice 不得被就地改写")
	assert.NotContains(t, got[0].Desc, "1879")
}

func TestReconcileStaleWordCountLeavesMatchingAndWindowOnlyDescsUntouched(t *testing.T) {
	for _, desc := range []string{
		"权威字数 3135（word_count），落在 3000–4000 字区间内。",
		"权威字数 3135（word_count）与 preference_id:135 的台账一致。",
		"第一轮五十场仅一行带过、同组其余场次与台下反应未铺开。",
	} {
		assert.Equal(t, desc, reconcileStaleWordCountDesc(desc, 3135, staleWordsTarget()),
			"与现场一致 / 只含有效区间口径 / 无字数陈述的描述必须逐字保持：%s", desc)
	}
}

func TestReconciledPrescriptionCarriesNoHistoricalWordCount(t *testing.T) {
	findings := ReconcileStaleWordCountFindings(
		[]Finding{{Check: "pacing", Desc: staleL1WordCountDesc, LedgerRefs: []string{"preference_id:135"}}},
		3135, staleWordsTarget())
	lines := RenderPrescriptions(BuildPrescriptions(findings, nil, 3135, staleWordsTarget()))
	require.Len(t, lines, 1)
	joined := strings.Join(lines, "\n")
	assert.NotContains(t, joined, "1879", "判官的【本章验收条件】不得再出现历史字数")
	assert.NotContains(t, joined, "1100+")
	assert.Contains(t, joined, "3135 字（word_count）")
	assert.Contains(t, joined, "第一轮五十场仅一行带过")
	assert.Contains(t, joined, "[pacing]", "处方行仍以 [check] 开头，发现项未被伪造或丢弃")
}
