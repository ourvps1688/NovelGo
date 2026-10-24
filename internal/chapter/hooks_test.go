package chapter

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

// ── 章节创建回填钩子（章纲任务卡 chapter_id 绑定等外围维护）────────
//
// 验收：①Create / Place 新建触发钩子（带阅读序号）；
// ②Place 移动既有章节不触发；③钩子为 nil / panic 时章节创建零影响（宁缺勿错）。

type hookCall struct {
	novelID       int64
	chapterID     int64
	readingNumber int
}

func captureHook(t *testing.T) *[]hookCall {
	t.Helper()
	calls := &[]hookCall{}
	SetCreatedHook(func(_ context.Context, _ *gorm.DB, novelID, chapterID int64, readingNumber int) {
		*calls = append(*calls, hookCall{novelID: novelID, chapterID: chapterID, readingNumber: readingNumber})
	})
	t.Cleanup(func() { SetCreatedHook(nil) })
	return calls
}

func chTitle(s string) *string { return &s }

func TestCreatedHookFiresOnCreateAndPlace(t *testing.T) {
	db := openChDB(t)
	s := NewStore(db, testChLogger())
	calls := captureHook(t)
	ctx := context.Background()

	first, err := s.Create(ctx, nil, 1, nil, "第一章")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := s.Place(ctx, nil, PlaceInput{NovelID: 1, Title: chTitle("第二章")})
	if err != nil {
		t.Fatalf("place new: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %+v, want 2 次（Create + Place 新建）", *calls)
	}
	if (*calls)[0].chapterID != first.ID || (*calls)[0].readingNumber != 1 {
		t.Fatalf("first call = %+v, want chapter=%d reading=1", (*calls)[0], first.ID)
	}
	if (*calls)[1].chapterID != second.ID || (*calls)[1].readingNumber != 2 {
		t.Fatalf("second call = %+v, want chapter=%d reading=2", (*calls)[1], second.ID)
	}

	// 移动既有章节不触发（不是创建）
	if _, err := s.Place(ctx, nil, PlaceInput{NovelID: 1, SourceChapterID: &first.ID}); err != nil {
		t.Fatalf("place move: %v", err)
	}
	if len(*calls) != 2 {
		t.Fatalf("移动既有章节不应触发钩子, calls = %+v", *calls)
	}
}

func TestCreatedHookNilAndPanicSafe(t *testing.T) {
	db := openChDB(t)
	s := NewStore(db, testChLogger())
	ctx := context.Background()

	// nil 钩子：创建照常
	SetCreatedHook(nil)
	t.Cleanup(func() { SetCreatedHook(nil) })
	if _, err := s.Create(ctx, nil, 1, nil, "第一章"); err != nil {
		t.Fatalf("create with nil hook: %v", err)
	}

	// panic 钩子：创建照常（recover 兜底），不中断
	SetCreatedHook(func(context.Context, *gorm.DB, int64, int64, int) { panic("hook boom") })
	ch, err := s.Create(ctx, nil, 1, nil, "第二章")
	if err != nil {
		t.Fatalf("create with panicking hook: %v", err)
	}
	if ch == nil || ch.ID == 0 {
		t.Fatal("章节须正常创建")
	}
}
