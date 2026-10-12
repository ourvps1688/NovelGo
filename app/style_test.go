package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/preference"
	"github.com/novelgo/novelgo/internal/style"
)

// ---------------------------------------------------------------------------
// ListStyleSamples
// ---------------------------------------------------------------------------

func TestListStyleSamples_Empty(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	result, err := app.ListStyleSamples(ListStyleSamplesInput{
		NovelID: novel.ID,
		Page:    1,
		Size:    20,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), result.Total)
	assert.Empty(t, result.Items)
}

// ---------------------------------------------------------------------------
// CreateStyleSample
// ---------------------------------------------------------------------------

func TestCreateStyleSample(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	content := strings.Repeat("这是一段很长的测试内容，用于验证预览截断功能是否正常工作。", 10)
	sample, err := app.CreateStyleSample(CreateStyleSampleInput{
		NovelID:  novel.ID,
		IsGlobal: false,
		Name:     "测试素材",
		Content:  content,
		Tags:     []string{"标签A", "标签B"},
	})
	require.NoError(t, err)
	require.NotZero(t, sample.ID)
	assert.Equal(t, novel.ID, sample.NovelID)
	assert.False(t, sample.IsGlobal)
	assert.Equal(t, "测试素材", sample.Name)
	assert.Equal(t, content, sample.Content)
	assert.Greater(t, sample.WordCount, 0)
	assert.Contains(t, sample.Tags, "标签A")
	assert.Contains(t, sample.Tags, "标签B")
	assert.Len(t, sample.Tags, 2)

	// Preview should be truncated to PreviewMaxRunes (120 runes).
	runes := []rune(content)
	if len(runes) > 120 {
		previewRunes := []rune(sample.Preview)
		assert.Equal(t, 121, len(previewRunes), "preview should be 120 runes + ellipsis")
		assert.True(t, strings.HasSuffix(sample.Preview, "…"))
	} else {
		assert.Equal(t, content, sample.Preview)
	}
}

// ---------------------------------------------------------------------------
// GetStyleSample
// ---------------------------------------------------------------------------

func TestGetStyleSample(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	created, err := app.CreateStyleSample(CreateStyleSampleInput{
		NovelID: novel.ID,
		Name:    "待查询",
		Content: "完整内容",
		Tags:    []string{"test"},
	})
	require.NoError(t, err)

	got, err := app.GetStyleSample(created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "待查询", got.Name)
	assert.Equal(t, "完整内容", got.Content)
}

// ---------------------------------------------------------------------------
// UpdateStyleSample
// ---------------------------------------------------------------------------

func TestUpdateStyleSample(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	created, err := app.CreateStyleSample(CreateStyleSampleInput{
		NovelID:  novel.ID,
		Name:     "原始名称",
		Content:  "原始内容",
		Tags:     []string{"旧标签"},
		IsGlobal: false,
	})
	require.NoError(t, err)

	updated, err := app.UpdateStyleSample(UpdateStyleSampleInput{
		ID:       created.ID,
		Name:     "更新后名称",
		Content:  "更新后的新内容",
		Tags:     []string{"新标签", "额外标签"},
		IsGlobal: true,
		NovelID:  novel.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "更新后名称", updated.Name)
	assert.Equal(t, "更新后的新内容", updated.Content)
	assert.True(t, updated.IsGlobal)
	assert.Contains(t, updated.Tags, "新标签")
	assert.Contains(t, updated.Tags, "额外标签")

	// WordCount should reflect the new content.
	assert.Greater(t, updated.WordCount, 0)
}

// ---------------------------------------------------------------------------
// DeleteStyleSample
// ---------------------------------------------------------------------------

func TestDeleteStyleSample(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	created, err := app.CreateStyleSample(CreateStyleSampleInput{
		NovelID: novel.ID,
		Name:    "待删除",
		Content: "内容",
		Tags:    nil,
	})
	require.NoError(t, err)

	err = app.DeleteStyleSample(DeleteStyleSampleInput{ID: created.ID})
	require.NoError(t, err)

	// Verify the record is gone.
	_, err = app.GetStyleSample(created.ID)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// ComputeStyleStats
// ---------------------------------------------------------------------------

func TestComputeStyleStats(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	var ids []int64
	for i := 0; i < 3; i++ {
		sample, err := app.CreateStyleSample(CreateStyleSampleInput{
			NovelID: novel.ID,
			Name:    string(rune('A' + rune(i))),
			Content: "这是第一句。这是第二句，包含逗号。这是第三句！这是第四句？",
			Tags:    nil,
		})
		require.NoError(t, err)
		ids = append(ids, sample.ID)
	}

	stats, err := app.ComputeStyleStats(ComputeStyleStatsInput{SampleIDs: ids})
	require.NoError(t, err)
	assert.Greater(t, stats.TotalChars, 0)
	assert.Greater(t, stats.TotalWords, 0)
	assert.Greater(t, stats.SentenceCount, 0)
	assert.Greater(t, stats.ParagraphCount, 0)
	assert.Greater(t, stats.CommaDensity, float64(0))
	assert.Greater(t, stats.PeriodDensity, float64(0))
}

func TestComputeStyleStats_NoSamples(t *testing.T) {
	app := setupTestApp(t)

	stats, err := app.ComputeStyleStats(ComputeStyleStatsInput{SampleIDs: []int64{999999}})
	assert.Nil(t, stats)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "没有选中的素材")
	}
}

