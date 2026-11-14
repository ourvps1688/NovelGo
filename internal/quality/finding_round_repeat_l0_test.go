package quality

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// finding_round_repeat_l0_test.go —— #124 契约：同一轮内重复 L0 重算
// 不得把刚消掉的发现项从 resolved 里抹掉、也不得虚增轮次。
//
// 复刻 2026-11-14 19:51–19:55 第 6 章真实写入序列（由 novelgo.log 的 5 条
// chapter_quality INSERT 还原）：三次 search_replace 各触发一次 recalcChapterL0，
// 19:54:43 手动重检的 RecalcChapterL0FromDisk 又重算一次，19:55:42 L1 收口。
// 修复前第 4 次以「已清空的旧行」为基线开新轮 ⇒ resolved=[]、repeat_count
// 归零不可见、round 被虚增到 32。

func TestRepeatL0RecalcKeepsResolvedThroughL1(t *testing.T) {
	ai := f("ai_flavor", "C1", "")
	key := "l0|ai_flavor|#0"

	// 基线：上一轮已收口（PendingL1=false），连续 28 轮未消除。
	base := FindingRound{
		Round:       28,
		PrevKeys:    []string{key},
		CurrentKeys: []string{key},
		Resolved:    []string{},
		Persisted:   []string{key},
		New:         []string{},
		L0Keys:      []string{key},
		RepeatCount: map[string]int{key: 28},
	}
	row := rowOf([]Finding{ai}, nil, base)

	// 19:51:05.625 第一次 search_replace 后：L0 仍有该发现，正常开新轮。
	r1 := NextFindingRound(row, "l0", []Finding{ai})
	assert.Equal(t, 29, r1.Round)
	assert.True(t, r1.PendingL1)
	assert.Equal(t, []string{key}, r1.Persisted)
	assert.Equal(t, 29, r1.RepeatCount[key])

	// 19:51:05.643 第二次 search_replace 后：L0 内容完全没变 ⇒ 同轮重复重算，
	// 不得再开一轮、不得把 repeat_count 从 29 推到 30。
	r2 := NextFindingRound(rowOf([]Finding{ai}, nil, r1), "l0", []Finding{ai})
	assert.Equal(t, 29, r2.Round, "同 L0 键的重复重算不得虚增轮次")
	assert.Equal(t, 29, r2.RepeatCount[key], "同轮只增删不累加")
	assert.True(t, r2.PendingL1)

	// 19:51:05.661 第三次 search_replace：三句否定铺排全改完，L0 清空。
	// L0 真变化 ⇒ 必须开新轮，并算出 resolved。
	r3 := NextFindingRound(rowOf([]Finding{ai}, nil, r2), "l0", nil)
	assert.Equal(t, 30, r3.Round, "L0 真变化必须开新轮")
	assert.Equal(t, []string{key}, r3.Resolved, "消掉的发现项必须可见")
	assert.Empty(t, r3.RepeatCount, "resolved 的连续轮数必须归零")
	assert.Empty(t, r3.GuardKeys(), "归零后不得再触发轮次保护")
	assert.True(t, r3.PendingL1)

	// 19:54:43 手动重检的 RecalcChapterL0FromDisk：按落盘正文重算，结果同为空。
	// 修复前这一写会把 PrevKeys 冲成 [] ⇒ resolved 消失（本测试的核心回归锁）。
	r4 := NextFindingRound(rowOf(nil, nil, r3), "l0", nil)
	assert.Equal(t, []string{key}, r4.Resolved, "重复 L0 重算不得吞掉已消解结果")
	assert.Empty(t, r4.RepeatCount)
	assert.Equal(t, 30, r4.Round)
	assert.True(t, r4.PendingL1)

	// 19:55:42 L1 收口：沿用同一份 PrevKeys，resolved 仍必须可见。
	r5 := NextFindingRound(rowOf(nil, nil, r4), "l1", nil)
	assert.False(t, r5.PendingL1)
	assert.Equal(t, []string{key}, r5.Resolved, "L1 收口后前端三态仍要能看到已消解")
	assert.Empty(t, r5.Persisted)
	assert.Empty(t, r5.New)
	assert.Empty(t, r5.RepeatCount)
	assert.Empty(t, r5.GuardKeys())
	assert.Equal(t, 30, r5.Round, "整轮改稿只应推进一次，而非 28→32")
}

func TestRepeatL0DoesNotBreakGuardRounds(t *testing.T) {
	// L1 已收口后的新一轮重检（PendingL1=false）必须照常开轮并累加，
	// 否则连续未消除的轮次保护永远触发不了。
	tail := f("tail_hook", "C1", "")
	key := "l0|tail_hook|#0"
	r := NextFindingRound(nil, "l0", []Finding{tail})
	row := rowOf([]Finding{tail}, nil, r)
	r = NextFindingRound(row, "l1", nil)
	row = rowOf([]Finding{tail}, nil, r)
	for i := 1; i < FindingGuardRounds; i++ {
		r = NextFindingRound(row, "l0", []Finding{tail})
		assert.True(t, r.PendingL1)
		row = rowOf([]Finding{tail}, nil, r)
		r = NextFindingRound(row, "l1", nil)
		row = rowOf([]Finding{tail}, nil, r)
	}
	require.Equal(t, FindingGuardRounds, r.RepeatCount[key],
		"每轮 L1 收口后不得被判成同轮重复重算")
	assert.Equal(t, []string{key}, r.GuardKeys())
}

func TestRepeatL0WiringIsPureAndLocal(t *testing.T) {
	src, err := os.ReadFile("finding_round.go")
	require.NoError(t, err)
	s := string(src)
	for _, want := range []string{
		`case "l0":`,
		`sameRound = oldRound.PendingL1 && oldRound.Round > 0 &&`,
		`equalKeys(oldRound.L0Keys, l0Keys)`,
		`if sameRound {`,
		`prev = oldRound.PrevKeys`,
		`carryRepeatCount(oldRound.RepeatCount, d)`,
	} {
		assert.Contains(t, s, want, "跨轮快照缺少 #124 接线 %q", want)
	}
	for _, banned := range []string{
		"RunSubAgent", "StartIssueFixTickets", "autoFix", "http.Post",
		"Upload", "SetLLMSignalSink",
	} {
		assert.False(t, strings.Contains(s, banned), "快照写入不得含执行体/自动修复/上传 %q", banned)
	}

	// 读侧零改动：DTO 只用写侧算好的 len(round.Resolved)，前端三态 badge 已有。
	dto, err := os.ReadFile("../pipeline/detect_status.go")
	require.NoError(t, err)
	assert.Contains(t, string(dto), "out.ResolvedCount = len(round.Resolved)",
		"三态必须继续由写侧快照单源透出，读侧不得自行重算")
}
