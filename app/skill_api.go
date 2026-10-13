package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/novelgo/novelgo/internal/apperr"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/skill/remote"
	"github.com/novelgo/novelgo/internal/storage"
)

// ListSkillsInput 是 ListSkills 的入参。
type ListSkillsInput struct {
	NovelID int64 `json:"novel_id"`
}

// ListSkills 返回所有可用 skill 的元数据（同名覆盖：novel > user > builtin）。
func (a *App) ListSkills(input ListSkillsInput) []skill.SkillMeta {
	if a.skill == nil {
		return nil
	}
	return a.skill.ListMeta(input.NovelID)
}

// GetEnabledSkills 返回小说勾选启用的技能名清单。
// 勾选的技能在对话中常驻注入（等效临时 always），与技能文件本身解耦。
func (a *App) GetEnabledSkills(novelID int64) ([]string, error) {
	names, err := skill.LoadEnabled(novelID)
	if err != nil {
		return nil, err
	}
	return names, nil
}

// SetEnabledSkillsInput 是 SetEnabledSkills 的入参，PUT 语义全量覆盖。
type SetEnabledSkillsInput struct {
	NovelID int64    `json:"novel_id"`
	Names   []string `json:"names"`
}

// SetEnabledSkills 全量覆盖小说勾选启用的技能名清单。
func (a *App) SetEnabledSkills(input SetEnabledSkillsInput) error {
	if input.NovelID <= 0 {
		return fmt.Errorf("小说 ID 无效")
	}
	return skill.SaveEnabled(input.NovelID, input.Names)
}

// DeleteSkillInput 是 DeleteSkill 的入参。
type DeleteSkillInput struct {
	NovelID int64  `json:"novel_id"`
	Name    string `json:"name"`
	Source  string `json:"source"` // "novel" | "user"
}

// DeleteSkill 删除用户级或小说级技能文件。内置技能不可删除。
func (a *App) DeleteSkill(input DeleteSkillInput) error {
	if a.skill == nil {
		return fmt.Errorf("skill store 未初始化")
	}
	if input.Name == "" {
		return fmt.Errorf("技能名称不能为空")
	}
	name := strings.TrimSuffix(filepath.Base(input.Name), ".md")
	if name == "" || name != input.Name {
		return fmt.Errorf("技能名称非法")
	}

	source := input.Source
	if source != "novel" && source != "user" {
		return fmt.Errorf("只能删除用户级或小说级技能")
	}

	var filePath string
	switch source {
	case "novel":
		if input.NovelID <= 0 {
			return fmt.Errorf("小说 ID 无效")
		}
		var err error
		filePath, err = git.SafePath(config.NovelSkillsDir(input.NovelID), name+".md")
		if err != nil {
			return fmt.Errorf("技能名称非法: %w", err)
		}
	case "user":
		var err error
		filePath, err = git.SafePath(config.UserSkillsDir(), name+".md")
		if err != nil {
			return fmt.Errorf("技能名称非法: %w", err)
		}
	}

	if err := os.Remove(filePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("技能文件不存在: %s", name)
		}
		return fmt.Errorf("删除技能文件失败: %w", err)
	}

	// 重新加载对应层级
	switch source {
	case "novel":
		if err := a.skill.ReloadNovel(input.NovelID, config.NovelSkillsDir(input.NovelID)); err != nil {
			a.logger.Warn("删除技能后重新加载小说级技能失败", "name", name, "err", err)
		}
	case "user":
		if err := a.skill.ReloadUser(config.UserSkillsDir()); err != nil {
			a.logger.Warn("删除技能后重新加载用户级技能失败", "name", name, "err", err)
		}
	}

	// T-AGG-5：源 skill 被删后，自动重建/清理受影响的跨书聚合档案（best-effort）。
	a.reconcileAggregated()

	return nil
}

// ListRemoteSkillsInput 是 ListRemoteSkills 的入参。
// Page/Size 走 storage.PageParams 归一化（Page<1 → 1，Size 默认 20，上限 100）。
// Query 为 name + description 的模糊匹配（大小写不敏感），空串表示不过滤。
type ListRemoteSkillsInput struct {
	Page  int    `json:"page"`
	Size  int    `json:"size"`
	Query string `json:"query"`
}

