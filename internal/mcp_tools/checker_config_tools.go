package mcp_tools

// checker_config_tools.go —— 题材卡机制化（R2b-3，提案 §9.2 末）：
// 「题材卡从文案约定变成机器可检参数」。novel 级检查器配置（novel_checker_configs）
// 的读写工具：爽点密度下限 / 章尾钩子必需 / 窗口 N / 禁忌清单 / 开篇体检关键词（P0-0）。
//
// 消费侧单源：RunL0Checks（章尾钩子门控 + 禁忌逐词扫描 + 开篇体检位次三查/章长区间）
// 与结构检查（伏笔超期宽限）参数化消费；缺省=现行默认（逐字一致）。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/novelgo/novelgo/internal/quality"
)

// ── upsert_checker_config ───────────────────────────────

// UpsertCheckerConfigArgs 是 upsert_checker_config 的参数（局部覆盖语义：
// 未给的字段保持现值；taboo_list/三组开篇关键词给空数组=清空清单）。
type UpsertCheckerConfigArgs struct {
	BeatDensityMin   *float64 `json:"beat_density_min" jsonschema:"description=爽点密度下限（次/章，>=0；0=不限）。节奏曲线数据源落地前先行配置" validate:"omitempty,min=0"`
	TailHookRequired *bool    `json:"tail_hook_required" jsonschema:"description=章尾钩子是否必需（缺省 true=缺钩告警；false=跳过章尾钩子检查）"`
	WindowN          *int     `json:"window_n" jsonschema:"description=窗口 N：伏笔超期宽限章数（>=0；0=零宽限=过期即告警）" validate:"omitempty,min=0"`
	TabooList        []string `json:"taboo_list" jsonschema:"description=禁忌清单（字符串数组，如 [\"虐主\",\"破墙\"]）：正文逐词确定性扫描。给空数组=清空；不给=保持现值"`
	PremiseKeywords  []string `json:"premise_keywords" jsonschema:"description=开篇核心看点关键词（标题/简介卖点词，字符串数组）：第 1 章前 500 字内须实体化，迟到/缺失落 L0 发现 premise_late（P0-0 开篇体检）。给空数组=清空；不给=保持现值"`
	CheatKeywords    []string `json:"cheat_keywords" jsonschema:"description=金手指/核心机制关键词（字符串数组）：第 1 章前 800 字内须出场，迟到/缺失落 L0 发现 cheat_late（P0-0 开篇体检）。给空数组=清空；不给=保持现值"`
	ProtagonistNames []string `json:"protagonist_names" jsonschema:"description=主角称呼（正名+常用别名，字符串数组）：第 1 章前 200 字内主角须首现入戏，过晚/缺失落 L0 发现 protagonist_late（P0-0 开篇体检）。给空数组=清空；不给=保持现值"`
}

// UpsertCheckerConfigTool 配置本书检查器参数（题材卡机制化）。
type UpsertCheckerConfigTool struct{}

func (t *UpsertCheckerConfigTool) Name() string { return "upsert_checker_config" }
func (t *UpsertCheckerConfigTool) Description() string {
	return "配置本书检查器参数（题材卡从文案约定变成机器可检参数）：\n" +
		"- beat_density_min：爽点密度下限（次/章，0=不限）\n" +
		"- tail_hook_required：章尾钩子是否必需（缺省 true；false=跳过钩子检查）\n" +
		"- window_n：伏笔超期宽限章数（0=零宽限）\n" +
		"- taboo_list：禁忌清单（正文逐词扫描，命中进 L0 发现）\n" +
		"- premise_keywords：开篇核心看点关键词（第 1 章前 500 字内须实体化，P0-0 开篇体检）\n" +
		"- cheat_keywords：金手指/核心机制关键词（第 1 章前 800 字内须出场）\n" +
		"- protagonist_names：主角称呼（第 1 章前 200 字内须首现入戏）\n" +
		"局部覆盖：未给的字段保持现值。缺省=现行默认行为。"
}
func (t *UpsertCheckerConfigTool) Category() ToolCategory { return CategoryWritingAssistant }
func (t *UpsertCheckerConfigTool) JSONSchema() json.RawMessage {
	return SchemaOf(UpsertCheckerConfigArgs{})
}
func (t *UpsertCheckerConfigTool) ExposeToLLM() bool { return true }
func (t *UpsertCheckerConfigTool) NewArgs() any      { return &UpsertCheckerConfigArgs{} }

