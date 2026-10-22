package retcon

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
)

// Store 封装 retcon_cases 表的读写。
type Store struct {
	db *gorm.DB
}

// NewStore 构造仓储。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Create 写入一条新 retcon 案件。
func (s *Store) Create(ctx context.Context, c *RetconCase) error {
	return s.db.WithContext(ctx).Create(c).Error
}

// GetByID 按 id 取案件。
func (s *Store) GetByID(ctx context.Context, id int64) (*RetconCase, error) {
	var c RetconCase
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListByNovel 取该小说的全部 retcon 案件（id 倒序，最新在前）。
func (s *Store) ListByNovel(ctx context.Context, novelID int64) ([]RetconCase, error) {
	var rows []RetconCase
	if err := s.db.WithContext(ctx).Where("novel_id = ?", novelID).
		Order("id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// FindOpenMatch 查找与（issueID, description）匹配且未终态的 pipeline 候选，
// 供体检反复沉淀同一 issue 时去重。无匹配返回 (nil, nil)。
func (s *Store) FindOpenMatch(ctx context.Context, novelID int64, issueID, description string) (*RetconCase, error) {
	var c RetconCase
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND origin = ? AND issue_id = ? AND description = ? AND status IN ?",
			novelID, OriginPipeline, issueID, description,
			[]string{StatusCandidate, StatusApproved, StatusEditing, StatusRecorded}).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SetVolume 绑定目标卷（只改 volume_id，不触碰状态机字段）。
func (s *Store) SetVolume(ctx context.Context, id, volumeID int64) error {
	return s.db.WithContext(ctx).Model(&RetconCase{}).
		Where("id = ?", id).Update("volume_id", volumeID).Error
}

// Advance 在单事务内完成「读取→校验迁移→改写」，把案件推进到目标状态并应用
// apply 补丁（写裁决/编辑/回滚等伴随字段）。校验失败不落任何改动。
func (s *Store) Advance(ctx context.Context, id int64, to string, apply func(*RetconCase)) (*RetconCase, error) {
	var out *RetconCase
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c RetconCase
		if err := tx.Where("id = ?", id).First(&c).Error; err != nil {
			return err
		}
		if err := ValidateTransition(c.Status, to); err != nil {
			return err
		}
		if apply != nil {
			apply(&c)
		}
		c.Status = to
		if err := tx.Save(&c).Error; err != nil {
			return err
		}
		out = &c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// VolumeFrozen 查询目标卷的冻结态。卷不存在统一返回 (false, gorm.ErrRecordNotFound)。
// retcon 只针对已冻结定稿卷（「回改前卷」），调用方据此把关审批/发起。
func (s *Store) VolumeFrozen(ctx context.Context, novelID, volumeID int64) (bool, error) {
	var frozen bool
	row := s.db.WithContext(ctx).Table("volumes").
		Select("frozen").Where("id = ? AND novel_id = ?", volumeID, novelID).Row()
	if err := row.Scan(&frozen); err != nil {
		// database/sql 在无行时返回 ErrNoRows；统一成 gorm.ErrRecordNotFound 便于调用方判定。
		if errors.Is(err, sql.ErrNoRows) {
			return false, gorm.ErrRecordNotFound
		}
		return false, err
	}
	return frozen, nil
}
