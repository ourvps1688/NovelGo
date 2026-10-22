//go:build cgo

package mcp_tools_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/session"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/timeline"
)

// ledgerHistoryFor 查某条目的全部变更履历（按写入序）。
func ledgerHistoryFor(t *testing.T, db *gorm.DB, table string, entryID int64) []storage.LedgerChangeHistory {
	t.Helper()
	var rows []storage.LedgerChangeHistory
	require.NoError(t, db.Where("entry_table = ? AND entry_id = ?", table, entryID).
		Order("id ASC").Find(&rows).Error)
	return rows
}

// TestLedgerTemporalHistoryThroughTools 是 WBS 2.6 验收用例：
// 「任意台账改动能查到 changed_at + history」——走真实 MCP 工具写路径
// （create/update/delete_record），断言 changed_at 维护 + ledger_change_history 履历，
// 以及 source 判定（工具=auto，前端 CRUD 形态=author）与「语义变更 vs 格式调整」判定。
func TestLedgerTemporalHistoryThroughTools(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	require.NoError(t, db.AutoMigrate(
		&character.Character{}, &character.CharacterRelation{}, &character.Group{}, &character.GroupMember{},
		&timeline.TimelineEntry{}, &storage.LedgerChangeHistory{}, &storage.OperationLogRecord{},
		&session.Session{}, &session.Message{},
	))
	require.NoError(t, storage.RegisterOplogHooks(db))
	require.NoError(t, storage.RegisterLedgerTemporalHooks(db))
	reg := newTestRegistry(t)

	// ① create_character：建行即有 changed_at + (create) 履历，source=auto（工具入口标注）
	res := reg.Execute(ctx, "create_character",
		json.RawMessage(`{"characters":[{"name":"陈三斗","description":"陈守拙之父"}]}`), tc, nil)
	require.True(t, res.Success, "%+v", res)

	var ch character.Character
	require.NoError(t, db.First(&ch, 1).Error)
	require.NotNil(t, ch.ChangedAt, "create 后 changed_at 不应为 NULL")

	rows := ledgerHistoryFor(t, db, "characters", ch.ID)
	require.Len(t, rows, 1)
	require.Equal(t, storage.HistoryFieldCreate, rows[0].Field)
	require.Equal(t, "陈三斗", rows[0].New)
	require.Equal(t, storage.HistorySourceAuto, rows[0].Source)

	// ② update_character 语义变更：changed_at 推进 + 逐字段履历
	firstChangedAt := *ch.ChangedAt
	res = reg.Execute(ctx, "update_character",
		json.RawMessage(`{"character_id":1,"name":"陈三斗·改"}`), tc, nil)
	require.True(t, res.Success, "%+v", res)

	require.NoError(t, db.First(&ch, 1).Error)
	require.True(t, ch.ChangedAt.After(firstChangedAt), "语义变更应推进 changed_at")

	rows = ledgerHistoryFor(t, db, "characters", ch.ID)
	require.Len(t, rows, 2)
	require.Equal(t, "name", rows[1].Field)
	require.Equal(t, "陈三斗", rows[1].Old)
	require.Equal(t, "陈三斗·改", rows[1].New)
	require.Equal(t, storage.HistorySourceAuto, rows[1].Source)

	// ③ 纯格式调整（description 仅空白差异）：只落盘，不动 changed_at、不记履历
	semanticChangedAt := *ch.ChangedAt
	res = reg.Execute(ctx, "update_character",
		json.RawMessage(`{"character_id":1,"description":"陈守拙之父  "}`), tc, nil)
	require.True(t, res.Success, "%+v", res)

	require.NoError(t, db.First(&ch, 1).Error)
	require.Equal(t, semanticChangedAt, *ch.ChangedAt, "格式调整不应动 changed_at")
	require.Len(t, ledgerHistoryFor(t, db, "characters", ch.ID), 2, "格式调整不应记履历")

	// ④ 第二张台账表（time_entries）验证机制通用：create + update
	res = reg.Execute(ctx, "create_timeline_entry", json.RawMessage(
		`{"entries":[{"category":"foreshadowing","title":"水鬼索命","content":"苏阿水的复仇","target_reading_number":1}]}`),
		tc, nil)
	require.True(t, res.Success, "%+v", res)

	res = reg.Execute(ctx, "update_timeline_entry",
		json.RawMessage(`{"entry_id":1,"status":"resolved"}`), tc, nil)
	require.True(t, res.Success, "%+v", res)

	rows = ledgerHistoryFor(t, db, "time_entries", 1)
	require.Len(t, rows, 2)
	require.Equal(t, storage.HistoryFieldCreate, rows[0].Field)
	require.Equal(t, "status", rows[1].Field)
	require.Equal(t, "pending", rows[1].Old)
	require.Equal(t, "resolved", rows[1].New)

	// ⑤ delete_record：(delete) 摘要履历，旧值=条目摘要
	res = reg.Execute(ctx, "delete_record",
		json.RawMessage(`{"table":"timeline_entry","id":1}`), tc, nil)
	require.True(t, res.Success, "%+v", res)

	rows = ledgerHistoryFor(t, db, "time_entries", 1)
	require.Len(t, rows, 3)
	require.Equal(t, storage.HistoryFieldDelete, rows[2].Field)
	require.Equal(t, "水鬼索命", rows[2].Old)
	require.Equal(t, "", rows[2].New)

	// ⑥ 前端 CRUD 形态（直接 GORM 写、无工具标注）：source=author
	ch2 := character.Character{NovelID: 1, Name: "小满"}
	require.NoError(t, db.Create(&ch2).Error)
	rows = ledgerHistoryFor(t, db, "characters", ch2.ID)
	require.Len(t, rows, 1)
	require.Equal(t, storage.HistorySourceAuthor, rows[0].Source)
}
