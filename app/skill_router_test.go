package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildAutoSkillInject_AutoMatch 验证 auto 技能按 useWhen 命中并自动注入正文。
func TestBuildAutoSkillInject_AutoMatch(t *testing.T) {
	app := setupTestApp(t)
	inject := app.buildAutoSkillInject(context.Background(), 0, "帮我设计一下力量体系", "", "")
	require.NotEmpty(t, inject, "写力量体系应自动注入 worldbuilding-design")
	require.Contains(t, inject, "worldbuilding-design")
	require.Contains(t, inject, "自动加载")
	require.Contains(t, inject, "<system-reminder>")
}

// TestBuildAutoSkillInject_ManualOrchestrator 验证写意图命中总调度 next（manual）并建议注入。
func TestBuildAutoSkillInject_ManualOrchestrator(t *testing.T) {
	app := setupTestApp(t)
	inject := app.buildAutoSkillInject(context.Background(), 0, "继续写下一章", "", "")
	require.NotEmpty(t, inject, "写下一章应触发 next 总调度")
	require.Contains(t, inject, "next")
	require.Contains(t, inject, "总调度")
}

// TestBuildAutoSkillInject_NoMatch 验证无关消息不产生注入。
func TestBuildAutoSkillInject_NoMatch(t *testing.T) {
	app := setupTestApp(t)
	inject := app.buildAutoSkillInject(context.Background(), 0, "今天天气真好", "", "")
	require.Empty(t, inject)
}

// TestBuildAutoSkillInject_ExplicitNameExcluded 验证消息中已点名技能时不重复注入。
func TestBuildAutoSkillInject_ExplicitNameExcluded(t *testing.T) {
	app := setupTestApp(t)
	inject := app.buildAutoSkillInject(context.Background(), 0, "用 worldbuilding-design 帮我看看设定", "", "")
	require.Empty(t, inject, "已显式点名 worldbuilding-design 不应再自动注入")
}

// TestBuildAutoSkillInject_SlashBypassed 验证斜杠消息不走预路由（由 resolveSlashCommand 处理）。
func TestBuildAutoSkillInject_SlashBypassed(t *testing.T) {
	app := setupTestApp(t)
	inject := app.buildAutoSkillInject(context.Background(), 0, "/worldbuilding-design", "", "")
	require.Empty(t, inject, "斜杠命令不应触发预路由自动注入")
}
