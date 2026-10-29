package app

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/personasetting"
	"github.com/novelgo/novelgo/internal/skill"
)

// SaveContentInput 是保存文件内容的入参。
type SaveContentInput struct {
	NovelID         int64   `json:"novel_id"`
	Path            string  `json:"path"`
	Content         string  `json:"content"`
	ExpectedContent *string `json:"expected_content,omitempty"`
}

// GetContent 返回小说仓库中指定路径的文件内容。文件不存在时返回空字符串。
// 内置 skill 路径（/builtin/skills/）从内存读取。
func (a *App) GetContent(novelID int64, path string) (string, error) {
	if err := a.validateChapterContentPath(novelID, path); err != nil {
		return "", err
	}
	if err := a.validateVolumeContentPath(novelID, path); err != nil {
		return "", err
	}
	if strings.HasPrefix(path, "/builtin/skills/") {
		name := strings.TrimSuffix(strings.TrimPrefix(path, "/builtin/skills/"), ".md")
		if a.skill == nil {
			return "", os.ErrNotExist
		}
		sk, ok := a.skill.Get(novelID, name)
		if !ok {
			return "", os.ErrNotExist
		}
		return sk.RawContent, nil
	}

	content, err := git.ReadFile(novelID, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return content, nil
}

// SaveContent 保存小说仓库中指定路径的文件内容。
//
// 约定：app 绑定层只做 DTO 校验与文件落盘，跨介质派生编排（RAG 刷新 / 搜索缓存 /
// 字数统计 / 写作记录）下沉到 content.Service.DeriveAfterSave，app 不承载领域编排。
func (a *App) SaveContent(input SaveContentInput) error {
	if err := a.validateChapterContentPath(input.NovelID, input.Path); err != nil {
		return err
	}
	if err := a.validateVolumeContentPath(input.NovelID, input.Path); err != nil {
		return err
	}
	if isSkillPath(input.Path) {
		if _, err := skill.ParseBytes([]byte(input.Content), ""); err != nil {
			return fmt.Errorf("skill 格式错误: %w", err)
		}
	}

	var err error
	if input.ExpectedContent != nil {
		err = git.WriteFileIfUnchanged(input.NovelID, input.Path, *input.ExpectedContent, input.Content)
	} else {
		err = git.WriteFile(input.NovelID, input.Path, input.Content)
	}
	if errors.Is(err, git.ErrFileChanged) {
		return fmt.Errorf("CONTENT_CONFLICT: 磁盘内容已变化，请处理冲突后再保存: %w", err)
	}
	if err != nil {
		return err
	}

	// skill 文件落盘后的跨书聚合与心智自动激活（App 层职责，保持在此）。
	if isSkillPath(input.Path) {
		a.reconcileAggregated()
		a.autoActivateMentalSkill(input.NovelID, input.Content)
	}

	// 章节正文落盘后，刷新全部派生介质（编排下沉至 content.Service）。
	if ref, ok := git.ParseChapterLikePath(input.Path); ok && !ref.IsOutline && !ref.IsNew && ref.VolumeID == 0 {
		a.contentService.DeriveAfterSave(a.ctx, input.NovelID, ref.ID, input.Content, input.ExpectedContent)
	}

	return nil
}

func (a *App) validateChapterContentPath(novelID int64, filePath string) error {
	// 剥离前导 /：ResolvePath 的 filepath.Join 会吞掉它，若不剥离则 /chapters/... 会绕过校验
	clean := strings.TrimPrefix(strings.ToLower(path.Clean(strings.ReplaceAll(filePath, "\\", "/"))), "/")
	if !strings.HasPrefix(clean, "chapters/") && !strings.HasPrefix(clean, "outlines/") {
		return nil
	}

	ref, ok := git.ParseChapterLikePath(filePath)
	if !ok || ref.IsNew || ref.ID <= 0 {
		return fmt.Errorf("章节路径无效: %q", filePath)
	}
	canonical := git.ChapterPath(ref.ID)
	if ref.IsOutline {
		canonical = git.OutlinePath(ref.ID)
	}
	if filePath != canonical {
		return fmt.Errorf("章节路径非规范格式: %q，应使用 %q", filePath, canonical)
	}
	return a.ensureChapterIDsInNovel(novelID, []int64{ref.ID})
}

func (a *App) validateVolumeContentPath(novelID int64, filePath string) error {
	normalized := strings.ToLower(strings.ReplaceAll(filePath, "\\", "/"))
	// 同 validateChapterContentPath：剥离前导 /，避免 /volumes/... 绕过校验
	clean := strings.TrimPrefix(path.Clean(normalized), "/")
	if normalized != "volumes" && !strings.HasPrefix(normalized, "volumes/") &&
		clean != "volumes" && !strings.HasPrefix(clean, "volumes/") {
		return nil
	}

	volumeID, ok := git.ParseVolumePath(filePath)
	if !ok || filePath != git.VolumePath(volumeID) {
		return fmt.Errorf("卷纲路径无效: %q", filePath)
	}
	_, err := a.volume.GetByID(a.ctx, nil, novelID, volumeID)
	return err
}

func isSkillPath(p string) bool {
	return strings.HasPrefix(p, "skills/") || strings.HasPrefix(p, "~/.novelgo/skills/")
}

// autoActivateMentalSkill 在提取/创建的心智（kind=mental）技能落盘后，自动让它对本书生效：
// (A) 合并追加到本书 .enabled.json 勾选启用（等效临时 always，聊天 + 号菜单可见已勾选、常驻注入）；
// (C) 若本书尚未开启写作人格，则自动设为本书 PenLine 人格（kind=skill, ref=技能名），使其在写作模式常驻注入。
// 幂等、best-effort：任何失败仅记录告警，不影响 SaveContent 主流程。
// 仅当路径为 skill 路径时由 SaveContent 调用（input.Path 已通过 isSkillPath 校验）。
func (a *App) autoActivateMentalSkill(novelID int64, content string) {
	if novelID <= 0 {
		return
	}
	sk, err := skill.ParseBytes([]byte(content), "")
	if err != nil {
		a.logger.Warn("自动激活心智：解析失败，已跳过", "novel_id", novelID, "err", err)
		return
	}
	if sk.Kind != "mental" || sk.Name == "" {
		return
	}

	// (A) 合并追加到本书启用清单（保留已有勾选，去重）。
	enabled, err := skill.LoadEnabled(novelID)
	if err != nil {
		a.logger.Warn("自动激活心智：读取启用清单失败", "novel_id", novelID, "err", err)
	} else {
		already := false
		for _, n := range enabled {
			if n == sk.Name {
				already = true
				break
			}
		}
		if !already {
			enabled = append(enabled, sk.Name)
			if err := skill.SaveEnabled(novelID, enabled); err != nil {
				a.logger.Warn("自动激活心智：写入启用清单失败", "novel_id", novelID, "err", err)
			} else {
				a.logger.Info("自动激活心智：已勾选常驻启用", "novel_id", novelID, "skill", sk.Name)
			}
		}
	}

	// (C) 自动设为本书写作人格（覆盖式：每次保存心智均以该书最新心智作为活跃人格，
	// 与提取流程原「提完即绑定为人格」语义一致）。
	ps := personasetting.NewStore(a.db, a.logger)
	if err := ps.Save(a.ctx, &personasetting.PersonaSetting{
		NovelID: novelID,
		Enabled: true,
		Kind:    "skill",
		Ref:     sk.Name,
	}); err != nil {
		a.logger.Warn("自动激活心智：写入 persona 设置失败", "novel_id", novelID, "err", err)
	} else {
		a.logger.Info("自动激活心智：已设为本书写作人格", "novel_id", novelID, "skill", sk.Name)
	}
}
