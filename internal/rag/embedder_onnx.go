//go:build cgo

package rag

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"runtime"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

const (
	bertMaxLen   = 512 // BERT 模型最大输入 token 数（含特殊 token）
	maxBatchSize = 8   // 单次 ONNX Run 最大样本数：批越大，注意力张量的峰值内存越高

	minEmbedThreads = 2 // 推理线程数下限：核少的机器也要能用
	maxEmbedThreads = 4 // 推理线程数上限：核多的机器不占满 CPU
)

// embeddingDim 是当前 embedding 模型的输出维度。
// 升级模型（如 bge-large-zh-v1.5 = 1024 维）时只改此常量，并重建向量库：
// vec0 虚拟表的 float[N] 维度写死在 schema 中，维度变更属于不兼容变更，必须重建。
// 此处集中定义，避免维度散落在 embedder 与 vector_store 的多个魔法数字里相互漂移。
const embeddingDim = 512 // bge-small-zh-v1.5 (int8)

// BGE 模型查询时需要加指令前缀，文档不加。
const queryInstruction = "为这个句子生成表示以用于检索相关文章："

const (
	memoryArenaShrinkKey   = "memory.enable_memory_arena_shrinkage"
	memoryArenaShrinkValue = "cpu:0"
	compactProbeText       = "内存整理"
)

var (
	instance  *OnnxEmbedder
	tokenizer *Tokenizer
	initOnce  sync.Once
	ready     = make(chan struct{})
	initErr   error
)

// OnnxEmbedder 使用 ONNX Runtime 加载 bge-small-zh-v1.5 (int8) 模型生成 embedding。
// 全局仅一个实例，通过 InitEmbedder 异步初始化。
type OnnxEmbedder struct {
	session   *ort.DynamicAdvancedSession
	tokenizer *Tokenizer
	clsID     int
	sepID     int
	mu        sync.Mutex
	log       *slog.Logger
	cache     *embedCache // 查询向量短 TTL 缓存（P0-2）
}

// InitEmbedder 加载 tokenizer（同步）后异步加载 ONNX 模型，不阻塞调用方。
// main 启动时尽早调用，模型在后台加载，GUI 可先行渲染。
// 调用后 GetTokenizer() 即可使用，无需等待模型就绪。
func InitEmbedder(modelsDir string, log *slog.Logger) {
	initOnce.Do(func() {
		t, err := NewTokenizer(filepath.Join(modelsDir, "vocab.txt"))
		if err != nil {
			initErr = err
			close(ready)
			return
		}
		tokenizer = t

		go func() {
			defer close(ready)
			e, err := newOnnxEmbedder(modelsDir, t, log)
			if err != nil {
				initErr = err
				return
			}
			instance = e
		}()
	})
}

// GetEmbedder 返回全局 embedder 实例。若尚未加载完成则阻塞等待。
func GetEmbedder() (*OnnxEmbedder, error) {
	<-ready
	if initErr != nil {
		return nil, initErr
	}
	return instance, nil
}

// TryGetEmbedder 非阻塞获取全局 embedder 实例。
// 若尚未初始化完成则返回 nil，不会阻塞。适用于 shutdown 等不能阻塞的场景。
func TryGetEmbedder() *OnnxEmbedder {
	select {
	case <-ready:
		if initErr != nil {
			return nil
		}
		return instance
	default:
		return nil
	}
}

// GetTokenizer 返回全局 tokenizer 实例。InitEmbedder 调用后即可使用，
// 未调用 InitEmbedder 时返回 nil。
func GetTokenizer() *Tokenizer {
	return tokenizer
}

func (e *OnnxEmbedder) Dim() int { return embeddingDim }

// embedThreads 返回批量推理使用的 ONNX 线程数：按 CPU 核数动态收缩，
// 避免核多的机器被推理占满，也避免核少的机器线程数过低而不可用。
func embedThreads() int {
	threads := runtime.NumCPU() / 4
	if threads < minEmbedThreads {
		threads = minEmbedThreads
	}
	if threads > maxEmbedThreads {
		threads = maxEmbedThreads
	}
	return threads
}

