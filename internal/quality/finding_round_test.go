package quality

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// finding_round_test.go —— #114 P1 跨轮对齐行为契约。
// 重点锁住「一轮内 L0 先写、L1 后写」的时序：L0 阶段消掉的发现，
// 不能在 L1 收口时被吞掉。

func rowOf(l0, l1 []Finding, round FindingRound) *ChapterQuality {
	return &ChapterQuality{
		NovelID: 1, ChapterID: 7,
		L0Findings:   MarshalFindings(l0),
		L1Findings:   MarshalFindings(l1),
		L1Status:     L1StatusChecked,
		FindingRound: MarshalFindingRound(round),
	}
}

func TestNextFindingRoundFirstRound(t *testing.T) {
	r := NextFindingRound(nil, "l0", []Finding{f("tail_hook", "C1", ""), f("beat_gap", "C2", "")})
	assert.Equal(t, 1, r.Round)
	assert.True(t, r.PendingL1)
	assert.Empty(t, r.Resolved)
	assert.Empty(t, r.Persisted)
	assert.Len(t, r.New, 2)
	for _, k := range r.New {
		assert.Equal(t, 1, r.RepeatCount[k])
	}
	assert.Empty(t, r.GuardKeys(), "首轮不得触发轮次保护")
}

func TestNextFindingRoundL0ThenL1KeepsResolved(t *testing.T) {
	tail := f("tail_hook", "C1", "")
	beat := f("beat_gap", "C2", "")
	reader := f("reader_state", "L1-1", "")

	// 轮 1：先写 L0，再写 L1（同轮收口）。
	r1 := NextFindingRound(nil, "l0", []Finding{tail, beat})
	row1 := rowOf([]Finding{tail, beat}, nil, r1)
	r1 = NextFindingRound(row1, "l1", []Finding{reader})
	assert.Equal(t, 1, r1.Round, "L1 收口不得新开第二轮")
	assert.False(t, r1.PendingL1)
	assert.Len(t, r1.New, 3)

	// 轮 2：改稿后重检——L0 消掉 beat_gap，L1 消掉 reader_state。
	row2 := rowOf([]Finding{tail, beat}, []Finding{reader}, r1)
	r2 := NextFindingRound(row2, "l0", []Finding{tail})
	assert.Equal(t, 2, r2.Round)
	assert.Equal(t, []string{"l0|beat_gap|#0"}, r2.Resolved, "L0 阶段消掉的发现必须可见")
	assert.Equal(t, []string{"l0|tail_hook|#0", "l1|reader_state|#0"}, r2.Persisted)

	row2b := rowOf([]Finding{tail}, []Finding{reader}, r2)
	r2b := NextFindingRound(row2b, "l1", nil)
	assert.Equal(t, 2, r2b.Round, "仍属轮 2")
	assert.False(t, r2b.PendingL1)
	assert.Equal(t, []string{"l0|beat_gap|#0", "l1|reader_state|#0"}, r2b.Resolved,
		"L1 收口后两半的 resolved 必须同时保留，不能被后写的一半吞掉")
	assert.Equal(t, []string{"l0|tail_hook|#0"}, r2b.Persisted)
}

func TestNextFindingRoundStandaloneL1StartsNewRound(t *testing.T) {
	tail := f("tail_hook", "C1", "")
	reader := f("reader_state", "L1-1", "")
	r1 := NextFindingRound(nil, "l1", []Finding{reader})
	row := rowOf([]Finding{tail}, []Finding{reader}, r1)
	r2 := NextFindingRound(row, "l1", nil)
	assert.Equal(t, 2, r2.Round, "PendingL1=false 时独立 L1 更新应开新轮")
	assert.Equal(t, []string{"l1|reader_state|#0"}, r2.Resolved)
}

func TestGuardKeysAfterThreePersistedRounds(t *testing.T) {
	tail := f("tail_hook", "C1", "")
	r := NextFindingRound(nil, "l0", []Finding{tail}) // 轮 1：new → 1
	row := rowOf([]Finding{tail}, nil, r)
	r = NextFindingRound(row, "l1", nil) // 轮 1 收口：计数保持
	row = rowOf([]Finding{tail}, nil, r)
	for i := 1; i < FindingGuardRounds; i++ {
		r = NextFindingRound(row, "l0", []Finding{tail}) // 新轮：persisted → i+1
		row = rowOf([]Finding{tail}, nil, r)
		r = NextFindingRound(row, "l1", nil) // 同轮收口：不得重置
		row = rowOf([]Finding{tail}, nil, r)
	}
	require.Equal(t, FindingGuardRounds, r.RepeatCount["l0|tail_hook|#0"])
	assert.Equal(t, []string{"l0|tail_hook|#0"}, r.GuardKeys(),
		"连续 N 轮未消除才升级为判定器冲突提示")
}

func TestParseFindingRoundBadData(t *testing.T) {
	assert.Equal(t, FindingRound{}, ParseFindingRound(""))
	assert.Equal(t, FindingRound{}, ParseFindingRound("{bad json"))
	assert.Empty(t, ParseFindingRound("").GuardKeys(), "旧档案无快照不得触发轮次保护")

	r := FindingRound{Round: 4, Persisted: []string{"a"}, RepeatCount: map[string]int{"a": 3}}
	assert.Equal(t, r, ParseFindingRound(MarshalFindingRound(r)), "序列化往返逐字一致")
}
