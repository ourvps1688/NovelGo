package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/mcp_tools"
)

// TestBlueprintAgentAllowlistScoped 验证蓝图 Agent 白名单的范围：
// 仅含建卷/读上下文/写卷纲的工具，不含越权工具（run_subagent/delete_record）。
func TestBlueprintAgentAllowlistScoped(t *testing.T) {
	allow := agentcfg.Allowlist(agentcfg.BlueprintAgent)
	require.True(t, allow["create_volume"], "蓝图应能用 create_volume 建卷")
	require.True(t, allow["read"], "蓝图应能用 read 读上下文")
	require.True(t, allow["list_volumes"], "蓝图应能用 list_volumes")
	require.True(t, allow["get_chapter_list"], "蓝图应能用 get_chapter_list")
	require.False(t, allow["run_subagent"], "蓝图禁止派发子 Agent")
	require.False(t, allow["delete_record"], "蓝图禁止删除类工具")
}

// TestBlueprintWhitelistRejectsForbiddenTool 验证：不在蓝图白名单内的工具被调用时，
// 注册表直接以「禁止使用」拒绝（越权被拒），无需真正执行。
func TestBlueprintWhitelistRejectsForbiddenTool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := mcp_tools.NewRegistry(logger)
	mcp_tools.RegisterAllTools(reg)

	allow := agentcfg.Allowlist(agentcfg.BlueprintAgent)
	raw, _ := json.Marshal(map[string]any{"instruction": "x", "agent_type": "review"})
	res := reg.Execute(context.Background(), "run_subagent", raw, mcp_tools.ToolContext{}, allow)

	require.False(t, res.Success, "越权工具 run_subagent 不应被执行")
	require.Contains(t, res.Error, "禁止使用")
}

// TestExecuteBlueprintPersistsAndGates 确定性验证 S2 蓝图执行（A 方案流程）：
// executeBlueprint 生成卷结构后进入蓝图体检（异步），体检通过落 G1 闸门；
// 卷结构概览（blueprint 字段）与体检结果（blueprint_audit 字段）均应写回 ContextJSON。
func TestExecuteBlueprintPersistsAndGates(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.skillRunner = func(_ context.Context, name string, _ SkillInput) (SkillOutput, error) {
		if name == "story-blueprint" {
			return SkillOutput{Text: "蓝图生成完成"}, nil
		}
		if name == "blueprint-audit" {
			return SkillOutput{Text: "体检：\n```json\n{\"verdict\":\"pass\",\"score\":26,\"summary\":\"ok\",\"issues\":[]}\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}
	ctx := context.Background()

	run, err := svc.StartPipeline(ctx, 1, "new", nil, false)
	require.NoError(t, err)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS2, StatusRunning, run.ContextJSON))

	// 蓝图生成完成后进入蓝图体检（异步 goroutine），等待体检落闸（G1/G1b）。
	svc.executeBlueprint(run.RunID, run.NovelID)
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	got, err := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, err)
	require.Contains(t, got.ContextJSON, "blueprint", "ContextJSON 应写入 blueprint 字段")
	require.Contains(t, got.ContextJSON, "蓝图生成完成", "blueprint 应汇总蓝图文本")
	require.Contains(t, got.ContextJSON, "blueprint_audit", "蓝图体检结果应写入 blueprint_audit 字段")
}

// TestSignabilityAgentAllowlistReadOnly 验证签约体检 Agent 白名单为只读：
// 可读章节/台账，但绝不含任何写工具（edit/create_*/update_*）或越权工具（run_subagent/delete_record）。
func TestSignabilityAgentAllowlistReadOnly(t *testing.T) {
	allow := agentcfg.Allowlist(agentcfg.SignabilityAgent)
	require.True(t, allow["read"], "体检应能用 read")
	require.True(t, allow["get_chapter_list"], "体检应能用 get_chapter_list")
	require.True(t, allow["list_volumes"], "体检应能用 list_volumes")
	require.True(t, allow["get_characters"], "体检应能用 get_characters")
	require.True(t, allow["get_reader_perspective"], "体检应能用 get_reader_perspective")
	require.True(t, allow["search_story_memory"], "体检应能用 search_story_memory")

	// 越权/写工具必须全部禁止
	for _, forbidden := range []string{"edit", "create_volume", "update_chapter_plan", "run_subagent", "delete_record"} {
		require.False(t, allow[forbidden], "签约体检禁止写/越权工具: "+forbidden)
	}
}

