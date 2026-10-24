package timeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func openTaskCardDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openTlDB(t)
	if err := db.AutoMigrate(&ChapterTaskCard{}); err != nil {
		t.Fatalf("migrate task card: %v", err)
	}
	return db
}

func validCard(reading int) *ChapterTaskCard {
	return &ChapterTaskCard{
		NovelID:             1,
		TargetReadingNumber: reading,
		HookTask:            "埋下黑玉佩伏笔，章尾断章钩：追兵火把出现在山口",
		BeatTask:            "主角反杀欺压者（打脸爽点），落点在章中",
		MomentTask:          "主角隐忍爆发的高光时刻",
		AdvanceTask:         "主线「逃亡」推进：离开青云镇，进卷一功能位「离村」",
		Source:              "ai",
	}
}

// TestTaskCardSaveGetRoundtrip 立卡→取卡闭环：四件套齐全即落库、可读回。
func TestTaskCardSaveGetRoundtrip(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())
	ctx := context.Background()

	if err := s.SaveTaskCard(ctx, validCard(3)); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.GetTaskCard(ctx, 1, 3)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.HookTask == "" || got.BeatTask == "" || got.MomentTask == "" || got.AdvanceTask == "" {
		t.Errorf("四件套应齐全，got %+v", got)
	}
	if got.TargetReadingNumber != 3 {
		t.Errorf("reading = %d, want 3", got.TargetReadingNumber)
	}
}

// TestTaskCardUpsertOverwritesSameChapter 同章重复立卡整卡覆盖（一章一卡，不留历史行）。
func TestTaskCardUpsertOverwritesSameChapter(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())
	ctx := context.Background()

	if err := s.SaveTaskCard(ctx, validCard(5)); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	updated := validCard(5)
	updated.HookTask = "改为回收旧坑「三年之约」，章尾断章钩：信使带回死讯"
	if err := s.SaveTaskCard(ctx, updated); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	var count int64
	db.Model(&ChapterTaskCard{}).Where("novel_id = ? AND target_reading_number = ?", 1, 5).Count(&count)
	if count != 1 {
		t.Fatalf("同章应只有一张卡，got %d 行", count)
	}
	got, err := s.GetTaskCard(ctx, 1, 5)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(got.HookTask, "三年之约") {
		t.Errorf("应覆盖为新卡内容，got %q", got.HookTask)
	}
}

// TestTaskCardValidateRejectsIncomplete 四件套缺一拒收（机器可验契约的拒收口径）：
// 逐个缺失字段都被拒，且错误信息点名缺失字段。
func TestTaskCardValidateRejectsIncomplete(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())
	ctx := context.Background()

	for _, tc := range []struct {
		name    string
		mutate  func(*ChapterTaskCard)
		missing string
	}{
		{"缺钩子", func(c *ChapterTaskCard) { c.HookTask = "" }, "hook_task"},
		{"缺爽点", func(c *ChapterTaskCard) { c.BeatTask = "" }, "beat_task"},
		{"缺人设时刻", func(c *ChapterTaskCard) { c.MomentTask = "" }, "moment_task"},
		{"缺推进", func(c *ChapterTaskCard) { c.AdvanceTask = "" }, "advance_task"},
		{"空白也算缺", func(c *ChapterTaskCard) { c.HookTask = "   " }, "hook_task"},
	} {
		card := validCard(7)
		tc.mutate(card)
		err := s.SaveTaskCard(ctx, card)
		if err == nil {
			t.Fatalf("%s：四件套不齐应拒收", tc.name)
		}
		if !strings.Contains(err.Error(), tc.missing) {
			t.Errorf("%s：错误信息应点名缺失字段 %s，got %q", tc.name, tc.missing, err.Error())
		}
	}
	if _, err := s.GetTaskCard(ctx, 1, 7); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("拒收后不应落库，got err=%v", err)
	}
}

// TestTaskCardValidateRejectsBadReadingNumber 阅读序号非法拒收。
func TestTaskCardValidateRejectsBadReadingNumber(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())

	card := validCard(1)
	card.TargetReadingNumber = 0
	if err := s.SaveTaskCard(context.Background(), card); err == nil {
		t.Fatal("target_reading_number=0 应拒收")
	}
}

// TestTaskCardGetNotFound 取不存在的卡返回 ErrRecordNotFound（供调用方转缺失提示）。
func TestTaskCardGetNotFound(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())

	if _, err := s.GetTaskCard(context.Background(), 1, 99); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("want ErrRecordNotFound, got %v", err)
	}
}
