package personasetting

import (
	"context"
	"errors"
	"log/slog"

	"gorm.io/gorm"
)

// Store 管理 PersonaSetting 持久化（仿 internal/writingconfig 的 DB 表 + Get/Save 幂等范式）。
type Store struct {
	DB     *gorm.DB
	logger *slog.Logger
}

// NewStore 创建 persona 设置存储。
func NewStore(db *gorm.DB, logger *slog.Logger) *Store {
	return &Store{DB: db, logger: logger}
}

// Get 返回本书 persona 设置，未配置时返回带缺省值的 PersonaSetting（Enabled=false，即普通模式）。
// 不落库，待 Save 才持久化。对脏数据（Enabled 缺省）以零值兜底即可——false 即普通模式。
func (s *Store) Get(ctx context.Context, novelID int64) (*PersonaSetting, error) {
	var p PersonaSetting
	err := s.DB.WithContext(ctx).First(&p, "novel_id = ?", novelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &PersonaSetting{NovelID: novelID, Enabled: false}, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Save 幂等 upsert：按 novel_id 定位，不存在则创建、存在则覆盖各字段。
// 仅覆盖 Enabled/Kind/Ref，保留 ID 与主键约束。
//
// 不使用 FirstOrCreate(struct)：其会把 p 的非零字段（含 Kind/Ref，而 Enabled=false
// 为零值被忽略）并入查询/属性，导致"已存在时按旧值匹配不到"而误建或漏更。
// 这里显式先按 novel_id 读，再对命中记录覆盖字段后 Save，语义清晰且稳定。
func (s *Store) Save(ctx context.Context, p *PersonaSetting) error {
	var existing PersonaSetting
	err := s.DB.WithContext(ctx).First(&existing, "novel_id = ?", p.NovelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.DB.WithContext(ctx).Create(p).Error
	}
	if err != nil {
		return err
	}
	existing.Enabled = p.Enabled
	existing.Kind = p.Kind
	existing.Ref = p.Ref
	return s.DB.WithContext(ctx).Save(&existing).Error
}
