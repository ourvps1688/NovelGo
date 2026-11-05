package app

import (
	"github.com/novelgo/novelgo/internal/quality"
)

// ── 平台风格 profile（P0-1，一键配置）──────────────────────

// ApplyPlatformProfile 设置/清除本书平台风格 profile（P0-1）：
// profileName 为注册名（"qimao"/"七猫"，注册表见 quality/platform_profile.go）；
// 空串=清除（平台三查回到不武装，缺省=现行行为）。
// 仅覆盖 platform_profile 字段，其余检查器配置保持现值；未注册名返回错误（Validate 拒收）。
func (a *App) ApplyPlatformProfile(novelID int64, profileName string) error {
	store := quality.NewStore(a.db, a.logger)
	cfg, err := store.GetCheckerConfig(a.ctx, novelID)
	if err != nil {
		return err
	}
	cfg.PlatformProfile = profileName
	return store.SaveCheckerConfig(a.ctx, cfg)
}
