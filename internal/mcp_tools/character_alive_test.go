//go:build cgo

package mcp_tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/session"
	"github.com/novelgo/novelgo/internal/storage"
)

// aliveTestEnv 造真实 sqlite 台账环境（含时间线钩子，验证 alive 变更可审计）。
func aliveTestEnv(t *testing.T) (*gorm.DB, mcp_tools.ToolContext, *mcp_tools.Registry) {
	t.Helper()
	db, tc, _ := setupRWEnv(t)
	require.NoError(t, db.AutoMigrate(
		&character.Character{}, &storage.LedgerChangeHistory{}, &storage.OperationLogRecord{},
		&session.Session{}, &session.Message{},
	))
	require.NoError(t, storage.RegisterOplogHooks(db))
	require.NoError(t, storage.RegisterLedgerTemporalHooks(db))
	return db, tc, newTestRegistry(t)
}

func aliveOf(t *testing.T, db *gorm.DB, id int64) *bool {
	t.Helper()
	var ch character.Character
	require.NoError(t, db.First(&ch, id).Error)
	return ch.Alive
}

func execAlive(t *testing.T, reg *mcp_tools.Registry, tc mcp_tools.ToolContext, name, args string) *mcp_tools.ToolResult {
	t.Helper()
	return reg.Execute(context.Background(), name, json.RawMessage(args), tc, nil)
}

// TestUpdateCharacterAliveFalseIsWritten 是 #102 的核心回归：旧工具没有 alive 写入口，
// 巡检发现「正文已死、alive=1」后修复只能改 description/personality，复验必然判「只改了一半」。
// 这里同时守住 GORM 零值跳写：显式 false 必须真的落成 0，不能被 default true 吃掉。
func TestUpdateCharacterAliveFalseIsWritten(t *testing.T) {
	db, tc, reg := aliveTestEnv(t)
	res := execAlive(t, reg, tc, "create_character", `{"characters":[{"name":"赵彦"}]}`)
	require.True(t, res.Success, "%+v", res)
	require.NotNil(t, aliveOf(t, db, 1))
	require.True(t, *aliveOf(t, db, 1), "新建角色缺省应为在世")

	res = execAlive(t, reg, tc, "update_character", `{"character_id":1,"alive":false}`)
	require.True(t, res.Success, "%+v", res, "只传 alive 也应通过入口空字段校验")
	got := aliveOf(t, db, 1)
	require.NotNil(t, got)
	require.False(t, *got, "显式 false 必须写入 DB（指针三态，勿回退成 bool）")
}

// TestUpdateCharacterAliveOmittedKeepsCurrent：不传 alive 时不得改动该列（含已标死状态）。
func TestUpdateCharacterAliveOmittedKeepsCurrent(t *testing.T) {
	db, tc, reg := aliveTestEnv(t)
	require.True(t, execAlive(t, reg, tc, "create_character", `{"characters":[{"name":"疤脸"}]}`).Success)
	require.True(t, *aliveOf(t, db, 1))

	require.True(t, execAlive(t, reg, tc, "update_character", `{"character_id":1,"name":"疤脸·改"}`).Success)
	require.True(t, *aliveOf(t, db, 1), "改名字不应把在世状态写脏")

	require.True(t, execAlive(t, reg, tc, "update_character", `{"character_id":1,"alive":false}`).Success)
	require.True(t, execAlive(t, reg, tc, "update_character", `{"character_id":1,"description":"已伏诛"}`).Success)
	got := aliveOf(t, db, 1)
	require.NotNil(t, got)
	require.False(t, *got, "已标死角色改自由文本字段时 alive 不得被重置为缺省 true")
}

// TestUpdateCharacterReviveRejected：复活属 retcon，只能走人工审批；LLM 不得为消解
// 一条张力项把已定稿的死亡事实改回去（已死不复活是机器校验的结构化规则）。
func TestUpdateCharacterReviveRejected(t *testing.T) {
	db, tc, reg := aliveTestEnv(t)
	require.True(t, execAlive(t, reg, tc, "create_character", `{"characters":[{"name":"赵彦"}]}`).Success)
	require.True(t, execAlive(t, reg, tc, "update_character", `{"character_id":1,"alive":false}`).Success)

	res := execAlive(t, reg, tc, "update_character", `{"character_id":1,"alive":true}`)
	require.False(t, res.Success)
	require.Contains(t, res.Error, "复活")
	require.Contains(t, res.Error, "人工")
	require.False(t, *aliveOf(t, db, 1), "被拒后 DB 状态不得变化")
}

// TestCreateCharacterAliveFalse：新建即传 false（正文已死角色补录）也应落 0；
// 不传时沿用 DB 缺省 true。
func TestCreateCharacterAliveFalse(t *testing.T) {
	db, tc, reg := aliveTestEnv(t)
	res := execAlive(t, reg, tc, "create_character",
		`{"characters":[{"name":"已故者","alive":false},{"name":"生者"}]}`)
	require.True(t, res.Success, "%+v", res)
	require.False(t, *aliveOf(t, db, 1), "显式 false 不得被 default true 覆盖")
	require.True(t, *aliveOf(t, db, 2), "未传 alive 沿用缺省在世")
}

// TestUpdateCharacterAliveKeepsImmutableFieldGuard：开放 alive 的同时，框架层
// DisallowUnknownFields 对 id/novel_id/is_current 等不可变字段的保护不得回退。
func TestUpdateCharacterAliveKeepsImmutableFieldGuard(t *testing.T) {
	db, tc, reg := aliveTestEnv(t)
	require.True(t, execAlive(t, reg, tc, "create_character", `{"characters":[{"name":"赵彦"}]}`).Success)

	res := execAlive(t, reg, tc, "update_character",
		`{"character_id":1,"alive":false,"id":999,"is_current":false}`)
	require.False(t, res.Success)
	require.Contains(t, res.Error, "参数格式不正确")
	require.True(t, *aliveOf(t, db, 1), "被拒请求不得产生任何写入")
}

// TestUpdateCharacterAliveChangeAudited：alive 变更应落到 ledger_change_history，
// 供复验/人工回溯「谁把角色标死了」，不成为无痕写入。
func TestUpdateCharacterAliveChangeAudited(t *testing.T) {
	db, tc, reg := aliveTestEnv(t)
	require.True(t, execAlive(t, reg, tc, "create_character", `{"characters":[{"name":"赵彦"}]}`).Success)
	require.True(t, execAlive(t, reg, tc, "update_character", `{"character_id":1,"alive":false}`).Success)

	rows := ledgerHistoryFor(t, db, "characters", 1)
	found := false
	for _, r := range rows {
		if r.Field == "alive" {
			found = true
		}
	}
	require.True(t, found, "alive 变更应进入台账履历：%+v", rows)
}
