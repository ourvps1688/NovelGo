package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/pattern"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/novelgo/novelgo/internal/voice"
)

type ExtractPatternInput = pattern.ExtractPatternInput
type ExtractPatternResult = pattern.ExtractPatternResult
type ExtractPatternAllResult = pattern.ExtractPatternAllResult
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
		VoiceSink:         a.upsertVoiceProfile,          // P1 双写：结构化维度并行落库（skill md 路径不变）
	}
	result, err := extractor.Extract(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("提取模式失败: %w", err)
	}
	return result, nil
}

// ExtractPatternAll 一次共享前缀、多份产物（套路 + 心智）。
// Flavors 字段为空时退化为单 flavor（等价于 ExtractPattern）。
func (a *App) ExtractPatternAll(input ExtractPatternInput) (*ExtractPatternAllResult, error) {
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
		VoiceSink:         a.upsertVoiceProfile,          // P1 双写：结构化维度并行落库（skill md 路径不变）
	}
	result, err := extractor.ExtractAll(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("提取模式失败: %w", err)
	}
	return result, nil
}

// upsertVoiceProfile 是 P1 双写的落库接线（WBS §0.3）：把 mental 提取产出的
// 结构化 Voice 维度并行写入 voice_dimensions（source=pattern_mental）。
// skill md 产出路径一字不动；单条写入失败仅记日志不阻断提取；
// 空清单不触发（LLM 未产出 → 降级不落库）。
func (a *App) upsertVoiceProfile(novelID int64, items []voice.ProfileItem) {
	if len(items) == 0 {
		return
	}
	store := voice.NewStore(a.db, a.logger)
	for _, it := range items {
		entry := &voice.Entry{
			NovelID:   novelID,
			Dimension: it.Dimension,
			Content:   it.Content,
			Metrics:   it.MetricsJSON(),
			Evidence:  it.EvidenceJSON(),
			Source:    string(voice.SourcePatternMental),
		}
		if err := store.Upsert(a.ctx, entry); err != nil {
			a.logger.Warn("voice 台账双写跳过", "novel_id", novelID, "dimension", it.Dimension, "err", err)
		}
	}
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
