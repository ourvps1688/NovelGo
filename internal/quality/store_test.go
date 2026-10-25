package quality

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewStore(newTestDB(t), logger)
}

// TestUpsertIdempotent 同章重复 Upsert 只留最新（幂等断言）。
func TestUpsertIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ts := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	first := &ChapterQuality{NovelID: 1, ChapterID: 7, ReadingNumber: 3,
		L0Metrics:  MarshalL0Metrics(L0Metrics{WordCount: 2100, HasTailHook: true}),
		L0Findings: MarshalFindings(nil), L1Status: L1StatusPending,
		CheckedAt: ts}
	require.NoError(t, s.Upsert(ctx, first))
	require.NotZero(t, first.ID)

	second := &ChapterQuality{NovelID: 1, ChapterID: 7, ReadingNumber: 5,
		L0Findings: MarshalFindings([]Finding{{ID: "C1", Check: "tail_hook", Level: FindingLevelDefault, Status: FindingStatusPending}}),
		L1Findings: MarshalFindings([]Finding{{ID: "L1-1", Check: "continuity", Level: FindingLevelDefault, Status: FindingStatusPending}}),
		L1Status:   L1StatusChecked, CheckedAt: ts.Add(time.Hour)}
	require.NoError(t, s.Upsert(ctx, second))

	list, err := s.ListByNovel(ctx, 1)
	require.NoError(t, err)
	require.Len(t, list, 1, "同章重复 Upsert 不得新增行")
	assert.Equal(t, first.ID, list[0].ID, "应更新同一行")
	assert.Equal(t, 5, list[0].ReadingNumber, "字段整体覆盖")
	assert.Empty(t, list[0].L0Metrics, "未带 l0_metrics 即覆盖为空（宁缺勿错）")
	assert.Equal(t, L1StatusChecked, list[0].L1Status)
	assert.Len(t, ParseFindings(list[0].L0Findings), 1)
}

// TestUpsertValidation 载荷校验：空档案/缺键/非法 l1_status 拒收。
func TestUpsertValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	assert.Error(t, s.Upsert(ctx, nil))
	assert.Error(t, s.Upsert(ctx, &ChapterQuality{NovelID: 0, ChapterID: 1}))
	assert.Error(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 0}))
	assert.Error(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 1, L1Status: "done"}))
	assert.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 1, L1Status: L1StatusChecked}))
}

// TestUpsertL0PreservesL1 分阶段写入：L0 落档不动 L1 列（异步 L1 结果不被同步 L0 覆盖）。
func TestUpsertL0PreservesL1(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// L1 先落（模拟上一轮 L1 结果仍在行上）。
	require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 7,
		L1Findings: MarshalFindings([]Finding{{ID: "L1-1", Check: "continuity", Level: FindingLevelDefault, Status: FindingStatusFixed}}),
		L1Status:   L1StatusChecked, Round: 2}))

	// L0 重跑（用户又编辑了一章）：只覆盖 L0 列。
	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{NovelID: 1, ChapterID: 7, ReadingNumber: 3,
		L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 2100}), L0Findings: MarshalFindings(nil), Round: 3}))

	got, err := s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotEmpty(t, got.L0Metrics, "L0 列已写")
	assert.Equal(t, 3, got.Round)
	assert.Equal(t, L1StatusChecked, got.L1Status, "L1 状态不被 L0 落档覆盖")
	assert.Len(t, ParseFindings(got.L1Findings), 1, "L1 发现不被 L0 落档覆盖")
}

// TestUpsertL1PreservesL0 分阶段写入：L1 落档不动 L0 列。
func TestUpsertL1PreservesL0(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{NovelID: 1, ChapterID: 7, ReadingNumber: 3,
		L0Metrics:  MarshalL0Metrics(L0Metrics{WordCount: 2100, HasTailHook: true}),
		L0Findings: MarshalFindings([]Finding{{ID: "C1", Check: "word_count", Level: FindingLevelDefault, Status: FindingStatusPending}})}))

	require.NoError(t, s.UpsertL1(ctx, &ChapterQuality{NovelID: 1, ChapterID: 7, ReadingNumber: 3,
		L1Findings: MarshalFindings(nil), L1Status: L1StatusChecked, Round: 1}))

	got, err := s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 2100, ParseL0Metrics(got.L0Metrics).WordCount, "L0 指标不被 L1 落档覆盖")
	assert.Len(t, ParseFindings(got.L0Findings), 1, "L0 发现不被 L1 落档覆盖")
	assert.Equal(t, L1StatusChecked, got.L1Status)
	assert.Equal(t, 1, got.Round)
}

