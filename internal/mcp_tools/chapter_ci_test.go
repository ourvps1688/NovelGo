//go:build cgo

package mcp_tools_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/structure"
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
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}, &novel.Novel{}, &voice.Entry{}, &quality.ChapterQuality{}, &quality.ChapterTension{}))
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

// TestScheduleChapterCIWritesTension 写后钩子（P0-2 情绪账）：L0 基线张力曲线自动落库，
// 正文变化后旧 L1 行一并作废（宁缺勿错）。
func TestScheduleChapterCIWritesTension(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	ch := seedCIChapter(t, db, 5)
	qs := quality.NewStore(db, ciLogger())

	// 预置一条 L1 行（模拟历史回执），写后应被作废。
	require.NoError(t, qs.ReplaceTensionCurve(ctx, 1, 5, 0, quality.TensionSourceL1,
		[]quality.TensionPoint{{Seg: 0, Tension: 90, Emotion: 80, BeatType: "custom"}}))

	mcp_tools.ScheduleChapterCI(ctx, tc, ch, "「你来了。」他看着她。「我等你很久了。」\n就在这时，门被人从外面推开了。")

	rows, err := qs.ListTensionCurve(ctx, 1, 5)
	require.NoError(t, err)
	require.NotEmpty(t, rows, "写后应落 L0 基线曲线")
	for _, r := range rows {
		assert.Equal(t, quality.TensionSourceL0, r.Source, "旧 L1 行应随正文变化作废")
		assert.GreaterOrEqual(t, r.Tension, 0.0)
		assert.LessOrEqual(t, r.Tension, 100.0)
		assert.NotEmpty(t, r.BeatType)
	}

	// nil 入参跳过不 panic；内容不变重放幂等（行数稳定）。
	mcp_tools.ScheduleChapterCI(ctx, tc, nil, "x")
	rows2, err := qs.ListTensionCurve(ctx, 1, 5)
	require.NoError(t, err)
	assert.Len(t, rows2, len(rows))
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
		assert.Contains(t, req.Instruction, "情绪曲线标注", "指令含情绪曲线标注段")
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

// TestRunChapterCILandsTensionCurve 情绪账 L1 落库（P0-2）：回执 tension_curve 经清洗
// （脏 seg 丢弃/分值截断/同 seg 后写胜）落 source=l1 行，段区间从 L0 基线回填；
// 空 tension_curve 的回执不动既有 L1 行。
func TestRunChapterCILandsTensionCurve(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	ch := seedCIChapter(t, db, 5)
	qs := quality.NewStore(db, ciLogger())

	// 写后链路先落多段 L0 基线（供段号对照与区间回填）。
	mcp_tools.ScheduleChapterCI(ctx, tc, ch, strings.Repeat("他推开门走进院子，惊觉院中有人，两人对峙良久。\n", 80))

	runSub := func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
		assert.Contains(t, req.Instruction, "段号对照", "L0 基线在时指令带段号对照")
		assert.Contains(t, req.Instruction, "seg 0（字", "段号对照含 seg 区间")
		return "小结\n```json\n{\"findings\":[],\"tension_curve\":[" +
			"{\"seg\":-1,\"tension\":90,\"emotion\":90,\"beat_type\":\"climax\"}," +
			"{\"seg\":1,\"tension\":150,\"emotion\":-20,\"beat_type\":\"conflict\"}," +
			"{\"seg\":1,\"tension\":60,\"emotion\":50,\"beat_type\":\"climax\"}," +
			"{\"seg\":0,\"tension\":62.5,\"emotion\":48,\"beat_type\":\"hook\"}" +
			"],\"summary\":\"标注 2 段\"}\n```\n", nil
	}
	err := mcp_tools.RunChapterCIForChapter(ctx, runSub, nil, db, ciLogger(), "tool-1", 1, 5)
	require.NoError(t, err)

	rows, err := qs.ListTensionCurve(ctx, 1, 5)
	require.NoError(t, err)
	l0Ranges := map[int][2]int{}
	var l1 []quality.ChapterTension
	for _, r := range rows {
		switch r.Source {
		case quality.TensionSourceL0:
			l0Ranges[r.Seg] = [2]int{r.StartRune, r.EndRune}
		case quality.TensionSourceL1:
			l1 = append(l1, r)
		}
	}
	require.GreaterOrEqual(t, len(l0Ranges), 2, "L0 基线应有多段")
	require.Len(t, l1, 2, "清洗后仅 seg 0/1 两行（脏 seg 丢弃、同 seg 去重）")
	assert.Equal(t, 0, l1[0].Seg)
	assert.Equal(t, 62.5, l1[0].Tension)
	assert.Equal(t, "hook", l1[0].BeatType)
	assert.Equal(t, 1, l1[1].Seg)
	assert.Equal(t, 60.0, l1[1].Tension, "同 seg 后写覆盖先写")
	assert.Equal(t, 50.0, l1[1].Emotion)
	assert.Equal(t, "climax", l1[1].BeatType)
	for _, r := range l1 {
		rg := l0Ranges[r.Seg]
		assert.Equal(t, rg[0], r.StartRune, "段区间从 L0 基线回填")
		assert.Equal(t, rg[1], r.EndRune)
	}

	// 空 tension_curve 回执：不动既有 L1 行。
	runSub2 := func(_ context.Context, _ mcp_tools.SubAgentRequest) (string, error) {
		return "```json\n{\"findings\":[],\"summary\":\"未发现增量问题\"}\n```\n", nil
	}
	err = mcp_tools.RunChapterCIForChapter(ctx, runSub2, nil, db, ciLogger(), "tool-1", 1, 5)
	require.NoError(t, err)
	rows2, err := qs.ListTensionCurve(ctx, 1, 5)
	require.NoError(t, err)
	assert.Len(t, rows2, len(rows), "空曲线不动既有 L1 行")
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

// TestScheduleChapterCIUsesCheckerConfig 题材卡机制化（R2b-3）：L0 消费本书检查器配置——
// 章尾钩子显式关（跳过缺钩发现）+ 禁忌清单命中；未配置=缺省=现行行为。
func TestScheduleChapterCIUsesCheckerConfig(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	require.NoError(t, db.AutoMigrate(&quality.CheckerConfig{}))
	f := false
	require.NoError(t, db.Create(&quality.CheckerConfig{
		NovelID: 1, TailHookRequired: &f, TabooList: `["虐主"]`,
	}).Error)
	ch := seedCIChapter(t, db, 6)

	mcp_tools.ScheduleChapterCI(ctx, tc, ch, "这一路虐主的展开让读者压抑。")

	got, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 6)
	require.NoError(t, err)
	require.NotNil(t, got)
	checks := map[string]bool{}
	for _, f := range quality.ParseFindings(got.L0Findings) {
		checks[f.Check] = true
	}
	assert.True(t, checks[quality.CheckTaboo], "禁忌命中应落发现")
	assert.False(t, checks[quality.CheckTailHook], "tail_hook_required=false 应跳过缺钩发现")
}

