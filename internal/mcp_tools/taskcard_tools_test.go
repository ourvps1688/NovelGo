package mcp_tools

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/timeline"
)

func openTaskCardToolDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&chapter.Chapter{}, &timeline.ChapterTaskCard{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func taskCardTc(db *gorm.DB) ToolContext {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return ToolContext{DB: db, NovelID: 1, Logger: logger}
}

// TestUpsertChapterTaskCard_ClosedLoop 立卡→查卡工具闭环：落库、可查、四件套渲染进上下文。
func TestUpsertChapterTaskCard_ClosedLoop(t *testing.T) {
	db := openTaskCardToolDB(t)
	tc := taskCardTc(db)
	ctx := context.Background()

	up := &UpsertChapterTaskCardTool{}
	res, err := up.Execute(ctx, &UpsertChapterTaskCardArgs{
		TargetReadingNumber: 4,
		HookTask:            "埋黑玉佩伏笔，章尾断章钩：追兵火把出现在山口",
		BeatTask:            "反杀欺压者的打脸爽点",
		MomentTask:          "主角隐忍爆发",
		AdvanceTask:         "主线推进：离开青云镇",
		Source:              "ai",
	}, tc)
	if err != nil || !res.Success {
		t.Fatalf("upsert = %+v, err = %v", res, err)
	}

	get := &GetChapterTaskCardTool{}
	res, err = get.Execute(ctx, &GetChapterTaskCardArgs{TargetReadingNumber: 4}, tc)
	if err != nil || !res.Success {
		t.Fatalf("get = %+v, err = %v", res, err)
	}
	content, _ := res.Data["content"].(string)
	for _, want := range []string{"钩子任务", "爽点任务", "人设时刻", "推进任务", "追兵火把"} {
		if !strings.Contains(content, want) {
			t.Errorf("content 应含 %q，got %q", want, content)
		}
	}
	if res.Data["exists"] != true {
		t.Errorf("exists 应为 true，got %+v", res.Data)
	}
}

// TestUpsertChapterTaskCard_RejectsIncomplete 四件套缺一拒收：业务拒绝（Success=false）且点名缺失字段。
func TestUpsertChapterTaskCard_RejectsIncomplete(t *testing.T) {
	db := openTaskCardToolDB(t)
	tc := taskCardTc(db)

	up := &UpsertChapterTaskCardTool{}
	res, err := up.Execute(context.Background(), &UpsertChapterTaskCardArgs{
		TargetReadingNumber: 2,
		HookTask:            "有钩子",
		BeatTask:            "有爽点",
		MomentTask:          "", // 缺
		AdvanceTask:         "有推进",
	}, tc)
	if err != nil {
		t.Fatalf("契约拒收应为业务拒绝而非系统错误，got err=%v", err)
	}
	if res.Success {
		t.Fatal("四件套不齐应 Success=false 拒收")
	}
	if !strings.Contains(res.Error, "moment_task") {
		t.Errorf("错误应点名 moment_task，got %q", res.Error)
	}

	var count int64
	db.Model(&timeline.ChapterTaskCard{}).Count(&count)
	if count != 0 {
		t.Errorf("拒收后不应落库，got %d 行", count)
	}
}

// TestGetChapterTaskCard_DefaultsToNext 不传序号时默认下一章（当前章数+1）。
func TestGetChapterTaskCard_DefaultsToNext(t *testing.T) {
	db := openTaskCardToolDB(t)
	tc := taskCardTc(db)
	ctx := context.Background()

	db.Create(&chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "一"})
	db.Create(&chapter.Chapter{NovelID: 1, SortOrder: 2, Title: "二"})

	get := &GetChapterTaskCardTool{}
	res, err := get.Execute(ctx, &GetChapterTaskCardArgs{}, tc)
	if err != nil || !res.Success {
		t.Fatalf("get = %+v, err = %v", res, err)
	}
	if res.Data["target_reading_number"] != 3 {
		t.Errorf("默认序号应为章数+1=3，got %+v", res.Data)
	}
	if res.Data["exists"] != false {
		t.Errorf("未立卡应 exists=false，got %+v", res.Data)
	}
	content, _ := res.Data["content"].(string)
	if !strings.Contains(content, "尚未立卡") {
		t.Errorf("缺失提示应含「尚未立卡」，got %q", content)
	}
}

// TestFormatTaskCardContext_InjectsAndNudges get_timeline 注入路径：本章/下一章卡注入，
// 缺失时给立卡提示（写前把关轻推力，只提示不拦截）。
func TestFormatTaskCardContext_InjectsAndNudges(t *testing.T) {
	db := openTaskCardToolDB(t)
	tc := taskCardTc(db)
	ctx := context.Background()

	store := timeline.NewStore(db, tc.LoggerOrDefault())
	if err := store.SaveTaskCard(ctx, &timeline.ChapterTaskCard{
		NovelID: 1, TargetReadingNumber: 6,
		HookTask: "收「三年之约」坑", BeatTask: "约战爽点", MomentTask: "主角守诺", AdvanceTask: "主线推进",
	}); err != nil {
		t.Fatal(err)
	}

	out := formatTaskCardContext(ctx, tc, 6)
	if !strings.Contains(out, "第 6 章任务卡") || !strings.Contains(out, "收「三年之约」坑") {
		t.Errorf("应注入本章任务卡，got %q", out)
	}
	if !strings.Contains(out, "第 7 章任务卡：尚未立卡") {
		t.Errorf("下一章缺失应有立卡提示，got %q", out)
	}

	// 两章都缺：两处提示，零报错。
	out = formatTaskCardContext(ctx, tc, 20)
	if strings.Count(out, "尚未立卡") != 2 {
		t.Errorf("两章都缺应两处提示，got %q", out)
	}
}
