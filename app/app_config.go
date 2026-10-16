package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/datamove"
	"github.com/novelgo/novelgo/internal/platform"
	"github.com/novelgo/novelgo/internal/storage"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// GetAppConfig 返回当前运行时配置信息（供前端诊断）。
func (a *App) GetAppConfig() map[string]any {
	if a.cfg == nil {
		return map[string]any{"initialized": false}
	}
	return map[string]any{
		"initialized": true,
		"data_dir":    config.DataDirPath(),
	}
}

// PickDirectory 弹出系统原生目录选择框，返回用户选定的目录路径；用户取消时返回空串。
// 目录选择逻辑放在 Go 侧（wails runtime 官方支持），前端仅调用此方法拿路径再传给
// UpdateDataDir，避免依赖前端未生成的 dialog 绑定。
func (a *App) PickDirectory() (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "选择数据目录",
	})
	if err != nil {
		return "", fmt.Errorf("打开目录选择框失败: %w", err)
	}
	return dir, nil
}

// UpdateDataDir 更改数据目录，并将原目录中的数据（小说 Git 仓库、SQLite 库、模型等）
// 安全迁移到新目录，随后重新初始化所有运行时模块。
//
// force=true 表示用户已确认覆盖非空目标目录（源数据优先合并进目标）。
// 目标非空且 force=false 时返回 datamove.ErrDstNotEmpty，由前端弹确认框后带 force 重试。
func (a *App) UpdateDataDir(newPath string, force bool) error {
	if newPath == "" {
		return fmt.Errorf("数据目录路径不能为空")
	}

	oldDir := config.DataDirPath()

	// 归一化：展开 ~ 并转为绝对路径（与 config.Save 保持一致）。
	home, _ := os.UserHomeDir()
	switch {
	case newPath == "~":
		newPath = home
	case strings.HasPrefix(newPath, "~/"):
		newPath = filepath.Join(home, newPath[2:])
	}
	abs, err := filepath.Abs(newPath)
	if err != nil {
		return fmt.Errorf("解析数据目录绝对路径失败: %w", err)
	}
	newPath = abs

	if newPath == oldDir {
		return fmt.Errorf("新目录与当前数据目录相同")
	}
	if isSubpathOf(newPath, oldDir) || isSubpathOf(oldDir, newPath) {
		return fmt.Errorf("新目录不能与当前数据目录互相嵌套")
	}

	// 校验目标非空：未授权时直接返回，不改任何状态，交由前端确认。
	if info, serr := os.Stat(newPath); serr == nil && info.IsDir() {
		empty, eerr := dirIsEmpty(newPath)
		if eerr == nil && !empty && !force {
			return datamove.ErrDstNotEmpty
		}
	}

	// 关闭旧数据库，避免文件被锁导致迁移失败。
	if a.db != nil {
		if cerr := storage.Close(a.db); cerr != nil {
			return fmt.Errorf("关闭旧数据库失败: %w", cerr)
		}
		a.db = nil
	}

	// 执行迁移：同盘 rename / 跨盘递归拷贝 + 删除源。
	if rerr := datamove.Relocate(oldDir, newPath, datamove.Options{
		Force: force,
		Progress: func(done, total int64) {
			wailsruntime.EventsEmit(a.ctx, "data:relocate-progress", map[string]any{
				"done":  done,
				"total": total,
			})
		},
	}); rerr != nil {
		// 尽量回退到旧目录，保证应用仍可启动。
		if berr := a.restoreDataDir(oldDir); berr != nil {
			return fmt.Errorf("数据迁移失败且回退失败: %v；原错误: %w", berr, rerr)
		}
		return fmt.Errorf("数据迁移失败: %w", rerr)
	}

	// 写入新配置并使 DataDir 缓存失效，使后续 DataDir() 解析到新目录。
	if serr := config.Save(newPath); serr != nil {
		return fmt.Errorf("保存配置失败: %w", serr)
	}
	platform.ResetDataDirCache()

	cfg, lerr := config.Load()
	if lerr != nil {
		return fmt.Errorf("加载新配置失败: %w", lerr)
	}
	if ierr := a.initWithConfig(cfg); ierr != nil {
		return fmt.Errorf("重新初始化失败: %w", ierr)
	}

	a.logger.Info("数据目录已更改", "data_dir", config.DataDirPath())
	return nil
}

// restoreDataDir 将配置指回 oldDir 并尝试重新初始化，用于迁移失败后的回退。
func (a *App) restoreDataDir(oldDir string) error {
	if serr := config.Save(oldDir); serr != nil {
		return serr
	}
	platform.ResetDataDirCache()
	cfg, lerr := config.Load()
	if lerr != nil {
		return lerr
	}
	return a.initWithConfig(cfg)
}

// GetPlatform 返回平台信息，供前端决定默认路径等行为。
func (a *App) GetPlatform() map[string]any {
	return map[string]any{
		"os":          runtime.GOOS,
		"defaultPath": config.DataDirPath(),
	}
}

// isSubpathOf 判断 child 是否位于 parent 之内（不含相等）。
func isSubpathOf(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// dirIsEmpty 报告目录是否为空。
func dirIsEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}
