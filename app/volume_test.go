package app

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumeManagementAPI(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	firstName := "第一卷"
	first, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &firstName})
	require.NoError(t, err)
	assert.Equal(t, git.VolumePath(first.ID), first.OutlineFilePath)
	secondName := "第二卷"
	second, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &secondName})
	require.NoError(t, err)
	assert.Equal(t, git.VolumePath(second.ID), second.OutlineFilePath)

	volumes, err := app.GetVolumes(novel.ID)
	require.NoError(t, err)
	require.Len(t, volumes, 2)
	assert.Equal(t, []int64{first.ID, second.ID}, []int64{volumes[0].ID, volumes[1].ID})
	assert.Equal(t, []string{first.OutlineFilePath, second.OutlineFilePath}, []string{volumes[0].OutlineFilePath, volumes[1].OutlineFilePath})

	require.NoError(t, app.UpdateVolume(novel.ID, second.ID, "终卷"))
	moved, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, SourceVolumeID: &first.ID})
	require.NoError(t, err)
	assert.Equal(t, first.OutlineFilePath, moved.OutlineFilePath)

	volumes, err = app.GetVolumes(novel.ID)
	require.NoError(t, err)
	require.Len(t, volumes, 2)
	assert.Equal(t, "终卷", volumes[0].Name)
	assert.Equal(t, []int64{second.ID, first.ID}, []int64{volumes[0].ID, volumes[1].ID})
	assert.Equal(t, []string{second.OutlineFilePath, first.OutlineFilePath}, []string{volumes[0].OutlineFilePath, volumes[1].OutlineFilePath})

	require.NoError(t, git.WriteFile(novel.ID, git.VolumePath(first.ID), "第一卷大纲"))
	require.NoError(t, app.DeleteVolume(novel.ID, first.ID))
	_, err = git.ReadFile(novel.ID, git.VolumePath(first.ID))
	assert.True(t, errors.Is(err, os.ErrNotExist), "err = %v, want os.ErrNotExist", err)
	volumes, err = app.GetVolumes(novel.ID)
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	assert.Equal(t, second.ID, volumes[0].ID)
	require.NoError(t, app.DeleteVolume(novel.ID, second.ID))
	volumes, err = app.GetVolumes(novel.ID)
	require.NoError(t, err)
	assert.Empty(t, volumes)
}

func TestDeleteVolumeRejectsNonEmptyOrForeignVolume(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	otherNovel := createTestNovel(t, app)

	volumeName := "第一卷"
	v, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &volumeName})
	require.NoError(t, err)
	_, err = app.CreateChapter(CreateChapterInput{NovelID: novel.ID, Title: "卷内章节"})
	require.NoError(t, err)
	require.NoError(t, git.WriteFile(novel.ID, git.VolumePath(v.ID), "不可删除的卷纲"))

	err = app.DeleteVolume(novel.ID, v.ID)
	assert.ErrorIs(t, err, volume.ErrHasChapters)
	content, err := git.ReadFile(novel.ID, git.VolumePath(v.ID))
	require.NoError(t, err)
	assert.Equal(t, "不可删除的卷纲", content)

	err = app.UpdateVolume(otherNovel.ID, v.ID, "越权改名")
	assert.True(t, errors.Is(err, volume.ErrNotFound), "err = %v, want ErrNotFound", err)
}

func TestDeleteVolumeRollsBackWhenOutlineCleanupFails(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	volumeName := "第一卷"
	v, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &volumeName})
	require.NoError(t, err)

	outlinePath, err := git.ResolvePath(git.VolumePath(v.ID), novel.ID)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(outlinePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outlinePath, "blocked"), []byte("blocked"), 0o644))

	err = app.DeleteVolume(novel.ID, v.ID)
	require.Error(t, err)
	_, err = app.volume.GetByID(app.ctx, nil, novel.ID, v.ID)
	require.NoError(t, err)
}

// TestSetVolumeFrozen_CommitsSnapshot 冻结接线（WBS 2.2）：unfrozen→frozen
// 提交 committed snapshot；幂等重复冻结不重复提交；解冻不涉快照；再冻结版本递增。
func TestSetVolumeFrozen_CommitsSnapshot(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	name := "第一卷"
	v, err := app.PlaceVolume(PlaceVolumeInput{NovelID: novel.ID, Name: &name})
	require.NoError(t, err)

	snapCount := func() int64 {
		var n int64
		require.NoError(t, app.db.Table("volume_snapshots").Where("volume_id = ?", v.ID).Count(&n).Error)
		return n
	}

	frozen, err := app.SetVolumeFrozen(novel.ID, v.ID, true)
	require.NoError(t, err)
	assert.True(t, frozen.Frozen)
	assert.NotNil(t, frozen.FrozenAt)
	assert.Equal(t, int64(1), snapCount(), "首冻应提交一份快照")

	// 幂等：重复冻结不重复提交
	again, err := app.SetVolumeFrozen(novel.ID, v.ID, true)
	require.NoError(t, err)
	assert.True(t, again.Frozen)
	assert.Equal(t, int64(1), snapCount(), "重复冻结不应再提交快照")

	// 解冻不涉快照
	thawed, err := app.SetVolumeFrozen(novel.ID, v.ID, false)
	require.NoError(t, err)
	assert.False(t, thawed.Frozen)
	assert.Nil(t, thawed.FrozenAt)
	assert.Equal(t, int64(1), snapCount())

	// 再冻结 = 新版本快照，diff_ref 指向基准
	refrozen, err := app.SetVolumeFrozen(novel.ID, v.ID, true)
	require.NoError(t, err)
	assert.True(t, refrozen.Frozen)
	assert.Equal(t, int64(2), snapCount())

	var snaps []volume.VolumeSnapshot
	require.NoError(t, app.db.Where("volume_id = ?", v.ID).Order("snapshot_version ASC").Find(&snaps).Error)
	require.Len(t, snaps, 2)
	assert.Equal(t, 1, snaps[0].SnapshotVersion)
	assert.Equal(t, 2, snaps[1].SnapshotVersion)
	assert.Equal(t, "base:"+strconv.FormatInt(snaps[0].ID, 10), snaps[1].DiffRef)
}
