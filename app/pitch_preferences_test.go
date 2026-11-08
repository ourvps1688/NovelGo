package app

// UpsertPitchPreferences 回归测试：幂等收敛、清空、隔离、平台联动。
// 覆盖 #107 事故：自动保存 + 手动采纳各写一组，重复行与冲突平台值一起进 system。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/quality"
)

func pitchInput() UpsertPitchPreferencesInput {
	return UpsertPitchPreferencesInput{
		Tags:      "玄幻、进化流",
		Direction: "从死牢杀到大荒",
		Platform:  "七猫",
		Style:     "爽文",
		OneLiner:  "万兽入我身，一拳开天门。",
	}
}

func pitchRows(t *testing.T, a *App, novelID int64) []preference.PreferenceItem {
	t.Helper()
	var rows []preference.PreferenceItem
	require.NoError(t, a.db.Where("novel_id = ? AND is_global = ?", novelID, false).
		Order("id ASC").Find(&rows).Error)
	return rows
}

func TestUpsertPitchPreferencesCreatesFiveRows(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))
	rows := pitchRows(t, a, n.ID)
	require.Len(t, rows, 5)
	byCat := map[string]string{}
	for _, r := range rows {
		byCat[r.Category] = r.Content
	}
	require.Equal(t, "玄幻、进化流", byCat["标签"])
	require.Equal(t, "从死牢杀到大荒", byCat["方向"])
	require.Equal(t, "七猫", byCat["平台约束"])
	require.Equal(t, "爽文", byCat["创作约束"])
	require.Equal(t, "万兽入我身，一拳开天门。", byCat["一句话卖点"])
}

// 旧链路的复现：两次保存不得堆积成 10 行，且以最新终稿为准。
func TestUpsertPitchPreferencesIdempotentAndLatestWins(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	first := pitchInput()
	first.Platform = "番茄"
	require.NoError(t, a.UpsertPitchPreferences(n.ID, first))
	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))

	rows := pitchRows(t, a, n.ID)
	require.Len(t, rows, 5, "重复保存不得新增行")
	for _, r := range rows {
		require.NotEqual(t, "番茄", r.Content, "旧稿平台值必须被覆盖")
	}
	byCat := map[string]string{}
	for _, r := range rows {
		byCat[r.Category] = r.Content
	}
	require.Equal(t, "七猫", byCat["平台约束"])
}

// 存量脏数据：同类多条（含冲突平台）一次收敛为单条，保留 id 最大者。
func TestUpsertPitchPreferencesCollapsesLegacyDuplicates(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	for _, c := range []CreatePreferenceInput{
		{Category: "平台约束", Content: "番茄"},
		{Category: "标签", Content: "旧标签"},
		{Category: "平台约束", Content: "七猫"},
		{Category: "标签", Content: "旧标签"},
		{Category: "创作约束", Content: "用户手写纪律"}, // 非 pitch 手写内容，本次全量 PUT 会覆盖
	} {
		_, err := a.CreatePreference(n.ID, c)
		require.NoError(t, err)
	}

	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))
	rows := pitchRows(t, a, n.ID)
	require.Len(t, rows, 5)
	for _, r := range rows {
		require.NotEqual(t, "番茄", r.Content)
		require.NotEqual(t, "旧标签", r.Content)
	}
}

func TestUpsertPitchPreferencesEmptyFieldClearsCategory(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)
	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))

	next := pitchInput()
	next.OneLiner = "  "
	require.NoError(t, a.UpsertPitchPreferences(n.ID, next))

	rows := pitchRows(t, a, n.ID)
	require.Len(t, rows, 4)
	for _, r := range rows {
		require.NotEqual(t, "一句话卖点", r.Category)
	}
}

