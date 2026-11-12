package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// #114 P2：改稿消息里的目标发现项块只透传 P1 单源稳定 key，不自行造 key，
// 不把处方行/轮次保护行误收进技能账本。

func TestParseTargetFindingKeysOnlyStableKeys(t *testing.T) {
	msg := "请修复第 1 章\n\n" +
		"1. [tail_hook] 章尾无钩子\n\n" +
		"【本章验收条件】\n- [tail_hook] 判定式：当前 0 → 目标 1\n\n" +
		"【本章目标发现项】\n- l0|tail_hook|#0\n- l1|reader_state|#0\n\n" +
		"【轮次保护】l0|beat_gap|#0 连续 3 轮未消除，请先判定阈值冲突\n" +
		"- 这不是稳定 key\n" +
		"请逐条回报达成情况"

	keys := parseTargetFindingKeys(msg)
	require.Equal(t, []string{"l0|tail_hook|#0", "l1|reader_state|#0"}, keys,
		"只收 - l0| / - l1| 前缀的列表行；处方行与轮次保护文本不收")
	require.Equal(t, `["l0|tail_hook|#0","l1|reader_state|#0"]`, findingKeysJSONFromMessage(msg))
}

func TestParseTargetFindingKeysNoMarkerOrDuplicates(t *testing.T) {
	require.Nil(t, parseTargetFindingKeys("普通对话：\n- l0|tail_hook|#0"),
		"无验收标记时不解析，避免把任何含竖线的列表误当目标")
	require.Empty(t, findingKeysJSONFromMessage("普通对话"))

	msg := "【本章目标发现项】\n- l0|tail_hook|#0\n- l0|tail_hook|#0\n"
	require.Equal(t, []string{"l0|tail_hook|#0"}, parseTargetFindingKeys(msg), "同轮重复 key 去重")
}

func TestBuildAcceptanceGuardStillRequiresMarker(t *testing.T) {
	// P2 不得回退 P0：改稿硬约束仍按 AcceptanceMarker 确定性注入。
	require.Empty(t, buildAcceptanceGuard("普通消息"))
	require.Contains(t, buildAcceptanceGuard("【本章验收条件】\n- [x]"), "<system-reminder>")
}
