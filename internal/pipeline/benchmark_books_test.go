package pipeline

// P2-3 对标书库：服务层测试——粘贴解析预览/落库（同书名最新粘贴胜）、无可入库行
// 拒写、列表/单删/全清、「对标书库摘要」块渲染与体检提示词注入（genre 之后、空块零 diff）。

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/quality"
)

// newBenchmarkBooksTest 装配对标书库测试：迁移样本表并构造 Service。
func newBenchmarkBooksTest(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&quality.BenchmarkBook{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	return svc, db
}

func TestPreviewBenchmarkBooksParseAndSkip(t *testing.T) {
	svc, _ := newBenchmarkBooksTest(t)
	ctx := context.Background()

	got, err := svc.PreviewBenchmarkBooks(ctx, 911,
		"书名｜钩子类型｜爽点类型｜节奏形态\n全球高武｜重生回归｜打脸逆袭｜快节奏\n某书｜｜｜")
	require.NoError(t, err)
	require.Len(t, got.Rows, 1)
	assert.Equal(t, "全球高武", got.Rows[0].Title)
	assert.Equal(t, "打脸逆袭", got.Rows[0].PleasureType)
	assert.Len(t, got.SkippedLines, 2, "表头 + 缺特征行")

	_, err = svc.PreviewBenchmarkBooks(ctx, 0, "甲书|钩")
	assert.Error(t, err)
	_, err = svc.PreviewBenchmarkBooks(ctx, 911, "")
	assert.Error(t, err, "空文本拒解析")
}

func TestSaveBenchmarkBooksTextUpsertAndReport(t *testing.T) {
	svc, _ := newBenchmarkBooksTest(t)
	ctx := context.Background()
	const novelID int64 = 912

	res, err := svc.SaveBenchmarkBooksText(ctx, novelID,
		"甲书｜钩A｜爽A｜节A\n乙书｜钩B\n垃圾行\n某书｜｜｜")
	require.NoError(t, err)
	assert.Len(t, res.Saved, 2)
	assert.Len(t, res.SkippedLines, 2)

	// 同书名重贴覆盖（最新胜）。
	res, err = svc.SaveBenchmarkBooksText(ctx, novelID, "甲书｜钩A2｜爽A2｜节A2")
	require.NoError(t, err)
	require.Len(t, res.Saved, 2, "回的是全书当前行集")
	byTitle := map[string]BenchmarkBookView{}
	for _, v := range res.Saved {
		byTitle[v.Title] = v
	}
	assert.Equal(t, "钩A2", byTitle["甲书"].HookType)
	assert.Equal(t, "钩B", byTitle["乙书"].HookType)

	// 列表与单删、全清。
	list, err := svc.ListBenchmarkBooksView(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "甲书", list[0].Title, "按录入顺序升序")

	require.NoError(t, svc.DeleteBenchmarkBook(ctx, novelID, list[0].ID))
	list, err = svc.ListBenchmarkBooksView(ctx, novelID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	require.NoError(t, svc.DeleteBenchmarkBooks(ctx, novelID))
	list, err = svc.ListBenchmarkBooksView(ctx, novelID)
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestSaveBenchmarkBooksTextRejectsEmpty(t *testing.T) {
	svc, _ := newBenchmarkBooksTest(t)
	ctx := context.Background()
	const novelID int64 = 913

	_, err := svc.SaveBenchmarkBooksText(ctx, novelID, "书名｜钩子\n某书｜｜｜")
	assert.Error(t, err, "无可入库行拒写")
	_, err = svc.SaveBenchmarkBooksText(ctx, 0, "甲书|钩")
	assert.Error(t, err)
	err = svc.DeleteBenchmarkBook(ctx, novelID, 0)
	assert.Error(t, err)
}

func TestBenchmarkBooksBlock(t *testing.T) {
	svc, db := newBenchmarkBooksTest(t)
	ctx := context.Background()
	const novelID int64 = 914

	assert.Empty(t, svc.benchmarkBooksBlock(ctx, novelID), "无样本=空串（零提示）")

	require.NoError(t, db.Create(&quality.BenchmarkBook{
		NovelID: novelID, Title: "全球高武", HookType: "重生回归", PleasureType: "打脸逆袭", RhythmShape: "快节奏",
	}).Error)
	block := svc.benchmarkBooksBlock(ctx, novelID)
	assert.Contains(t, block, "【对标书库摘要】")
	assert.Contains(t, block, "《全球高武》")
	assert.Contains(t, block, "共 1 本")
}

// TestSignabilityPromptBenchmarkBlock dim2 摘要块注入：genre 之后渲染、空块零 diff。
func TestSignabilityPromptBenchmarkBlock(t *testing.T) {
	base := signabilityPrompt(1, auditInput{}, contextasm.ContextBlocks{})
	empty := signabilityPrompt(1, auditInput{benchmark: ""}, contextasm.ContextBlocks{})
	require.Equal(t, base, empty, "空块=提示词零 diff")

	withB := signabilityPrompt(1, auditInput{benchmark: "【对标书库摘要】x"}, contextasm.ContextBlocks{})
	assert.Contains(t, withB, "【对标书库摘要】x")

	both := signabilityPrompt(1, auditInput{
		genre:     "【本书题材卡约束】y",
		benchmark: "【对标书库摘要】x",
	}, contextasm.ContextBlocks{})
	assert.Greater(t, strings.Index(both, "【对标书库摘要】x"), strings.Index(both, "【本书题材卡约束】y"),
		"benchmark 块在 genre 之后渲染")
}
