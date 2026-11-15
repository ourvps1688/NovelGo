//go:build cgo

package mcp_tools

// #129 契约：同一份落盘正文的写后 L0 重算只做一次；正文变化必须重算；
// 用户显式「重新检测本章」的强制重算路径不得被短路。

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
)

func l0IdempotentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newL0IdempotentDB(t *testing.T) (*gorm.DB, ToolContext, context.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "l0-idempotent.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(
		&chapter.Chapter{}, &volume.Volume{}, &novel.Novel{}, &voice.Entry{},
		&quality.ChapterQuality{}, &quality.ChapterTension{},
	))
	require.NoError(t, db.Create(&chapter.Chapter{ID: 5, NovelID: 1, Title: "测试章"}).Error)
	return db, ToolContext{DB: db, NovelID: 1, Logger: l0IdempotentLogger()}, context.Background()
}

func l0FindingsRaw(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var raw string
	require.NoError(t, db.Raw(
		"SELECT l0_findings FROM chapter_quality WHERE novel_id = ? AND chapter_id = ?", 1, 5,
	).Scan(&raw).Error)
	return raw
}

func TestChapterL0IdempotentSkipsSameContent(t *testing.T) {
	resetChapterL0Fingerprints()
	db, tc, ctx := newL0IdempotentDB(t)
	ch := &chapter.Chapter{ID: 5, NovelID: 1}
	contentA := "他推开门走进院子。风从背后过来，把门合上了一半。"
	contentB := "她把灯芯挑短了一截，屋里的影子跟着矮下去。桌上的账本还摊着，最后一行墨没干。"

	ScheduleChapterCI(ctx, tc, ch, contentA)
	first := l0FindingsRaw(t, db)
	require.NotEmpty(t, first, "首次写后 L0 必须落档")
	assert.True(t, chapterL0AlreadyComputed(db, 1, 5, contentA), "成功后应登记正文指纹")

	// 手工把档案改成哨兵值：若第二次仍执行 UpsertL0，哨兵会被覆盖。
	require.NoError(t, db.Model(&quality.ChapterQuality{}).
		Where("novel_id = ? AND chapter_id = ?", 1, 5).
		Update("l0_findings", "SENTINEL").Error)
	ScheduleChapterCI(ctx, tc, ch, contentA)
	assert.Equal(t, "SENTINEL", l0FindingsRaw(t, db),
		"同一份正文不得重复 UpsertL0（避免 finding_round 反复开轮 / resolved 被冲）")

	// 正文真的变了：必须完整重算，哨兵被覆盖。
	ScheduleChapterCI(ctx, tc, ch, contentB)
	got := l0FindingsRaw(t, db)
	assert.NotEqual(t, "SENTINEL", got, "正文变化必须重算 L0")
	assert.False(t, chapterL0AlreadyComputed(db, 1, 5, contentA), "正文变化后旧指纹不得再命中")
	assert.True(t, chapterL0AlreadyComputed(db, 1, 5, contentB))
}

func TestChapterL0IdempotentFingerprintSemantics(t *testing.T) {
	resetChapterL0Fingerprints()
	var db *gorm.DB // 纯函数语义用 nil 实例即可
	assert.False(t, chapterL0AlreadyComputed(db, 7, 2911, "甲"), "进程刚启动时缓存为空，首次必须重算")
	chapterL0MarkComputed(db, 7, 2911, "甲")
	assert.True(t, chapterL0AlreadyComputed(db, 7, 2911, "甲"))
	assert.False(t, chapterL0AlreadyComputed(db, 7, 2911, "乙"), "同章不同正文不得命中")
	assert.False(t, chapterL0AlreadyComputed(db, 8, 2911, "甲"), "不同小说不得互相命中")
	assert.False(t, chapterL0AlreadyComputed(db, 7, 2912, "甲"), "不同章节不得互相命中")
	// 失败路径不登记：仅调 Already 不应产生缓存，下次仍可自愈重跑。
	assert.False(t, chapterL0AlreadyComputed(db, 7, 2913, "丙"))
	chapterL0MarkComputed(db, 7, 2913, "丙")
	resetChapterL0Fingerprints()
	assert.False(t, chapterL0AlreadyComputed(db, 7, 2913, "丙"), "重启/重置后必须重新落档")
}

func TestChapterL0IdempotentCacheIsolatedPerDB(t *testing.T) {
	resetChapterL0Fingerprints()
	dbA, _, _ := newL0IdempotentDB(t)
	dbB, _, _ := newL0IdempotentDB(t)
	chapterL0MarkComputed(dbA, 1, 5, "同一份正文")
	assert.True(t, chapterL0AlreadyComputed(dbA, 1, 5, "同一份正文"))
	assert.False(t, chapterL0AlreadyComputed(dbB, 1, 5, "同一份正文"),
		"不同 DB 实例（不同小说库/测试用例）不得互相短路")
}

func TestChapterL0IdempotentManualRecheckNotShortCircuited(t *testing.T) {
	src, err := os.ReadFile("chapter_ci.go")
	require.NoError(t, err)
	code := string(src)
	assert.Equal(t, 1, strings.Count(code, "chapterL0AlreadyComputed("),
		"短路只允许出现在写后 runChapterL0 一处")
	assert.Contains(t, code, "if complete {")
	assert.Contains(t, code, "chapterL0MarkComputed(")

	// 手动重检 RecalcChapterL0FromDisk 必须永远按当前落盘正文强制重算。
	start := strings.Index(code, "func RecalcChapterL0FromDisk(")
	require.GreaterOrEqual(t, start, 0)
	rest := code[start:]
	next := strings.Index(rest, "\nfunc ")
	if next > 0 {
		rest = rest[:next]
	}
	assert.NotContains(t, rest, "chapterL0AlreadyComputed")
	assert.Contains(t, rest, "recalcChapterL0(ctx, tc, chapterID, content)")

	policy, err := os.ReadFile("chapter_l0_idempotent.go")
	require.NoError(t, err)
	for _, banned := range []string{
		"StartPipeline", "StartIssueFixTickets", "autoFix", "Upload", "http.Post", "time.AfterFunc",
	} {
		assert.NotContains(t, string(policy), banned, "幂等短路不得引入执行体/自动修复/上传/定时器")
	}
}
