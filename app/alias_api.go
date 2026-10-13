package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/skill"
)

// AuthorAliasGroupDTO 是「作者别名分组」的前端传输结构：规范名 + 其别名（等价署名）列表。
// 与 internal/skill.AliasConfig 解耦，避免在绑定层直接暴露内部类型。
type AuthorAliasGroupDTO struct {
	Canonical string   `json:"canonical"`
	Aliases   []string `json:"aliases"`
}

// GetAuthorAliasesResult 是 GetAuthorAliases 的返回结构。
type GetAuthorAliasesResult struct {
	Version int                   `json:"version"`
	Groups  []AuthorAliasGroupDTO `json:"groups"`
}

// SaveAuthorAliasesInput 是 SaveAuthorAliases 的入参。
type SaveAuthorAliasesInput struct {
	Groups []AuthorAliasGroupDTO `json:"groups"`
}

// GetAuthorAliases 读取用户级「作者别名映射」配置，供前端别名管理面板展示与编辑。
// 文件不存在时返回空配置（不报错），与 LoadAliasConfig 的语义一致。
func (a *App) GetAuthorAliases() (*GetAuthorAliasesResult, error) {
	if a.skill == nil {
		return nil, fmt.Errorf("skill store 未初始化")
	}
	cfg, err := skill.LoadAliasConfig(config.UserSkillsDir())
	if err != nil {
		return nil, err
	}
	res := &GetAuthorAliasesResult{Version: cfg.Version}
	for _, g := range cfg.Groups {
		res.Groups = append(res.Groups, AuthorAliasGroupDTO{
			Canonical: g.Canonical,
			Aliases:   append([]string(nil), g.Aliases...),
		})
	}
	return res, nil
}

// SaveAuthorAliases 保存作者别名映射并立即重建跨书聚合档案，使新别名即时生效。
// 规范名为空的分组会被忽略（不写入）。保存后先清除用户级全部派生（aggregated）skill 文件、
// 再全量重建，避免别名归并导致旧的同名/孤立派生档遗留（RebuildAggregated 本身不清旧档）。
func (a *App) SaveAuthorAliases(input SaveAuthorAliasesInput) error {
	if a.skill == nil {
		return fmt.Errorf("skill store 未初始化")
	}
	cfg := &skill.AliasConfig{Version: 1, Groups: []skill.AuthorAliasGroup{}}
	for _, g := range input.Groups {
		canon := strings.TrimSpace(g.Canonical)
		if canon == "" {
			continue
		}
		cfg.Groups = append(cfg.Groups, skill.AuthorAliasGroup{
			Canonical: canon,
			Aliases:   append([]string(nil), g.Aliases...),
		})
	}
	if err := skill.SaveAliasConfig(config.UserSkillsDir(), cfg); err != nil {
		return fmt.Errorf("保存作者别名配置失败: %w", err)
	}
	// 别名变更后全量重建派生档案（先清旧派生档，避免归并后遗留孤立档）。
	a.rebuildAllAggregated()
	return nil
}

// rebuildAllAggregated 清除用户级全部派生（aggregated）skill 文件后全量重建（别名变更专用）。
// 派生档为自动产物，删除重建安全且幂等；删除失败不致命（RebuildAggregated 会覆盖同名）。
func (a *App) rebuildAllAggregated() {
	if a.skill == nil {
		return
	}
	existing, err := skill.LoadDir(config.UserSkillsDir())
	if err != nil {
		a.logger.Warn("别名重建：加载现有用户技能失败", "err", err)
		return
	}
	for i := range existing {
		if existing[i].Aggregated {
			_ = os.Remove(filepath.Join(config.UserSkillsDir(), skill.SanitizeFileName(existing[i].Name)+".md"))
		}
	}
	if _, err := a.RebuildAggregated(RebuildAggregatedInput{}); err != nil {
		a.logger.Warn("别名重建：全量重建跨书聚合档案失败", "err", err)
	}
}
