package mcp_tools_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/approval"
	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/timeline"
)

func linkTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func linkCardStore(db *gorm.DB) *timeline.Store {
	return timeline.NewStore(db, linkTestLogger())
}

func linkChapterStore(db *gorm.DB) *chapter.Store {
	return chapter.NewStore(db, linkTestLogger())
}

func migrateTaskCards(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.AutoMigrate(&timeline.ChapterTaskCard{}); err != nil {
		t.Fatalf("migrate task cards: %v", err)
	}
}

// ── 大纲审批 × 章纲任务卡联动核验 + ChapterID 回填闭环 ─────────────
//
// 验收：①有卡：审批载荷带四件套 + 写后对卡核验提示；
// ②本书启用任务卡但本章缺卡：立卡提示（只提示不拦截，写入照常）；
// ③整本零卡（纯即兴）：零提示零打扰（降级逐字一致）；
// ④新建大纲建章后 chapter_id 回填闭环（立卡 → 建章 → 绑定）。

// captureApprover 捕获审批载荷的桩（自动通过）。
type captureApprover struct {
	payload map[string]any
}

func (c *captureApprover) RequestApproval(_ context.Context, _ string, payload any) (approval.Approval, error) {
	if m, ok := payload.(map[string]any); ok {
		c.payload = m
	}
	return approval.Approval{Approved: true}, nil
}

func seedTaskCard(t *testing.T, db *gorm.DB, reading int) *timeline.ChapterTaskCard {
	t.Helper()
	card := &timeline.ChapterTaskCard{
		NovelID:             1,
		TargetReadingNumber: reading,
		HookTask:            "埋黑玉佩伏笔，章尾断章钩：追兵火把出现在山口",
		BeatTask:            "反杀欺压者的打脸爽点",
		MomentTask:          "主角隐忍爆发",
		AdvanceTask:         "主线推进：离开青云镇",
		Source:              "ai",
	}
	if err := linkCardStore(db).SaveTaskCard(context.Background(), card); err != nil {
		t.Fatalf("seed task card: %v", err)
	}
	return card
}

