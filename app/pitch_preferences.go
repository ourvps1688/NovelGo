package app

// 卖点孵化引导项（book-pitch 采纳方案 / 书架编辑）幂等收敛。
//
// 背景：旧链路每次保存都走 CreatePreference 纯新增，自动保存与手动「采纳方案」
// 各写一组，导致 NovelProfile 把重复行和冲突平台值（如「番茄」+「七猫」）
// 一起注入 system 消息——白烧 token 且制造语义矛盾。intentFromPitch 取最新
// 规避了，但 format.go 的 FormatPreferences 是全量注入，没有 category 过滤。
//
// 本 API 把五类按 (novel_id, category) 收敛引导行为唯一一条，语义为全量 PUT：
// 未提供的字段 / 空串 = 清除该类引导行（用户手写长纪律保留）。平台值命中已注册
// profile 且本书尚无任何检查器配置行时顺手武装（存在配置行即视为用户已表态，不覆盖）。

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/quality"
	"gorm.io/gorm"
)

// pitchManualRuleMinRunes：用户手写的长篇创作纪律与 pitch 的短引导值共用
// 「平台约束 / 创作约束」两个历史分类（如「第5–7章审读确立的写作纪律…」数百字）。
// 收敛只处理短引导行与本次写入值；超过该长度且不等于本次值的行视为手写纪律，
// 一律不动，避免用户在偏好面板写的规则被建书表单覆盖或删除。
// 方向/标签/卖点三类没有这个历史包袱，仍做全量收敛。
const pitchManualRuleMinRunes = 80

// UpsertPitchPreferencesInput 是本书五类引导项的全量 PUT 入参。
// 字段与前端 preferenceCategories.ts 的 CATEGORY_* 常量一一对应
// （后端按 pitchCategories 固定中文分类名落库，勿改）。
type UpsertPitchPreferencesInput struct {
	Tags      string `json:"tags"`      // 标签
	Direction string `json:"direction"` // 方向（表单「选定方向」）
	Platform  string `json:"platform"`  // 平台约束
	Style     string `json:"style"`     // 创作约束（表单「风格」）
	OneLiner  string `json:"one_liner"` // 一句话卖点
}

// UpsertPitchPreferences 幂等收敛本书五类引导项。
//
// 事务内按 pitchCategories 固定顺序处理：同类多条保留 id 最大（最近终稿）的一条、
// 删除其余旧稿；内容变化时就地更新，避免无谓刷新 updated_at；内容为空则整组删除。
// 只处理 novel_id 本书且 is_global=false 的行，全局偏好与其它书不受影响。
func (a *App) UpsertPitchPreferences(novelID int64, input UpsertPitchPreferencesInput) error {
	values := map[string]string{
		"一句话卖点": strings.TrimSpace(input.OneLiner),
		"方向":    strings.TrimSpace(input.Direction),
		"平台约束":  strings.TrimSpace(input.Platform),
		"创作约束":  strings.TrimSpace(input.Style),
		"标签":    strings.TrimSpace(input.Tags),
	}

	err := a.db.WithContext(a.ctx).Transaction(func(tx *gorm.DB) error {
		for _, cat := range pitchCategories {
			content := values[cat]
			var rows []preference.PreferenceItem
			if err := tx.Where("novel_id = ? AND is_global = ? AND category = ?", novelID, false, cat).
				Order("id ASC").Find(&rows).Error; err != nil {
				return fmt.Errorf("读取引导项 %s 失败: %w", cat, err)
			}
			// 只在「可由本表单管理」的行上收敛：手写长纪律原样保留。
			managed := rows[:0]
			protectLong := cat == "平台约束" || cat == "创作约束"
			for _, row := range rows {
				if protectLong && utf8.RuneCountInString(row.Content) > pitchManualRuleMinRunes && row.Content != content {
					continue
				}
				managed = append(managed, row)
			}
			rows = managed

			if len(rows) == 0 {
				if content == "" {
					continue
				}
				if err := tx.Create(&preference.PreferenceItem{
					NovelID:  novelID,
					IsGlobal: false,
					Category: cat,
					Content:  content,
				}).Error; err != nil {
					return fmt.Errorf("写入引导项 %s 失败: %w", cat, err)
				}
				continue
			}

			keep := rows[len(rows)-1] // id 最大 = 最近一次终稿
			if len(rows) > 1 {
				ids := make([]int64, 0, len(rows)-1)
				for _, row := range rows[:len(rows)-1] {
					ids = append(ids, row.ID)
				}
				if err := tx.Where("id IN ?", ids).Delete(&preference.PreferenceItem{}).Error; err != nil {
					return fmt.Errorf("清理引导项 %s 重复行失败: %w", cat, err)
				}
			}
			if content == "" {
				if err := tx.Delete(&preference.PreferenceItem{}, keep.ID).Error; err != nil {
					return fmt.Errorf("清除引导项 %s 失败: %w", cat, err)
				}
				continue
			}
			if keep.Content != content {
				if err := tx.Model(&preference.PreferenceItem{}).Where("id = ?", keep.ID).
					Update("content", content).Error; err != nil {
					return fmt.Errorf("更新引导项 %s 失败: %w", cat, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return a.armPlatformProfile(novelID, values["平台约束"])
}

// armPlatformProfile 把「平台约束」里的平台名联动到检查器 platform_profile：
// 只有已注册量化口径的平台（当前仅七猫/qimao）才武装，未注册名（番茄/起点等）
// 静默跳过——宁缺勿错，不臆造口径。仅当本书尚无 profile 配置时补齐，
// 用户已显式设定的值一律不覆盖。
func (a *App) armPlatformProfile(novelID int64, platform string) error {
	preset := quality.PlatformProfileByName(platform)
	if preset == nil {
		return nil
	}
	// 只看「有没有配置行」而非 profile 值：已有行说明用户或工具已经碰过配置
	// （包括显式清空 profile 表示「不要武装」），一律不覆盖。
	var existing []quality.CheckerConfig
	if err := a.db.WithContext(a.ctx).Where("novel_id = ?", novelID).Limit(1).
		Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	cfg := quality.DefaultCheckerConfig(novelID)
	cfg.PlatformProfile = preset.Name
	return quality.NewStore(a.db, a.logger).SaveCheckerConfig(a.ctx, cfg)
}
