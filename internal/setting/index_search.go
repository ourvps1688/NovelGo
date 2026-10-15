package setting

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/rag"
)

// BuildSettingChunk 组装一条设定用于向量索引的 Chunk。
// chunk_id 用稳定格式 "setting:<id>"，Content 用 "分类：内容" 便于语义检索命中主题词。
func BuildSettingChunk(item SettingItem) rag.Chunk {
	return rag.Chunk{
		ID:           settingChunkID(item.ID),
		Content:      buildSettingChunkText(item.Category, item.Content),
		ChapterID:    0,
		ChunkType:    SettingChunkType,
		ChunkIndex:   0,
		StartRunePos: 0,
	}
}

// buildSettingChunkText 将设定组装为可检索文本：分类做主题前缀，提升语义命中率。
// 仅在首个"："处拆分，避免正文本身含"："被误拆（splitCatContent 对称处理）。
func buildSettingChunkText(category, content string) string {
	cat := strings.TrimSpace(category)
	if cat == "" {
		return content
	}
	return cat + "：" + content
}

// IndexSettingByID 按 ID 查设定并写入向量库（幂等 upsert）。
// 向量库未初始化（无 ONNX）时静默跳过，连查库都省；
// 设定已删除（查不到）则清理可能残留的向量块。
func IndexSettingByID(ctx context.Context, db *gorm.DB, novelID, settingID int64) error {
	vs := rag.GetVectorStore()
	if vs == nil {
		return nil
	}
	var item SettingItem
	if err := db.WithContext(ctx).First(&item, settingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return vs.DeleteChunk(ctx, novelID, settingChunkID(settingID))
		}
		return fmt.Errorf("query setting for index: %w", err)
	}
	return vs.UpsertChunks(ctx, novelID, []rag.Chunk{BuildSettingChunk(item)})
}

// DeleteSettingVector 删除单条设定的向量块。
// 向量库未初始化或表不存在时静默 no-op（该 novel 从未索引过设定）。
func DeleteSettingVector(ctx context.Context, novelID, settingID int64) error {
	vs := rag.GetVectorStore()
	if vs == nil {
		return nil
	}
	return vs.DeleteChunk(ctx, novelID, settingChunkID(settingID))
}

// ReindexNovelSettings 重索引整本小说的所有设定（冷启动/补建用）。
// 供 rag 的 RebuildAll 钩子调用；向量库未初始化时静默跳过。
func ReindexNovelSettings(ctx context.Context, db *gorm.DB, novelID int64) error {
	vs := rag.GetVectorStore()
	if vs == nil {
		return nil
	}
	var items []SettingItem
	if err := db.WithContext(ctx).Where("novel_id = ?", novelID).Find(&items).Error; err != nil {
		return fmt.Errorf("reindex settings: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	chunks := make([]rag.Chunk, 0, len(items))
	for _, it := range items {
		chunks = append(chunks, BuildSettingChunk(it))
	}
	return vs.UpsertChunks(ctx, novelID, chunks)
}

// SearchSettingsResult 是语义检索设定返回的条目。
type SearchSettingsResult struct {
	SettingID int64
	Category  string
	Content   string
	Relevance float64
}

// SearchSettings 按查询语义检索最相关设定。
// 向量库可用时走语义检索（仅检索设定块），不可用（无 ONNX）时退化为 DB LIKE 关键词检索。
func SearchSettings(ctx context.Context, db *gorm.DB, novelID int64, query string, topK int) ([]SearchSettingsResult, error) {
	if topK <= 0 {
		topK = 5
	}
	if vs := rag.GetVectorStore(); vs != nil {
		results, err := vs.Search(ctx, novelID, query, topK, &rag.SearchFilter{ChunkTypes: []string{SettingChunkType}})
		if err == nil {
			return mapSettingResults(results), nil
		}
		// 语义检索失败不致命，退化为关键词检索。
		dbResults, derr := keywordSearchSettings(ctx, db, novelID, query, topK)
		if derr != nil {
			return nil, err
		}
		return dbResults, nil
	}
	return keywordSearchSettings(ctx, db, novelID, query, topK)
}

// mapSettingResults 将向量检索结果还原为设定条目（去掉主题前缀、解析 setting_id）。
func mapSettingResults(results []rag.SearchResult) []SearchSettingsResult {
	out := make([]SearchSettingsResult, 0, len(results))
	for _, r := range results {
		id := parseSettingID(r.ChunkID)
		if id == 0 {
			continue
		}
		cat, content := splitCatContent(r.Content)
		out = append(out, SearchSettingsResult{
			SettingID: id,
			Category:  cat,
			Content:   content,
			Relevance: r.Relevance,
		})
	}
	return out
}

// keywordSearchSettings 无 ONNX 时的退化检索：按 content/category LIKE 模糊匹配。
func keywordSearchSettings(ctx context.Context, db *gorm.DB, novelID int64, query string, topK int) ([]SearchSettingsResult, error) {
	q := strings.TrimSpace(query)
	tx := db.WithContext(ctx).Model(&SettingItem{}).Where("novel_id = ?", novelID)
	if q != "" {
		tx = tx.Where("content LIKE ? OR category LIKE ?", "%"+q+"%", "%"+q+"%")
	}
	var items []SettingItem
	if err := tx.Order("updated_at DESC").Limit(topK).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("keyword search settings: %w", err)
	}
	out := make([]SearchSettingsResult, 0, len(items))
	for _, it := range items {
		out = append(out, SearchSettingsResult{
			SettingID: it.ID,
			Category:  it.Category,
			Content:   it.Content,
			Relevance: 0,
		})
	}
	return out, nil
}