// TestFixAgentAllowlistCanWrite 验证修复 Agent 白名单：可改稿/补卷纲/同步台账，
// 但禁止 run_subagent（不递归派发）与 delete_record（不误删台账）。
func TestFixAgentAllowlistCanWrite(t *testing.T) {
	allow := agentcfg.Allowlist(agentcfg.FixAgent)
	require.True(t, allow["edit"], "修复应能用 edit 改稿")
	require.True(t, allow["update_chapter_plan"], "修复应能用 update_chapter_plan 修章细纲")
	require.True(t, allow["create_volume"], "修复应能用 create_volume 补卷")
	require.True(t, allow["create_character"], "修复应能用 create_character 补角色")
	require.True(t, allow["update_timeline_entry"], "修复应能用 update_timeline_entry 同步时间线")
	require.True(t, allow["create_reader_perspective_entry"], "修复应能用 create_reader_perspective_entry 同步读者认知")

	for _, forbidden := range []string{"run_subagent", "delete_record"} {
		require.False(t, allow[forbidden], "修复禁止越权/删除工具: "+forbidden)
	}
}

// TestBlueprintFixAgentAllowlistScoped 验证蓝图修复 Agent 白名单为「限权」：
// 仅能改卷纲（edit 仅用于 volumes/）与补卷（create_volume），保留全部只读上下文工具以便定位问题；
// 严禁碰章节正文（无 chapter 写工具）、严禁角色/弧线/时间线/读者认知写工具，
// 更严禁 run_subagent / delete_record（结构性跨台账改动由 S5 签约修复承接）。
func TestBlueprintFixAgentAllowlistScoped(t *testing.T) {
	allow := agentcfg.Allowlist(agentcfg.BlueprintFixAgent)
	// 允许的：只读上下文 + 限权写（卷纲 / 补卷）
	require.True(t, allow["read"], "蓝图修复应能用 read 读上下文")
	require.True(t, allow["list_volumes"], "蓝图修复应能用 list_volumes")
	require.True(t, allow["get_chapter_list"], "蓝图修复应能用 get_chapter_list")
	require.True(t, allow["get_characters"], "蓝图修复应能用 get_characters 了解角色以便对齐卷纲")
	require.True(t, allow["edit"], "蓝图修复应能用 edit 改卷纲（仅 volumes/）")
	require.True(t, allow["create_volume"], "蓝图修复应能用 create_volume 补卷")

	// 严禁：章节正文写、角色/弧线/时间线/读者认知写、越权/删除
	for _, forbidden := range []string{
		"update_chapter_plan", "create_character", "update_character",
		"update_timeline_entry", "create_arc", "create_reader_perspective_entry",
		"run_subagent", "delete_record",
	} {
		require.False(t, allow[forbidden], "蓝图修复禁止越权/跨台账写工具: "+forbidden)
	}
}

// TestSkillAgentTypeBlueprintMappings 验证蓝图技能 → Agent 类型映射：
// blueprint-audit 复用签约体检的只读白名单（SignabilityAgent），blueprint-fix 走限权蓝图修复（BlueprintFixAgent）。
func TestSkillAgentTypeBlueprintMappings(t *testing.T) {
	require.Equal(t, agentcfg.SignabilityAgent, skillAgentType("blueprint-audit"),
		"蓝图体检应复用签约体检只读白名单")
	require.Equal(t, agentcfg.BlueprintFixAgent, skillAgentType("blueprint-fix"),
		"蓝图修复应走限权 BlueprintFixAgent")
	require.Equal(t, agentTypeSignability, skillAgentTypeString("blueprint-audit"),
		"蓝图体检 AgentType 字符串应复用 signability（同属只读体检）")
	require.Equal(t, agentTypeBlueprintFix, skillAgentTypeString("blueprint-fix"),
		"蓝图修复 AgentType 字符串应为 blueprintfix")
}
