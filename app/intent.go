package app

// P0 WBS T4.1：Intent 块（创作意图锚定）正文源。
//
// 定源结论（详见 docs/future/p0-context-assembler-implementation-wbs.md §T4.1）：
// book-pitch 应用产物**已落库**（双通道：novels 表 title/description/genre +
// preference_items 表五类 pitch 偏好），无需退化到卷纲摘要作为唯一源。
// provider 定源优先级：book-pitch 应用产物 → 当前卷纲正文 → 空串（降级不注入）。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/storage"
)

// pitchCategories 是 book-pitch 终稿回填落库的偏好分类（与前端
// frontend/src/components/preference/preferenceCategories.ts 的 CATEGORY_* 常量一一对应），
// 固定顺序即意图正文渲染顺序。仅这些分类视为「book-pitch 应用产物」，其余偏好不混入。
var pitchCategories = []string{"一句话卖点", "方向", "平台约束", "创作约束", "标签"}

// intentText 提供 P0 Intent 块正文（contextasm.TextProvider 口径：只回正文，
// 块头/尾注由装配器渲染）。定源优先级：book-pitch 应用产物 → 当前卷纲正文 → 空。
// 空串即 Intent 块降级不注入（提示词与旧行为逐字一致）。
func (a *App) intentText(ctx context.Context, novelID int64) (string, error) {
	text, err := a.intentFromPitch(ctx, novelID)
	if err != nil {
		return "", err
	}
	if text != "" {
		return text, nil
	}
	return a.intentFromVolumeOutline(ctx, novelID)
}

// intentFromPitch 组装 book-pitch 应用产物正文：
//   - novels 表：题材（genre）+ 简介（description）；
//   - preference_items 表：五类 pitch 偏好，同类多条取最新一条
//     （book-pitch 每次应用新建行，取最新即最近一次应用的终稿）。
//
// 逐行「分类：内容」拼接，全空返回空串。
func (a *App) intentFromPitch(ctx context.Context, novelID int64) (string, error) {
	var row struct {
		Genre       string
		Description string
	}
	if err := a.db.WithContext(ctx).Table("novels").
		Select("genre", "description").
		Where("id = ?", novelID).
		Scan(&row).Error; err != nil {
		return "", fmt.Errorf("intent: 读小说信息失败: %w", err)
	}

	res, err := a.preference.ListNovelPreferences(ctx, novelID, preference.ListOptions{
		PageParams: storage.PageParams{Size: -1},
		Order:      "created_at ASC, id ASC",
	})
	if err != nil {
		return "", fmt.Errorf("intent: 读偏好失败: %w", err)
	}
	latest := make(map[string]string, len(pitchCategories))
	for _, it := range res.Items {
		for _, cat := range pitchCategories {
			if it.Category == cat {
				// 按创建序遍历，同类后者覆盖前者 = 取最新终稿。
				latest[cat] = strings.TrimSpace(it.Content)
			}
		}
	}

	var lines []string
	for _, cat := range pitchCategories {
		if v := latest[cat]; v != "" {
			lines = append(lines, cat+"："+v)
		}
	}
	if v := strings.TrimSpace(row.Genre); v != "" {
		lines = append(lines, "题材："+v)
	}
	if v := strings.TrimSpace(row.Description); v != "" {
		lines = append(lines, "简介："+v)
	}
	return strings.Join(lines, "\n"), nil
}

// intentFromVolumeOutline 退化源：当前卷（最新写就章节所属卷，口径同
// pipeline.currentVolumeScope）的卷纲正文（volumes/id_{id}.md）。
// 未分卷 / 卷纲文件不存在返回空串（降级）；预算截断由装配器 fitBudget 统一负责。
func (a *App) intentFromVolumeOutline(ctx context.Context, novelID int64) (string, error) {
	var row struct{ VolumeID *int64 }
	if err := a.db.WithContext(ctx).Table("chapters").
		Select("volume_id").
		Where("novel_id = ?", novelID).
		Order("id DESC").Limit(1).
		Scan(&row).Error; err != nil {
		return "", fmt.Errorf("intent: 查当前卷失败: %w", err)
	}
	if row.VolumeID == nil {
		return "", nil
	}
	content, err := git.ReadFile(novelID, git.VolumePath(*row.VolumeID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("intent: 读卷纲失败: %w", err)
	}
	return strings.TrimSpace(content), nil
}