// newOnnxEmbedder 同步初始化 ONNX Runtime 并加载模型。仅由 InitEmbedder 调用。
func newOnnxEmbedder(modelsDir string, t *Tokenizer, log *slog.Logger) (*OnnxEmbedder, error) {
	modelPath := filepath.Join(modelsDir, "model.onnx")

	clsID, ok1 := t.vocab["[CLS]"]
	sepID, ok2 := t.vocab["[SEP]"]
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("rag: vocab missing [CLS] or [SEP]")
	}

	if err := ensureOrtEnv(); err != nil {
		return nil, fmt.Errorf("rag: init onnx environment: %w", err)
	}

	options, err := ort.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("rag: create session options: %w", err)
	}
	defer options.Destroy()

	// 保持 CPU arena 开启，使连续推理复用稳定的高水位内存；全量重建结束后由
	// Compact 显式 shrink。关闭 arena 会把释放行为交给 native allocator，Linux
	// 多线程推理下反而可能持续抬高 RSS。
	if err := options.SetCpuMemArena(true); err != nil {
		return nil, fmt.Errorf("rag: enable cpu mem arena: %w", err)
	}
	if err := options.SetMemPattern(false); err != nil {
		return nil, fmt.Errorf("rag: disable mem pattern: %w", err)
	}
	// 线程数按机器规模动态收缩；Run 已由 e.mu 串行化，inter-op 并行无意义。
	if err := options.SetIntraOpNumThreads(embedThreads()); err != nil {
		return nil, fmt.Errorf("rag: set intra op threads: %w", err)
	}
	if err := options.SetInterOpNumThreads(1); err != nil {
		return nil, fmt.Errorf("rag: set inter op threads: %w", err)
	}

	session, err := ort.NewDynamicAdvancedSession(modelPath,
		[]string{"input_ids", "attention_mask", "token_type_ids"},
		[]string{"last_hidden_state"}, options)
	if err != nil {
		return nil, fmt.Errorf("rag: load model: %w", err)
	}

	log.Info("ONNX embedder 已初始化", "model", modelPath)
	return &OnnxEmbedder{
		session:   session,
		tokenizer: t,
		clsID:     clsID,
		sepID:     sepID,
		log:       log,
		cache:     newEmbedCache(),
	}, nil
}

// ── 模型适配层 ────────────────────────────────────────────

// prepare 对原始 token 先截断再加 [CLS]/[SEP]，保证总长度 ≤ bertMaxLen。
func (e *OnnxEmbedder) prepare(text string) []int {
	raw := e.tokenizer.Tokenize(text)
	limit := bertMaxLen - 2
	if len(raw) > limit {
		e.log.Warn("token 序列超过模型上限，已截断", "len", len(raw), "max", limit)
		raw = raw[:limit]
	}
	ids := make([]int, len(raw)+2)
	ids[0] = e.clsID
	copy(ids[1:], raw)
	ids[len(ids)-1] = e.sepID
	return ids
}

// splitTokenWindows 将 token 序列按窗口大小非重叠切分，每窗 ≤ winSize。
// 用于长文分段聚合（P1）：每窗后续加 [CLS]/[SEP] 独立编码。纯函数，无 ONNX 依赖。
func splitTokenWindows(tokens []int, winSize int) [][]int {
	if winSize <= 0 {
		winSize = bertMaxLen - 2
	}
	if len(tokens) == 0 {
		return nil
	}
	var windows [][]int
	for off := 0; off < len(tokens); off += winSize {
		end := off + winSize
		if end > len(tokens) {
			end = len(tokens)
		}
		w := make([]int, end-off)
		copy(w, tokens[off:end])
		windows = append(windows, w)
	}
	return windows
}

// ── Embedding ────────────────────────────────────────────

func (e *OnnxEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// 查询向量走缓存：命中即跳过 ONNX（P0-2 核心收益）。
	if v, ok := e.cache.get(text); ok {
		return v, nil
	}
	vecs, err := e.embedLongTexts(ctx, []string{text}, nil, true)
	if err != nil {
		return nil, err
	}
	e.cache.add(text, vecs[0])
	return vecs[0], nil
}

// EmbedBatch 批量嵌入文档（不加 BGE 指令前缀），单次 ONNX Run 完成一批。
// 用于 IndexChunks / UpsertChunks 等文档索引——这些内容唯一且一次性，故绕过查询缓存。
// 长文自动分段聚合（P1）：维度恒定 embeddingDim，与既有向量库兼容。
func (e *OnnxEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	results := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := e.embedLongTexts(ctx, texts[i:end], nil, false)
		if err != nil {
			return nil, fmt.Errorf("rag: batch [%d:%d]: %w", i, end, err)
		}
		results = append(results, batch...)
	}
	return results, nil
}

