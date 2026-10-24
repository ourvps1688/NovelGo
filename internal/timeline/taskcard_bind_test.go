package timeline

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// ── ChapterID 回填钩子（章节创建后绑定）────────────────────────
//
// 验收：①只回填未绑定的卡（绝不覆盖既有绑定，宁缺勿错）；
// ②按 chapter_id 可查 / 零卡工作流判定；③参数非法零操作。

func TestBindChapterIDBackfillsOnlyUnbound(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())
	ctx := context.Background()

	// 序号 2 的卡待绑定
	if err := s.SaveTaskCard(ctx, validCard(2)); err != nil {
		t.Fatalf("save: %v", err)
	}
	bound, err := s.BindChapterID(ctx, nil, 1, 101, 2)
	if err != nil || !bound {
		t.Fatalf("bind = %v, err = %v, want true", bound, err)
	}
	card, err := s.GetTaskCardByChapterID(ctx, 1, 101)
	if err != nil {
		t.Fatalf("get by chapter: %v", err)
	}
	if card.TargetReadingNumber != 2 || card.ChapterID == nil || *card.ChapterID != 101 {
		t.Fatalf("card = %+v, want reading=2 chapter_id=101", card)
	}

	// 回填语义：已绑定的卡不再被覆盖（宁缺勿错）
	bound, err = s.BindChapterID(ctx, nil, 1, 202, 2)
	if err != nil || bound {
		t.Fatalf("rebind = %v, err = %v, want false（已绑定不覆盖）", bound, err)
	}
	card, _ = s.GetTaskCard(ctx, 1, 2)
	if card.ChapterID == nil || *card.ChapterID != 101 {
		t.Fatalf("chapter_id 被覆盖为 %v，回填不得覆盖既有绑定", card.ChapterID)
	}

	// 无卡的阅读序号 / 非法参数：零操作
	bound, err = s.BindChapterID(ctx, nil, 1, 303, 9)
	if err != nil || bound {
		t.Fatalf("missing card bind = %v, err = %v, want false", bound, err)
	}
	bound, err = s.BindChapterID(ctx, nil, 1, 0, 2)
	if err != nil || bound {
		t.Fatalf("invalid chapter = %v, err = %v, want false", bound, err)
	}
	bound, err = s.BindChapterID(ctx, nil, 1, 404, 0)
	if err != nil || bound {
		t.Fatalf("invalid reading = %v, err = %v, want false", bound, err)
	}
}

// TestBindChapterIDJoiningTx 同事务绑定：绑定写入走调用方事务（创建回滚时一并回滚）。
func TestBindChapterIDJoiningTx(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())
	ctx := context.Background()
	if err := s.SaveTaskCard(ctx, validCard(1)); err != nil {
		t.Fatalf("save: %v", err)
	}

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, berr := s.BindChapterID(ctx, tx, 1, 55, 1); berr != nil {
			return berr
		}
		// 事务内可见
		card, gerr := s.GetTaskCardByChapterID(ctx, 1, 55)
		if gerr != nil {
			return gerr
		}
		if card.TargetReadingNumber != 1 {
			t.Fatalf("card = %+v", card)
		}
		// 造回滚
		return errors.New("rollback")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	// 回滚后绑定随事务消失（原子一致）
	if _, err := s.GetTaskCardByChapterID(ctx, 1, 55); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("rollback 后绑定应消失, err = %v", err)
	}
}

func TestHasAnyTaskCard(t *testing.T) {
	db := openTaskCardDB(t)
	s := NewStore(db, testTlLogger())
	ctx := context.Background()

	ok, err := s.HasAnyTaskCard(ctx, 1)
	if err != nil || ok {
		t.Fatalf("empty = %v, err = %v, want false", ok, err)
	}
	if err := s.SaveTaskCard(ctx, validCard(1)); err != nil {
		t.Fatalf("save: %v", err)
	}
	ok, err = s.HasAnyTaskCard(ctx, 1)
	if err != nil || !ok {
		t.Fatalf("after save = %v, err = %v, want true", ok, err)
	}
	// 其他书不受影响（novel 隔离）
	ok, err = s.HasAnyTaskCard(ctx, 2)
	if err != nil || ok {
		t.Fatalf("other novel = %v, err = %v, want false", ok, err)
	}
}
