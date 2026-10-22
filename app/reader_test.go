package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetReaderPerspectives_Empty(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	perspectives, err := app.GetReaderPerspectives(novelID)
	require.NoError(t, err)
	assert.Empty(t, perspectives)
}

func TestCreateReaderPerspective(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID
	chapter := createTestChapter(t, app, novelID)

	p, err := app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:             "known",
		Content:          "The protagonist is an orphan",
		PlantedChapterID: chapter.ID,
		RelatedTruth:     "Parents are alive and in hiding",
	})
	require.NoError(t, err)
	assert.Equal(t, "known", p.Type)
	assert.Equal(t, "The protagonist is an orphan", p.Content)
	require.NotNil(t, p.PlantedChapterID)
	assert.Equal(t, chapter.ID, *p.PlantedChapterID)
	assert.Equal(t, "Parents are alive and in hiding", p.RelatedTruth)
	assert.Equal(t, novelID, p.NovelID)
}

func TestCreateReaderPerspective_EmptyFields(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	_, err := app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:    "",
		Content: "Some content",
	})
	assert.Error(t, err)

	_, err = app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:    "suspense",
		Content: "",
	})
	assert.Error(t, err)
}

func TestGetReaderPerspectives_AfterCreate(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID
	chapter1 := createTestChapter(t, app, novelID)
	chapter2 := createTestChapter(t, app, novelID)

	_, err := app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:             "known",
		Content:          "Reader knows the hero's name",
		PlantedChapterID: chapter1.ID,
	})
	require.NoError(t, err)

	_, err = app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:             "suspense",
		Content:          "Who is the traitor?",
		PlantedChapterID: chapter2.ID,
	})
	require.NoError(t, err)

	perspectives, err := app.GetReaderPerspectives(novelID)
	require.NoError(t, err)
	assert.Len(t, perspectives, 2)
}

func TestUpdateReaderPerspective(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID
	planted := createTestChapter(t, app, novelID)
	revealed := createTestChapter(t, app, novelID)

	p, err := app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:             "suspense",
		Content:          "Who killed the king?",
		PlantedChapterID: planted.ID,
	})
	require.NoError(t, err)

	err = app.UpdateReaderPerspective(p.ID, novelID, UpdateReaderPerspectiveInput{
		Type:              "suspense", // PUT 全量语义：前端全量传，测试须补全 type
		PlantedChapterID:  int64Ptr(planted.ID),
		RevealedChapterID: int64Ptr(revealed.ID),
		Content:           "Who killed the king? (resolved)",
	})
	require.NoError(t, err)

	perspectives, err := app.GetReaderPerspectives(novelID)
	require.NoError(t, err)
	require.Len(t, perspectives, 1)
	require.NotNil(t, perspectives[0].RevealedChapterID)
	assert.Equal(t, revealed.ID, *perspectives[0].RevealedChapterID)
	assert.Equal(t, "Who killed the king? (resolved)", perspectives[0].Content)
}

func TestDeleteReaderPerspective(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID
	chapter := createTestChapter(t, app, novelID)

	p, err := app.CreateReaderPerspective(novelID, CreateReaderPerspectiveInput{
		Type:             "misconception",
		Content:          "Reader thinks the mentor is good",
		PlantedChapterID: chapter.ID,
	})
	require.NoError(t, err)

	err = app.DeleteReaderPerspective(p.ID, novelID)
	require.NoError(t, err)

	perspectives, err := app.GetReaderPerspectives(novelID)
	require.NoError(t, err)
	assert.Empty(t, perspectives)
}

func TestCreateReaderPerspective_RejectsForeignChapter(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	otherNovel := createTestNovel(t, app)
	otherChapter := createTestChapter(t, app, otherNovel.ID)

	_, err := app.CreateReaderPerspective(novel.ID, CreateReaderPerspectiveInput{
		Type:             "known",
		Content:          "跨小说章节",
		PlantedChapterID: otherChapter.ID,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不属于当前小说")
}

// lifecycle 校验（WBS 2.7）：known 是单点生命周期（揭示事件），不得带回收章。
func TestCreateReaderPerspective_RejectsKnownWithRevealed(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	chapter := createTestChapter(t, app, novel.ID)

	_, err := app.CreateReaderPerspective(novel.ID, CreateReaderPerspectiveInput{
		Type:              "known",
		Content:           "读者知道真相",
		PlantedChapterID:  chapter.ID,
		RevealedChapterID: int64Ptr(chapter.ID),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "单点生命周期")
}

// lifecycle 校验（WBS 2.7）：回收章阅读序不得早于埋设章。
func TestUpdateReaderPerspective_RejectsPayoffBeforePlant(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	chapter1 := createTestChapter(t, app, novel.ID)
	chapter2 := createTestChapter(t, app, novel.ID)

	p, err := app.CreateReaderPerspective(novel.ID, CreateReaderPerspectiveInput{
		Type:             "suspense",
		Content:          "谁是幕后黑手",
		PlantedChapterID: chapter2.ID, // 埋在第二章
	})
	require.NoError(t, err)

	// 回收写到第一章（早于埋设）→ 拒绝。
	err = app.UpdateReaderPerspective(p.ID, novel.ID, UpdateReaderPerspectiveInput{
		Type:              "suspense",
		Content:           "谁是幕后黑手",
		PlantedChapterID:  int64Ptr(chapter2.ID),
		RevealedChapterID: int64Ptr(chapter1.ID),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "回收章不能早于埋设章")

	// 同章埋同章收（quick reveal 语义）→ 放行。
	err = app.UpdateReaderPerspective(p.ID, novel.ID, UpdateReaderPerspectiveInput{
		Type:              "suspense",
		Content:           "谁是幕后黑手",
		PlantedChapterID:  int64Ptr(chapter2.ID),
		RevealedChapterID: int64Ptr(chapter2.ID),
	})
	require.NoError(t, err)
}
