package chapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/deletion"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/volume"
)

func newTestService(db *gorm.DB) *Service {
	guard := deletion.NewGuard()
	return NewService(NewStore(db, testChLogger()), volume.NewStore(db, testChLogger()), guard, testChLogger(), nil, nil)
}

// TestServiceReconcileOrphanFiles 验证：DB 存活章节的文件被保留，DB 中不存在的
// id_<N>.md 孤儿文件（正文与大纲）被回收。DB 列表是真相源。
func TestServiceReconcileOrphanFiles(t *testing.T) {
	db := openChDB(t)
	s := newTestService(db)
	ctx := context.Background()
	const novelID int64 = 7

	live := Chapter{NovelID: novelID, SortOrder: 1, Title: "alive"}
	db.Create(&live)

	if err := git.WriteFile(novelID, git.ChapterPath(live.ID), "body"); err != nil {
		t.Fatalf("write live file: %v", err)
	}
	orphanID := live.ID + 999
	if err := git.WriteFile(novelID, git.ChapterPath(orphanID), "orphan"); err != nil {
		t.Fatalf("write orphan file: %v", err)
	}
	if err := git.WriteFile(novelID, git.OutlinePath(orphanID), "orphan-outline"); err != nil {
		t.Fatalf("write orphan outline: %v", err)
	}

	removed, err := s.ReconcileOrphanFiles(ctx, novelID)
	if err != nil {
		t.Fatalf("ReconcileOrphanFiles: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	livePath := filepath.Join(config.NovelDirPath(novelID), git.ChapterPath(live.ID))
	if _, err := os.Stat(livePath); err != nil {
		t.Errorf("live chapter file should remain: %v", err)
	}
	orphanPath := filepath.Join(config.NovelDirPath(novelID), git.ChapterPath(orphanID))
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Errorf("orphan chapter file should be removed")
	}
	orphanOutlinePath := filepath.Join(config.NovelDirPath(novelID), git.OutlinePath(orphanID))
	if _, err := os.Stat(orphanOutlinePath); !os.IsNotExist(err) {
		t.Errorf("orphan outline file should be removed")
	}
}

// TestServiceDeleteRemovesFilesAfterCommit 验证 Delete 在 DB 事务提交后才删文件：
// 文件删除不在事务回滚范围内，DB 记录删除成功即视为删除，文件随后被移除。
func TestServiceDeleteRemovesFilesAfterCommit(t *testing.T) {
	db := openChDB(t)
	s := newTestService(db)
	ctx := context.Background()
	const novelID int64 = 9

	ch := Chapter{NovelID: novelID, SortOrder: 1, Title: "to-delete"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	if err := git.WriteFile(novelID, git.ChapterPath(ch.ID), "body"); err != nil {
		t.Fatalf("write file: %v", err)
	}

	res, err := s.Delete(ctx, novelID, ch.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !res.Deleted {
		t.Errorf("Deleted = false, want true")
	}

	// DB 记录已删
	var count int64
	db.Model(&Chapter{}).Where("id = ?", ch.ID).Count(&count)
	if count != 0 {
		t.Errorf("db record should be deleted, count = %d", count)
	}
	// 文件应在提交后被移除
	path := filepath.Join(config.NovelDirPath(novelID), git.ChapterPath(ch.ID))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("chapter file should be removed after commit")
	}
}