// ListRemoteSkills 列出远程 skill 市场的所有 skill，支持分页和搜索。
// forceRefresh=false 优先使用内存缓存（1h TTL），前端"刷新"按钮可改为 true 强制刷新。
// 错误以 *apperr.Result[*storage.PageResult[remote.RemoteSkillMeta]] 透传，前端按 err_code 分类反馈。
func (a *App) ListRemoteSkills(input ListRemoteSkillsInput) *apperr.Result[*storage.PageResult[remote.RemoteSkillMeta]] {
	all, err := a.remote.ListRemoteSkills(a.ctx, false)
	if err != nil {
		return apperr.Err[*storage.PageResult[remote.RemoteSkillMeta]](err)
	}

	// 1. 搜索过滤（name + description 模糊匹配，大小写不敏感）
	filtered := filterRemoteSkillsByQuery(all, input.Query)

	// 2. 分页（复用 storage.PageParams.Normalize + storage.NewPageResult）
	p := (&storage.PageParams{Page: input.Page, Size: input.Size}).Normalize()
	start := (p.Page - 1) * p.Size
	end := start + p.Size
	if start > len(filtered) {
		start = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}
	page := storage.NewPageResult(filtered[start:end], int64(len(filtered)), p.Page, p.Size)

	return apperr.Ok(page)
}

// filterRemoteSkillsByQuery 按 query 模糊匹配 name + description（大小写不敏感）。
// query 为空串时返回全部。
func filterRemoteSkillsByQuery(skills []remote.RemoteSkillMeta, query string) []remote.RemoteSkillMeta {
	if query == "" {
		return skills
	}
	q := strings.ToLower(query)
	out := make([]remote.RemoteSkillMeta, 0, len(skills))
	for _, s := range skills {
		if strings.Contains(strings.ToLower(s.Name), q) ||
			strings.Contains(strings.ToLower(s.Description), q) {
			out = append(out, s)
		}
	}
	return out
}

// GetRemoteSkillContent 拉取指定远程 skill 的 markdown 原文内容。
// 用于详情面板展示全文。
func (a *App) GetRemoteSkillContent(name string) *apperr.Result[string] {
	content, err := a.remote.GetRemoteSkillContent(a.ctx, name)
	if err != nil {
		return apperr.Err[string](err)
	}
	return apperr.Ok(content)
}

// InstallRemoteSkillInput 是 InstallRemoteSkill 的入参。
// Target 取值 "user" 或 "novel"；Target=novel 时 NovelID 必填。
type InstallRemoteSkillInput struct {
	Name            string  `json:"name"`
	Target          string  `json:"target"`   // "user" or "novel"
	NovelID         int64   `json:"novel_id"` // target=novel 时必填
	ExpectedContent *string `json:"expected_content"`
}

// InstallRemoteSkill 将指定远程 skill 安装到目标层（user 或 novel）。
// 安装成功后触发 skill.Store 热重载（失败只 Warn 不返回 error）。
// 后端不做存在性判断，前端弹确认框处理覆盖语义。
// expected_content 是用户看到目标时的内容，服务端在写入时校验它。
func (a *App) InstallRemoteSkill(input InstallRemoteSkillInput) *apperr.Result[apperr.Empty] {
	if input.ExpectedContent == nil {
		return apperr.Err[apperr.Empty](apperr.NewInvalid("expected_content is required"))
	}
	if err := a.remote.InstallRemoteSkill(a.ctx, input.Name, input.Target, input.NovelID, *input.ExpectedContent); err != nil {
		return apperr.Err[apperr.Empty](err)
	}
	return apperr.Ok(apperr.Empty{})
}

// ── V3 跨书聚合（cross-book mental-skill aggregation）──────────────────

// MergeMentalSkillsInput 是 MergeMentalSkills 的入参。
// Names 指定要合并的源 skill 名（跨 novel/user 级）；为空时按 CanonicalAuthor 自动枚举同作者源。
type MergeMentalSkillsInput struct {
	NovelID         int64             `json:"novel_id"`
	Names           []string          `json:"names"`            // 手动合并时指定的源 skill 名
	CanonicalAuthor string            `json:"canonical_author"` // 规范作者名；Names 为空时用于自动枚举同作者源
	AliasMap        map[string]string `json:"alias_map"`        // 可选别名映射
}

