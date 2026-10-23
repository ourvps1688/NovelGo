package app

// P0 WBS T4.1：intent provider 单测（有源/无源/退化源/优先级）。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/volume"
)

// TestIntentTextFromBookPitch 有源：book-pitch 应用产物（novels 字段 + 五类偏好）全量组装。
func TestIntentTextFromBookPitch(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	n := &novel.Novel{Title: "凡人修仙", Genre: "玄幻", Description: "一个凡人的修仙路"}
	require.NoError(t, a.db.Create(n).Error)

	for _, p := range []CreatePreferenceInput{
		{Category: "一句话卖点", Content: "凡人也能登仙"},
		{Category: "方向", Content: "废柴逆袭"},
		{Category: "平台约束", Content: "起点风"},
		{Category: "创作约束", Content: "少用金手指"},
		{Category: "标签", Content: "修仙、种田"},
	} {
		_, err := a.CreatePreference(n.ID, p)
		require.NoError(t, err)
	}

	got, err := a.intentText(ctx, n.ID)
	require.NoError(t, err)
	want := "一句话卖点：凡人也能登仙\n" +
		"方向：废柴逆袭\n" +
		"平台约束：起点风\n" +
		"创作约束：少用金手指\n" +
		"标签：修仙、种田\n" +
		"题材：玄幻\n" +
		"简介：一个凡人的修仙路"
	require.Equal(t, want, got)
}

// TestIntentTextPitchLatestWins 同类多条取最新一条（book-pitch 重复应用只留终稿）。
func TestIntentTextPitchLatestWins(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	_, err := a.CreatePreference(n.ID, CreatePreferenceInput{Category: "一句话卖点", Content: "旧稿卖点"})
	require.NoError(t, err)
	_, err = a.CreatePreference(n.ID, CreatePreferenceInput{Category: "一句话卖点", Content: "终稿卖点"})
	require.NoError(t, err)

	got, err := a.intentText(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, "一句话卖点：终稿卖点", got)
}

// TestIntentTextNonPitchCategoriesIgnored 非 pitch 分类偏好不混入意图正文。
func TestIntentTextNonPitchCategoriesIgnored(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	_, err := a.CreatePreference(n.ID, CreatePreferenceInput{Category: "排版偏好", Content: "正文宋体"})
	require.NoError(t, err)

	got, err := a.intentText(ctx, n.ID)
	require.NoError(t, err)
	require.Empty(t, got, "非 pitch 分类不构成 intent 源")
}

// TestIntentTextFallsBackToVolumeOutline 退化源：无 book-pitch 产物 → 当前卷纲正文。
func TestIntentTextFallsBackToVolumeOutline(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)
	v := &volume.Volume{NovelID: n.ID, Name: "第一卷", SortOrder: 1}
	require.NoError(t, a.db.Create(v).Error)
	// 最新章归卷 → 当前卷=v.ID。
	ch := createTestChapter(t, a, n.ID)
	require.NoError(t, a.db.Model(ch).Update("volume_id", v.ID).Error)
	require.NoError(t, git.WriteFile(n.ID, git.VolumePath(v.ID), "本卷讲主角初入山门。"))

	got, err := a.intentText(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, "本卷讲主角初入山门。", got)
}

// TestIntentTextPitchPreferredOverOutline 优先级：book-pitch 产物非空时不退化卷纲。
func TestIntentTextPitchPreferredOverOutline(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)
	v := &volume.Volume{NovelID: n.ID, Name: "第一卷", SortOrder: 1}
	require.NoError(t, a.db.Create(v).Error)
	ch := createTestChapter(t, a, n.ID)
	require.NoError(t, a.db.Model(ch).Update("volume_id", v.ID).Error)
	require.NoError(t, git.WriteFile(n.ID, git.VolumePath(v.ID), "卷纲内容不该出现。"))
	_, err := a.CreatePreference(n.ID, CreatePreferenceInput{Category: "方向", Content: "热血升级"})
	require.NoError(t, err)

	got, err := a.intentText(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, "方向：热血升级", got)
}

// TestIntentTextEmptyWhenNoSource 无源：既无 book-pitch 产物又无卷纲 → 空串（降级不注入）。
func TestIntentTextEmptyWhenNoSource(t *testing.T) {
	a := setupTestApp(t)
	ctx := context.Background()
	n := &novel.Novel{Title: "T"}
	require.NoError(t, a.db.Create(n).Error)

	got, err := a.intentText(ctx, n.ID)
	require.NoError(t, err)
	require.Empty(t, got)
}
