package content

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/writing"
)

func testContentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func openContentDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&chapter.Chapter{}, &writing.WritingLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestDeriveAfterSaveUpdatesWordCount 验证 SaveContent 原本直连 a.chapter.DB 写 word_count
// 的逻辑已下沉到 content.Service：DeriveAfterSave 通过后 DB 字数被正确更新。
func TestDeriveAfterSaveUpdatesWordCount(t *testing.T) {
	db := openContentDB(t)
	ch := chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "c1"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	svc := NewService(testContentLogger(), chapter.NewStore(db, testContentLogger()), writing.NewStore(db, testContentLogger()), nil)

	content := "这是一段用于统计字数的测试正文。"
	expected := "旧内容"
	svc.DeriveAfterSave(context.Background(), 1, ch.ID, content, &expected)

	var got chapter.Chapter
	if err := db.First(&got, ch.ID).Error; err != nil {
		t.Fatalf("read chapter: %v", err)
	}
	if got.WordCount <= 0 {
		t.Errorf("word_count = %d, want > 0 after DeriveAfterSave", got.WordCount)
	}

	// 写作变更记录也应写入（expected 与 content 不同，必然有增减）。
	var logCount int64
	if err := db.Model(&writing.WritingLog{}).Where("novel_id = ?", 1).Count(&logCount).Error; err != nil {
		t.Fatalf("count writing log: %v", err)
	}
	if logCount != 1 {
		t.Errorf("writing log records = %d, want 1", logCount)
	}
}

// TestDeriveAfterSaveNilSearchLoader 验证 searchLoader 为 nil 时不 panic（搜索服务未就绪场景）。
func TestDeriveAfterSaveNilSearchLoader(t *testing.T) {
	db := openContentDB(t)
	ch := chapter.Chapter{NovelID: 2, SortOrder: 1, Title: "c2"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	svc := NewService(testContentLogger(), chapter.NewStore(db, testContentLogger()), writing.NewStore(db, testContentLogger()), nil)
	// 不应 panic：searchLoader 为 nil 时跳过搜索缓存更新。
	svc.DeriveAfterSave(context.Background(), 2, ch.ID, "正文", nil)
}
