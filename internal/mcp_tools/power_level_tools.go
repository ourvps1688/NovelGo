package mcp_tools

// power_level_tools.go —— 力量体系境界等级表（R2b-6，提案 §9.2 战力/境界崩坏）：
// 境界有序枚举 + 数值锚（power_levels）的读写工具。
//
// 消费侧单源：quality L0 越级取胜/低阶碾压高阶检查 + structure capability_ceiling
// 境界合流比对；未登记境界/无等级表 → 相关检查自然跳过（缺省=现行行为逐字一致）。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/novelgo/novelgo/internal/structure"
)

// ── upsert_power_level ──────────────────────────────────

// UpsertPowerLevelArgs 是 upsert_power_level 的参数（(novel_id, name) 唯一，
// 同名重复调用覆盖数值锚/描述）。
type UpsertPowerLevelArgs struct {
	Name        string `json:"name" jsonschema:"description=境界名（如「金丹期」「元婴期」），同书内唯一" validate:"required"`
	Rank        int    `json:"rank" jsonschema:"description=数值锚（>=1，越大越强）：供越级/碾压判定与战力上限比对" validate:"required,min=1"`
	Description string `json:"description" jsonschema:"description=境界说明（可空）"`
}

// UpsertPowerLevelTool 立/覆盖一条境界等级（力量体系数值序台账）。
type UpsertPowerLevelTool struct{}

func (t *UpsertPowerLevelTool) Name() string { return "upsert_power_level" }
func (t *UpsertPowerLevelTool) Description() string {
	return "立/覆盖一条力量体系境界等级（境界名 + 数值锚）：\n" +
		"- name：境界名（如「金丹期」），同书内唯一，同名重复调用覆盖\n" +
		"- rank：数值锚（>=1，越大越强）\n" +
		"- description：境界说明（可空）\n" +
		"用途：越级取胜/低阶碾压高阶确定性拦截、战力上限按境界数值比对。" +
		"全书境界请按从弱到强逐级登记。"
}
func (t *UpsertPowerLevelTool) Category() ToolCategory { return CategoryWritingAssistant }
func (t *UpsertPowerLevelTool) JSONSchema() json.RawMessage {
	return SchemaOf(UpsertPowerLevelArgs{})
}
func (t *UpsertPowerLevelTool) ExposeToLLM() bool { return true }
func (t *UpsertPowerLevelTool) NewArgs() any      { return &UpsertPowerLevelArgs{} }

func (t *UpsertPowerLevelTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*UpsertPowerLevelArgs)
	p := &structure.PowerLevel{
		NovelID:     tc.NovelID,
		Name:        a.Name,
		Rank:        a.Rank,
		Description: a.Description,
	}
	// 契约拒收（名称空/数值锚非法）是业务错误，LLM 补齐可成功——
	// 按 error_semantics 走业务拒绝（Success=false），不计入中断。
	if err := structure.NewStore(tc.DB).UpsertPowerLevel(ctx, p); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &ToolResult{Success: true, Data: powerLevelData(p)}, nil
}

// ── list_power_levels ───────────────────────────────────

// ListPowerLevelsArgs 是 list_power_levels 的参数（无参数）。
type ListPowerLevelsArgs struct{}

// ListPowerLevelsTool 查看本书境界等级表（弱→强）。
type ListPowerLevelsTool struct{}

func (t *ListPowerLevelsTool) Name() string { return "list_power_levels" }
func (t *ListPowerLevelsTool) Description() string {
	return "查看本书力量体系境界等级表（按数值锚升序，弱→强）：境界名/数值锚/说明。" +
		"未登记时返回空清单。"
}
func (t *ListPowerLevelsTool) Category() ToolCategory { return CategoryMemoryRetrieval }
func (t *ListPowerLevelsTool) JSONSchema() json.RawMessage {
	return SchemaOf(ListPowerLevelsArgs{})
}
func (t *ListPowerLevelsTool) ExposeToLLM() bool { return true }
func (t *ListPowerLevelsTool) NewArgs() any      { return &ListPowerLevelsArgs{} }

func (t *ListPowerLevelsTool) Execute(ctx context.Context, _ any, tc ToolContext) (*ToolResult, error) {
	rows, err := structure.NewStore(tc.DB).ListPowerLevels(ctx, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("list power levels: %w", err)
	}
	items := make([]map[string]any, 0, len(rows))
	for i := range rows {
		items = append(items, powerLevelData(&rows[i]))
	}
	return &ToolResult{Success: true, Data: map[string]any{"power_levels": items}}, nil
}

// powerLevelData 投影境界条目为工具返回（消费侧口径：name/rank/description）。
func powerLevelData(p *structure.PowerLevel) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"rank":        p.Rank,
		"description": p.Description,
	}
}

// RegisterPowerLevelTools 注册境界等级表工具。
func RegisterPowerLevelTools(r *Registry) {
	r.Register(&UpsertPowerLevelTool{})
	r.Register(&ListPowerLevelsTool{})
}
