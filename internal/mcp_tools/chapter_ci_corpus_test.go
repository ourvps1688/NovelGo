//go:build cgo

package mcp_tools

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/text"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/volume"
)

func ciCorpusLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newCICorpusDB 造三章 + 本章任务卡 + 一张相关台账的内存库（方案 C 取数面）。
func newCICorpusDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ci-corpus.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// ListAllByNovel 会 LEFT JOIN volumes，表缺失会整段降级（与 setupCIEnv 同款装配）。
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}, &timeline.ChapterTaskCard{}))
	for i, id := range []int64{1, 2, 3} {
		require.NoError(t, db.Create(&chapter.Chapter{
			ID: id, NovelID: 27, SortOrder: i, Title: []string{"前章", "本章", "次章"}[i],
		}).Error)
	}
	cid := int64(2)
	require.NoError(t, db.Create(&timeline.ChapterTaskCard{
		NovelID: 27, TargetReadingNumber: 2, ChapterID: &cid,
		HookTask: "埋下铜铃伏笔", BeatTask: "铃响时反击", MomentTask: "他不再退让", AdvanceTask: "推进到院门",
	}).Error)
	require.NoError(t, db.Exec(`CREATE TABLE reader_perspectives (id INTEGER PRIMARY KEY, novel_id INTEGER, known TEXT, created_at TEXT)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO reader_perspectives (id, novel_id, known, created_at) VALUES (1, 27, '读者已知铜铃来历', '2026-01-01')`).Error)
	return db
}

// withCIText 临时替换正文读取入口；测试不并行，defer 恢复避免串扰。
func withCIText(t *testing.T, bodies map[int64]string, fail map[int64]bool) {
	t.Helper()
	old := chapterCITextReader
	t.Cleanup(func() { chapterCITextReader = old })
	chapterCITextReader = func(_ context.Context, _, id int64) (string, error) {
		if fail[id] {
			return "", context.DeadlineExceeded
		}
		return bodies[id], nil
	}
}

func ciCorpusBodies() map[int64]string {
	return map[int64]string{
		1: "前章开头。" + strings.Repeat("前章铺陈", 200) + "前章结尾铜铃响起。",
		2: "本章正文开始。" + strings.Repeat("他推开门。", 300) + "本章章尾钩子：门外没有人。",
		3: "次章开头一声闷响。" + strings.Repeat("次章铺陈", 200),
	}
}

// TestBuildChapterCICorpusInline #117 P1：语料一次取齐，正文/邻居/任务卡/台账
// 全部内联，权威字数按落盘正文现算，且整块受总帽约束。
func TestBuildChapterCICorpusInline(t *testing.T) {
	db := newCICorpusDB(t)
	bodies := ciCorpusBodies()
	withCIText(t, bodies, nil)

	corpus := buildChapterCICorpus(context.Background(), db, ciCorpusLogger(), 27, 2)
	require.NotEmpty(t, corpus)
	require.Contains(t, corpus, "【本章质检语料")
	require.Contains(t, corpus, "本章章尾钩子：门外没有人。")
	require.Contains(t, corpus, "前章结尾铜铃响起。", "前章尾片段须内联")
	require.Contains(t, corpus, "次章开头一声闷响。", "次章头片段须内联")
	require.Contains(t, corpus, "不计入本章字数", "邻居/台账必须明确排除在字数口径外")
	require.Contains(t, corpus, "埋下铜铃伏笔", "本章任务卡须内联")
	require.Contains(t, corpus, "#### reader_perspectives", "相关台账须内联")
	require.Contains(t, corpus, "读者已知铜铃来历")
	require.NotContains(t, corpus, "created_at=", "时间戳噪声列须跳过")

	wantWords := text.ComputeStats(bodies[2]).WordCount
	require.Contains(t, corpus, "权威字数 "+strconv.Itoa(wantWords)+" 字（word_count）")
	require.LessOrEqual(t, len([]rune(corpus)), chapterCICorpusCap+32,
		"语料整块须受 12000 rune 总帽约束")
}

// TestBuildChapterCICorpusBudgetAndDegrade #117 P1：超长正文掐头尾保章尾钩子；
// 证据缺失时整体降级为空串（调用方回落旧自取指令，零阻断）。
func TestBuildChapterCICorpusBudgetAndDegrade(t *testing.T) {
	db := newCICorpusDB(t)
	long := strings.Repeat("长", 20000) + "结尾钩子仍在。"
	bodies := ciCorpusBodies()
	bodies[2] = long
	withCIText(t, bodies, nil)

	corpus := buildChapterCICorpus(context.Background(), db, ciCorpusLogger(), 27, 2)
	require.Contains(t, corpus, "中段已截断")
	require.Contains(t, corpus, "结尾钩子仍在。", "掐头尾须保住章尾钩子")
	require.LessOrEqual(t, len([]rune(corpus)), chapterCICorpusCap+32)

	// 三级降级：db 未注入 / 本章正文不可读 / 正文为空。
	require.Empty(t, buildChapterCICorpus(context.Background(), nil, ciCorpusLogger(), 27, 2))
	withCIText(t, bodies, map[int64]bool{2: true})
	require.Empty(t, buildChapterCICorpus(context.Background(), db, ciCorpusLogger(), 27, 2))
	withCIText(t, map[int64]string{2: "  \n "}, nil)
	require.Empty(t, buildChapterCICorpus(context.Background(), db, ciCorpusLogger(), 27, 2))

	// 总帽截断本身按 rune 计，不会切半个中文字符。
	got := chapterCICapRunes(strings.Repeat("汉", 100), 40)
	require.Equal(t, 40, len([]rune(strings.Split(got, "\n")[0])))
	require.Contains(t, got, "已按预算截断")
}
