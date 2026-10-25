package structure

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func ptrID(v int64) *int64 { return &v }

// TestCheckForeshadowLifecycleThreeKinds 三类告警齐发 + 干净条目不告警。
func TestCheckForeshadowLifecycleThreeKinds(t *testing.T) {
	entries := []ForeshadowEntry{
		{ID: 1, Title: "黑衣人身份", Status: "pending", TargetReadingNumber: 12, SourceChapterID: ptrID(3)},
		{ID: 2, Title: "母亲死因", Status: "pending", TargetReadingNumber: 0},
		{ID: 3, Title: "三年之约", Status: "resolved", TargetReadingNumber: 20, SourceChapterID: nil, ResolvedChapterID: ptrID(19)},
		{ID: 4, Title: "干净伏笔", Status: "pending", TargetReadingNumber: 30, SourceChapterID: ptrID(5)},
		{ID: 5, Title: "已收有源", Status: "resolved", SourceChapterID: ptrID(2), ResolvedChapterID: ptrID(8)},
		{ID: 6, Title: "作者弃坑", Status: "abandoned", TargetReadingNumber: 0},
	}
	got := CheckForeshadowLifecycle(entries, 20, 0)
	require.Len(t, got, 3)

	require.Equal(t, "S1", got[0].ID)
	require.Equal(t, CheckForeshadowOverdue, got[0].Check)
	require.Equal(t, "time_entries:1", got[0].Ref)
	require.Contains(t, got[0].Desc, "黑衣人身份")
	require.Contains(t, got[0].Desc, "预期第 12 章前回收")
	require.Contains(t, got[0].Desc, "当前进度第 20 章")

	require.Equal(t, "S2", got[1].ID)
	require.Equal(t, CheckForeshadowNoTarget, got[1].Check)
	require.Equal(t, "time_entries:2", got[1].Ref)

	require.Equal(t, "S3", got[2].ID)
	require.Equal(t, CheckForeshadowUnsourced, got[2].Check)
	require.Equal(t, "time_entries:3", got[2].Ref)
	require.Contains(t, got[2].Desc, "收未埋=无源")

	for _, f := range got {
		require.Equal(t, FindingLevelDefault, f.Level)
	}
}

// TestCheckForeshadowLifecycleZeroFindings 零告警降级：空清单/全干净/仅弃坑 → 空清单。
func TestCheckForeshadowLifecycleZeroFindings(t *testing.T) {
	require.Empty(t, CheckForeshadowLifecycle(nil, 50, 0))
	require.Empty(t, CheckForeshadowLifecycle([]ForeshadowEntry{}, 50, 0))
	require.Empty(t, CheckForeshadowLifecycle([]ForeshadowEntry{
		{ID: 1, Title: "在轨", Status: "pending", TargetReadingNumber: 30},
		{ID: 2, Title: "已收", Status: "resolved", SourceChapterID: ptrID(1), ResolvedChapterID: ptrID(9)},
		{ID: 3, Title: "弃坑", Status: "abandoned"},
	}, 20, 0))
}

// TestCheckForeshadowLifecycleDeterminism 确定性：乱序输入产出逐字一致（按条目 ID 定序编号）。
func TestCheckForeshadowLifecycleDeterminism(t *testing.T) {
	entries := []ForeshadowEntry{
		{ID: 7, Title: "B", Status: "pending", TargetReadingNumber: 0},
		{ID: 3, Title: "A", Status: "pending", TargetReadingNumber: 2, SourceChapterID: ptrID(1)},
		{ID: 5, Title: "C", Status: "resolved"},
	}
	shuffled := []ForeshadowEntry{entries[1], entries[2], entries[0]}
	a := CheckForeshadowLifecycle(entries, 9, 0)
	b := CheckForeshadowLifecycle(shuffled, 9, 0)
	require.Equal(t, a, b)
	// 编号按条目 ID 升序：3 → 5 → 7。
	require.Equal(t, []string{"S1", "S2", "S3"}, []string{a[0].ID, a[1].ID, a[2].ID})
	require.Equal(t, []string{"time_entries:3", "time_entries:5", "time_entries:7"}, []string{a[0].Ref, a[1].Ref, a[2].Ref})
}

// TestCheckForeshadowLifecycleBoundary 超期边界：当前进度==预期收章不算超期，过一章才算；
// 无章节进度（now=0）不触发超期，其余检查照常。
func TestCheckForeshadowLifecycleBoundary(t *testing.T) {
	entries := []ForeshadowEntry{
		{ID: 1, Title: "临界", Status: "pending", TargetReadingNumber: 10, SourceChapterID: ptrID(1)},
	}
	require.Empty(t, CheckForeshadowLifecycle(entries, 10, 0), "当章到期不算超期")
	got := CheckForeshadowLifecycle(entries, 11, 0)
	require.Len(t, got, 1)
	require.Equal(t, CheckForeshadowOverdue, got[0].Check)

	// now=0：超期不触发，无收点照常告警。
	mixed := append(entries, ForeshadowEntry{ID: 2, Title: "无收点", Status: "pending", TargetReadingNumber: 0})
	got = CheckForeshadowLifecycle(mixed, 0, 0)
	require.Len(t, got, 1)
	require.Equal(t, CheckForeshadowNoTarget, got[0].Check)
}

// TestCheckForeshadowLifecycleOverdueGrace 题材卡窗口 N（R2b-3）：
// 宽限内不告警、出宽限照常告警；负宽限归 0（宁缺勿错）；宽限只压超期，
// 无收点/无源不受宽限影响。
func TestCheckForeshadowLifecycleOverdueGrace(t *testing.T) {
	entries := []ForeshadowEntry{
		{ID: 1, Title: "临界", Status: "pending", TargetReadingNumber: 10, SourceChapterID: ptrID(1)},
	}
	// 宽限 3 章：now=13 在宽限内零告警，now=14 出宽限告警。
	require.Empty(t, CheckForeshadowLifecycle(entries, 13, 3))
	got := CheckForeshadowLifecycle(entries, 14, 3)
	require.Len(t, got, 1)
	require.Equal(t, CheckForeshadowOverdue, got[0].Check)

	// 负宽限归 0：与零宽限同口径（now=11 即告警）。
	got = CheckForeshadowLifecycle(entries, 11, -5)
	require.Len(t, got, 1)
	require.Equal(t, CheckForeshadowOverdue, got[0].Check)

	// 宽限只压超期：无收点/无源照常告警。
	mixed := []ForeshadowEntry{
		{ID: 1, Title: "临界", Status: "pending", TargetReadingNumber: 10, SourceChapterID: ptrID(1)},
		{ID: 2, Title: "无收点", Status: "pending", TargetReadingNumber: 0},
		{ID: 3, Title: "无源", Status: "resolved"},
	}
	got = CheckForeshadowLifecycle(mixed, 13, 3)
	require.Len(t, got, 2)
	require.Equal(t, CheckForeshadowNoTarget, got[0].Check)
	require.Equal(t, CheckForeshadowUnsourced, got[1].Check)
}