// TestScheduleChapterCIUsesCharacterLedger 角色台账检查（R2b-5）：称呼混用/死人出镜
// 进 L0 发现落档案；未标注生死的出镜不误报。
func TestScheduleChapterCIUsesCharacterLedger(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	require.NoError(t, db.AutoMigrate(&character.Character{}))
	dead := false
	alive := true
	require.NoError(t, db.Create(&character.Character{NovelID: 1, Name: "赵铁衣", Aliases: `["铁衣"]`, Alive: &dead}).Error)
	require.NoError(t, db.Create(&character.Character{NovelID: 1, Name: "林澈", Aliases: `["阿澈"]`, Alive: &alive}).Error)
	ch := seedCIChapter(t, db, 7)

	mcp_tools.ScheduleChapterCI(ctx, tc, ch, "铁衣的传说仍在。阿澈与林澈并肩站在城头。")

	got, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	checks := map[string]bool{}
	for _, f := range quality.ParseFindings(got.L0Findings) {
		checks[f.Check] = true
	}
	assert.True(t, checks[quality.CheckNameDrift], "林澈/阿澈 混用应落发现")
	assert.True(t, checks[quality.CheckDeadAppearance], "已死的铁衣出镜应落发现")
}

// TestScheduleChapterCIUsesPowerLevels 战力/境界数值序（R2b-6）：越级取胜未见代价
// 进 L0 发现落档案；无等级表=零发现（缺省=现行行为）。
func TestScheduleChapterCIUsesPowerLevels(t *testing.T) {
	db, tc, ctx := setupCIEnv(t)
	require.NoError(t, db.AutoMigrate(&structure.PowerLevel{}))
	require.NoError(t, structure.NewStore(db).UpsertPowerLevel(ctx, &structure.PowerLevel{NovelID: 1, Name: "金丹期", Rank: 3}))
	require.NoError(t, structure.NewStore(db).UpsertPowerLevel(ctx, &structure.PowerLevel{NovelID: 1, Name: "元婴期", Rank: 5}))
	ch := seedCIChapter(t, db, 8)

	mcp_tools.ScheduleChapterCI(ctx, tc, ch, "金丹期的他击败了元婴期的强敌。")

	got, err := quality.NewStore(db, ciLogger()).GetByChapter(ctx, 1, 8)
	require.NoError(t, err)
	require.NotNil(t, got)
	checks := map[string]bool{}
	for _, f := range quality.ParseFindings(got.L0Findings) {
		checks[f.Check] = true
	}
	assert.True(t, checks[quality.CheckPowerUpset], "越级取胜未见代价应落发现")
}
