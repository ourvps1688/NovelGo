//go:build cgo

package mcp_tools_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
)

// ── 测试环境（章节 CI，R2）──────────────────────────────────

func ciLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// setupCIEnv 准备章节 CI 测试 DB（sqlite 内存同款装配：章节/小说/voice/质量档案）。
func setupCIEnv(t *testing.T) (*gorm.DB, mcp_tools.ToolContext, context.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ci.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}, &novel.Novel{}, &voice.Entry{}, &quality.ChapterQuality{}))
	tc := mcp_tools.ToolContext{DB: db, NovelID: 1}
	return db, tc, context.Background()
}

func seedCIChapter(t *testing.T, db *gorm.DB, id int64) *chapter.Chapter {
	t.Helper()
	ch := &chapter.Chapter{ID: id, NovelID: 1, Title: "测试章"}
	require.NoError(t, db.Create(ch).Error)
	return ch
}

// TestScheduleChapterCIWritesL0 写后钩子：L0 同步落档案，L1 侧待查（无子 Agent 能力不派发）。
func TestScheduleChapterCIWritesL0(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	ch := seedCIChapter(t, db, 5)

	// RunSubAgent 为 nil：只跑 L0，不排 L1 定时器（降级零提示）。
	mcp_tools.ScheduleChapterCI(ctx, tc, ch, "他推开门走进院子。")

	got, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 5)
	require.NoError(t, err)
	require.NotNil(t, got, "L0 应同步落档案")
	assert.NotEmpty(t, got.L0Metrics, "L0 指标全集落档")
	assert.NotEmpty(t, got.L0Findings, "短文应有 C 系发现（字数偏短/章尾无钩）")
	assert.Equal(t, quality.L1StatusPending, got.L1Status, "L1 未跑=待查（规范化 pending）")
	assert.Empty(t, got.L1Findings)
	assert.False(t, got.CheckedAt.IsZero())

	// ch 为 nil / DB 为 nil：整体跳过（不 panic 不写）。
	mcp_tools.ScheduleChapterCI(ctx, tc, nil, "x")
	mcp_tools.ScheduleChapterCI(ctx, mcp_tools.ToolContext{}, ch, "x")
	got2, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 5)
	require.NoError(t, err)
	require.NotNil(t, got2)
	assert.Equal(t, got.L0Findings, got2.L0Findings, "nil 输入不得改档")
}

// TestRunChapterCIReceiptLanded L1 回执落档案：checked + 发现投影 + 不动 L0 列。
func TestRunChapterCIReceiptLanded(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	ch := seedCIChapter(t, db, 5)
	mcp_tools.ScheduleChapterCI(ctx, tc, ch, "他推开门走进院子。") // 先落 L0

	runSub := func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
		assert.Equal(t, "chapterci", req.AgentType, "路由 ChapterCIAgent（agentTypeFromString 映射）")
		assert.Equal(t, int64(1), req.NovelID)
		assert.Contains(t, req.Instruction, quality.L1ReceiptFormatLine(), "指令内联回执契约格式串")
		assert.Contains(t, req.Instruction, "continuity", "指令含检查项枚举")
		return "小结\n```json\n{\"findings\":[{\"id\":\"L1-1\",\"check\":\"continuity\",\"desc\":\"与前章矛盾\",\"chapter_ids\":[4,5]}],\"summary\":\"1 项\"}\n```\n", nil
	}
	err := mcp_tools.RunChapterCIForChapter(ctx, runSub, nil, db, ciLogger(), "tool-1", 1, 5)
	require.NoError(t, err)

	got, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 5)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, quality.L1StatusChecked, got.L1Status)
	findings := quality.ParseFindings(got.L1Findings)
	require.Len(t, findings, 1)
	assert.Equal(t, "L1-1", findings[0].ID)
	assert.Equal(t, quality.L1CheckContinuity, findings[0].Check)
	assert.Equal(t, []int64{4, 5}, findings[0].ChapterIDs)
	assert.Equal(t, quality.FindingStatusPending, findings[0].Status, "只报告不改写，落档即待修")
	assert.NotEmpty(t, got.L0Metrics, "L1 落档不动 L0 列（分阶段互不覆盖）")
}

// TestRunChapterCIDegradesToSkipped 降级逐字一致：回执坏/派发失败 → skipped，零提示零报错。
func TestRunChapterCIDegradesToSkipped(t *testing.T) {
	db, _, ctx := setupCIEnv(t)
	seedCIChapter(t, db, 5)
	store := quality.NewStore(db, ciLogger())

	// 回执坏：返回 error（调用方可忽略），档案记 skipped。
	bad := func(context.Context, mcp_tools.SubAgentRequest) (string, error) { return "没有回执块", nil }
	err := mcp_tools.RunChapterCIForChapter(ctx, bad, nil, db, ciLogger(), "tool-1", 1, 5)
	require.Error(t, err)
	got, gerr := store.GetByChapter(ctx, 1, 5)
	require.NoError(t, gerr)
	require.NotNil(t, got)
	assert.Equal(t, quality.L1StatusSkipped, got.L1Status)
	assert.Empty(t, got.L1Findings)

	// 派发失败：同样 skipped。
	boom := func(context.Context, mcp_tools.SubAgentRequest) (string, error) { return "", context.DeadlineExceeded }
	err = mcp_tools.RunChapterCIForChapter(ctx, boom, nil, db, ciLogger(), "tool-1", 1, 5)
	require.Error(t, err)
	got, gerr = store.GetByChapter(ctx, 1, 5)
	require.NoError(t, gerr)
	require.NotNil(t, got)
	assert.Equal(t, quality.L1StatusSkipped, got.L1Status)

	// runSub 为 nil：不派发不落档（静默）。
	require.NoError(t, mcp_tools.RunChapterCIForChapter(ctx, nil, nil, db, ciLogger(), "tool-1", 1, 999))
	gotNil, gerr := store.GetByChapter(ctx, 1, 999)
	require.NoError(t, gerr)
	assert.Nil(t, gotNil, "未派发不得造档案行")
}

// TestRunChapterCIEmptyFindingsChecked 零发现回执=已查零发现（"[]" 与未查语义区分）。
func TestRunChapterCIEmptyFindingsChecked(t *testing.T) {
	db, _, ctx := setupCIEnv(t)
	seedCIChapter(t, db, 6)
	clean := func(context.Context, mcp_tools.SubAgentRequest) (string, error) {
		return "```json\n{\"findings\":[],\"summary\":\"未发现增量问题\"}\n```", nil
	}
	require.NoError(t, mcp_tools.RunChapterCIForChapter(ctx, clean, nil, db, ciLogger(), "tool-1", 1, 6))

	got, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 6)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, quality.L1StatusChecked, got.L1Status)
	assert.Equal(t, "[]", got.L1Findings, "零发现=已查空清单（区别于未查）")
}
