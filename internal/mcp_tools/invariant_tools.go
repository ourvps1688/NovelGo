package mcp_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/structure"
)

// ── upsert_pinned_invariant ─────────────────────────────

// UpsertPinnedInvariantArgs 是 upsert_pinned_invariant 的参数。
type UpsertPinnedInvariantArgs struct {
	Kind      string `json:"kind" jsonschema:"required,description=不变量类型,enum=character_alive_before,enum=capability_ceiling,enum=custom" validate:"required,oneof=character_alive_before capability_ceiling custom"`
	Statement string `json:"statement" jsonschema:"required,description=作者声明原文（如「主角第50章前不死」「力量体系上限9000」）" validate:"required"`
	Params    string `json:"params" jsonschema:"description=结构化参数 JSON 对象文本：character_alive_before 须含 character+before_reading_number（<=0=全书不死）；capability_ceiling 须含 character+ceiling；custom 可省略" validate:"omitempty"`
	Confirmed bool   `json:"confirmed" jsonschema:"description=人工确认标记（作者显式确认后置 true）"`
}

// UpsertPinnedInvariantTool 立/更新一条书写不变量（作者钉住的硬约束）。
type UpsertPinnedInvariantTool struct{}

func (t *UpsertPinnedInvariantTool) Name() string { return "upsert_pinned_invariant" }
func (t *UpsertPinnedInvariantTool) Description() string {
	return "立/更新一条书写不变量（作者钉住的本书硬约束，写前把关的最高优先级规则）：\n" +
		"- character_alive_before：角色第 X 章前不死（params 须含 character + before_reading_number，<=0=全书不死）\n" +
		"- capability_ceiling：角色战力天花板钉住（params 须含 character + ceiling）\n" +
		"- custom：自由文本硬约束（机器不判，仅登记供各环节参照）\n" +
		"同 (kind, statement) 重复调用覆盖参数与确认标记。声明后系统对台账确定性比对，违例进结构告警。"
}
func (t *UpsertPinnedInvariantTool) Category() ToolCategory { return CategoryWritingAssistant }
func (t *UpsertPinnedInvariantTool) JSONSchema() json.RawMessage {
	return SchemaOf(UpsertPinnedInvariantArgs{})
}
func (t *UpsertPinnedInvariantTool) ExposeToLLM() bool { return true }
func (t *UpsertPinnedInvariantTool) NewArgs() any      { return &UpsertPinnedInvariantArgs{} }

func (t *UpsertPinnedInvariantTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*UpsertPinnedInvariantArgs)

	p := structure.PinnedInvariant{
		NovelID:    tc.NovelID,
		Kind:       a.Kind,
		Statement:  a.Statement,
		ParamsJSON: a.Params,
		Confirmed:  a.Confirmed,
	}
	if err := structure.NewStore(tc.DB).Upsert(ctx, &p); err != nil {
		// 契约拒收（kind 非法/声明空/规则 kind 参数缺项）是业务错误，LLM 补齐字段可成功——
		// 按 error_semantics 走业务拒绝（Success=false），不计入中断。
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &ToolResult{
		Success: true,
		Data: map[string]any{
			"id":        p.ID,
			"kind":      p.Kind,
			"statement": p.Statement,
			"confirmed": p.Confirmed,
		},
	}, nil
}

// ── list_pinned_invariants ──────────────────────────────

// ListPinnedInvariantsArgs 是 list_pinned_invariants 的参数。
type ListPinnedInvariantsArgs struct {
	Kind string `json:"kind" jsonschema:"description=按类型筛选,enum=character_alive_before,enum=capability_ceiling,enum=custom" validate:"omitempty,oneof=character_alive_before capability_ceiling custom"`
}

// ListPinnedInvariantsTool 列出本书全部书写不变量。
type ListPinnedInvariantsTool struct{}

func (t *ListPinnedInvariantsTool) Name() string { return "list_pinned_invariants" }
func (t *ListPinnedInvariantsTool) Description() string {
	return "列出本书的书写不变量（作者钉住的硬约束清单：声明原文 + 结构化参数 + 确认标记）。" +
		"写作/修复全程必须遵守这些约束；可按 kind 筛选。"
}
func (t *ListPinnedInvariantsTool) Category() ToolCategory { return CategoryMemoryRetrieval }
func (t *ListPinnedInvariantsTool) JSONSchema() json.RawMessage {
	return SchemaOf(ListPinnedInvariantsArgs{})
}
func (t *ListPinnedInvariantsTool) ExposeToLLM() bool { return true }
func (t *ListPinnedInvariantsTool) NewArgs() any      { return &ListPinnedInvariantsArgs{} }

func (t *ListPinnedInvariantsTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*ListPinnedInvariantsArgs)

	rows, err := structure.NewStore(tc.DB).ListByNovel(ctx, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("list pinned invariants: %w", err)
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if a.Kind != "" && row.Kind != a.Kind {
			continue
		}
		items = append(items, map[string]any{
			"id":        row.ID,
			"kind":      row.Kind,
			"statement": row.Statement,
			"params":    strings.TrimSpace(row.ParamsJSON),
			"confirmed": row.Confirmed,
		})
	}
	return &ToolResult{
		Success: true,
		Data: map[string]any{
			"count": len(items),
			"items": items,
		},
	}, nil
}

// FormatPinnedInvariant 渲染单条书写不变量为上下文文本（消费侧单源）。
func FormatPinnedInvariant(p *structure.PinnedInvariant) string {
	mark := "未确认"
	if p.Confirmed {
		mark = "已确认"
	}
	return fmt.Sprintf("- [%s][%s] %s（%s）", p.Kind, mark, p.Statement, strings.TrimSpace(p.ParamsJSON))
}

// RegisterInvariantTools 注册书写不变量工具。
func RegisterInvariantTools(r *Registry) {
	r.Register(&UpsertPinnedInvariantTool{})
	r.Register(&ListPinnedInvariantsTool{})
}
