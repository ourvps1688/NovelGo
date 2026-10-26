package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/config"
)

// writeUserSkillFile 在测试隔离的 user skill 目录写入一个 .md 技能文件，并返回其路径。
// 写入后需调用 app.skill.ReloadUser(config.UserSkillsDir()) 才会进入内存 store，
// 以便 agentcfg.BuildPersonaSystemText 的 skill / author 解析命中。
func writeUserSkillFile(t *testing.T, name, kind, sourceAuthor, body string, aggregated bool) {
	t.Helper()
	dir := config.UserSkillsDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	agg := ""
	if aggregated {
		agg = "\naggregated: true"
	}
	fm := "---\nname: " + name + "\nmode: always\ndescription: 测试心智 " + name +
		"\nkind: " + kind + "\nsource_author: " + sourceAuthor + agg + "\n---\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(fm), 0o644))
}

// TestWriteUserSkillFile_NeverTouchesRealHome 哨兵：测试夹具绝不写真实 ~/.novelgo/skills。
// 回归背景：writeUserSkillFile 曾把「X 的创作心智」夹具（mode: always）漏进真实用户技能目录，
// 被 always 注入链路带进所有小说的每个会话。隔离依赖 configDir 的 NOVELGO_CONFIG_DIR 通道
// （testsupport.Isolate 设置）。
func TestWriteUserSkillFile_NeverTouchesRealHome(t *testing.T) {
	_ = setupTestApp(t)
	writeUserSkillFile(t, "X 的创作心智", "mental", "刘慈欣", "X_MENTAL_BODY", false)

	dir := config.UserSkillsDir()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	realDir := filepath.Join(home, ".novelgo", "skills")
	absDir, err := filepath.Abs(dir)
	require.NoError(t, err)
	absReal, err := filepath.Abs(realDir)
	require.NoError(t, err)
	require.NotEqual(t, absReal, absDir, "user skill 目录必须与真实 ~/.novelgo/skills 隔离")

	_, statErr := os.Stat(filepath.Join(realDir, "X 的创作心智.md"))
	require.True(t, os.IsNotExist(statErr), "测试夹具不得出现在真实用户技能目录：%v", statErr)
}

// TestWriteSystemMessages_PersonaBlockPersisted 验证新会话的 persona 引导段被作为常驻
// system 消息写入 DB，并且 loadAPIMessages 能读回（含 PersonaMarker）。
func TestWriteSystemMessages_PersonaBlockPersisted(t *testing.T) {
	app := setupTestApp(t)
	ctx := context.Background()
	n := createTestNovel(t, app)

	writeUserSkillFile(t, "X 的创作心智", "mental", "刘慈欣", "X_MENTAL_BODY", false)
	require.NoError(t, app.skill.ReloadUser(config.UserSkillsDir()))

	sessionID := "sess_persona_persist"
	turnID := 1
	persona := &agentcfg.PersonaRef{Kind: "skill", Ref: "X 的创作心智"}

	sysMsgs, err := agentcfg.BuildSystemMessages(ctx, app.db, n.ID, app.skill, persona)
	require.NoError(t, err)
	require.Contains(t, sysMsgs.Persona, agentcfg.PersonaMarker, "persona 引导段应被构建")

	require.NoError(t, writeSystemMessages(app.db, sessionID, turnID, sysMsgs))

	msgs, _, err := app.loadAPIMessages(ctx, sessionID, 1)
	require.NoError(t, err)

	found := false
	for _, m := range msgs {
		if content, _ := m["content"].(string); strings.Contains(content, agentcfg.PersonaMarker) {
			found = true
			require.Contains(t, content, "X_MENTAL_BODY", "读回的 persona 块应包含技能正文")
			require.Contains(t, content, agentcfg.PersonaHonestyNote, "读回的 persona 块应包含诚实边界声明")
		}
	}
	require.True(t, found, "应从 DB 读回 persona 常驻块")
}