func (t *UpsertCheckerConfigTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*UpsertCheckerConfigArgs)
	store := quality.NewStore(tc.DB, tc.LoggerOrDefault())

	cfg, err := store.GetCheckerConfig(ctx, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("get checker config: %w", err)
	}
	// 局部覆盖：nil=保持现值（taboo_list 语义同——nil 不改、空数组清空）。
	if a.BeatDensityMin != nil {
		cfg.BeatDensityMin = *a.BeatDensityMin
	}
	if a.TailHookRequired != nil {
		v := *a.TailHookRequired
		cfg.TailHookRequired = &v
	}
	if a.WindowN != nil {
		cfg.WindowN = *a.WindowN
	}
	// 四组清单同一语义：nil=保持现值、空数组=清空（序列化后落库，Validate 归一）。
	for _, item := range []struct {
		src []string
		dst *string
	}{
		{a.TabooList, &cfg.TabooList},
		{a.PremiseKeywords, &cfg.PremiseList},
		{a.CheatKeywords, &cfg.CheatList},
		{a.ProtagonistNames, &cfg.ProtagonistList},
	} {
		if item.src == nil {
			continue
		}
		b, merr := json.Marshal(item.src)
		if merr != nil {
			return &ToolResult{Success: false, Error: "清单序列化失败：" + merr.Error()}, nil
		}
		*item.dst = string(b)
	}
	// 契约拒收（负数/清单非 JSON 数组）是业务错误，LLM 补齐可成功——
	// 按 error_semantics 走业务拒绝（Success=false），不计入中断。
	if err := store.SaveCheckerConfig(ctx, cfg); err != nil {
		return &ToolResult{Success: false, Error: err.Error()}, nil
	}
	return &ToolResult{
		Success: true,
		Data:    checkerConfigData(cfg),
	}, nil
}

// ── get_checker_config ──────────────────────────────────

// GetCheckerConfigArgs 是 get_checker_config 的参数（无参数）。
type GetCheckerConfigArgs struct{}

// GetCheckerConfigTool 查看本书检查器配置（题材卡机制化参数）。
type GetCheckerConfigTool struct{}

func (t *GetCheckerConfigTool) Name() string { return "get_checker_config" }
func (t *GetCheckerConfigTool) Description() string {
	return "查看本书检查器配置（题材卡机制化参数）：爽点密度下限/章尾钩子必需/窗口 N/禁忌清单/" +
		"开篇体检关键词（核心看点/金手指/主角）。未配置时返回缺省（=现行默认行为）。"
}
func (t *GetCheckerConfigTool) Category() ToolCategory { return CategoryMemoryRetrieval }
func (t *GetCheckerConfigTool) JSONSchema() json.RawMessage {
	return SchemaOf(GetCheckerConfigArgs{})
}
func (t *GetCheckerConfigTool) ExposeToLLM() bool { return true }
func (t *GetCheckerConfigTool) NewArgs() any      { return &GetCheckerConfigArgs{} }

func (t *GetCheckerConfigTool) Execute(ctx context.Context, _ any, tc ToolContext) (*ToolResult, error) {
	cfg, err := quality.NewStore(tc.DB, tc.LoggerOrDefault()).GetCheckerConfig(ctx, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("get checker config: %w", err)
	}
	return &ToolResult{Success: true, Data: checkerConfigData(cfg)}, nil
}

// checkerConfigData 投影配置为工具返回（解析后的口径：hook_required 缺省归一、
// 各清单给解析后数组）。消费侧单源。
func checkerConfigData(cfg *quality.CheckerConfig) map[string]any {
	return map[string]any{
		"beat_density_min":   cfg.BeatDensityMin,
		"tail_hook_required": cfg.HookRequired(),
		"window_n":           cfg.OverdueGrace(),
		"taboo_list":         cfg.Taboos(),
		"premise_keywords":   cfg.Premises(),
		"cheat_keywords":     cfg.Cheats(),
		"protagonist_names":  cfg.Protagonists(),
	}
}

// RegisterCheckerConfigTools 注册检查器配置工具。
func RegisterCheckerConfigTools(r *Registry) {
	r.Register(&UpsertCheckerConfigTool{})
	r.Register(&GetCheckerConfigTool{})
}
