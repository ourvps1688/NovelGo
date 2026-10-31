//go:build cgo

package rag

import (
	"context"
	"io"
	"log/slog"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// contentStubEmbedder 返回由文本派生的确定性向量（首维反映内容），
// 用于在不加载 ONNX 模型的前提下验证检索排序 / 批处理 / 缓存失效行为。
type contentStubEmbedder struct{}

func (contentStubEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	return contentVec(text), nil
}
func (contentStubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = contentVec(t)
	}
	return out, nil
}
func (contentStubEmbedder) EmbedBatchQuery(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = contentVec(t)
	}
	return out, nil
}
func (contentStubEmbedder) Compact(context.Context) error { return nil }
func (contentStubEmbedder) Dim() int                      { return embeddingDim }
func (contentStubEmbedder) Close() error                  { return nil }

// contentVec 由文本 FNV-1a 哈希派生一个确定性向量，相同文本 → 相同向量（距离 0）。
func contentVec(text string) []float32 {
	v := make([]float32, embeddingDim)
	var h uint32 = 2166136261
	for i := 0; i < len(text); i++ {
		h ^= uint32(text[i])
		h *= 16777619
	}
	v[0] = float32(h%1000) / 1000.0
	return v
}

func newCacheTestStore(t *testing.T) (*VectorStore, func()) {
	t.Helper()
	sqlite_vec.Auto()
	db, err := gorm.Open(sqlite.Open("file:vs_cache?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB(): %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	vs := NewVectorStore(sqlDB, contentStubEmbedder{}, logger)
	return vs, func() { _ = sqlDB.Close() }
}

// TestVectorStore_SearchBatch_ReturnsPerQueryResults 验证多个查询合并一次批处理嵌入后，
// 每条查询各自返回其最相关 chunk：apple 查询命中 apple chunk，banana 查询命中 banana chunk。
func TestVectorStore_SearchBatch_ReturnsPerQueryResults(t *testing.T) {
	vs, cleanup := newCacheTestStore(t)
	defer cleanup()
	ctx := context.Background()
	novelID := int64(7)

	chunks := []Chunk{
		{ID: "a1", Content: "apple", ChapterID: 1, ChunkType: "content"},
		{ID: "b1", Content: "banana", ChapterID: 2, ChunkType: "content"},
	}
	if err := vs.IndexChunks(ctx, novelID, chunks); err != nil {
		t.Fatalf("IndexChunks: %v", err)
	}

	perQuery, err := vs.SearchBatch(ctx, novelID, []string{"apple", "banana"}, 1, nil)
	if err != nil {
		t.Fatalf("SearchBatch: %v", err)
	}
	if len(perQuery) != 2 {
		t.Fatalf("want 2 result sets, got %d", len(perQuery))
	}
	if len(perQuery[0]) != 1 || perQuery[0][0].Content != "apple" {
		t.Fatalf("query 'apple' should return apple chunk, got %+v", perQuery[0])
	}
	if len(perQuery[1]) != 1 || perQuery[1][0].Content != "banana" {
		t.Fatalf("query 'banana' should return banana chunk, got %+v", perQuery[1])
	}
}

// TestVectorStore_SearchCacheInvalidation 验证索引删除（触发 epoch 失效）后，
// 重复相同查询不会返回已被删除的陈旧 chunk。
func TestVectorStore_SearchCacheInvalidation(t *testing.T) {
	vs, cleanup := newCacheTestStore(t)
	defer cleanup()
	ctx := context.Background()
	novelID := int64(8)

	if err := vs.IndexChunks(ctx, novelID, []Chunk{
		{ID: "x1", Content: "alpha", ChapterID: 1, ChunkType: "content"},
	}); err != nil {
		t.Fatalf("IndexChunks: %v", err)
	}

	before, err := vs.Search(ctx, novelID, "alpha", 5, nil)
	if err != nil {
		t.Fatalf("Search before delete: %v", err)
	}
	if len(before) != 1 || before[0].Content != "alpha" {
		t.Fatalf("expected alpha before delete, got %+v", before)
	}

	// 删除该章节全部向量（bumpIndexEpoch 清空检索缓存）
	if err := vs.DeleteChapterChunks(ctx, novelID, 1); err != nil {
		t.Fatalf("DeleteChapterChunks: %v", err)
	}

	after, err := vs.Search(ctx, novelID, "alpha", 5, nil)
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected empty after delete (no stale cache), got %+v", after)
	}
}
