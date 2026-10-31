//go:build cgo

package rag

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// VectorStore 使用 sqlite-vec 管理向量索引，每部小说一张 vec0 虚拟表。
type VectorStore struct {
	db          *sql.DB
	embedder    Embedder
	log         *slog.Logger
	ensuredOnce sync.Map // map[int64]bool，避免重复 CREATE TABLE IF NOT EXISTS

	// 检索结果缓存（P0-2）：key = novelID + query + topK + filter，短 TTL + 索引 epoch 失效。
	// 命中可跳过 ONNX 嵌入与 sqlite-vec 查询；索引发生任何写变后整体失效，避免返回陈旧 chunk。
	searchCacheMu sync.RWMutex
	searchCache   map[string]searchCacheEntry
	indexEpoch    uint64
}

// searchCacheEntry 是单条检索结果缓存项；epoch 记录写入时的索引版本。
type searchCacheEntry struct {
	results []SearchResult
	epoch   uint64
	exp     time.Time
}

const searchCacheTTL = 5 * time.Minute

// NewVectorStore 创建向量存储。db 应为已启用 sqlite-vec 扩展的 SQLite 连接。
func NewVectorStore(db *sql.DB, embedder Embedder, log *slog.Logger) *VectorStore {
	return &VectorStore{
		db:          db,
		embedder:    embedder,
		log:         log,
		searchCache: make(map[string]searchCacheEntry),
	}
}

func (s *VectorStore) tableName(novelID int64) string {
	return fmt.Sprintf("vec_novel_%d", novelID)
}

// ensureTable 确保指定小说的向量表存在，不存在则创建。结果缓存在 ensuredOnce 中。
func (s *VectorStore) ensureTable(ctx context.Context, novelID int64) error {
	if _, ok := s.ensuredOnce.Load(novelID); ok {
		return nil
	}

	tableName := s.tableName(novelID)
	// 迁移：旧表无 start_position 列时重建。列不存在和查询出错需区分处理。
	hasCol, colErr := tableHasColumn(ctx, s.db, tableName, "start_position")
	if colErr != nil {
		s.log.Warn("rag: 检查列失败，跳过迁移", "table", tableName, "err", colErr)
	} else if !hasCol {
		s.log.Info("rag: 旧表缺少 start_position 列，重建中", "table", tableName)
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)); err != nil {
			return fmt.Errorf("rag: drop old vec table %s: %w", tableName, err)
		}
	}
	// v1.6.0 迁移：旧表仍用 chapter_number 列（迁移前 schema）时 DROP 重建。
	// vec0 虚拟表不支持改列名，只能重建为新 schema（chapter_id 列）。
	// 向量是派生索引，丢失后由 RebuildAll 覆盖度检查自动重建，无需数据搬迁。
	if hasOld, _ := tableHasColumn(ctx, s.db, tableName, "chapter_number"); hasOld {
		s.log.Warn("rag: 旧表仍含 chapter_number 列，重建为新 schema", "table", tableName)
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)); err != nil {
			return fmt.Errorf("rag: drop legacy vec table %s: %w", tableName, err)
		}
	}
	sql := fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(
		embedding float[%d] distance_metric=cosine,
		chunk_id text,
		content text,
		chunk_type text,
		chapter_id integer,
		chunk_index integer,
		start_position integer
	)`, tableName, embeddingDim)

	_, err := s.db.ExecContext(ctx, sql)
	if err != nil {
		return fmt.Errorf("rag: create vec table for novel %d: %w", novelID, err)
	}
	s.ensuredOnce.Store(novelID, true)
	return nil
}

// tableExists 判断小说的向量表是否已创建。
// vec0 虚拟表同样登记在 sqlite_master 中，用它可以区分「从未索引」与「已建表但为空」，
// 避免对从未索引的小说执行 DELETE 时报 no such table。
func (s *VectorStore) tableExists(ctx context.Context, novelID int64) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		s.tableName(novelID)).Scan(&n); err != nil {
		return false, fmt.Errorf("rag: 检查向量表是否存在: %w", err)
	}
	return n > 0, nil
}

// IndexChunks 将文本块批量生成 embedding 并在事务中写入向量表。
func (s *VectorStore) IndexChunks(ctx context.Context, novelID int64, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	if err := s.ensureTable(ctx, novelID); err != nil {
		return err
	}

	// 批量生成 embedding，一次 ONNX Run。
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embs, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("rag: batch embed: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rag: begin tx: %w", err)
	}
	defer tx.Rollback()

	tableName := s.tableName(novelID)
	for i, chunk := range chunks {
		v, err := sqlite_vec.SerializeFloat32(embs[i])
		if err != nil {
			return fmt.Errorf("rag: serialize chunk %s: %w", chunk.ID, err)
		}

		_, err = tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (embedding, chunk_id, content, chunk_type, chapter_id, chunk_index, start_position) VALUES (?, ?, ?, ?, ?, ?, ?)`, tableName),
			v, chunk.ID, chunk.Content, chunk.ChunkType, chunk.ChapterID, chunk.ChunkIndex, chunk.StartRunePos,
		)
		if err != nil {
			return fmt.Errorf("rag: insert chunk %s: %w", chunk.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rag: commit tx: %w", err)
	}

	s.bumpIndexEpoch() // 索引写入后使检索缓存失效
	s.log.Info("向量索引完成", "novel_id", novelID, "chunks", len(chunks))
	return nil
}

