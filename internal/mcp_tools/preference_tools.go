package mcp_tools

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/preference"
)

// ── upsert_preference ─────────────────────────────────

// UpsertPreferenceItem 是 upsert_preference 的单条参数。
type UpsertPreferenceItem struct {
	PreferenceID *int64 `json:"preference_id,omitempty" jsonschema:"description=已存在偏好ID；不传=新建"`
	Category     string `json:"category" jsonschema:"required,description=偏好分类(自由文本标签)" validate:"required"`
	Content      string `json:"content" jsonschema:"required,description=偏好内容" validate:"required"`
	IsGlobal     *bool  `json:"is_global,omitempty" jsonschema:"description=是否全局偏好(对所有小说生效)；不传=新建默认false，更新保持原值"`
}

// UpsertPreferenceArgs 是 upsert_preference 的参数。
//
// 主形态是 preferences 数组（传 preference_id=更新，不传=创建）。额外提供顶层平铺字段
// PreferenceID/Category/Content/IsGlobal 作为容错：LLM 偶发会把 preferences 数组包裹漏掉、
// 直接把 category/content 平铺到顶层（upsert_setting 截图实测同构行为），这里接收后由
// Execute 归一化进 Preferences。id/novel_id 等不可变字段不在本结构体，Registry 的
// DisallowUnknownFields 仍会拒绝，安全不受影响。
type UpsertPreferenceArgs struct {
	Preferences []UpsertPreferenceItem `json:"preferences" jsonschema:"required,description=要upsert的偏好列表(1-5个，传preference_id=更新，不传=创建)；也支持平铺单条：直接传顶层 category/content（可选 preference_id/is_global）" validate:"max=5,dive"`
	// 容错平铺单条：LLM 漏 preferences 包裹时的兜底入口。
	PreferenceID *int64 `json:"preference_id,omitempty" jsonschema:"description=平铺单条更新时：已存在偏好ID；不传=新建"`
	Category     string `json:"category,omitempty" jsonschema:"description=平铺单条时：偏好分类(自由文本标签)"`
	Content      string `json:"content,omitempty" jsonschema:"description=平铺单条时：偏好内容"`
	IsGlobal     *bool  `json:"is_global,omitempty" jsonschema:"description=平铺单条时：是否全局偏好(对所有小说生效)"`
}

// UpsertPreferenceTool 批量 upsert 创作偏好（创建或更新，单事务原子性）。
type UpsertPreferenceTool struct{}

func (t *UpsertPreferenceTool) Name() string { return "upsert_preference" }
func (t *UpsertPreferenceTool) Description() string {
	return "批量创建或更新创作偏好（1-5个，单事务原子性，失败全部回滚）。" +
		"传 preference_id=更新该条（PATCH 语义，只覆盖传入字段），不传=新建。" +
		"开局已全量注入偏好到上下文(带 [preference_id:N | 分类] 前缀)，更新时从注入里取 preference_id 传入。" +
		"新增相似分类的偏好前，应优先更新已有条目(在原文基础上合并)而非创建重复条目。" +
		"参数主形态为 {\"preferences\":[{\"category\":\"分类\",\"content\":\"内容\",\"preference_id\":可选,\"is_global\":可选}]}；" +
		"也支持平铺单条 {\"category\":\"分类\",\"content\":\"内容\",\"preference_id\":可选,\"is_global\":可选}（漏 preferences 包裹时自动归一化）。"
}
func (t *UpsertPreferenceTool) Category() ToolCategory { return CategoryWritingAssistant }

func (t *UpsertPreferenceTool) JSONSchema() json.RawMessage { return SchemaOf(UpsertPreferenceArgs{}) }
func (t *UpsertPreferenceTool) ExposeToLLM() bool           { return true }
func (t *UpsertPreferenceTool) NewArgs() any                { return &UpsertPreferenceArgs{} }

