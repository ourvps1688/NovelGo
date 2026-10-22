package snapshot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/volume"
)

// maxCommitAttempts 快照版本冲突（uk_volume_snapshot）时的最大提交次数。
// 「冻结即串行提交」下并发极少；重试只是存储层兜底。
const maxCommitAttempts = 3

// diffRefPrefix diff_ref 对象键前缀：base:<snapshot_id> 指向基准快照行。
const diffRefPrefix = "base:"

// Service 提供卷快照的提交、查询、差异与回滚（WBS 2.2），及冻结前台账审计（WBS 2.4）。
type Service struct {
	db       *gorm.DB
	volumes  *volume.Store
	chapters *chapter.Store
}

// NewService 创建卷快照服务。
func NewService(db *gorm.DB, volumes *volume.Store, chapters *chapter.Store) *Service {
	return &Service{db: db, volumes: volumes, chapters: chapters}
}

// Freeze 置卷冻结（G_freeze），并在 unfrozen→frozen 迁移的同一事务内提交 committed
// snapshot（「冻结即串行提交」）。已是冻结态时幂等返回、不重复提交快照。
//
// 冻结迁移前先跑台账审计（WBS 2.4 防垃圾进垃圾出）：存在 error 级结构性不自洽时
// 拒绝冻结并返回 *LedgerInconsistentError（携带完整问题清单）；warning 级不阻断。
// 审计只在真正迁移时执行，幂等路径不重查。
//
// 返回最新卷状态与本次提交的快照（幂等路径快照为 nil）。
func (s *Service) Freeze(ctx context.Context, novelID, volumeID int64) (*volume.Volume, *volume.VolumeSnapshot, error) {
	for attempt := 0; ; attempt++ {
		v, snap, err := s.freezeOnce(ctx, novelID, volumeID)
		if err == nil {
			return v, snap, nil
		}
		if attempt+1 >= maxCommitAttempts || !isUniqueViolation(err) {
			return nil, nil, err
		}
	}
}

// Commit 提交该卷一份 committed snapshot，snapshot_version 自增
// （与 volumes.snapshot_version 对齐）。不依赖冻结态，供显式提交与后续 G_freeze
// 闸门（2.5）复用。diff_ref 指向基准快照（base:<id>），首版为空。
func (s *Service) Commit(ctx context.Context, novelID, volumeID int64) (*volume.VolumeSnapshot, error) {
	for attempt := 0; ; attempt++ {
		snap, err := s.commitOnce(ctx, novelID, volumeID)
		if err == nil {
			return snap, nil
		}
		if attempt+1 >= maxCommitAttempts || !isUniqueViolation(err) {
			return nil, err
		}
	}
}

// GetSnapshot 按 id 取快照行。
func (s *Service) GetSnapshot(ctx context.Context, snapshotID int64) (*volume.VolumeSnapshot, error) {
	var snap volume.VolumeSnapshot
	if err := s.db.WithContext(ctx).Where("id = ?", snapshotID).First(&snap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("snapshot: 快照不存在: %d", snapshotID)
		}
		return nil, fmt.Errorf("snapshot: 读取快照: %w", err)
	}
	return &snap, nil
}

// ListByVolume 返回该卷全部已提交快照，按 snapshot_version 升序。
func (s *Service) ListByVolume(ctx context.Context, novelID, volumeID int64) ([]volume.VolumeSnapshot, error) {
	var snaps []volume.VolumeSnapshot
	if err := s.db.WithContext(ctx).
		Where("novel_id = ? AND volume_id = ?", novelID, volumeID).
		Order("snapshot_version ASC").
		Find(&snaps).Error; err != nil {
		return nil, fmt.Errorf("snapshot: 列出快照: %w", err)
	}
	return snaps, nil
}

// DiffSnapshots 比较两份已提交快照（base→target）。
func (s *Service) DiffSnapshots(ctx context.Context, baseID, targetID int64) (*Delta, error) {
	base, err := s.GetSnapshot(ctx, baseID)
	if err != nil {
		return nil, err
	}
	target, err := s.GetSnapshot(ctx, targetID)
	if err != nil {
		return nil, err
	}
	basePayload, err := parsePayload(base.Payload)
	if err != nil {
		return nil, err
	}
	targetPayload, err := parsePayload(target.Payload)
	if err != nil {
		return nil, err
	}
	return Diff(basePayload, targetPayload), nil
}

// DiffToPrevious 比较快照与其 diff_ref 指向的基准版本（base:<id>）。
// 首版（diff_ref 空）以空基线比较=全量新增。
func (s *Service) DiffToPrevious(ctx context.Context, snapshotID int64) (*Delta, error) {
	target, err := s.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	targetPayload, err := parsePayload(target.Payload)
	if err != nil {
		return nil, err
	}
	var basePayload *Payload
	if baseID, ok := parseDiffRef(target.DiffRef); ok {
		base, err := s.GetSnapshot(ctx, baseID)
		if err != nil {
			return nil, err
		}
		if basePayload, err = parsePayload(base.Payload); err != nil {
			return nil, err
		}
	}
	return Diff(basePayload, targetPayload), nil
}

// DiffAgainstCurrent 比较快照与当前台账状态（前向修复/retcon 巡检的差异依据）。
func (s *Service) DiffAgainstCurrent(ctx context.Context, snapshotID int64) (*Delta, error) {
	snap, err := s.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	snapPayload, err := parsePayload(snap.Payload)
	if err != nil {
		return nil, err
	}
	current, err := capturePayload(ctx, s.db, snap.NovelID, snap.VolumeID,
		snap.SnapshotVersion, snap.CommittedAt)
	if err != nil {
		return nil, err
	}
	return Diff(snapPayload, current), nil
}