// UpsertChunks 幂等写入文本块：先按 chunk_id 删除已有行，再插入，避免重复索引。
// 用于设定等需按 stable id 覆盖更新的场景（与章节的「DeleteChapterChunks + IndexChunks」等价，
// 但粒度从 chapter_id 细化到 chunk_id）。
func (s *VectorStore) UpsertChunks(ctx context.Context, novelID int64, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}
	if err := s.ensureTable(ctx, novelID); err != nil {
		return err
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}
	embs, err := s.embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return fmt.Errorf("rag: batch embed: %w", err)
	}

	tableName := s.tableName(novelID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rag: begin tx: %w", err)
	}
	defer tx.Rollback()
	for i, chunk := range chunks {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE chunk_id = ?`, tableName), chunk.ID); err != nil {
			return fmt.Errorf("rag: delete chunk %s: %w", chunk.ID, err)
		}
		v, err := sqlite_vec.SerializeFloat32(embs[i])
		if err != nil {
			return fmt.Errorf("rag: serialize chunk %s: %w", chunk.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (embedding, chunk_id, content, chunk_type, chapter_id, chunk_index, start_position) VALUES (?, ?, ?, ?, ?, ?, ?)`, tableName),
			v, chunk.ID, chunk.Content, chunk.ChunkType, chunk.ChapterID, chunk.ChunkIndex, chunk.StartRunePos,
		); err != nil {
			return fmt.Errorf("rag: insert chunk %s: %w", chunk.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("rag: commit tx: %w", err)
	}

	s.bumpIndexEpoch() // 索引写入后使检索缓存失效
	s.log.Info("向量幂等写入完成", "novel_id", novelID, "chunks", len(chunks))
	return nil
}

