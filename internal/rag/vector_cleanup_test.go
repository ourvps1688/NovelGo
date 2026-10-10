//go:build cgo

package rag

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/novel"
	"github.com/novelgo/novelgo/internal/volume"
)

// stubEmbedder 返回确定性向量，让这套单测不依赖 ONNX 模型。
type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, string) ([]float32, error) { return stubVector(), nil }

func (stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = stubVector()
	}
	return out, nil
}

func (stubEmbedder) Compact(context.Context) error { return nil }
func (stubEmbedder) Dim() int                      { return 512 }
func (stubEmbedder) Close() error                  { return nil }

// stubVector 全零向量在余弦距离下没有意义，给首维一个非零值。
func stubVector() []float32 {
	v := make([]float32, 512)
	v[0] = 0.5
	return v
}

// newCleanupTestQueue 构造一套独立于全局单例的 RefreshQueue：
// 内存库（shared cache 让连接池共享同一个库）+ 真实 vec0 表 + 桩 embedder。
func newCleanupTestQueue(t *testing.T) (*RefreshQueue, *VectorStore, *gorm.DB) {
	t.Helper()

	// vec0 模块由 sqlite-vec 扩展提供，正常路径是 storage.Open 调 Auto()，
	// 这里必须早于建立连接，扩展才对新连接生效。
	sqlite_vec.Auto()

	db, err := gorm.Open(sqlite.Open("file:rag_cleanup?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.AutoMigrate(&novel.Novel{}, &volume.Volume{}, &chapter.Chapter{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	vs := NewVectorStore(sqlDB, stubEmbedder{}, logger)
	q := &RefreshQueue{
		vs:         vs,
		chStore:    chapter.NewStore(db, logger),
		novelStore: novel.NewStore(db, logger),
		logger:     logger,
	}
	return q, vs, db
}

// 章节被全部删除（小说还在）时，残留的整表向量应在覆盖度检查中被清空。
// rebuildNovelIfIncomplete 曾在 len(chapters)==0 时直接 return，跳过孤儿清理。
func TestRebuildNovelIfIncomplete_ClearsVectorsOfNovelWithoutChapters(t *testing.T) {
	q, vs, db := newCleanupTestQueue(t)
	ctx := context.Background()

	n := &novel.Novel{Title: "空章节小说"}
	if err := db.Create(n).Error; err != nil {
		t.Fatalf("create novel: %v", err)
	}

	// 先建一章并索引，制造「小说在、向量在」的残留前提
	ch := &chapter.Chapter{NovelID: n.ID, Title: "第一章", SortOrder: 1}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("create chapter: %v", err)
	}
	chunks := []Chunk{{ID: "c1", Content: "夜色沉沉。", ChapterID: ch.ID, ChunkType: "content"}}
	if err := vs.IndexChunks(ctx, n.ID, chunks); err != nil {
		t.Fatalf("IndexChunks: %v", err)
	}
	before, err := vs.CountChunks(ctx, n.ID)
	if err != nil {
		t.Fatalf("CountChunks before: %v", err)
	}
	if before == 0 {
		t.Fatal("expected indexed chunks before cleanup")
	}

	// 删掉该小说全部章节，保留小说与向量（模拟整章被删后的残留）
	if err := db.Where("novel_id = ?", n.ID).Delete(&chapter.Chapter{}).Error; err != nil {
		t.Fatalf("delete chapters: %v", err)
	}

	didInfer := false
	if err := q.rebuildNovelIfIncomplete(ctx, n.ID, &didInfer); err != nil {
		t.Fatalf("rebuildNovelIfIncomplete: %v", err)
	}

	after, err := vs.CountChunks(ctx, n.ID)
	if err != nil {
		t.Fatalf("CountChunks after: %v", err)
	}
	if after != 0 {
		t.Errorf("orphan chunks = %d, want 0（空章节小说的残留向量应被清空）", after)
	}
}

// 从未索引过的小说（无章节、无向量表）：覆盖度检查不应报错，也不应顺手建一张空表。
func TestRebuildNovelIfIncomplete_SkipsNovelWithoutVectors(t *testing.T) {
	q, _, db := newCleanupTestQueue(t)
	ctx := context.Background()

	n := &novel.Novel{Title: "从未索引"}
	if err := db.Create(n).Error; err != nil {
		t.Fatalf("create novel: %v", err)
	}

	didInfer := false
	if err := q.rebuildNovelIfIncomplete(ctx, n.ID, &didInfer); err != nil {
		t.Fatalf("rebuildNovelIfIncomplete: %v", err)
	}

	var tables int
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		fmt.Sprintf("vec_novel_%d", n.ID)).Scan(&tables).Error; err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tables != 0 {
		t.Errorf("vec tables = %d, want 0（不应为从未索引的小说建表）", tables)
	}
}

// 手动全量重建走的也是空章节分支；从未索引的小说不应报错或创建空表。
func TestRebuildNovel_SkipsNovelWithoutVectors(t *testing.T) {
	q, _, db := newCleanupTestQueue(t)
	ctx := context.Background()

	n := &novel.Novel{Title: "从未索引"}
	if err := db.Create(n).Error; err != nil {
		t.Fatalf("create novel: %v", err)
	}

	didInfer := false
	if err := q.rebuildNovel(ctx, n.ID, &didInfer); err != nil {
		t.Fatalf("rebuildNovel: %v", err)
	}

	var tables int
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		fmt.Sprintf("vec_novel_%d", n.ID)).Scan(&tables).Error; err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tables != 0 {
		t.Errorf("vec tables = %d, want 0（不应为从未索引的小说建表）", tables)
	}
}
