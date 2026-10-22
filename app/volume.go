package app

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/volume"
)

// PlaceVolume 创建或移动卷，并将其置于锚点卷前或小说末尾。
type PlaceVolumeInput = volume.PlaceInput

func (a *App) PlaceVolume(input PlaceVolumeInput) (*volume.Volume, error) {
	v, err := a.volume.Place(a.ctx, nil, input)
	if err != nil {
		return nil, fmt.Errorf("place volume: %w", err)
	}
	return v, nil
}

// UpdateVolume 重命名指定小说中的卷。
func (a *App) UpdateVolume(novelID, volumeID int64, name string) error {
	if err := a.volume.Update(a.ctx, nil, novelID, volumeID, name); err != nil {
		return fmt.Errorf("update volume: %w", err)
	}
	return nil
}

// DeleteVolume 删除指定小说中的空卷。
func (a *App) DeleteVolume(novelID, volumeID int64) error {
	return a.db.WithContext(a.ctx).Transaction(func(tx *gorm.DB) error {
		if err := a.volume.Delete(a.ctx, tx, novelID, volumeID); err != nil {
			return fmt.Errorf("delete volume: %w", err)
		}
		if err := git.RemoveFile(novelID, git.VolumePath(volumeID)); err != nil {
			return fmt.Errorf("卷已删除，但清理卷纲失败: %w", err)
		}
		return nil
	})
}

// GetVolumes 返回指定小说的全部卷，按阅读顺序排列。
func (a *App) GetVolumes(novelID int64) ([]volume.Volume, error) {
	volumes, err := a.volume.ListByNovel(a.ctx, nil, novelID)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	return volumes, nil
}

// SetVolumeFrozen 置位/解除卷冻结态（G_freeze）：frozen=true 后该卷正文与既定设定
// 不再被流水线自动回改（S5 写保护拒写），跨卷问题默认前向适配本卷。
// 冻结（unfrozen→frozen）在同一事务内提交 committed snapshot（WBS 2.2 卷快照），
// 作为后续卷的既定事实与 retcon 回滚点；已是冻结态时幂等、不重复提交。
// 返回最新卷状态。解冻供作者显式撤回定稿（如需继续修订本卷），不涉快照。
func (a *App) SetVolumeFrozen(novelID, volumeID int64, frozen bool) (*volume.Volume, error) {
	if frozen {
		v, _, err := a.snapshots.Freeze(a.ctx, novelID, volumeID)
		if err != nil {
			return nil, fmt.Errorf("set volume frozen: %w", err)
		}
		return v, nil
	}
	v, err := a.volume.SetFrozen(a.ctx, nil, novelID, volumeID, false)
	if err != nil {
		return nil, fmt.Errorf("set volume frozen: %w", err)
	}
	return v, nil
}