func (t *UpsertPreferenceTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*UpsertPreferenceArgs)

	// 容错：LLM 偶发把 preferences 数组包裹漏掉、直接平铺单条
	// category/content/preference_id/is_global，这里归一化为 preferences 列表，
	// 再走同一套 upsert 逻辑（安全：id/novel_id 仍被框架层 DisallowUnknownFields 拒绝）。
	items := a.Preferences
	if len(items) == 0 {
		if a.PreferenceID == nil && a.Category == "" && a.Content == "" && a.IsGlobal == nil {
			return &ToolResult{
				Success: false,
				Error:   "参数校验失败: 需提供 preferences 数组，或平铺的 category/content（可选 preference_id/is_global）",
			}, nil
		}
		items = []UpsertPreferenceItem{{PreferenceID: a.PreferenceID, Category: a.Category, Content: a.Content, IsGlobal: a.IsGlobal}}
	}
	// 兜底校验每条必填字段（平铺归一化的条目未走框架层 dive 校验）。
	for i, item := range items {
		if item.Category == "" || item.Content == "" {
			return &ToolResult{
				Success: false,
				Error:   fmt.Sprintf("第 %d 条偏好缺少 category 或 content", i),
			}, nil
		}
	}

	type failure struct {
		index    int
		category string
		err      error
	}
	var failed *failure
	var ids []int64

	err := tc.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, item := range items {
			id, err := upsertOnePreference(tx, tc.NovelID, item)
			if err != nil {
				failed = &failure{index: i, category: item.Category, err: err}
				return err
			}
			ids = append(ids, id)
		}
		return nil
	})
	if err != nil {
		if failed == nil {
			return nil, fmt.Errorf("upsert preference: %w", err)
		}
		if !isBusinessError(failed.err) {
			return nil, fmt.Errorf("upsert preference 第 %d 条 [%s]（事务已回滚）: %w", failed.index, failed.category, failed.err)
		}
		return &ToolResult{
			Success: false,
			Error:   fmt.Sprintf("第 %d 条偏好 [%s] 失败: %s", failed.index, failed.category, failed.err),
			Data:    map[string]any{"failed_index": failed.index, "failed_category": failed.category},
		}, nil
	}

	return &ToolResult{
		Success: true,
		Data:    map[string]any{"preference_ids": ids, "count": len(ids)},
	}, nil
}

// upsertOnePreference 在事务内 upsert 单条偏好，返回偏好 ID。
func upsertOnePreference(tx *gorm.DB, novelID int64, item UpsertPreferenceItem) (int64, error) {
	// 更新分支
	if item.PreferenceID != nil {
		var existing preference.PreferenceItem
		if err := tx.First(&existing, *item.PreferenceID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return 0, businessErrorf("偏好条目 %d 不存在", *item.PreferenceID)
			}
			return 0, fmt.Errorf("query preference: %w", err)
		}
		// 归属校验：只能改全局偏好或当前小说的偏好
		if !existing.IsGlobal && existing.NovelID != novelID {
			return 0, businessErrorf("偏好条目 %d 不属于当前小说", *item.PreferenceID)
		}
		// PATCH 覆盖（指针字段判断"传没传"，值字段 required 总传值）
		if item.Category != "" {
			existing.Category = item.Category
		}
		if item.Content != "" {
			existing.Content = item.Content
		}
		if item.IsGlobal != nil {
			existing.IsGlobal = *item.IsGlobal
			// is_global 切换时 NovelID 联动调整（rawArgs patch 表达不了的联动，手动补）
			if *item.IsGlobal {
				existing.NovelID = 0
			} else {
				existing.NovelID = novelID
			}
		}
		if err := tx.Save(&existing).Error; err != nil {
			return 0, fmt.Errorf("save preference: %w", err)
		}
		return existing.ID, nil
	}

	// 创建分支
	pref := preference.PreferenceItem{
		Category: item.Category,
		Content:  item.Content,
		NovelID:  novelID,
	}
	if item.IsGlobal != nil && *item.IsGlobal {
		pref.IsGlobal = true
		pref.NovelID = 0
	}
	if err := tx.Create(&pref).Error; err != nil {
		return 0, fmt.Errorf("create preference: %w", err)
	}
	return pref.ID, nil
}

// ── 注册 ──────────────────────────────────────────────

// RegisterPreferenceTools 注册偏好工具。
func RegisterPreferenceTools(r *Registry) {
	r.Register(&UpsertPreferenceTool{})
}