// TestUpsertL0CreatesWithPendingL1 行不存在时 L0 落档创建行，L1 侧为待查。
func TestUpsertL0CreatesWithPendingL1(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertL0(ctx, &ChapterQuality{NovelID: 1, ChapterID: 7, ReadingNumber: 3,
		L0Findings: MarshalFindings(nil)}))

	got, err := s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, L1StatusPending, got.L1Status, "L0 创建行时 L1 为待查（规范化 pending）")
	assert.Empty(t, got.L1Findings)
}

// TestGetByChapter 不存在返回 (nil, nil)。
func TestGetByChapter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetByChapter(ctx, 1, 404)
	require.NoError(t, err)
	assert.Nil(t, got, "不存在返回 (nil, nil)")

	require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 7}))
	got, err = s.GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(7), got.ChapterID)

	got, err = s.GetByChapter(ctx, 2, 7)
	require.NoError(t, err)
	assert.Nil(t, got, "他书同号章不串档")
}

// TestListByNovel 按阅读序号升序返回。
func TestListByNovel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 乱序写入。
	for _, rn := range []int{5, 1, 3} {
		require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: int64(rn * 10), ReadingNumber: rn}))
	}
	require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 2, ChapterID: 99, ReadingNumber: 1}))

	list, err := s.ListByNovel(ctx, 1)
	require.NoError(t, err)
	require.Len(t, list, 3, "不混入他书档案")
	assert.Equal(t, []int{1, 3, 5}, []int{list[0].ReadingNumber, list[1].ReadingNumber, list[2].ReadingNumber})

	empty, err := s.ListByNovel(ctx, 404)
	require.NoError(t, err)
	assert.Empty(t, empty)
}

// TestSummary 档案汇总：覆盖章数/L1 已查/发现计数/状态分布（L2 档案汇总快审输入）。
func TestSummary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 1, ReadingNumber: 1,
		L0Metrics: MarshalL0Metrics(L0Metrics{WordCount: 2100}),
		L0Findings: MarshalFindings([]Finding{
			{ID: "C1", Check: "word_count", Level: FindingLevelDefault, Status: FindingStatusPending},
			{ID: "C2", Check: "tail_hook", Level: FindingLevelDefault, Status: FindingStatusFixed},
		}),
		L1Findings: MarshalFindings(nil), L1Status: L1StatusChecked}))

	require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 2, ReadingNumber: 2,
		L0Findings: MarshalFindings([]Finding{
			{ID: "C1", Check: "style_drift", Level: FindingLevelDefault, Status: ""},
		}),
		L1Findings: MarshalFindings([]Finding{
			{ID: "L1-1", Check: "continuity", Level: FindingLevelDefault, Status: FindingStatusSkipped},
		}),
		L1Status: L1StatusChecked}))

	require.NoError(t, s.Upsert(ctx, &ChapterQuality{NovelID: 1, ChapterID: 3, ReadingNumber: 3,
		L0Findings: MarshalFindings(nil), L1Status: L1StatusPending}))

	sum, err := s.Summary(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, sum)
	assert.Equal(t, 3, sum.Chapters)
	assert.Equal(t, 2, sum.L1Checked)
	assert.Equal(t, 3, sum.L0Findings)
	assert.Equal(t, 1, sum.L1Findings)
	assert.Equal(t, 2, sum.OpenFindings, "pending 与空状态都算 open")
	assert.Equal(t, 1, sum.ByCheck["word_count"])
	assert.Equal(t, 1, sum.ByCheck["tail_hook"])
	assert.Equal(t, 1, sum.ByCheck["style_drift"])
	assert.Equal(t, 1, sum.ByCheck["continuity"])
	assert.Equal(t, 2, sum.ByStatus[FindingStatusPending], "空状态归入 pending")
	assert.Equal(t, 1, sum.ByStatus[FindingStatusFixed])
	assert.Equal(t, 1, sum.ByStatus[FindingStatusSkipped])

	// 空书：零汇总，非 nil。
	empty, err := s.Summary(ctx, 404)
	require.NoError(t, err)
	require.NotNil(t, empty)
	assert.Equal(t, 0, empty.Chapters)
	assert.NotNil(t, empty.ByCheck)
}