// Restore 把该小说的 series bible 台账整体回滚到快照提交时刻（retcon 回滚点）。
// 其后的全部台账改动会被丢弃；快照行自身、正文与邻接组织表不受影响。
// 警告：破坏性操作，调用方（retcon 状态机/作者显式确认）负责把关。
func (s *Service) Restore(ctx context.Context, snapshotID int64) (*RestoreSummary, error) {
	snap, err := s.GetSnapshot(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	payload, err := parsePayload(snap.Payload)
	if err != nil {
		return nil, err
	}
	var sum *RestoreSummary
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		sum, err = restoreTables(ctx, tx, payload)
		return err
	})
	if err != nil {
		return nil, err
	}
	return sum, nil
}

// freezeOnce 单次冻结事务：置冻结 + 迁移时提交快照。
func (s *Service) freezeOnce(ctx context.Context, novelID, volumeID int64) (*volume.Volume, *volume.VolumeSnapshot, error) {
	var outV *volume.Volume
	var outSnap *volume.VolumeSnapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		v, err := s.volumes.GetByID(ctx, tx, novelID, volumeID)
		if err != nil {
			return err
		}
		if v.Frozen {
			outV = v
			return nil // 幂等：已是冻结态，不动 frozen_at、不重复提交快照
		}
		// 2.4 冻结前台账审计：error 级不自洽禁止入快照（防垃圾进垃圾出）。
		// 走事务句柄 tx：SQLite 多连接下用 s.db 会读到另一份库态。
		report, err := s.auditLedgerIn(ctx, tx, novelID)
		if err != nil {
			return err
		}
		if report.ErrorCount > 0 {
			return &LedgerInconsistentError{Report: report}
		}
		if _, err := s.volumes.SetFrozen(ctx, tx, novelID, volumeID, true); err != nil {
			return err
		}
		snap, err := commitInTx(ctx, tx, s.volumes, novelID, volumeID)
		if err != nil {
			return err
		}
		outSnap = snap
		outV, err = s.volumes.GetByID(ctx, tx, novelID, volumeID)
		return err
	})
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot: 冻结卷: %w", err)
	}
	return outV, outSnap, nil
}

// commitOnce 单次提交事务（外层 Freeze/Commit 做版本冲突重试）。
func (s *Service) commitOnce(ctx context.Context, novelID, volumeID int64) (*volume.VolumeSnapshot, error) {
	var snap *volume.VolumeSnapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		snap, err = commitInTx(ctx, tx, s.volumes, novelID, volumeID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return snap, nil
}

// commitInTx 在事务内提交快照：
// 版本号 = max(volumes.snapshot_version, 已有快照最大版本)+1（两侧对齐、防漂移）；
// 采集全量 payload 后写 volume_snapshots 行并同步 volumes.snapshot_version。
// 冲突（uk_volume_snapshot）由外层重试。
func commitInTx(ctx context.Context, tx *gorm.DB, volumes *volume.Store, novelID, volumeID int64) (*volume.VolumeSnapshot, error) {
	v, err := volumes.GetByID(ctx, tx, novelID, volumeID)
	if err != nil {
		return nil, err
	}
	var maxRow int
	if err := tx.WithContext(ctx).Table("volume_snapshots").
		Select("COALESCE(MAX(snapshot_version), 0)").
		Where("volume_id = ?", volumeID).
		Scan(&maxRow).Error; err != nil {
		return nil, fmt.Errorf("snapshot: 查询最新快照版本: %w", err)
	}
	version := max(v.SnapshotVersion, maxRow) + 1

	diffRef := ""
	var prevs []volume.VolumeSnapshot
	if err := tx.WithContext(ctx).
		Where("volume_id = ? AND snapshot_version = ?", volumeID, version-1).
		Limit(1).Find(&prevs).Error; err != nil {
		return nil, fmt.Errorf("snapshot: 查询基准快照: %w", err)
	}
	if len(prevs) > 0 {
		diffRef = fmt.Sprintf("%s%d", diffRefPrefix, prevs[0].ID)
	} // 首版无基准，diff_ref 留空

	now := time.Now()
	payload, err := capturePayload(ctx, tx, novelID, volumeID, version, now)
	if err != nil {
		return nil, err
	}
	payloadJSON, err := payload.marshal()
	if err != nil {
		return nil, err
	}

	snap := &volume.VolumeSnapshot{
		NovelID:         novelID,
		VolumeID:        volumeID,
		SnapshotVersion: version,
		CommittedAt:     now,
		Payload:         payloadJSON,
		DiffRef:         diffRef,
	}
	if err := tx.WithContext(ctx).Create(snap).Error; err != nil {
		return nil, fmt.Errorf("snapshot: 提交快照: %w", err)
	}
	if err := tx.WithContext(ctx).Model(&volume.Volume{}).
		Where("id = ?", volumeID).
		Update("snapshot_version", version).Error; err != nil {
		return nil, fmt.Errorf("snapshot: 对齐卷快照版本: %w", err)
	}
	return snap, nil
}

// parseDiffRef 解析 diff_ref 对象键（base:<id>）。空/未知格式返回 ok=false。
func parseDiffRef(ref string) (int64, bool) {
	if !strings.HasPrefix(ref, diffRefPrefix) {
		return 0, false
	}
	var id int64
	if _, err := fmt.Sscanf(ref, diffRefPrefix+"%d", &id); err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// isUniqueViolation 判定存储层唯一约束冲突（uk_volume_snapshot 串行兜底的重试依据）。
// 按错误文本匹配：gorm 未开 TranslateError，驱动错误经 %w 包装后保留 sqlite 文本。
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