// MergeMentalSkills 跨书聚合编排：
// 枚举当前用户域所有小说的 mental skill → 调用 skill.MergeMentalSkills → 落盘 UserSkillsDir 派生文件 → 触发技能热重载。
// 返回生成的派生 skill（含 frontmatter 聚合字段）。
func (a *App) MergeMentalSkills(input MergeMentalSkillsInput) (*skill.Skill, error) {
	if a.skill == nil {
		return nil, fmt.Errorf("skill store 未初始化")
	}

	sources, bookTitles, err := a.collectMentalSources()
	if err != nil {
		return nil, err
	}

	// 别名映射：前端未显式传入时回退到持久化的 author_aliases.json，
	// 使跨书聚合能把同一作者的不同署名归并到一起。
	aliasMap := input.AliasMap
	if len(aliasMap) == 0 {
		aliasMap = a.aliasResolved()
	}

	// 选定参与合并的源。
	var selected []skill.MergeSource
	switch {
	case len(input.Names) > 0:
		nameSet := make(map[string]bool, len(input.Names))
		for _, n := range input.Names {
			nameSet[n] = true
		}
		for _, s := range sources {
			if s.Skill != nil && nameSet[s.Skill.Name] {
				selected = append(selected, s)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("未找到指定的源 skill：%v", input.Names)
		}
	case input.CanonicalAuthor != "":
		target := skill.NormalizeAuthor(input.CanonicalAuthor, aliasMap)
		for _, s := range sources {
			if s.Skill != nil && skill.NormalizeAuthor(s.Skill.SourceAuthor, aliasMap) == target {
				selected = append(selected, s)
			}
		}
		if len(selected) == 0 {
			return nil, fmt.Errorf("未找到作者 %q 的 mental skill", input.CanonicalAuthor)
		}
	default:
		return nil, fmt.Errorf("必须指定 names 或 canonical_author")
	}

	opts := skill.MergeOptions{
		CanonicalAuthor: input.CanonicalAuthor,
		AliasMap:        aliasMap,
		OutputDir:       config.UserSkillsDir(),
		AggregatedBy:    "aggregate",
		BookTitles:      bookTitles,
	}
	derived, err := skill.MergeMentalSkills(a.ctx, selected, opts)
	if err != nil {
		return nil, err
	}

	if _, err := skill.WriteSkill(config.UserSkillsDir(), derived); err != nil {
		return nil, fmt.Errorf("落盘派生 skill 失败: %w", err)
	}
	// 热重载用户级 skill，使新档案立即生效。
	if err := a.skill.ReloadUser(config.UserSkillsDir()); err != nil {
		a.logger.Warn("合并后重新加载用户级 skill 失败", "err", err)
	}
	return derived, nil
}

// RebuildAggregatedInput 是 RebuildAggregated 的入参。
type RebuildAggregatedInput struct {
	NovelID int64 `json:"novel_id"`
}

// RebuildAggregatedResult 是 RebuildAggregated 的返回。
type RebuildAggregatedResult struct {
	Count int `json:"count"` // 重建生成的作者档案数量
}

// RebuildAggregated 重建所有作者的跨书聚合档案（基础实现）。
// 枚举全部 mental skill（排除派生聚合源），按归一作者分组，逐一重新生成并落盘派生文件。
func (a *App) RebuildAggregated(input RebuildAggregatedInput) (*RebuildAggregatedResult, error) {
	if a.skill == nil {
		return nil, fmt.Errorf("skill store 未初始化")
	}

	sources, _, err := a.collectMentalSources()
	if err != nil {
		return nil, err
	}

	// 持久化别名映射：使不同署名的同一作者归并到同一组。
	aliasMap := a.aliasResolved()

	groups := make(map[string][]skill.MergeSource)
	for _, s := range sources {
		if s.Skill == nil || s.Skill.Aggregated {
			continue
		}
		if s.Skill.Kind != "mental" {
			continue
		}
		author := skill.NormalizeAuthor(s.Skill.SourceAuthor, aliasMap)
		if author == "" {
			continue
		}
		groups[author] = append(groups[author], s)
	}

	count := 0
	for author, grp := range groups {
		opts := skill.MergeOptions{
			CanonicalAuthor: author,
			AliasMap:        aliasMap,
			OutputDir:       config.UserSkillsDir(),
			AggregatedBy:    "aggregate",
		}
		derived, err := skill.MergeMentalSkills(a.ctx, grp, opts)
		if err != nil {
			a.logger.Warn("重建聚合档案失败，已跳过", "author", author, "err", err)
			continue
		}
		if _, err := skill.WriteSkill(config.UserSkillsDir(), derived); err != nil {
			a.logger.Warn("落盘重建聚合档案失败，已跳过", "author", author, "err", err)
			continue
		}
		count++
	}

	if err := a.skill.ReloadUser(config.UserSkillsDir()); err != nil {
		a.logger.Warn("重建后重新加载用户级 skill 失败", "err", err)
	}
	return &RebuildAggregatedResult{Count: count}, nil
}

// collectMentalSources 枚举当前用户域所有小说级 + 用户级 mental skill，并附带书名溯源。
func (a *App) collectMentalSources() ([]skill.MergeSource, map[string]string, error) {
	if a.novel == nil {
		return nil, nil, fmt.Errorf("novel store 未初始化")
	}

	novels, err := a.GetNovels()
	if err != nil {
		return nil, nil, fmt.Errorf("枚举小说失败: %w", err)
	}

	bookTitles := make(map[string]string)
	var sources []skill.MergeSource

	for _, nv := range novels {
		skills, err := skill.LoadDir(config.NovelSkillsDir(nv.ID))
		if err != nil {
			a.logger.Warn("加载小说 skill 失败，已跳过", "novel_id", nv.ID, "err", err)
			continue
		}
		for i := range skills {
			sk := skills[i]
			if _, ok := bookTitles[sk.Name]; !ok {
				bookTitles[sk.Name] = nv.Title
			}
			sources = append(sources, skill.MergeSource{Skill: &sk, BookTitle: nv.Title, BookID: nv.ID})
		}
	}

	userSkills, err := skill.LoadDir(config.UserSkillsDir())
	if err != nil {
		a.logger.Warn("加载用户级 skill 失败", "err", err)
	} else {
		for i := range userSkills {
			sk := userSkills[i]
			if _, ok := bookTitles[sk.Name]; !ok {
				bookTitles[sk.Name] = "用户"
			}
			sources = append(sources, skill.MergeSource{Skill: &sk, BookTitle: "用户"})
		}
	}

	return sources, bookTitles, nil
}

// reconcileAggregated 是 T-AGG-5「自动重建」的触发入口：比对当前源与现有派生聚合档，
// 重建过期档、清理孤儿档。在应用启动、删除 skill、保存 skill 文件后调用（best-effort，失败仅日志）。
func (a *App) reconcileAggregated() {
	if a.skill == nil || a.novel == nil {
		return
	}
	sources, _, err := a.collectMentalSources()
	if err != nil {
		a.logger.Warn("跨书聚合自动重建：枚举源失败", "err", err)
		return
	}
	existing, err := skill.LoadDir(config.UserSkillsDir())
	if err != nil {
		a.logger.Warn("跨书聚合自动重建：加载现有聚合档失败", "err", err)
		return
	}
	res := skill.ReconcileAggregates(a.ctx, sources, existing, config.UserSkillsDir(), a.aliasResolved())
	if len(res.Rebuilt) == 0 && len(res.Deleted) == 0 {
		return
	}
	// 源/派生档已变化，重载用户级 skill 使最新档案立即生效。
	if err := a.skill.ReloadUser(config.UserSkillsDir()); err != nil {
		a.logger.Warn("跨书聚合自动重建后重载用户技能失败", "err", err)
	}
	if len(res.Rebuilt) > 0 {
		a.logger.Info("跨书聚合自动重建完成", "rebuilt", res.Rebuilt)
	}
	if len(res.Deleted) > 0 {
		a.logger.Info("跨书聚合孤儿清理完成", "deleted", res.Deleted)
	}
}

// aliasResolved 从 UserSkillsDir 加载作者别名配置并编译为「归一键 → 规范名」映射。
// 每次调用实时读取（文件很小），保证别名编辑后立即生效、无需重启应用。
// 加载失败时降级为 nil（按无别名处理），仅记录告警。
func (a *App) aliasResolved() map[string]string {
	cfg, err := skill.LoadAliasConfig(config.UserSkillsDir())
	if err != nil {
		a.logger.Warn("加载作者别名配置失败，按无别名处理", "err", err)
		return nil
	}
	return cfg.ToMap()
}
