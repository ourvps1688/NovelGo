package mcp_tools

// plan_check_test.go —— 写前对账 L1.5（R2b-4）接线闭环：
// 立卡/更新计划后置检查出告警随回执返回（只报告不拦截）、零告警不加 warnings 键、
// 取数失败静默降级（零提示零报错）。

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
)

func openPlanCheckToolDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &timeline.ChapterTaskCard{}, &timeline.ChapterPlan{},
		&timeline.TimelineEntry{}, &structure.PinnedInvariant{}, &character.Character{}, &quality.CheckerConfig{}))
	return db
}

func planCheckTc(db *gorm.DB) ToolContext {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ToolContext{DB: db, NovelID: 1, Logger: logger}
}

// seedPlanCheckFixtures 植入对账底料：坑表 1 条伏笔 + 禁忌清单 + 不变量/角色。
func seedPlanCheckFixtures(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&timeline.TimelineEntry{
		Category: "foreshadowing", Status: "pending", Title: "黑衣人身份",
		NovelID: 1, TargetReadingNumber: 3,
	}).Error)
	require.NoError(t, db.Create(&quality.CheckerConfig{
		NovelID: 1, TabooList: `["虐主"]`,
	}).Error)
	dead := true
	require.NoError(t, db.Create(&character.Character{NovelID: 1, Name: "林澈", Alive: &dead}).Error)
	require.NoError(t, db.Create(&structure.PinnedInvariant{
		NovelID: 1, Kind: structure.KindAliveBefore, Statement: "林澈第 50 章前不死",
		ParamsJSON: `{"character":"林澈","before_reading_number":50}`,
	}).Error)
}

// TestTaskCardUpsertPlanCheckWarnings 立卡后置对账：坑不在表/收章超区间/禁忌命中/死亡违例
// 随回执告警返回；立卡照常成功（只报告不拦截）。
func TestTaskCardUpsertPlanCheckWarnings(t *testing.T) {
	db := openPlanCheckToolDB(t)
	tc := planCheckTc(db)
	seedPlanCheckFixtures(t, db)

	res, err := (&UpsertChapterTaskCardTool{}).Execute(context.Background(), &UpsertChapterTaskCardArgs{
		TargetReadingNumber: 5,
		HookTask:            "本章回收「黑衣人身份」，另起「母亲死因」线索，让林澈战死，全程虐主。",
		BeatTask:            "反杀爽点",
		MomentTask:          "主角爆发",
		AdvanceTask:         "离开青云镇",
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success, "只报告不拦截：立卡照常成功")
	warnings, _ := res.Data["warnings"].(string)
	require.NotEmpty(t, warnings)
	require.Contains(t, warnings, "【写前对账告警】")
	require.Contains(t, warnings, "plan_pit_missing", "「母亲死因」不在坑表")
	require.Contains(t, warnings, "plan_pit_window", "第 5 章收预期第 3 章的坑超区间")
	require.Contains(t, warnings, "plan_taboo", "命中禁忌「虐主」")
	require.Contains(t, warnings, "plan_alive_violation", "窗口内计划林澈死亡")
	require.Contains(t, warnings, "S1[", "S 系顺编进告警文本")
}

// TestTaskCardUpsertCleanNoWarnings 零告警不加 warnings 键（回执保持精简）。
func TestTaskCardUpsertCleanNoWarnings(t *testing.T) {
	db := openPlanCheckToolDB(t)
	tc := planCheckTc(db)
	seedPlanCheckFixtures(t, db)

	res, err := (&UpsertChapterTaskCardTool{}).Execute(context.Background(), &UpsertChapterTaskCardArgs{
		TargetReadingNumber: 2,
		HookTask:            "埋下「黑衣人身份」伏笔，章尾断钩。",
		BeatTask:            "打脸爽点",
		MomentTask:          "隐忍爆发",
		AdvanceTask:         "离开青云镇",
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.NotContains(t, res.Data, "warnings", "零告警不加键")
}

// TestChapterPlanUpdatePlanCheckWarnings 更新计划后置对账：next 对账坑表/死亡，
// near/far 仅禁忌；命中告警随回执返回。
func TestChapterPlanUpdatePlanCheckWarnings(t *testing.T) {
	db := openPlanCheckToolDB(t)
	tc := planCheckTc(db)
	seedPlanCheckFixtures(t, db)
	up := &UpdateChapterPlanTool{}

	// next：坑不在表 + 禁忌命中 + 窗口内死亡。
	res, err := up.Execute(context.Background(), &UpdateChapterPlanArgs{
		Scope:   "next",
		Content: "本章回收「母亲死因」，让林澈战死，基调虐主。",
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success, "只报告不拦截")
	warnings, _ := res.Data["warnings"].(string)
	require.Contains(t, warnings, "plan_pit_missing")
	require.Contains(t, warnings, "plan_taboo")
	require.Contains(t, warnings, "plan_alive_violation")

	// near：无章号锚点——只扫禁忌（坑表/死亡不判）。
	res, err = up.Execute(context.Background(), &UpdateChapterPlanArgs{
		Scope:   "near",
		Content: "近期回收「母亲死因」，让林澈战死，基调虐主。",
	}, tc)
	require.NoError(t, err)
	warnings, _ = res.Data["warnings"].(string)
	require.Contains(t, warnings, "plan_taboo")
	require.NotContains(t, warnings, "plan_pit_missing", "near 无锚点不做坑对账")
	require.NotContains(t, warnings, "plan_alive_violation", "near 无锚点不做死亡对账")

	// 干净计划：零告警不加键。
	res, err = up.Execute(context.Background(), &UpdateChapterPlanArgs{
		Scope:   "far",
		Content: "远期主线推进。",
	}, tc)
	require.NoError(t, err)
	require.NotContains(t, res.Data, "warnings")
}

// TestPlanCheckDegradesSilent 取数失败（坑表缺表）→ 静默降级零提示：立卡照常成功、
// 无 warnings 键、零报错。
func TestPlanCheckDegradesSilent(t *testing.T) {
	db := openPlanCheckToolDB(t)
	tc := planCheckTc(db)
	// 不植入任何底料且删掉 time_entries 表：坑表取数失败。
	require.NoError(t, db.Migrator().DropTable(&timeline.TimelineEntry{}))

	res, err := (&UpsertChapterTaskCardTool{}).Execute(context.Background(), &UpsertChapterTaskCardArgs{
		TargetReadingNumber: 3,
		HookTask:            "本章回收「不存在的坑」。",
		BeatTask:            "爽点",
		MomentTask:          "时刻",
		AdvanceTask:         "推进",
	}, tc)
	require.NoError(t, err)
	require.True(t, res.Success)
	require.NotContains(t, res.Data, "warnings", "坑表失败跳过该项，其余项无告警=零提示")
	require.NotContains(t, res.Data, "error", "降级零报错")
}