// TestInjectPersonaMessages_InjectsAndDedupes 验证已有会话每轮注入 persona（前置 system 消息），
// 且对已含 PersonaMarker 的消息去重、不重复注入。
func TestInjectPersonaMessages_InjectsAndDedupes(t *testing.T) {
	app := setupTestApp(t)
	_ = context.Background()
	n := createTestNovel(t, app)

	writeUserSkillFile(t, "X 的创作心智", "mental", "刘慈欣", "X_MENTAL_BODY", false)
	require.NoError(t, app.skill.ReloadUser(config.UserSkillsDir()))

	persona := &agentcfg.PersonaRef{Kind: "skill", Ref: "X 的创作心智"}
	runningTokens := map[string]int{"system": 0, "user": 0, "assistant": 0, "tool": 0}

	// 模拟已有会话（无 persona 常驻块）
	base := []map[string]any{
		{"role": "system", "content": "identity"},
		{"role": "user", "content": "写一章"},
	}

	injected := injectPersonaMessages(base, runningTokens, persona, app.skill, n.ID)
	require.Len(t, injected, 3, "应前置追加一条 persona 系统消息")
	require.Equal(t, "system", injected[0]["role"])
	require.Contains(t, injected[0]["content"], agentcfg.PersonaMarker)

	// 再次注入：injected 已含 PersonaMarker，应原样返回，不重复。
	again := injectPersonaMessages(injected, runningTokens, persona, app.skill, n.ID)
	require.Len(t, again, 3, "去重后不应重复注入")
	count := 0
	for _, m := range again {
		if c, _ := m["content"].(string); strings.Contains(c, agentcfg.PersonaMarker) {
			count++
		}
	}
	require.Equal(t, 1, count, "persona 系统消息应恰好一条")
}

// TestInjectPersonaMessages_NilOrUnresolvedReturnsOriginal 验证 nil persona 或解析失败时原样返回。
func TestInjectPersonaMessages_NilOrUnresolvedReturnsOriginal(t *testing.T) {
	app := setupTestApp(t)
	_ = context.Background()

	base := []map[string]any{{"role": "user", "content": "hi"}}
	runningTokens := map[string]int{"system": 0, "user": 0, "assistant": 0, "tool": 0}

	// nil persona
	out := injectPersonaMessages(base, runningTokens, nil, app.skill, 1)
	require.Equal(t, base, out, "nil persona 应原样返回")

	// 引用不存在的 skill
	out2 := injectPersonaMessages(base, runningTokens, &agentcfg.PersonaRef{Kind: "skill", Ref: "不存在"}, app.skill, 1)
	require.Equal(t, base, out2, "解析失败应原样返回")

	// 已含 PersonaMarker（如新会话常驻块读回）应原样返回
	withMarker := []map[string]any{{"role": "system", "content": agentcfg.PersonaMarker + " 块"}}
	out3 := injectPersonaMessages(withMarker, runningTokens, &agentcfg.PersonaRef{Kind: "skill", Ref: "不存在"}, app.skill, 1)
	require.Equal(t, withMarker, out3)
}

// TestPersonaSetting_SaveGetIdempotent 验证 SavePersonaSetting 按 novel_id 幂等 upsert，
// 以及 GetPersonaSetting 对未配置的书返回默认（Enabled=false）。
func TestPersonaSetting_SaveGetIdempotent(t *testing.T) {
	app := setupTestApp(t)
	n := createTestNovel(t, app)

	// 未配置时默认普通模式
	def, err := app.GetPersonaSetting(n.ID)
	require.NoError(t, err)
	require.False(t, def.Enabled, "未配置应默认 false")
	require.Empty(t, def.Kind)
	require.Empty(t, def.Ref)

	// 首次保存
	require.NoError(t, app.SavePersonaSetting(n.ID, SavePersonaSettingInput{
		Enabled: true, Kind: "skill", Ref: "X 的创作心智",
	}))
	got, err := app.GetPersonaSetting(n.ID)
	require.NoError(t, err)
	require.True(t, got.Enabled)
	require.Equal(t, "skill", got.Kind)
	require.Equal(t, "X 的创作心智", got.Ref)

	// 幂等覆盖（切换为 author 并关闭）
	require.NoError(t, app.SavePersonaSetting(n.ID, SavePersonaSettingInput{
		Enabled: false, Kind: "author", Ref: "刘慈欣",
	}))
	got2, err := app.GetPersonaSetting(n.ID)
	require.NoError(t, err)
	require.False(t, got2.Enabled, "应覆盖为关闭")
	require.Equal(t, "author", got2.Kind)
	require.Equal(t, "刘慈欣", got2.Ref)

	// 不同书互不影响
	def2, err := app.GetPersonaSetting(n.ID + 999)
	require.NoError(t, err)
	require.False(t, def2.Enabled, "其他书仍默认普通模式")
}
