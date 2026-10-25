package structure

// power_level.go —— 力量体系境界等级表（R2b-6，提案 §9.2 战力/境界崩坏）。
//
// 境界有序枚举 + 数值锚：把「修仙等级/战力阶位」从自由文本机制化为可数值比对的台账，
// 供两类确定性检查消费：
//   - capability_ceiling 数值比对合流（invariant_check.go）：上限是境界名时按数值锚比对；
//   - 越级取胜/低阶碾压高阶规则拦截（quality L0 power_upset/power_crush）。
//
// 刻度纪律：数值文本与境界数值锚不同源，禁止混比（宁缺勿错）；
// 缺表/未登记境界 → 相关检查自然跳过（缺省=现行行为逐字一致）。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// PowerLevel 是一条境界等级（力量体系数值序条目）。
// (novel_id, name) 唯一：同名重复 upsert 覆盖数值锚/描述；rank 越大越强。
type PowerLevel struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID     int64     `gorm:"column:novel_id;not null;uniqueIndex:uk_power_level,priority:1;index" json:"novel_id"`
	Name        string    `gorm:"column:name;not null;uniqueIndex:uk_power_level,priority:2" json:"name"` // 境界名（如「金丹期」）
	Rank        int       `gorm:"column:rank;not null" json:"rank"`                                       // 数值锚：越大越强，同书内可比
	Description string    `gorm:"column:description" json:"description"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (PowerLevel) TableName() string { return "power_levels" }

// Validate 校验境界条目契约：名称非空、数值锚 >= 1（越级/碾压比对的可比性前提）。
func (p *PowerLevel) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return fmt.Errorf("境界名不能为空")
	}
	if p.Rank < 1 {
		return fmt.Errorf("境界「%s」数值锚非法：%d（须 >= 1，越大越强）", p.Name, p.Rank)
	}
	return nil
}

// UpsertPowerLevel 立/覆盖一条境界等级（(novel_id, name) 唯一，同名重复调用覆盖）。
// 先过 Validate：名称为空/数值锚非法直接拒收，不落库。
func (s *Store) UpsertPowerLevel(ctx context.Context, p *PowerLevel) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "novel_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"rank", "description", "updated_at"}),
	}).Create(p).Error
}

// ListPowerLevels 列出某书全部境界等级（rank ASC, id ASC，确定性顺序：弱→强）。
func (s *Store) ListPowerLevels(ctx context.Context, novelID int64) ([]PowerLevel, error) {
	var rows []PowerLevel
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("rank ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("structure store: list power levels: %w", err)
	}
	return rows, nil
}

// RankOfLevel 按境界名查数值锚（精确名匹配，TrimSpace 后比较）；
// 未登记/非法条目返回 ok=false（宁缺勿错）。
func RankOfLevel(levels []PowerLevel, name string) (int, bool) {
	target := strings.TrimSpace(name)
	if target == "" {
		return 0, false
	}
	for _, lv := range levels {
		if lv.Rank >= 1 && strings.TrimSpace(lv.Name) == target {
			return lv.Rank, true
		}
	}
	return 0, false
}
