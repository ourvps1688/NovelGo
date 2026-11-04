package mcp_tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
)

// ── 回归：JSON 文本参数传真 JSON 数组/对象不再炸 ──────────────────
//
// 线上真实报错：「创建新角色：参数格式不正确: json cannot unmarshal array into
// Go struct field CreateCharacterArgs.characters.0.abilities of type string」。
// 根因：abilities/personality/detail_json/tags/params 等字段 Go 侧声明 string
// （存 JSON 文本），LLM 按工具描述传真 JSON 数组/对象。修复见 args_coerce.go：
// Registry.Execute 统一把 string 字段的数组/对象值规范化为紧凑 JSON 文本，
// Args 解码与 update 工具的 tc.RawArgs PATCH 二次解码同时受益。

func jsonTextArgsFixture(t *testing.T) (*mcp_tools.Registry, mcp_tools.ToolContext) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&character.Character{}, &character.Group{}, &character.GroupMember{},
		&location.Location{}, &location.LocationRelation{},
		&structure.PinnedInvariant{}, &storage.OperationLogRecord{},
	))
	require.NoError(t, db.Create(&character.Character{
		ID: 1, NovelID: 1, Name: "林川",
		Personality: `{"old":true}`, Abilities: `["旧能力"]`,
	}).Error)
	return newTestRegistry(t), mcp_tools.ToolContext{DB: db, NovelID: 1}
}

// TestCreateCharacterAcceptsJSONArrayAndObject 复现线上报错的原始载荷形态
// （abilities 为 JSON 数组、personality 为 JSON 对象）应成功落库为紧凑 JSON 文本。
func TestCreateCharacterAcceptsJSONArrayAndObject(t *testing.T) {
	reg, tc := jsonTextArgsFixture(t)
	args := `{"characters":[{"name":"苏晚","description":"冷静的女医师",` +
		`"personality":{"brief":"冷静","traits":["细心"]},"abilities":["医术","毒术"]}]}`
	res := reg.Execute(context.Background(), "create_character", json.RawMessage(args), tc, nil)
	require.True(t, res.Success, "数组/对象参数不应报参数格式不正确: %s", res.Error)

	var ch character.Character
	require.NoError(t, tc.DB.Where("novel_id = ? AND name = ?", 1, "苏晚").First(&ch).Error)
	require.Equal(t, `["医术","毒术"]`, ch.Abilities)
	require.Equal(t, `{"brief":"冷静","traits":["细心"]}`, ch.Personality)
}

// TestCreateCharacterAcceptsStringJSONForm 旧的字符串形式 JSON 传法保持不变。
func TestCreateCharacterAcceptsStringJSONForm(t *testing.T) {
	reg, tc := jsonTextArgsFixture(t)
	args := `{"characters":[{"name":"苏晚","personality":"{\"brief\":\"冷静\"}","abilities":"[\"医术\"]"}]}`
	res := reg.Execute(context.Background(), "create_character", json.RawMessage(args), tc, nil)
	require.True(t, res.Success, "%s", res.Error)

	var ch character.Character
	require.NoError(t, tc.DB.Where("novel_id = ? AND name = ?", 1, "苏晚").First(&ch).Error)
	require.Equal(t, `["医术"]`, ch.Abilities)
	require.Equal(t, `{"brief":"冷静"}`, ch.Personality)
}

// TestUpdateCharacterPatchesWithJSONArrayAbilities update 工具的 tc.RawArgs
// PATCH 二次解码路径同样修复：abilities 数组正常覆盖，未传字段保持原值（PATCH 语义）。
func TestUpdateCharacterPatchesWithJSONArrayAbilities(t *testing.T) {
	reg, tc := jsonTextArgsFixture(t)
	args := `{"character_id":1,"abilities":["新能力","轻功"]}`
	res := reg.Execute(context.Background(), "update_character", json.RawMessage(args), tc, nil)
	require.True(t, res.Success, "PATCH 传真数组不应报参数格式不正确: %s", res.Error)

	var ch character.Character
	require.NoError(t, tc.DB.Where("id = ?", 1).First(&ch).Error)
	require.Equal(t, `["新能力","轻功"]`, ch.Abilities)
	require.Equal(t, "林川", ch.Name, "未传字段保持原值")
	require.Equal(t, `{"old":true}`, ch.Personality, "未传字段保持原值")
}

// TestCreateLocationAcceptsJSONArrayTags 地点 tags/detail_json 同类字段覆盖。
func TestCreateLocationAcceptsJSONArrayTags(t *testing.T) {
	reg, tc := jsonTextArgsFixture(t)
	args := `{"locations":[{"name":"黑森林","location_type":"森林",` +
		`"tags":["危险","神秘"],"detail_json":{"climate":"cold"}}]}`
	res := reg.Execute(context.Background(), "create_location", json.RawMessage(args), tc, nil)
	require.True(t, res.Success, "%s", res.Error)

	var loc location.Location
	require.NoError(t, tc.DB.Where("novel_id = ? AND name = ?", 1, "黑森林").First(&loc).Error)
	require.Equal(t, `["危险","神秘"]`, loc.Tags)
	require.Equal(t, `{"climate":"cold"}`, loc.DetailJSON)
}

// TestUpsertPinnedInvariantAcceptsObjectParams 不变量 params 整个对象参数覆盖
// （对象转紧凑文本后仍满足 kind 参数契约校验）。
func TestUpsertPinnedInvariantAcceptsObjectParams(t *testing.T) {
	reg, tc := jsonTextArgsFixture(t)
	args := `{"kind":"character_alive_before","statement":"主角第50章前不死",` +
		`"params":{"character":"主角","before_reading_number":50},"confirmed":true}`
	res := reg.Execute(context.Background(), "upsert_pinned_invariant", json.RawMessage(args), tc, nil)
	require.True(t, res.Success, "%s", res.Error)

	var p structure.PinnedInvariant
	require.NoError(t, tc.DB.Where("novel_id = ?", 1).First(&p).Error)
	require.Equal(t, `{"before_reading_number":50,"character":"主角"}`, p.ParamsJSON)
}

// TestJSONTextCoercionKeepsUnknownFieldRejection 宽容化只针对形态不匹配，
// schema 外字段仍被 DisallowUnknownFields 拒绝（防覆盖 entity 不可变字段的
// 契约不变）。
func TestJSONTextCoercionKeepsUnknownFieldRejection(t *testing.T) {
	reg, tc := jsonTextArgsFixture(t)
	args := `{"characters":[{"name":"苏晚","id":999,"abilities":["医术"]}]}`
	res := reg.Execute(context.Background(), "create_character", json.RawMessage(args), tc, nil)
	require.False(t, res.Success)
	if !strings.Contains(res.Error, "参数格式不正确") {
		t.Errorf("unknown field must still be rejected, got: %s", res.Error)
	}
}
