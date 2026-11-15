package quality

// A 方案：档案按「维度 × 正文版本」记账的真相源契约。
// UpsertL0 本身仍只写 L0 列（store 层分阶段写入契约不变）；由业务层在
// 「按落盘正文重算 L0」成功后显式 InvalidateL1，作废上一版正文的 L1 结论。

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvalidateL1ClearsPreviousVersionOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 11, 15, 12, 36, 0, 0, time.UTC)

	// 上一版正文（1879 字）：L0 与 L1 均已收口，L1 desc 里带历史硬编码字数。
	require.NoError(t, s.Upsert(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7, ReadingNumber: 7,
		L0Metrics:  MarshalL0Metrics(L0Metrics{WordCount: 1879}),
		L0Findings: MarshalFindings([]Finding{{ID: "C1", Check: CheckPacingDrag, Level: FindingLevelDefault, Status: FindingStatusPending}}),
		L1Findings: MarshalFindings([]Finding{{ID: "L1-1", Check: "pacing", Level: FindingLevelDefault,
			Status: FindingStatusPending, Desc: "权威字数 1879（word_count），尚差约 1100 字"}}),
		L1Status: L1StatusChecked, Round: 4, CheckedAt: ts,
	}))

	// 本版正文（3135 字）：L0 重算落档 + 显式作废旧 L1。
	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{
		NovelID: 1, ChapterID: 7, ReadingNumber: 7,
		L0Metrics:  MarshalL0Metrics(L0Metrics{WordCount: 3135}),
		L0Findings: MarshalFindings(nil), CheckedAt: ts.Add(time.Hour),
	}))
	require.NoError(t, s.InvalidateL1(ctx, 1, 7))

	got, err := s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	// L0 是本版正文的确定性结果，逐字不动。
	assert.Equal(t, 3135, ParseL0Metrics(got.L0Metrics).WordCount)
	assert.Empty(t, ParseFindings(got.L0Findings), "本版 L0 零发现不得被作废动作改写")
	assert.True(t, got.CheckedAt.After(ts), "checked_at 仍归 UpsertL0 刷新，作废动作不动它")
	assert.NotEmpty(t, got.FindingRound, "跨轮快照仍归 UpsertL0 维护，作废动作不动它")
	// 旧版 L1 结论必须消失，状态诚实回到 pending。
	assert.Equal(t, L1StatusPending, got.L1Status, "本版 L1 未复核，不得沿用旧版 checked")
	assert.Empty(t, ParseFindings(got.L1Findings), "旧版 1879 字描述不得留在档案里")
	assert.NotContains(t, got.L1Findings, "1879")
	assert.NotContains(t, got.L1Findings, "1100")
	// finding_round.pending_l1 由 UpsertL0 置起，作废动作不得把它抹掉。
	assert.True(t, ParseFindingRound(got.FindingRound).PendingL1, "本轮 L1 尚未收口")
}

func TestInvalidateL1ThenUpsertL1ClosesCurrentVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{
		NovelID: 2, ChapterID: 9, ReadingNumber: 9,
		L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 3135}), CheckedAt: time.Now(),
	}))
	require.NoError(t, s.InvalidateL1(ctx, 2, 9))
	// 用户显式点「重新检测本章」后 L1 收口：本版结论才允许 checked。
	require.NoError(t, s.UpsertL1(ctx, &ChapterQuality{
		NovelID: 2, ChapterID: 9, ReadingNumber: 9,
		L1Findings: MarshalFindings([]Finding{{ID: "L1-2", Check: "continuity", Level: FindingLevelDefault,
			Status: FindingStatusPending, Desc: "实测 3135 字，已达区间"}}),
		L1Status: L1StatusChecked, CheckedAt: time.Now(),
	}))

	got, err := s.GetByChapter(ctx, 2, 9)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, L1StatusChecked, got.L1Status)
	require.Len(t, ParseFindings(got.L1Findings), 1)
	assert.Contains(t, ParseFindings(got.L1Findings)[0].Desc, "3135")
	assert.False(t, ParseFindingRound(got.FindingRound).PendingL1, "L1 收口后本轮才算完成")
	assert.Equal(t, 3135, ParseL0Metrics(got.L0Metrics).WordCount, "L1 收口不得覆盖本版 L0")
}

func TestInvalidateL1IsIdempotentAndChapterScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, chID := range []int64{11, 12} {
		require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{
			NovelID: 3, ChapterID: chID, ReadingNumber: int(chID),
			L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 2000}), CheckedAt: time.Now(),
		}))
		require.NoError(t, s.UpsertL1(ctx, &ChapterQuality{
			NovelID: 3, ChapterID: chID, ReadingNumber: int(chID),
			L1Findings: MarshalFindings([]Finding{{ID: "L1", Check: "pacing", Level: FindingLevelDefault, Status: FindingStatusPending}}),
			L1Status:   L1StatusChecked, CheckedAt: time.Now(),
		}))
	}
	// 只作废第 11 章；重复作废不报错；第 12 章不受影响。
	require.NoError(t, s.InvalidateL1(ctx, 3, 11))
	require.NoError(t, s.InvalidateL1(ctx, 3, 11))
	// 不存在的章作废是安全的 no-op（纯 UPDATE，零新增行）。
	require.NoError(t, s.InvalidateL1(ctx, 3, 999))

	ch11, err := s.GetByChapter(ctx, 3, 11)
	require.NoError(t, err)
	assert.Equal(t, L1StatusPending, ch11.L1Status)
	assert.Empty(t, ParseFindings(ch11.L1Findings))
	ch12, err := s.GetByChapter(ctx, 3, 12)
	require.NoError(t, err)
	assert.Equal(t, L1StatusChecked, ch12.L1Status, "不得误伤其他章")
	assert.Len(t, ParseFindings(ch12.L1Findings), 1)
	missing, err := s.GetByChapter(ctx, 3, 999)
	require.NoError(t, err)
	assert.Nil(t, missing, "作废不存在的章不得凭空建行")
}
