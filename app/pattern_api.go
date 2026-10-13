package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/pattern"
	"github.com/novelgo/novelgo/internal/rag"
)

type ExtractPatternInput = pattern.ExtractPatternInput
type ExtractPatternResult = pattern.ExtractPatternResult
type ExtractPatternProgress = pattern.Progress

// ExtractPattern 从指定小说中提取叙事套路模式
func (a *App) ExtractPattern(input ExtractPatternInput) (*ExtractPatternResult, error) {
	if a.llmClient == nil {
		return nil, fmt.Errorf("LLM 客户端未初始化")
	}
	if a.chapter == nil {
		return nil, fmt.Errorf("章节存储未初始化")
	}

	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	key := fmt.Sprintf("%s%d", agent.CancelPrefixPattern, input.NovelID)
	a.cancelMgr.Cancel(key)
	a.cancelMgr.Register(key, cancel)
	defer func() {
		if ctx.Err() == nil {
			a.cancelMgr.Unregister(key)
		}
	}()

	extractor := &pattern.Extractor{
		Chapters:          a.chapter,
		LLMClient:         a.llmClient,
		DB:                a.db,
		Progress:          a.emitExtractPatternProgress,
		EvidenceRetriever: newMentalEvidenceRetriever(a), // V2-D：向量证据前置（无 ONNX 时自动为 nil）
	}
	result, err := extractor.Extract(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("提取模式失败: %w", err)
	}
	return result, nil
}

// CancelExtractPattern 取消正在进行的小说模式提取
func (a *App) CancelExtractPattern(novelID int64) {
	a.cancelMgr.Cancel(fmt.Sprintf("%s%d", agent.CancelPrefixPattern, novelID))
}

func (a *App) emitExtractPatternProgress(progress pattern.Progress) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "pattern:progress", progress)
}

// ragEvidenceRetriever 是 V2-D 的向量证据检索器：从本书向量索引取 top-K 片段，
// 注入 mental flavor 的最终提示词，减少 LLM 重读全本。仅在 ONNX 嵌入/向量索引可用时启用。
type ragEvidenceRetriever struct {
	store *rag.VectorStore
	topK  int
}

func (r *ragEvidenceRetriever) RetrieveEvidence(ctx context.Context, novelID int64, queries []string, topK int) ([]string, error) {
	if r.store == nil {
		return nil, nil
	}
	k := topK
	if k <= 0 {
		k = r.topK
	}
	if k <= 0 {
		k = 4
	}
	var out []string
	for _, q := range queries {
		res, err := r.store.Search(ctx, novelID, q, k, nil)
		if err != nil {
			return nil, err
		}
		for _, it := range res {
			c := strings.TrimSpace(it.Content)
			if c != "" {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// newMentalEvidenceRetriever 在向量索引可用时返回检索器，否则返回 nil（V2-D 自动跳过）。
func newMentalEvidenceRetriever(a *App) pattern.EvidenceRetriever {
	store := a.vectorStore.Load()
	if store == nil {
		return nil
	}
	return &ragEvidenceRetriever{store: store, topK: 4}
}
