package mcp_tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/novelgo/novelgo/internal/setting"
)

// ── search_settings ───────────────────────────────

// SearchSettingsArgs 是 search_settings 的参数。
type SearchSettingsArgs struct {
	Query string `json:"query" jsonschema:"required,description=当前章节将要涉及的主题/角色/设定关键词，用于语义检索最相关设定（如'林惊羽的血脉''御剑飞行原理'）"`
	TopK  int    `json:"top_k,omitempty" jsonschema:"description=返回条数，默认5，范围1-10"`
}

// SearchSettingsTool 写作前按需语义检索与当前章节最相关的设定，避免凭记忆写导致吃书/设定矛盾。
type SearchSettingsTool struct{}

func (t *SearchSettingsTool) Name() string { return "search_settings" }
func (t *SearchSettingsTool) Description() string {
	return "写作前按需检索与当前章节主题最相关的设定（语义优先，无向量检索时退化为关键词）。" +
		"返回带 [setting_id:N | 分类] 的设定条目，供写时精确引用、避免吃书/设定冲突。" +
		"每次写新章节或切换场景前调用：query 用本章将涉及的核心实体/矛盾/地点/力量规则，" +
		"例如'主角的血脉诅咒''御剑飞行的灵气消耗'。需要更新设定时拿返回的 setting_id 调 upsert_setting。"
}
func (t *SearchSettingsTool) Category() ToolCategory { return CategoryWritingAssistant }

func (t *SearchSettingsTool) JSONSchema() json.RawMessage { return SchemaOf(SearchSettingsArgs{}) }
func (t *SearchSettingsTool) ExposeToLLM() bool           { return true }
func (t *SearchSettingsTool) NewArgs() any                { return &SearchSettingsArgs{} }

func (t *SearchSettingsTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*SearchSettingsArgs)
	topK := a.TopK
	if topK <= 0 || topK > 10 {
		topK = 5
	}

	results, err := setting.SearchSettings(ctx, tc.DB, tc.NovelID, a.Query, topK)
	if err != nil {
		return nil, fmt.Errorf("search settings: %w", err)
	}

	items := make([]map[string]any, 0, len(results))
	for _, r := range results {
		items = append(items, map[string]any{
			"setting_id": r.SettingID,
			"category":   r.Category,
			"content":    r.Content,
			"relevance":  r.Relevance,
		})
	}

	return &ToolResult{
		Success: true,
		Data:    map[string]any{"count": len(results), "settings": items},
	}, nil
}
