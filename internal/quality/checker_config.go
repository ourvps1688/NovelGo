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
	TabooList string `gorm:"column:taboo_list;type:text" json:"taboo_list"`
	// PremiseList 是开篇核心看点关键词（JSON 字符串数组，如 ["灾星","吞星炼命"]）：
	// 标题/简介卖点词，第 1 章前 500 字内须实体化（P0-0 开篇体检，七猫量化口径）；
	// 空清单=不检查（现行行为）。
	PremiseList string `gorm:"column:premise_list;type:text" json:"premise_list"`
	// CheatList 是金手指/核心机制关键词（JSON 字符串数组）：第 1 章前 800 字内须出场
	// （P0-0 开篇体检）；空清单=不检查（现行行为）。
	CheatList string `gorm:"column:cheat_list;type:text" json:"cheat_list"`
	// ProtagonistList 是主角称呼（正名+常用别名，JSON 字符串数组）：第 1 章前 200 字内
	// 主角须首现入戏（P0-0 开篇体检）；空清单=不检查（现行行为）。
	ProtagonistList string    `gorm:"column:protagonist_list;type:text" json:"protagonist_list"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
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
		PremiseList:      "[]",
		CheatList:        "[]",
		ProtagonistList:  "[]",
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
func (c *CheckerConfig) Taboos() []string { return parseStringList(c.TabooList) }

// Premises 解析开篇核心看点关键词（坏数据按空清单，宁缺勿错）。
func (c *CheckerConfig) Premises() []string { return parseStringList(c.PremiseList) }

// Cheats 解析金手指/核心机制关键词（坏数据按空清单，宁缺勿错）。
func (c *CheckerConfig) Cheats() []string { return parseStringList(c.CheatList) }

// Protagonists 解析主角称呼（坏数据按空清单，宁缺勿错）。
func (c *CheckerConfig) Protagonists() []string { return parseStringList(c.ProtagonistList) }

// Opening 投影开篇体检规则到 L0 输入（P0-0）：三组关键词全空 → nil=整专项不检查
// （缺省=现行行为逐字一致）。
func (c *CheckerConfig) Opening() *L0Opening {
	o := &L0Opening{
		PremiseKeywords:  c.Premises(),
		CheatKeywords:    c.Cheats(),
		ProtagonistNames: c.Protagonists(),
	}
	if len(o.PremiseKeywords) == 0 && len(o.CheatKeywords) == 0 && len(o.ProtagonistNames) == 0 {
		return nil
	}
	return o
}

// parseStringList 解析 JSON 字符串数组（坏数据按空清单，宁缺勿错）。
func parseStringList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

// normalizeStringList 归一化 JSON 字符串数组：TrimSpace 去空去重保序后重新序列化；
// 空输入归一 []；非 JSON 字符串数组报错。
func normalizeStringList(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "[]", nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return "", err
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
		return "", fmt.Errorf("序列化失败：%w", err)
	}
	return string(b), nil
}

// Validate 校验并归一化配置：负数拒绝；四组清单须为 JSON 字符串数组（TrimSpace 去空去重）。
func (c *CheckerConfig) Validate() error {
	if c.BeatDensityMin < 0 {
		return fmt.Errorf("检查器配置爽点密度下限不得为负（beat_density_min=%.2f）", c.BeatDensityMin)
	}
	if c.WindowN < 0 {
		return fmt.Errorf("检查器配置窗口 N 不得为负（window_n=%d）", c.WindowN)
	}
	lists := []struct {
		name string
		raw  *string
	}{
		{"禁忌清单", &c.TabooList},
		{"开篇核心看点关键词", &c.PremiseList},
		{"金手指关键词", &c.CheatList},
		{"主角称呼", &c.ProtagonistList},
	}
	for _, item := range lists {
		out, err := normalizeStringList(*item.raw)
		if err != nil {
			return fmt.Errorf("检查器配置%s不是 JSON 字符串数组：%w", item.name, err)
		}
		*item.raw = out
	}
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
		DoUpdates: clause.AssignmentColumns([]string{"beat_density_min", "tail_hook_required", "window_n", "taboo_list", "premise_list", "cheat_list", "protagonist_list", "updated_at"}),
	}).Create(cfg).Error
}