// EmbedBatchQuery 批量嵌入查询语句（带 BGE 指令前缀），单次 ONNX Run 完成一批。
// 供多查询检索（如 V2-D 证据前置 ragEvidenceRetriever）合并推理，消除逐条 Embed 的串行开销。
// 每条查询命中 embedCache 时跳过推理；结果写入缓存供同会话后续重复查询复用。
// 长文自动分段聚合（P1）。
func (e *OnnxEmbedder) EmbedBatchQuery(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	results := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]
		vecs, err := e.embedLongTexts(ctx, batch, nil, true)
		if err != nil {
			return nil, fmt.Errorf("rag: batch query [%d:%d]: %w", i, end, err)
		}
		for j, t := range batch {
			e.cache.add(t, vecs[j])
		}
		results = append(results, vecs...)
	}
	return results, nil
}

// embedLongTexts 长文分段聚合（P1 核心）。
// 每条文本按 ≤(bertMaxLen-2) token 切窗，每窗加 [CLS]/[SEP] 独立编码为 embeddingDim 维向量，
// 再跨窗均值池化 + L2 归一，得到单条 embeddingDim 维向量。
// 短文本（单窗）与旧 prepare 截断路径逐字节等价；维度恒定，故无需重建向量库、不影响既有检索。
func (e *OnnxEmbedder) embedLongTexts(ctx context.Context, texts []string, runOptions *ort.RunOptions, withQuery bool) ([][]float32, error) {
	// 1. 构造每条文本的窗口序列（已含 [CLS]/[SEP]）。
	allSeqs := make([][]int, 0, len(texts))
	starts := make([]int, len(texts))
	counts := make([]int, len(texts))
	for i, text := range texts {
		raw := text
		if withQuery {
			raw = queryInstruction + text
		}
		tokens := e.tokenizer.Tokenize(raw)
		var seqs [][]int
		if len(tokens) == 0 {
			// 空文本：退化为 [CLS, SEP]，与旧 prepare("") 行为一致，避免产生零向量改变语义。
			seqs = [][]int{e.prepare(text)}
		} else {
			for _, w := range splitTokenWindows(tokens, bertMaxLen-2) {
				seq := make([]int, len(w)+2)
				seq[0] = e.clsID
				copy(seq[1:], w)
				seq[len(seq)-1] = e.sepID
				seqs = append(seqs, seq)
			}
		}
		starts[i] = len(allSeqs)
		counts[i] = len(seqs)
		allSeqs = append(allSeqs, seqs...)
	}

	// 2. 全部窗口批量编码（内部按 maxBatchSize 分批）。
	windowVecs, err := e.runSequences(ctx, allSeqs, runOptions)
	if err != nil {
		return nil, err
	}

	// 3. 跨窗均值池化 + L2 归一。
	results := make([][]float32, len(texts))
	for i := range texts {
		if counts[i] == 0 {
			results[i] = make([]float32, embeddingDim)
			continue
		}
		acc := make([]float32, embeddingDim)
		base := starts[i]
		for k := 0; k < counts[i]; k++ {
			v := windowVecs[base+k]
			for d := 0; d < embeddingDim; d++ {
				acc[d] += v[d]
			}
		}
		inv := float32(1) / float32(counts[i])
		var norm float64
		for d := range acc {
			acc[d] *= inv
			norm += float64(acc[d]) * float64(acc[d])
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for d := range acc {
				acc[d] /= float32(norm)
			}
		}
		results[i] = acc
	}
	return results, nil
}

