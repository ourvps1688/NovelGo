//go:build cgo

package rag

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// P0-1 二阶段 Reranker：在向量粗排（余弦）之后，用本地交叉编码器对候选做精排。
// 复用 embedder 的范式（单例 + 异步加载 + e.mu 串行 Run + arena 治理），但输入/输出
// 完全不同：输入是 (query, passage) 拼接的交叉编码，输出是每对的相关度 logit。
//
// 优雅降级：reranker 模型缺失或推理失败时，调用方（Rerank）回落到 MMR，行为不变。

const (
	rerankerModelFile  = "reranker.onnx" // 由 scripts/download-onnx.sh 下载
	rerankerVocabFile  = "reranker_vocab.txt"
	rerankerOutputName = "logits" // Xenova/bge-reranker-base 的 ONNX 输出名（单对相关度 logit）
	rerankerOutDim     = 1        // 单对相关度 logit；若换用 num_labels=2 的导出，需改为 2
)

var (
	rerankerInstance *OnnxReranker
	rerankerOnce     sync.Once
	rerankerReady    = make(chan struct{})
	rerankerInitErr  error
)

// OnnxReranker 是 bge-reranker-base（或兼容交叉编码器）的 ONNX 封装。
type OnnxReranker struct {
	session   *ort.DynamicAdvancedSession
	tokenizer *Tokenizer
	clsID     int
	sepID     int
	mu        sync.Mutex
	log       *slog.Logger
}

// InitReranker 与 InitEmbedder 对称：同步加载 tokenizer 后异步加载 reranker 模型，不阻塞调用方。
// reranker 是可选增强——词表或模型缺失时仅记录错误并 close(rerankerReady)，TryGetReranker 返回 nil，
// 调用方回落 MMR，不影响应用启动与运行。
func InitReranker(modelsDir string, log *slog.Logger) {
	rerankerOnce.Do(func() {
		t, err := NewTokenizer(filepath.Join(modelsDir, rerankerVocabFile))
		if err != nil {
			rerankerInitErr = fmt.Errorf("rag: load reranker vocab: %w", err)
			close(rerankerReady)
			return
		}
		go func() {
			defer close(rerankerReady)
			r, err := newOnnxReranker(modelsDir, t, log)
			if err != nil {
				rerankerInitErr = err
				return
			}
			rerankerInstance = r
		}()
	})
}

// GetReranker 阻塞等待 reranker 就绪并返回；若加载失败返回错误。
func GetReranker() (*OnnxReranker, error) {
	<-rerankerReady
	if rerankerInitErr != nil {
		return nil, rerankerInitErr
	}
	return rerankerInstance, nil
}

// TryGetReranker 非阻塞获取 reranker 实例；未就绪或加载失败返回 nil（优雅降级）。
func TryGetReranker() *OnnxReranker {
	select {
	case <-rerankerReady:
		if rerankerInitErr != nil {
			return nil
		}
		return rerankerInstance
	default:
		return nil
	}
}

func newOnnxReranker(modelsDir string, t *Tokenizer, log *slog.Logger) (*OnnxReranker, error) {
	modelPath := filepath.Join(modelsDir, rerankerModelFile)

	clsID, ok1 := t.vocab["[CLS]"]
	sepID, ok2 := t.vocab["[SEP]"]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("rag: reranker vocab missing [CLS] or [SEP]")
	}

	if err := ensureOrtEnv(); err != nil {
		return nil, fmt.Errorf("rag: init onnx environment: %w", err)
	}

	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("rag: create reranker session options: %w", err)
	}
	defer options.Destroy()

	// 与 embedder 一致的治理：保持 CPU arena、关闭 mem pattern、推理线程按核数收缩。
	if err := options.SetCpuMemArena(true); err != nil {
		return nil, fmt.Errorf("rag: enable cpu mem arena: %w", err)
	}
	if err := options.SetMemPattern(false); err != nil {
		return nil, fmt.Errorf("rag: disable mem pattern: %w", err)
	}
	if err := options.SetIntraOpNumThreads(embedThreads()); err != nil {
		return nil, fmt.Errorf("rag: set intra op threads: %w", err)
	}
	if err := options.SetInterOpNumThreads(1); err != nil {
		return nil, fmt.Errorf("rag: set inter op threads: %w", err)
	}

	session, err := ort.NewDynamicAdvancedSession(modelPath,
		[]string{"input_ids", "attention_mask"},
		[]string{rerankerOutputName}, options)
	if err != nil {
		return nil, fmt.Errorf("rag: load reranker model: %w", err)
	}

	log.Info("ONNX reranker 已初始化", "model", modelPath)
	return &OnnxReranker{
		session:   session,
		tokenizer: t,
		clsID:     clsID,
		sepID:     sepID,
		log:       log,
	}, nil
}

