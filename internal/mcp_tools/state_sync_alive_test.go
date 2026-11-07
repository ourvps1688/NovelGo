//go:build cgo

package mcp_tools_test

// #103 第三层接线回归：statesync 派发前必须从本章正文派生 alive，并把名单写进
// 指令供 LLM 复核；读不到正文/没有 DB 时优雅降级，绝不阻断同步。

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/storage"
)

func stateSyncAliveEnv(t *testing.T, chapterID int64, content string) *gorm.DB {
	t.Helper()
	db, _, _ := setupRWEnv(t)
	require.NoError(t, db.AutoMigrate(
		&character.Character{}, &storage.LedgerChangeHistory{}, &storage.OperationLogRecord{},
	))
	require.NoError(t, storage.RegisterOplogHooks(db))
	require.NoError(t, storage.RegisterLedgerTemporalHooks(db))

	yes := true
	require.NoError(t, db.Create(&character.Character{NovelID: 1, Name: "赵彦", Alive: &yes}).Error)
	require.NoError(t, db.Create(&character.Character{NovelID: 1, Name: "林澈", Alive: &yes}).Error)

	dir := config.NovelDirPath(1)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "chapters"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "chapters", fmt.Sprintf("id_%d.md", chapterID)),
		[]byte(content), 0o644,
	))
	return db
}

func aliveStateOf(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var ch character.Character
	require.NoError(t, db.Where("novel_id = ? AND name = ?", 1, name).First(&ch).Error)
	require.NotNil(t, ch.Alive)
	return *ch.Alive
}

func captureStateSync(t *testing.T, db *gorm.DB, chapterID int64) string {
	t.Helper()
	var instruction string
	stub := func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
		instruction = req.Instruction
		return "", nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, mcp_tools.RunStateSyncForChapter(
		context.Background(), db, stub, logger, "test", 1, chapterID,
	))
	return instruction
}

func TestRunStateSyncForChapter_DerivesAliveBeforeDispatch(t *testing.T) {
	db := stateSyncAliveEnv(t, 7, "赵彦当场气绝，林澈吓得后退。")

	instruction := captureStateSync(t, db, 7)

	require.False(t, aliveStateOf(t, db, "赵彦"), "派发前系统应从正文派生标死")
	require.True(t, aliveStateOf(t, db, "林澈"), "未明写死亡的角色不得被波及")
	require.Contains(t, instruction, "生死态本地派生")
	require.Contains(t, instruction, "赵彦")
	require.Contains(t, instruction, "气绝", "指令须带原文摘录供 LLM 复核")
	require.Contains(t, instruction, "update_character(alive=false)", "指令须给出补漏工具")
	require.Contains(t, instruction, "不得复活")
	require.NotContains(t, instruction, "本地派生不可用")
}

func TestRunStateSyncForChapter_NoDeathStillStatesDuty(t *testing.T) {
	db := stateSyncAliveEnv(t, 8, "赵彦推门而入，林澈递上一封信。")

	instruction := captureStateSync(t, db, 8)

	require.True(t, aliveStateOf(t, db, "赵彦"))
	require.Contains(t, instruction, "未发现")
	require.Contains(t, instruction, "update_character(alive=false)",
		"即使本地零命中，也必须告知 LLM 生死态同步职责，防止漏报后无人补齐")
}

func TestRunStateSyncForChapter_NilDBDegradesWithoutBlocking(t *testing.T) {
	var instruction string
	stub := func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
		instruction = req.Instruction
		return "", nil
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	require.NoError(t, mcp_tools.RunStateSyncForChapter(
		context.Background(), nil, stub, logger, "test", 1, 9,
	))
	require.Contains(t, instruction, "本地派生不可用")
	require.Contains(t, instruction, "update_character(alive=false)")
}

func TestRunStateSyncForChapter_MissingChapterFileDegrades(t *testing.T) {
	db := stateSyncAliveEnv(t, 10, "")
	// 删掉正文文件，模拟章节记录存在但正文尚未落盘。
	require.NoError(t, os.Remove(filepath.Join(config.NovelDirPath(1), "chapters", "id_10.md")))

	instruction := captureStateSync(t, db, 10)
	require.Contains(t, instruction, "本地派生不可用")
	require.True(t, aliveStateOf(t, db, "赵彦"), "降级时不得乱标死")
}

func TestStateSyncIdentityDeclaresAliveDuty(t *testing.T) {
	identity := agentcfg.AgentIdentity(agentcfg.StateSyncAgent)
	require.Contains(t, identity, "生死态")
	require.Contains(t, identity, "update_character(alive=false)")
	require.Contains(t, identity, "已死不复活")
	require.True(t, strings.Contains(identity, "characters.alive"),
		"须点明 alive 是 L0/巡检的事实源，避免 LLM 当成可有可无的备注字段")
}