// runSequences 对一批已带 [CLS]/[SEP] 的 token 序列（长度可不等，均 ≤ bertMaxLen）执行 ONNX 推理，
// 逐条 CLS 池化 + L2 归一，返回 embeddingDim 维向量。替代旧 embedBatch 内循环，供长文聚合复用。
// 样本数超过 maxBatchSize 时自动分批。
func (e *OnnxEmbedder) runSequences(ctx context.Context, seqs [][]int, runOptions *ort.RunOptions) ([][]float32, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if len(seqs) == 0 {
		return nil, nil
	}
	if len(seqs) > maxBatchSize {
		out := make([][]float32, 0, len(seqs))
		for i := 0; i < len(seqs); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(seqs) {
				end = len(seqs)
			}
			part, err := e.runSequences(ctx, seqs[i:end], runOptions)
			if err != nil {
				return nil, err
			}
			out = append(out, part...)
		}
		return out, nil
	}

	maxLen := 0
	for _, s := range seqs {
		if len(s) > maxLen {
			maxLen = len(s)
		}
	}
	if maxLen == 0 {
		results := make([][]float32, len(seqs))
		for i := range results {
			results[i] = make([]float32, embeddingDim)
		}
		return results, nil
	}

	batchSize := int64(len(seqs))
	seqLen := int64(maxLen)

	inputIDs := make([]int64, batchSize*seqLen)
	attentionMask := make([]int64, batchSize*seqLen)
	tokenTypeIDs := make([]int64, batchSize*seqLen)

	for i := int64(0); i < batchSize; i++ {
		ids := seqs[i]
		base := i * seqLen
		for j, id := range ids {
			inputIDs[base+int64(j)] = int64(id)
			attentionMask[base+int64(j)] = 1
		}
	}

	inputTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), inputIDs)
	if err != nil {
		return nil, fmt.Errorf("rag: create batch input tensor: %w", err)
	}
	defer inputTensor.Destroy()

	maskTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), attentionMask)
	if err != nil {
		return nil, fmt.Errorf("rag: create batch mask tensor: %w", err)
	}
	defer maskTensor.Destroy()

	typeIDsTensor, err := ort.NewTensor(ort.NewShape(batchSize, seqLen), tokenTypeIDs)
	if err != nil {
		return nil, fmt.Errorf("rag: create batch type_ids tensor: %w", err)
	}
	defer typeIDsTensor.Destroy()

	outputTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(batchSize, seqLen, embeddingDim))
	if err != nil {
		return nil, fmt.Errorf("rag: create batch output tensor: %w", err)
	}
	defer outputTensor.Destroy()

	e.mu.Lock()
	defer e.mu.Unlock()
	err = e.session.RunWithOptions(
		[]ort.Value{inputTensor, maskTensor, typeIDsTensor},
		[]ort.Value{outputTensor},
		runOptions,
	)
	if err != nil {
		return nil, fmt.Errorf("rag: batch onnx run: %w", err)
	}

	hidden := outputTensor.GetData()
	results := make([][]float32, batchSize)
	sampleSize := int(seqLen) * embeddingDim
	for i := int64(0); i < batchSize; i++ {
		start := int(i) * sampleSize
		results[i] = clsPool(hidden[start:start+sampleSize], embeddingDim)
	}

	return results, nil
}

// embedTruncated 短路径：沿用旧 prepare 截断语义（≤ bertMaxLen-2 token），供 Compact 探针等无需分段聚合的场景。
func (e *OnnxEmbedder) embedTruncated(ctx context.Context, texts []string, runOptions *ort.RunOptions, withQuery bool) ([][]float32, error) {
	seqs := make([][]int, len(texts))
	for i, t := range texts {
		if withQuery {
			seqs[i] = e.prepare(queryInstruction + t)
		} else {
			seqs[i] = e.prepare(t)
		}
	}
	return e.runSequences(ctx, seqs, runOptions)
}

// Compact 在一次极小推理结束时触发 ONNX Runtime CPU arena shrink。
// 用于完整索引重建后的任务级收尾；普通查询和增量刷新不应逐次调用。
func (e *OnnxEmbedder) Compact(ctx context.Context) error {
	options, err := ort.NewRunOptions()
	if err != nil {
		return fmt.Errorf("rag: create compact run options: %w", err)
	}
	defer options.Destroy()
	if err := options.AddRunConfigEntry(memoryArenaShrinkKey, memoryArenaShrinkValue); err != nil {
		return fmt.Errorf("rag: configure memory arena shrink: %w", err)
	}
	if _, err := e.embedTruncated(ctx, []string{compactProbeText}, options, false); err != nil {
		return fmt.Errorf("rag: compact memory arena: %w", err)
	}
	e.log.Info("ONNX CPU 内存池已整理")
	return nil
}

func (e *OnnxEmbedder) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.session != nil {
		e.session.Destroy()
		e.session = nil
	}
	return nil
}

// ── Pooling ──────────────────────────────────────────────

// clsPool 取 [CLS] token 的 embedding 并做 L2 归一化。
// BGE 模型使用 CLS 池化 + L2 归一化，优于均值池化。
func clsPool(hidden []float32, dim int) []float32 {
	vec := make([]float32, dim)
	copy(vec, hidden[:dim])
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= float32(norm)
		}
	}
	return vec
}
