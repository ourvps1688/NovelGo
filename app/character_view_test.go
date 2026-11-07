package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCharacters_Empty(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	chars, err := app.GetCharacters(novelID)
	require.NoError(t, err)
	assert.Empty(t, chars)
}

func TestCreateCharacter(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	char, err := app.CreateCharacter(novelID, CreateCharacterInput{
		Name:        "张三",
		Description: "主角",
		Personality: `{"traits":["勇敢","冲动"]}`,
		Abilities:   `["剑术","隐身"]`,
	})
	require.NoError(t, err)
	assert.NotZero(t, char.ID)
	assert.Equal(t, novelID, char.NovelID)
	assert.Equal(t, "张三", char.Name)
	assert.Equal(t, "主角", char.Description)
	assert.Equal(t, `{"traits":["勇敢","冲动"]}`, char.Personality)
	assert.Equal(t, `["剑术","隐身"]`, char.Abilities)
}

func TestCreateCharacter_EmptyName(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	_, err := app.CreateCharacter(novelID, CreateCharacterInput{
		Name: "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "角色名称不能为空")
}

func TestGetCharacters_AfterCreate(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	_, err := app.CreateCharacter(novelID, CreateCharacterInput{Name: "张三"})
	require.NoError(t, err)
	_, err = app.CreateCharacter(novelID, CreateCharacterInput{Name: "李四"})
	require.NoError(t, err)

	chars, err := app.GetCharacters(novelID)
	require.NoError(t, err)
	assert.Len(t, chars, 2)

	names := map[string]bool{}
	for _, c := range chars {
		names[c.Name] = true
	}
	assert.True(t, names["张三"])
	assert.True(t, names["李四"])
}

func TestUpdateCharacter(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	char, err := app.CreateCharacter(novelID, CreateCharacterInput{
		Name:        "旧名",
		Description: "旧描述",
	})
	require.NoError(t, err)

	err = app.UpdateCharacter(novelID, char.ID, UpdateCharacterInput{
		Name:        "新名",
		Description: "新描述",
	})
	require.NoError(t, err)

	chars, err := app.GetCharacters(novelID)
	require.NoError(t, err)
	require.Len(t, chars, 1)
	assert.Equal(t, "新名", chars[0].Name)
	assert.Equal(t, "新描述", chars[0].Description)
}

// #103 第三层：alive 的人工审批入口。mcp_tools 的 update_character 只允许
// 在世→已死单向；作者纠正派生误标 / retcon 复活只能经 app 这条 PUT 通道。
func TestCreateCharacter_AliveFalse(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	dead := false

	char, err := app.CreateCharacter(novel.ID, CreateCharacterInput{
		Name:  "已故者",
		Alive: &dead,
	})
	require.NoError(t, err)
	require.NotNil(t, char.Alive)
	assert.False(t, *char.Alive, "显式 false 不得被 DB 缺省 true 覆盖")
}

func TestUpdateCharacter_AlivePUTSemantics(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	dead, alive := false, true

	char, err := app.CreateCharacter(novel.ID, CreateCharacterInput{Name: "赵彦"})
	require.NoError(t, err)

	// ① 显式标死。
	require.NoError(t, app.UpdateCharacter(novel.ID, char.ID, UpdateCharacterInput{
		Name: "赵彦", Alive: &dead,
	}))
	chars, err := app.GetCharacters(novel.ID)
	require.NoError(t, err)
	require.Len(t, chars, 1)
	require.NotNil(t, chars[0].Alive)
	assert.False(t, *chars[0].Alive)

	// ② 普通文本编辑不传 alive：绝不能把已标死状态写回缺省 true（lost update）。
	require.NoError(t, app.UpdateCharacter(novel.ID, char.ID, UpdateCharacterInput{
		Name: "赵彦", Description: "补一段描述",
	}))
	chars, err = app.GetCharacters(novel.ID)
	require.NoError(t, err)
	assert.False(t, *chars[0].Alive)
	assert.Equal(t, "补一段描述", chars[0].Description)

	// ③ 人工通道允许 retcon 复活（工具侧会拒绝，作者侧是审批动作）。
	require.NoError(t, app.UpdateCharacter(novel.ID, char.ID, UpdateCharacterInput{
		Name: "赵彦", Description: "补一段描述", Alive: &alive,
	}))
	chars, err = app.GetCharacters(novel.ID)
	require.NoError(t, err)
	assert.True(t, *chars[0].Alive)
}

func TestDeleteCharacter(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	char, err := app.CreateCharacter(novelID, CreateCharacterInput{Name: "张三"})
	require.NoError(t, err)

	err = app.DeleteCharacter(novelID, char.ID)
	require.NoError(t, err)

	chars, err := app.GetCharacters(novelID)
	require.NoError(t, err)
	assert.Empty(t, chars)
}

func TestGetCharacterRelations(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	rels, err := app.GetCharacterRelations(novelID)
	require.NoError(t, err)
	assert.Empty(t, rels)
}
