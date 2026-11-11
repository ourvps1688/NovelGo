package quality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// finding_round_store_test.go —— #114 P1 落库接线：UpsertL0/UpsertL1 覆盖前
// 必须把跨轮三态写进同一行，且不破坏「L0/L1 分阶段互不覆盖」既有契约。

func TestUpsertL0L1AdvanceFindingRound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	tail := f("tail_hook", "C1", "")
	beat := f("beat_gap", "C2", "")
	reader := f("reader_state", "L1-1", "")

	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7, ReadingNumber: 1,
		L0Findings: MarshalFindings([]Finding{tail, beat}), CheckedAt: ts,
	}))
	row, err := s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, row)
	r1 := ParseFindingRound(row.FindingRound)
	assert.Equal(t, 1, r1.Round)
	assert.True(t, r1.PendingL1)
	assert.Len(t, r1.New, 2)

	require.NoError(t, s.UpsertL1(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7, ReadingNumber: 1,
		L1Findings: MarshalFindings([]Finding{reader}),
		L1Status:   L1StatusChecked, CheckedAt: ts,
	}))
	row, err = s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	r1b := ParseFindingRound(row.FindingRound)
	assert.Equal(t, 1, r1b.Round, "L1 收口不得新开第二轮")
	assert.False(t, r1b.PendingL1)
	assert.Len(t, r1b.New, 3)
	assert.Len(t, ParseFindings(row.L0Findings), 2, "UpsertL1 不得覆盖 L0 列")

	// 改稿后重检：L0 只剩 tail_hook（beat_gap 已消掉）。
	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7, ReadingNumber: 1,
		L0Findings: MarshalFindings([]Finding{tail}), CheckedAt: ts.Add(time.Hour),
	}))
	row, err = s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	r2 := ParseFindingRound(row.FindingRound)
	assert.Equal(t, 2, r2.Round)
	assert.Equal(t, []string{"l0|beat_gap|#0"}, r2.Resolved)
	assert.Equal(t, []string{"l0|tail_hook|#0", "l1|reader_state|#0"}, r2.Persisted)
	assert.Len(t, ParseFindings(row.L1Findings), 1, "UpsertL0 不得覆盖 L1 列")
}

func TestUpsertL0LegacyRowWithoutSnapshot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// 旧数据：用 Upsert 写入一行且不带 finding_round（模拟本功能上线前的档案）。
	require.NoError(t, s.Upsert(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7, L1Status: L1StatusChecked,
		L0Findings: MarshalFindings([]Finding{f("tail_hook", "C1", "")}),
	}))
	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7,
		L0Findings: MarshalFindings(nil),
	}))
	row, err := s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	r := ParseFindingRound(row.FindingRound)
	assert.Equal(t, []string{"l0|tail_hook|#0"}, r.Resolved,
		"旧档案无快照时按当前行作上一轮基线，仍能算出消掉了哪条")
	assert.Empty(t, r.GuardKeys(), "旧数据不得凭空触发轮次保护")
}
