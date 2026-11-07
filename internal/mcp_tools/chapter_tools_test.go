package mcp_tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/mcp_tools"
)

// execMoveChapter 经完整注册表执行 move_chapter（走 args 宽容化与注册链路）。
func execMoveChapter(t *testing.T, ctx context.Context, tc mcp_tools.ToolContext, args map[string]any) *mcp_tools.ToolResult {
	t.Helper()
	reg := newTestRegistry(t)
	b, err := json.Marshal(args)
	require.NoError(t, err)
	return reg.Execute(ctx, "move_chapter", b, tc, nil)
}

// 跨卷移动：目标卷插入位之前 + 原卷收缝 + 全书阅读序号实时重算。
func TestMoveChapter_MovesAcrossVolumes(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	v1 := seedVolume(t, db, 1, "第一卷", 1)
	v2 := seedVolume(t, db, 1, "第二卷", 2)
	c1 := seedChapter(t, db, 1, &v1, 1, 1)
	c2 := seedChapter(t, db, 1, &v1, 2, 2)
	c3 := seedChapter(t, db, 1, &v2, 3, 1)
	c4 := seedChapter(t, db, 1, &v2, 4, 2)
	// 阅读序：c1=1 c2=2 c3=3 c4=4

	res := execMoveChapter(t, ctx, tc, map[string]any{
		"chapter_id":        c1,
		"before_chapter_id": c4,
	})
	require.True(t, res.Success, "move_chapter 失败: %s", res.Error)
	require.Equal(t, v2, res.Data["volume_id"])
	require.Equal(t, 2, res.Data["sort_order"])
	require.Equal(t, 3, res.Data["reading_number"])

	// 卷一收缝：c2 顶到 1；卷二让位：c3=1 c1=2 c4=3
	got := fetchChapter(t, db, c1)
	require.NotNil(t, got.VolumeID)
	require.Equal(t, v2, *got.VolumeID)
	require.Equal(t, 2, got.SortOrder)
	require.Equal(t, 1, fetchChapter(t, db, c2).SortOrder)
	require.Equal(t, 1, fetchChapter(t, db, c3).SortOrder)
	require.Equal(t, 3, fetchChapter(t, db, c4).SortOrder)
}

// 只传 before_chapter_id：目标组跟随 before 章（含未分卷）。
func TestMoveChapter_BeforeInfersTargetGroup(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	v1 := seedVolume(t, db, 1, "第一卷", 1)
	c1 := seedChapter(t, db, 1, &v1, 1, 1)
	c2 := seedChapter(t, db, 1, nil, 2, 1) // 未分卷
	c3 := seedChapter(t, db, 1, nil, 3, 2) // 未分卷

	res := execMoveChapter(t, ctx, tc, map[string]any{
		"chapter_id":        c1,
		"before_chapter_id": c3,
	})
	require.True(t, res.Success, "move_chapter 失败: %s", res.Error)
	require.Nil(t, res.Data["volume_id"], "应跟随 before 章移入未分卷")

	got := fetchChapter(t, db, c1)
	require.Nil(t, got.VolumeID)
	require.Equal(t, 2, got.SortOrder) // c2(1) c1(2) c3(3)
	require.Equal(t, 1, fetchChapter(t, db, c2).SortOrder)
	require.Equal(t, 3, fetchChapter(t, db, c3).SortOrder)
}

// target_volume_id=0：显式移入未分卷组。
func TestMoveChapter_TargetZeroToUnassigned(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	v1 := seedVolume(t, db, 1, "第一卷", 1)
	c1 := seedChapter(t, db, 1, &v1, 1, 1)
	seedChapter(t, db, 1, nil, 2, 1)

	res := execMoveChapter(t, ctx, tc, map[string]any{
		"chapter_id":       c1,
		"target_volume_id": 0,
	})
	require.True(t, res.Success, "move_chapter 失败: %s", res.Error)
	require.Nil(t, res.Data["volume_id"])
	require.Equal(t, 2, res.Data["sort_order"])

	got := fetchChapter(t, db, c1)
	require.Nil(t, got.VolumeID)
	require.Equal(t, 2, got.SortOrder)
}

// 只传 target_volume_id：追加到目标卷末尾。
func TestMoveChapter_AppendsToVolumeEnd(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	v1 := seedVolume(t, db, 1, "第一卷", 1)
	v2 := seedVolume(t, db, 1, "第二卷", 2)
	c1 := seedChapter(t, db, 1, &v1, 1, 1)
	seedChapter(t, db, 1, &v2, 2, 1)
	seedChapter(t, db, 1, &v2, 3, 2)

	res := execMoveChapter(t, ctx, tc, map[string]any{
		"chapter_id":       c1,
		"target_volume_id": v2,
	})
	require.True(t, res.Success, "move_chapter 失败: %s", res.Error)
	require.Equal(t, v2, res.Data["volume_id"])
	require.Equal(t, 3, res.Data["sort_order"])
}

// 入参与归属校验：缺目标/章节不存在/before 不存在/before 自身/before 跨组/卷不属本书。
func TestMoveChapter_ValidationErrors(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	v1 := seedVolume(t, db, 1, "第一卷", 1)
	v2 := seedVolume(t, db, 1, "第二卷", 2)
	c1 := seedChapter(t, db, 1, &v1, 1, 1)
	c2 := seedChapter(t, db, 1, &v1, 2, 2)
	c3 := seedChapter(t, db, 1, &v2, 3, 1)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"缺目标", map[string]any{"chapter_id": c1}, "须指定"},
		{"章节不存在", map[string]any{"chapter_id": int64(9999), "target_volume_id": v1}, "章节不存在"},
		{"before 不存在", map[string]any{"chapter_id": c1, "before_chapter_id": int64(9999)}, "before 章节不存在"},
		{"before 自身", map[string]any{"chapter_id": c1, "before_chapter_id": c1}, "不能是章节自身"},
		{"before 跨组", map[string]any{"chapter_id": c1, "target_volume_id": v2, "before_chapter_id": c2}, "不属于目标卷"},
		{"卷不属本书", map[string]any{"chapter_id": c1, "target_volume_id": int64(9999)}, "卷不存在"},
	}
	for _, tc2 := range cases {
		t.Run(tc2.name, func(t *testing.T) {
			res := execMoveChapter(t, ctx, tc, tc2.args)
			require.False(t, res.Success, "expected failure")
			require.Contains(t, res.Error, tc2.want)
		})
	}
	// 全部失败后原状不变
	require.Equal(t, 1, fetchChapter(t, db, c1).SortOrder)
	require.Equal(t, 2, fetchChapter(t, db, c2).SortOrder)
	require.Equal(t, 1, fetchChapter(t, db, c3).SortOrder)
}