// DeleteChunk 删除指定 stable chunk_id 的全部向量块（用于设定删除后清理）。
// 向量表不存在时静默 no-op（该 novel 从未索引过设定）。
func (s *VectorStore) DeleteChunk(ctx context.Context, novelID int64, chunkID string) error {
	exists, err := s.tableExists(ctx, novelID)
	if err != nil {
		return fmt.Errorf("rag: check table before delete chunk: %w", err)
	}
	if !exists {
		return nil
	}
	tableName := s.tableName(novelID)
	if _, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE chunk_id = ?`, tableName), chunkID); err != nil {
		return fmt.Errorf("rag: delete chunk %s: %w", chunkID, err)
	}
	s.bumpIndexEpoch() // 索引删除后使检索缓存失效
	s.log.Info("已删除向量块", "novel_id", novelID, "chunk_id", chunkID)
	return nil
}

// CompactEmbeddingMemory 释放完整重建留下的 ONNX CPU arena 扩展块。
// 普通查询和增量索引不应逐次调用，以免反复释放和重新分配推理内存。
func (s *VectorStore) CompactEmbeddingMemory(ctx context.Context) error {
	return s.embedder.Compact(ctx)
}

// Search 在指定小说的向量索引中执行语义检索。
// 命中检索结果缓存（相同 novelID+query+topK+filter 且在索引 epoch / TTL 内）时直接返回，跳过嵌入与查询。
func (s *VectorStore) Search(ctx context.Context, novelID int64, query string, topK int, filter *SearchFilter) ([]SearchResult, error) {
	if err := s.ensureTable(ctx, novelID); err != nil {
		return nil, err
	}

	key := s.searchCacheKey(novelID, query, topK, filter)
	if res, ok := s.getSearchCache(key); ok {
		return res, nil
	}

	queryEmb, err := s.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("rag: embed query: %w", err)
	}

	res, err := s.searchWithEmbedding(ctx, novelID, queryEmb, topK, filter)
	if err != nil {
		return nil, err
	}
	s.putSearchCache(key, res)
	return res, nil
}

// SearchBatch 批量语义检索：一次性批量嵌入所有查询（带 BGE 指令前缀，见 EmbedBatchQuery），
// 再各自检索。相比逐条 Search（每条一次串行 Embed），ONNX 推理从 N 次降为 ⌈N/maxBatchSize⌉ 次。
// 每条查询仍独立返回结果，供调用方（如 V2-D 证据前置）分别做二阶段重排。
func (s *VectorStore) SearchBatch(ctx context.Context, novelID int64, queries []string, topK int, filter *SearchFilter) ([][]SearchResult, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	if err := s.ensureTable(ctx, novelID); err != nil {
		return nil, err
	}

	qvecs, err := s.embedder.EmbedBatchQuery(ctx, queries)
	if err != nil {
		return nil, fmt.Errorf("rag: batch embed queries: %w", err)
	}

	out := make([][]SearchResult, len(queries))
	for i, q := range queries {
		key := s.searchCacheKey(novelID, q, topK, filter)
		if res, ok := s.getSearchCache(key); ok {
			out[i] = res
			continue
		}
		res, err := s.searchWithEmbedding(ctx, novelID, qvecs[i], topK, filter)
		if err != nil {
			return nil, err
		}
		s.putSearchCache(key, res)
		out[i] = res
	}
	return out, nil
}

// searchWithEmbedding 用预计算的查询向量执行 sqlite-vec 余弦检索与结果装配。
// 嵌入与缓存由调用方（Search / SearchBatch）负责，本方法只做 SQL 检索。
func (s *VectorStore) searchWithEmbedding(ctx context.Context, novelID int64, queryEmb []float32, topK int, filter *SearchFilter) ([]SearchResult, error) {
	q, err := sqlite_vec.SerializeFloat32(queryEmb)
	if err != nil {
		return nil, fmt.Errorf("rag: serialize query: %w", err)
	}

	tableName := s.tableName(novelID)
	whereClauses := []string{}
	args := []any{q}

	if filter != nil {
		if len(filter.ChapterIDs) > 0 {
			placeholders := make([]string, len(filter.ChapterIDs))
			for i, id := range filter.ChapterIDs {
				placeholders[i] = "?"
				args = append(args, id)
			}
			whereClauses = append(whereClauses,
				fmt.Sprintf("chapter_id IN (%s)", strings.Join(placeholders, ",")))
		}
		if len(filter.ChunkTypes) > 0 {
			placeholders := make([]string, len(filter.ChunkTypes))
			for i, t := range filter.ChunkTypes {
				placeholders[i] = "?"
				args = append(args, t)
			}
			whereClauses = append(whereClauses,
				fmt.Sprintf("chunk_type IN (%s)", strings.Join(placeholders, ",")))
		}
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " AND " + strings.Join(whereClauses, " AND ")
	}

	querySQL := fmt.Sprintf(
		`SELECT chunk_id, content, chunk_type, chapter_id, distance, start_position, embedding FROM %s WHERE embedding MATCH ?%s ORDER BY distance LIMIT ?`,
		tableName, whereSQL,
	)
	args = append(args, topK)

	rows, err := s.db.QueryContext(ctx, querySQL, args...)
	if err != nil {
		return nil, fmt.Errorf("rag: search query: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var chunkID, content, chunkType string
		var chapterID int64
		var startRunePos int
		var distance float64
		var embBlob []byte
		if err := rows.Scan(&chunkID, &content, &chunkType, &chapterID, &distance, &startRunePos, &embBlob); err != nil {
			return nil, fmt.Errorf("rag: scan result: %w", err)
		}
		relevance := 1.0 - distance
		if relevance < 0 {
			relevance = 0
		}
		results = append(results, SearchResult{
			ChunkID:      chunkID,
			Content:      content,
			SourceType:   chunkType,
			ChapterID:    chapterID,
			StartRunePos: startRunePos,
			Distance:     distance,
			Relevance:    relevance,
			Embedding:    deserializeFloat32(embBlob),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rag: iterate results: %w", err)
	}

	s.log.Info("向量检索完成", "novel_id", novelID, "results", len(results))
	return results, nil
}

// ── 检索结果缓存 ──────────────────────────────────────────

// bumpIndexEpoch 在向量索引发生任何写变后调用：递增 epoch 并清空检索缓存，
// 使基于旧索引的缓存项整体失效，避免返回已被增删改的陈旧 chunk。
func (s *VectorStore) bumpIndexEpoch() {
	s.searchCacheMu.Lock()
	defer s.searchCacheMu.Unlock()
	s.indexEpoch++
	s.searchCache = make(map[string]searchCacheEntry)
}

// searchCacheKey 生成检索缓存键：novelID + query + topK + filter 稳定串。
func (s *VectorStore) searchCacheKey(novelID int64, query string, topK int, filter *SearchFilter) string {
	fk := ""
	if filter != nil {
		var b strings.Builder
		for _, id := range filter.ChapterIDs {
			fmt.Fprintf(&b, "c%d,", id)
		}
		for _, t := range filter.ChunkTypes {
			b.WriteString(t)
			b.WriteByte(',')
		}
		fk = b.String()
	}
	return fmt.Sprintf("%d\x00%s\x00%d\x00%s", novelID, query, topK, fk)
}

// getSearchCache 命中（未过期且 epoch 匹配）时返回结果副本，否则 (nil, false)。
func (s *VectorStore) getSearchCache(key string) ([]SearchResult, bool) {
	s.searchCacheMu.RLock()
	defer s.searchCacheMu.RUnlock()
	e, ok := s.searchCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(e.exp) || e.epoch != s.indexEpoch {
		return nil, false
	}
	out := make([]SearchResult, len(e.results))
	for i := range e.results {
		r := e.results[i]
		if r.Embedding != nil {
			cp := make([]float32, len(r.Embedding))
			copy(cp, r.Embedding)
			r.Embedding = cp
		}
		out[i] = r
	}
	return out, true
}

// putSearchCache 写入检索缓存（深拷 Embedding，避免后续查询误改缓存切片）。
func (s *VectorStore) putSearchCache(key string, results []SearchResult) {
	s.searchCacheMu.Lock()
	defer s.searchCacheMu.Unlock()
	cp := make([]SearchResult, len(results))
	for i := range results {
		r := results[i]
		if r.Embedding != nil {
			v := make([]float32, len(r.Embedding))
			copy(v, r.Embedding)
			r.Embedding = v
		}
		cp[i] = r
	}
	s.searchCache[key] = searchCacheEntry{
		results: cp,
		epoch:   s.indexEpoch,
		exp:     time.Now().Add(searchCacheTTL),
	}
}

// DeleteChapterChunks 删除指定章节的所有向量块。
// 参数 chapterID 为 chapters.id。
func (s *VectorStore) DeleteChapterChunks(ctx context.Context, novelID int64, chapterID int64) error {
	tableName := s.tableName(novelID)
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE chapter_id = ?`, tableName),
		chapterID,
	)
	if err != nil {
		return fmt.Errorf("rag: delete chapter %d chunks: %w", chapterID, err)
	}
	s.bumpIndexEpoch() // 索引删除后使检索缓存失效
	s.log.Info("已删除章节向量", "novel_id", novelID, "chapter_id", chapterID)
	return nil
}

