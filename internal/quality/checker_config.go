package quality

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CheckerConfig 是 novel 级检查器配置（题材卡机制化，提案 §9.2 末）：
// 「题材卡从文案约定变成机器可检参数」——爽点密度下限/章尾钩子必需/窗口 N/禁忌清单
// 由本书配置驱动 RunL0Checks 与结构检查，而非技能文案自觉。
//
// 缺省纪律（勿改）：DefaultCheckerConfig = 现行默认行为（逐字一致）；
// 未配置/读取失败一律回落缺省，绝不因配置缺失改变检查口径。
type CheckerConfig struct {
	NovelID int64 `gorm:"column:novel_id;primaryKey" json:"novel_id"`
	// BeatDensityMin 是爽点密度下限（次/章）。节奏曲线数据源（L1 逐章爽点产出）
	// 尚未落地，本参数先行入配置（§9.2 参数清单齐备），落地节奏曲线检查时消费；
	// 0=不限（缺省）。
	BeatDensityMin float64 `gorm:"column:beat_density_min;not null;default:0" json:"beat_density_min"`
	// TailHookRequired 章尾钩子是否必需：nil=未配置→缺省 true（现行行为：缺钩告警）。
	// 指针三态避免 GORM 零值跳写把显式 false 写成默认 true（characters.alive 同坑同解）。
	TailHookRequired *bool `gorm:"column:tail_hook_required" json:"tail_hook_required,omitempty"`
	// WindowN 是窗口 N（题材卡节奏窗口）：伏笔超期宽限章数——超预期收章 N 章内不告警
	// （防预期收章估算误差）；0=零宽限（现行行为：过期即告警）。
	WindowN int `gorm:"column:window_n;not null;default:0" json:"window_n"`
	// TabooList 是禁忌清单（JSON 字符串数组，如 ["虐主","破墙"]）：L0 逐词确定性扫描，
	// 空清单=不检查（现行行为）。主题级禁忌（禁虐主等）由作者以关键词形式钉入。
	TabooList string    `gorm:"column:taboo_list;type:text" json:"taboo_list"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (CheckerConfig) TableName() string { return "novel_checker_configs" }

// DefaultCheckerConfig 返回缺省配置：全部现行默认（消费侧逐字一致）。
func DefaultCheckerConfig(novelID int64) *CheckerConfig {
	return &CheckerConfig{
		NovelID:          novelID,
		BeatDensityMin:   0,
		TailHookRequired: nil,
		WindowN:          0,
		TabooList:        "[]",
	}
}

// HookRequired 返回章尾钩子是否必需（nil=缺省 true=现行行为）。
func (c *CheckerConfig) HookRequired() bool {
	return c.TailHookRequired == nil || *c.TailHookRequired
}

// OverdueGrace 返回伏笔超期宽限章数（负数归 0，宁缺勿错）。
func (c *CheckerConfig) OverdueGrace() int {
	if c.WindowN < 0 {
		return 0
	}
	return c.WindowN
}

// Taboos 解析禁忌清单（坏数据按空清单，宁缺勿错）。
func (c *CheckerConfig) Taboos() []string {
	if strings.TrimSpace(c.TabooList) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(c.TabooList), &list); err != nil {
		return nil
	}
	return list
}

// Validate 校验并归一化配置：负数拒绝；禁忌清单须为 JSON 字符串数组（TrimSpace 去空去重）。
func (c *CheckerConfig) Validate() error {
	if c.BeatDensityMin < 0 {
		return fmt.Errorf("检查器配置爽点密度下限不得为负（beat_density_min=%.2f）", c.BeatDensityMin)
	}
	if c.WindowN < 0 {
		return fmt.Errorf("检查器配置窗口 N 不得为负（window_n=%d）", c.WindowN)
	}
	raw := strings.TrimSpace(c.TabooList)
	if raw == "" {
		c.TabooList = "[]"
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return fmt.Errorf("检查器配置禁忌清单不是 JSON 字符串数组：%w", err)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, item := range list {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("检查器配置禁忌清单序列化失败：%w", err)
	}
	c.TabooList = string(b)
	return nil
}

// GetCheckerConfig 读取本书检查器配置；未配置返回缺省（不报错）。
func (s *Store) GetCheckerConfig(ctx context.Context, novelID int64) (*CheckerConfig, error) {
	var cfg CheckerConfig
	if err := s.DB.WithContext(ctx).Where("novel_id = ?", novelID).First(&cfg).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return DefaultCheckerConfig(novelID), nil
		}
		return nil, fmt.Errorf("quality store: get checker config: %w", err)
	}
	return &cfg, nil
}

// SaveCheckerConfig 保存本书检查器配置（按 novel_id 覆盖）。先过 Validate。
func (s *Store) SaveCheckerConfig(ctx context.Context, cfg *CheckerConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "novel_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"beat_density_min", "tail_hook_required", "window_n", "taboo_list", "updated_at"}),
	}).Create(cfg).Error
}