func TestUpsertPitchPreferencesIsolatesNovelsAndGlobal(t *testing.T) {
	a := setupTestApp(t)
	n1 := &novel.Novel{Title: "A"}
	n2 := &novel.Novel{Title: "B"}
	require.NoError(t, a.db.Create(n1).Error)
	require.NoError(t, a.db.Create(n2).Error)

	require.NoError(t, a.UpsertPitchPreferences(n1.ID, pitchInput()))
	_, err := a.CreatePreference(n1.ID, CreatePreferenceInput{
		IsGlobal: true, Category: "标签", Content: "全局标签",
	})
	require.NoError(t, err)
	require.NoError(t, a.UpsertPitchPreferences(n2.ID, UpsertPitchPreferencesInput{
		Tags: "B 的标签", Platform: "番茄",
	}))

	require.Len(t, pitchRows(t, a, n1.ID), 5, "全局偏好不归属任何书，且不应被收敛误删")
	require.Len(t, pitchRows(t, a, n2.ID), 2, "另一本书只写自己传入的两类")
	var globalRows []preference.PreferenceItem
	require.NoError(t, a.db.Where("is_global = ?", true).Find(&globalRows).Error)
	require.Len(t, globalRows, 1)
	require.Equal(t, "全局标签", globalRows[0].Content)
}

func TestUpsertPitchPreferencesArmsPlatformProfile(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)
	store := quality.NewStore(a.db, a.logger)

	// 未注册平台不武装任何口径。
	input := pitchInput()
	input.Platform = "番茄"
	require.NoError(t, a.UpsertPitchPreferences(n.ID, input))
	cfg, err := store.GetCheckerConfig(a.ctx, n.ID)
	require.NoError(t, err)
	require.Empty(t, cfg.PlatformProfile)

	// 七猫别名归一为注册名 qimao。
	input.Platform = "七猫"
	require.NoError(t, a.UpsertPitchPreferences(n.ID, input))
	cfg, err = store.GetCheckerConfig(a.ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, "qimao", cfg.PlatformProfile)

	// 用户显式设的值不被覆盖。
	require.NoError(t, a.ApplyPlatformProfile(n.ID, ""))
	input.Platform = "七猫"
	require.NoError(t, a.UpsertPitchPreferences(n.ID, input))
	cfg, err = store.GetCheckerConfig(a.ctx, n.ID)
	require.NoError(t, err)
	require.Empty(t, cfg.PlatformProfile, "用户显式清空后，后续保存不得重新武装")
}

func TestUpsertPitchPreferencesKeepsManualWritingRules(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	// 历史数据里「创作约束」同时装着 pitch 短风格与用户手写的长篇纪律。
	manual := strings.Repeat("禁状态面板体；", 20)
	_, err := a.CreatePreference(n.ID, CreatePreferenceInput{Category: "创作约束", Content: "旧风格"})
	require.NoError(t, err)
	_, err = a.CreatePreference(n.ID, CreatePreferenceInput{Category: "创作约束", Content: manual})
	require.NoError(t, err)

	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))
	rows := pitchRows(t, a, n.ID)
	require.Len(t, rows, 6, "手写纪律保留 + 收敛后的五类引导行")
	foundManual := false
	foundOldStyle := false
	for _, r := range rows {
		if r.Content == manual {
			foundManual = true
		}
		if r.Content == "旧风格" {
			foundOldStyle = true
		}
	}
	require.True(t, foundManual, "手写长纪律不得被收敛删除")
	require.False(t, foundOldStyle, "pitch 旧风格行应被收敛为本次值")

	// 再保存一次仍不丢纪律、也不新增行。
	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))
	require.Len(t, pitchRows(t, a, n.ID), 6)
}

func TestUpsertPitchPreferencesIntentBlockUsesFinalDraft(t *testing.T) {
	a := setupTestApp(t)
	n := &novel.Novel{Title: "T", Genre: "玄幻"}
	require.NoError(t, a.db.Create(n).Error)
	first := pitchInput()
	first.OneLiner = "旧卖点"
	require.NoError(t, a.UpsertPitchPreferences(n.ID, first))
	require.NoError(t, a.UpsertPitchPreferences(n.ID, pitchInput()))

	got, err := a.intentText(a.ctx, n.ID)
	require.NoError(t, err)
	require.Contains(t, got, "一句话卖点：万兽入我身，一拳开天门。")
	require.NotContains(t, got, "旧卖点")
	require.Contains(t, got, "平台约束：七猫")
	require.NotContains(t, got, "平台约束：番茄")
}