func injectTexts(res *mcp_tools.ToolResult) string {
	var b strings.Builder
	for _, in := range res.Inject {
		b.WriteString(in.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// TestOutlineApprovalCarriesTaskCard 有卡：审批载荷带四件套（联动），写后注入对卡核验提示。
func TestOutlineApprovalCarriesTaskCard(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	vol := seedVolume(t, db, 1, "第一卷", 1)
	migrateTaskCards(t, db)
	seedTaskCard(t, db, 1) // 第 1 章任务卡（尚未绑定 chapter_id）

	created, err := linkChapterStore(db).Create(ctx, nil, 1, &vol, "夜入皇城")
	if err != nil {
		t.Fatalf("create chapter: %v", err)
	}

	approver := &captureApprover{}
	tc.Approver = approver
	res := execEdit(t, ctx, tc, editArgs(git.OutlinePath(created.ID), "full_replace",
		"本章大纲：主角借商队掩护入城，章尾断章钩——追兵火把出现在山口。"))
	if !res.Success {
		t.Fatalf("outline edit failed: %s", res.Error)
	}

	cardPayload, ok := approver.payload["task_card"].(map[string]any)
	if !ok {
		t.Fatalf("审批载荷缺少 task_card 联动块: %+v", approver.payload)
	}
	for _, key := range []string{"hook_task", "beat_task", "moment_task", "advance_task", "target_reading_number"} {
		if _, ok := cardPayload[key]; !ok {
			t.Errorf("task_card 载荷缺 %s: %+v", key, cardPayload)
		}
	}
	if got, _ := cardPayload["hook_task"].(string); !strings.Contains(got, "黑玉佩") {
		t.Errorf("hook_task = %q, want 四件套原文", got)
	}
	if texts := injectTexts(res); !strings.Contains(texts, "对卡核验") && !strings.Contains(texts, "任务卡") {
		t.Errorf("写后须注入对卡核验提示, got %q", texts)
	}
}

// TestOutlineEditMissingCardNudges 本书启用任务卡但本章缺卡：立卡提示（只提示不拦截）。
func TestOutlineEditMissingCardNudges(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	vol := seedVolume(t, db, 1, "第一卷", 1)
	migrateTaskCards(t, db)
	seedTaskCard(t, db, 7) // 本书有卡，但本章（第 1 章）没卡

	created, err := linkChapterStore(db).Create(ctx, nil, 1, &vol, "夜入皇城")
	if err != nil {
		t.Fatalf("create chapter: %v", err)
	}

	approver := &captureApprover{}
	tc.Approver = approver
	res := execEdit(t, ctx, tc, editArgs(git.OutlinePath(created.ID), "full_replace",
		"本章大纲：主角借商队掩护入城，避开守卫盘查。"))
	if !res.Success {
		t.Fatalf("缺卡不得拦截大纲写入（只提示不拦截）: %s", res.Error)
	}
	if _, ok := approver.payload["task_card_missing"]; !ok {
		t.Errorf("审批载荷缺少 task_card_missing 提示: %+v", approver.payload)
	}
	if texts := injectTexts(res); !strings.Contains(texts, "尚未立卡") {
		t.Errorf("写后须注入立卡提示, got %q", texts)
	}
}

// TestOutlineEditImprovisationZeroNudge 整本零卡（纯即兴写作）：零提示零打扰。
func TestOutlineEditImprovisationZeroNudge(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	vol := seedVolume(t, db, 1, "第一卷", 1)
	migrateTaskCards(t, db)

	created, err := linkChapterStore(db).Create(ctx, nil, 1, &vol, "夜入皇城")
	if err != nil {
		t.Fatalf("create chapter: %v", err)
	}

	approver := &captureApprover{}
	tc.Approver = approver
	res := execEdit(t, ctx, tc, editArgs(git.OutlinePath(created.ID), "full_replace",
		"本章大纲：主角借商队掩护入城，避开守卫盘查。"))
	if !res.Success {
		t.Fatalf("outline edit failed: %s", res.Error)
	}
	if _, ok := approver.payload["task_card"]; ok {
		t.Error("零卡本书不应带 task_card 载荷")
	}
	if _, ok := approver.payload["task_card_missing"]; ok {
		t.Error("零卡本书不应提示缺卡（纯即兴零打扰）")
	}
	if texts := injectTexts(res); strings.Contains(texts, "任务卡") {
		t.Errorf("零卡本书须零任务卡提示, got %q", texts)
	}
}

// TestNewOutlineBackfillsChapterID 闭环：先立「下一章」卡 → outlines/new.md 建章 → chapter_id 回填。
func TestNewOutlineBackfillsChapterID(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	seedVolume(t, db, 1, "第一卷", 1)
	migrateTaskCards(t, db)
	card := seedTaskCard(t, db, 1) // 下一章=第 1 章

	// 安装真实回填钩子（app/handler.go 6b 的同款装配）
	chapter.SetCreatedHook(func(hookCtx context.Context, tx *gorm.DB, novelID, chapterID int64, readingNumber int) {
		if _, err := linkCardStore(db).BindChapterID(hookCtx, tx, novelID, chapterID, readingNumber); err != nil {
			t.Errorf("bind: %v", err)
		}
	})
	t.Cleanup(func() { chapter.SetCreatedHook(nil) })

	res := execEdit(t, ctx, tc, editArgs("outlines/new.md", "full_replace",
		"本章大纲：主角借商队掩护入城，章尾断章钩——追兵火把出现在山口。"))
	if !res.Success {
		t.Fatalf("outline new channel failed: %s", res.Error)
	}
	chapterID, _ := res.Data["chapter_id"].(int64)
	if chapterID == 0 {
		t.Fatalf("应返回新建 chapter_id: %+v", res.Data)
	}

	bound, err := linkCardStore(db).GetTaskCardByChapterID(ctx, 1, chapterID)
	if err != nil {
		t.Fatalf("chapter_id 回填失败（立卡→建章→绑定闭环断裂）: %v", err)
	}
	if bound.TargetReadingNumber != card.TargetReadingNumber {
		t.Fatalf("绑定的卡 = %+v, want reading=%d", bound, card.TargetReadingNumber)
	}
}