// CountChunks 返回指定小说的向量块总数。表不存在时自动创建后返回 0。
func (s *VectorStore) CountChunks(ctx context.Context, novelID int64) (int, error) {
	if err := s.ensureTable(ctx, novelID); err != nil {
		return 0, err
	}
	var count int
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", s.tableName(novelID))).Scan(&count)
	return count, err
}

// DistinctChapterIDs 返回向量表中已索引的去重章节 id 集合。
// 供 RebuildAll 覆盖度检查使用：与 chapters 表实际 id 对比，
// 找出缺失章节（增量补建）和孤儿 chunk（清理）。
func (s *VectorStore) DistinctChapterIDs(ctx context.Context, novelID int64) ([]int64, error) {
	if err := s.ensureTable(ctx, novelID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		fmt.Sprintf("SELECT DISTINCT chapter_id FROM %s", s.tableName(novelID)))
	if err != nil {
		return nil, fmt.Errorf("rag: distinct chapter_id: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("rag: scan distinct chapter_id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rag: iterate distinct chapter_id: %w", err)
	}
	return ids, nil
}

// DeleteOrphanChunks 删除向量表中 chapter_id 不在有效列表内的行（孤儿 chunk）。
// 典型场景：章节被删除后其向量残留，或迁移期写入的 chapter_id=0 未知归属行。
// 不触发 DROP/CREATE，直接 DELETE；validChapterIDs 为空时清空整表。
func (s *VectorStore) DeleteOrphanChunks(ctx context.Context, novelID int64, validChapterIDs []int64) error {
	exists, err := s.tableExists(ctx, novelID)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	tableName := s.tableName(novelID)
	if len(validChapterIDs) == 0 {
		_, err = s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", tableName))
		if err != nil {
			return fmt.Errorf("rag: clear orphan chunks for novel %d: %w", novelID, err)
		}
		return nil
	}

	placeholders := make([]string, len(validChapterIDs))
	args := make([]any, len(validChapterIDs))
	for i, id := range validChapterIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	sqlStr := fmt.Sprintf(
		"DELETE FROM %s WHERE chapter_id IS NULL OR chapter_id NOT IN (%s)",
		tableName, strings.Join(placeholders, ","))
	if _, err := s.db.ExecContext(ctx, sqlStr, args...); err != nil {
		return fmt.Errorf("rag: delete orphan chunks for novel %d: %w", novelID, err)
	}
	return nil
}

// DeleteNovel 删除整部小说的向量表。
func (s *VectorStore) DeleteNovel(ctx context.Context, novelID int64) error {
	tableName := s.tableName(novelID)
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName),
	)
	if err != nil {
		return fmt.Errorf("rag: drop table for novel %d: %w", novelID, err)
	}
	s.ensuredOnce.Delete(novelID)
	s.bumpIndexEpoch() // 整表删除后使检索缓存失效
	s.log.Info("已删除小说向量表", "novel_id", novelID)
	return nil
}

// ── 全局单例 ──────────────────────────────────────────────

var (
	globalVSOnce sync.Once
	globalVS     *VectorStore
)

// InitVectorStore 初始化全局 VectorStore，多次调用只生效一次。
func InitVectorStore(db *sql.DB, embedder Embedder, log *slog.Logger) {
	globalVSOnce.Do(func() {
		globalVS = NewVectorStore(db, embedder, log)
	})
}

// tableHasColumn 检查虚拟表中是否存在指定列。vec0 不支持 PRAGMA，
// 所以通过查询后 Columns() 判断。返回 (exists, error)。
func tableHasColumn(ctx context.Context, db *sql.DB, tableName, col string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 0", tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return false, err
	}
	for _, c := range cols {
		if strings.EqualFold(c, col) {
			return true, nil
		}
	}
	return false, nil
}

// GetVectorStore 返回全局 VectorStore，未初始化时返回 nil。
func GetVectorStore() *VectorStore {
	return globalVS
}