// encodePair 构造交叉编码器输入：
//
//	input_ids = [CLS] query [SEP] passage [SEP]
//
// 总长受限 bertMaxLen：优先保留 query，passage 超出则截断（检索场景 passage 信息量更大但更长）。
//
// 注意：bge-reranker-base（及 bge-reranker-v2-m3）仅需 input_ids + attention_mask，
// 不接收 token_type_ids，query/passage 的分隔依赖 [SEP]，故此处不构造 segment id。
func (r *OnnxReranker) encodePair(query, passage string) []int {
	q := r.tokenizer.Tokenize(query)
	p := r.tokenizer.Tokenize(passage)

	maxPairTokens := bertMaxLen - 3 // [CLS] + 2×[SEP]
	room := maxPairTokens - len(q)
	if room < 0 {
		q = q[:maxPairTokens]
		room = 0
	}
	if room < len(p) {
		p = p[:room]
	}

	ids := make([]int, 0, len(q)+len(p)+3)
	ids = append(ids, r.clsID)
	ids = append(ids, q...)
	ids = append(ids, r.sepID)
	ids = append(ids, p...)
	ids = append(ids, r.sepID)
	return ids
}

// Score 对 (query, passage) 批量打分，返回每个 passage 的相关度 logit（降序无关，按输入顺序）。
func (r *OnnxReranker) Score(ctx context.Context, query string, passages []string) ([]float32, error) {
	if len(passages) == 0 {
		return nil, nil
	}
	if len(passages) <= maxBatchSize {
		return r.scoreBatch(ctx, query, passages)
	}
	scores := make([]float32, 0, len(passages))
	for i := 0; i < len(passages); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(passages) {
			end = len(passages)
		}
		b, err := r.scoreBatch(ctx, query, passages[i:end])
		if err != nil {
			return nil, err
		}
		scores = append(scores, b...)
	}
	return scores, nil
}

// scoreBatch 单次 ONNX 推理对一批 (query, passage) 打分；调用方保证 len(passages) ≤ maxBatchSize。
func (r *OnnxReranker) scoreBatch(ctx context.Context, query string, passages []string) ([]float32, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	inputIDsList := make([][]int, len(passages))
	maxLen := 0
	for i, p := range passages {
		ids := r.encodePair(query, p)
		inputIDsList[i] = ids
		if len(ids) > maxLen {
			maxLen = len(ids)
		}
	}
	if maxLen == 0 {
		return make([]float32, len(passages)), nil
	}

	batchSize := int64(len(passages))
	seqLen := int64(maxLen)

	inputIDs := make([]int64, batchSize*seqLen)
	attentionMask := make([]int64, batchSize*seqLen)
	for i := int64(0); i < batchSize; i++ {
		ids := inputIDsList[i]
		base := i * seqLen
		for j, id := range ids {
			inputIDs[base+int64(j)] = int64(id)
			attentionMask[base+int64(j)] = 1
		}
	}

	inputTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), inputIDs)
	if err != nil {
		return nil, fmt.Errorf("rag: create reranker input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	maskTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), attentionMask)
	if err != nil {
		return nil, fmt.Errorf("rag: create reranker mask tensor: %w", err)
	}
	defer maskTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(batchSize, rerankerOutDim))
	if err != nil {
		return nil, fmt.Errorf("rag: create reranker output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.session.Run(
		[]ort.Value{inputTensor, maskTensor},
		[]ort.Value{outputTensor},
	); err != nil {
		return nil, fmt.Errorf("rag: reranker onnx run: %w", err)
	}

	data := outputTensor.GetData() // 长度 = batchSize * rerankerOutDim
	scores := make([]float32, batchSize)
	for i := int64(0); i < batchSize; i++ {
		scores[i] = data[i*rerankerOutDim] // rerankerOutDim==1 时等价于 data[i]
	}
	return scores, nil
}

// Close 释放 reranker ONNX 会话。
func (r *OnnxReranker) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		r.session.Destroy()
		r.session = nil
	}
	return nil
}

// Rerank 二阶段精排的统一入口：
//   - reranker 已加载：用本地交叉编码器对候选打分，按分数降序返回前 topK，并回填 Relevance；
//   - reranker 未加载（或推理失败）：回落 MMRRerank，保持历史行为与优雅降级不变量。
//
// 因此：未部署 reranker.onnx 时，所有调用方行为完全不变；部署后自动获得精排增益。
func Rerank(ctx context.Context, query string, candidates []SearchResult, topK int) []SearchResult {
	rk := TryGetReranker()
	if rk == nil {
		return MMRRerank(query, candidates, topK, 0.7)
	}

	texts := make([]string, len(candidates))
	for i, c := range candidates {
		texts[i] = c.Content
	}
	scores, err := rk.Score(ctx, query, texts)
	if err != nil || len(scores) != len(candidates) {
		// 模型推理失败也降级到 MMR，绝不阻断检索。
		if err != nil {
			slog.Warn("reranker 推理失败，降级 MMR", "err", err)
		}
		return MMRRerank(query, candidates, topK, 0.7)
	}

	order := make([]int, len(candidates))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})

	if topK > len(order) {
		topK = len(order)
	}
	out := make([]SearchResult, topK)
	for i := 0; i < topK; i++ {
		idx := order[i]
		c := candidates[idx]
		c.Relevance = float64(scores[idx])
		out[i] = c
	}
	return out
}