// ---------------------------------------------------------------------------
// CreateStyleProfile
// ---------------------------------------------------------------------------

func TestCreateStyleProfile(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	// 缺书名（novel_id=0）应报错。
	_, err := app.CreateStyleProfile(CreateStyleProfileInput{
		NovelID: 0,
		Name:    "X",
		Content: "C",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "请先打开一本书")

	// 缺名称或内容应报错。
	_, err = app.CreateStyleProfile(CreateStyleProfileInput{
		NovelID: novel.ID,
		Name:    "",
		Content: "C",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少名称或内容")

	// 正常创建。
	profile, err := app.CreateStyleProfile(CreateStyleProfileInput{
		NovelID:     novel.ID,
		Name:        "古风雅致",
		Description: "适合历史正剧",
		Content:     "# 古风雅致\n## 句式特征\n...",
	})
	require.NoError(t, err)
	require.NotZero(t, profile.ID)
	assert.Equal(t, novel.ID, profile.NovelID)
	assert.False(t, profile.IsGlobal)
	assert.Equal(t, "古风雅致", profile.Name)

	// 可被 ListStyleProfiles 取回。
	list, err := app.ListStyleProfiles(ListStyleProfilesInput{NovelID: novel.ID})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, profile.ID, list[0].ID)
}

// ---------------------------------------------------------------------------
// DeleteStyleProfile（级联移除激活偏好）
// ---------------------------------------------------------------------------

func TestDeleteStyleProfile_Cascade(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)

	profile, err := app.CreateStyleProfile(CreateStyleProfileInput{
		NovelID: novel.ID,
		Name:    "古风雅致",
		Content: "# 古风雅致\n...",
	})
	require.NoError(t, err)

	// 由该画像派生的激活偏好（content 首行带 marker）。
	marker := fmt.Sprintf("%s%d】", style.StyleMarkerPrefix, profile.ID)
	active := &preference.PreferenceItem{
		NovelID:  novel.ID,
		IsGlobal: false,
		Category: style.StyleConstraintCategory,
		Content:  marker + "\n\n" + profile.Content,
	}
	require.NoError(t, app.db.Create(active).Error)

	// 用户自定义的同分类偏好（无 marker），不应被级联删除。
	custom := &preference.PreferenceItem{
		NovelID:  novel.ID,
		IsGlobal: false,
		Category: style.StyleConstraintCategory,
		Content:  "我自己手写的一段写作风格偏好，没有 marker。",
	}
	require.NoError(t, app.db.Create(custom).Error)

	// 删除画像 → 级联清理激活偏好，但自定义偏好保留。
	require.NoError(t, app.DeleteStyleProfile(DeleteStyleProfileInput{ID: profile.ID}))

	// 画像已不存在。
	remaining, err := app.ListStyleProfiles(ListStyleProfilesInput{NovelID: novel.ID})
	require.NoError(t, err)
	assert.Empty(t, remaining)

	// 激活偏好被删除。
	var cnt int64
	require.NoError(t, app.db.Model(&preference.PreferenceItem{}).
		Where("id = ?", active.ID).Count(&cnt).Error)
	assert.Equal(t, int64(0), cnt)

	// 自定义偏好仍在。
	require.NoError(t, app.db.Model(&preference.PreferenceItem{}).
		Where("id = ?", custom.ID).Count(&cnt).Error)
	assert.Equal(t, int64(1), cnt)
}
