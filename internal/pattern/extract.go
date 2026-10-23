package pattern

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/voice"
)

const (
	outputBoundariesTool       = "output_boundary_hints"
	outputChapterSummariesTool = "output_chapter_summaries"
	outputChunksTool           = "output_chunks"
	outputFidelityTool         = "output_fidelity"
	finalChunkBudget           = 80_000
	defaultContextWindow       = 128_000
	// mentalFinalMaxTokens 是 V2-A 心智技能最终生成的 token 护栏：限制整体产出体量，
	// 并要求「叙事心智模型」一节控制在约 1.5k–2.5k token（见 mentalFinalSkillSystemPrompt）。
	mentalFinalMaxTokens = 8_000
	// 多镜头精提（V3 P1）三路聚焦生成的 token 预算：
	//   expr 6k + mental 8k + decision 6k。心智镜头含 ≤3k 正文 + name/description/evidence，
	//   JSON 转义后更长；思考模型（SupportsThinking 默认开启）的 reasoning token 也计入 max_tokens，
	//   原 3k/2k 预算会被 max_tokens 截断导致 tool arguments JSON 不完整，现已上调。
	//   三路为相互独立的 callTool 调用，故每段预算只需 ≤ 单轮护栏 mentalFinalMaxTokens(8k)，
	//   不要求三者之和 ≤ 8k（旧 7k 之和约束已不适用）。
	lensExprMaxTokens     = 6_000 // 表达镜头
	lensMentalMaxTokens   = 8_000 // 心智镜头（主镜头，含 name/description/evidence）
	lensDecisionMaxTokens = 6_000 // 决策镜头
)

// EvidenceRetriever 是 V2-D 的可选证据检索器。当 Extractor 持有非 nil 实例时，
// mental flavor 的最终提示词会前置来自全书向量检索的证据片段，减少 LLM 重读全本。
// 缺失（如本机无 ONNX 嵌入）时自动跳过，不影响主流程。
type EvidenceRetriever interface {
	// RetrieveEvidence 返回与 queries 相关的证据文本片段（已截断）。topK<=0 时使用实现默认值。
	RetrieveEvidence(ctx context.Context, novelID int64, queries []string, topK int) ([]string, error)
}

// mentalEvidenceQueries 是 V2-D 注入向量检索的种子查询，覆盖心智提取的四个关注面。
var mentalEvidenceQueries = []string{
	"叙事冲突如何生成与解决",
	"作者做出关键创作决策的场景",
	"独特的句式与表达习惯",
	"作者刻意回避或拒绝的写法",
}

type Extractor struct {
	Chapters  *chapter.Store
	LLMClient *llm.Client
	DB        *gorm.DB
	Progress  func(Progress)
	// EvidenceRetriever 可选；非 nil 时启用 V2-D 向量证据前置。
	EvidenceRetriever EvidenceRetriever
	// VoiceSink 可选；mental 提取产出 voice_profile 时回调（P1 双写，WBS §0.3）。
	// LLM 未产出（空清单）不触发——降级不落库；skill md 产出路径不经过此回调。
	VoiceSink func(novelID int64, items []voice.ProfileItem)
}

// Extract 执行完整的模式提取 Pipeline：Pre-step 边界分析 → Step 1 章节摘要 → Step 2 递归压缩 → Step 3 生成技能
// prefixResult 是共享前缀（Pre-step 边界 + Step1 摘要 + Step2 压缩）的产出，
// 可供多个 flavor 的 final 阶段复用，避免对全书的多遍重读。
type prefixResult struct {
	chunks       []Chunk
	trace        *Trace
	chapterCount int
}

func (e *Extractor) Extract(ctx context.Context, input ExtractPatternInput) (*ExtractPatternResult, error) {
	pr, err := e.runPrefix(ctx, &input)
	if err != nil {
		return nil, err
	}
	flavor := input.Flavor
	if flavor == "" {
		flavor = string(FlavorStruct)
	}
	return e.runFinal(ctx, input, pr, flavor)
}

// runPrefix 执行与 flavor 无关的共享前缀：加载章节 → 结构边界 → 章节摘要 → 递归压缩叙事阶段块。
// 接收 *ExtractPatternInput 以便回填自动生成的 TaskID 给调用方。
func (e *Extractor) runPrefix(ctx context.Context, input *ExtractPatternInput) (*prefixResult, error) {
	if e.Chapters == nil {
		return nil, fmt.Errorf("章节存储未初始化")
	}
	if e.LLMClient == nil {
		return nil, fmt.Errorf("LLM 客户端未初始化")
	}
	if input.NovelID <= 0 {
		return nil, fmt.Errorf("novel_id 不能为空")
	}
	if input.ProviderName == "" || input.ModelID == "" {
		return nil, fmt.Errorf("provider_name 和 model_id 不能为空")
	}
	if input.TaskID == "" {
		input.TaskID = fmt.Sprintf("pattern-%d", time.Now().UnixNano())
	}

	chapters, err := e.loadChapters(ctx, input.NovelID, input.ChapterIDs)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("请先导入或创建章节，然后再提取模式")
	}
	if len(chapters) < 5 {
		return nil, fmt.Errorf("章节数量不足，模式提取至少需要 5 个有内容的章节")
	}
	readingNumbers := readingNumbersFromChapters(chapters)

	contextWindow := e.modelContextWindow(input.ProviderName, input.ModelID)
	budget := batchBudget(contextWindow)
	trace := &Trace{
		TaskID:        input.TaskID,
		NovelID:       input.NovelID,
		ChapterCount:  len(chapters),
		ContextWindow: contextWindow,
		BatchBudget:   budget,
	}
	e.emit(*input, Progress{
		Stage:   StageLoaded,
		Message: fmt.Sprintf("已加载 %d 章，开始分析结构边界", len(chapters)),
	})

	// Pre-step：章节标题分析，获取疑似阶段边界
	e.emit(*input, Progress{
		Stage:   StageBoundaries,
		Message: "正在分析结构边界...",
	})
	boundaries, err := e.preAnalyzeBoundaries(ctx, *input, chapters)
	if err != nil {
		return nil, err
	}
	trace.Boundaries = boundaries
	e.emit(*input, Progress{
		Stage:      StageBoundaries,
		Message:    fmt.Sprintf("已找到 %d 条疑似叙事边界", len(boundaries)),
		Boundaries: boundaries,
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Step 1：增量生成章节摘要（已有摘要的跳过）
	summaries, err := e.ensureSummaries(ctx, *input, chapters, boundaries, budget)
	if err != nil {
		return nil, err
	}
	trace.Summaries = summaries
	e.emit(*input, Progress{
		Stage:   StageSummaries,
		Message: fmt.Sprintf("已准备 %d 条章节摘要", len(summaries)),
	})
	if len(summaries) == 0 {
		return nil, fmt.Errorf("没有可用的章节摘要，无法提取模式")
	}

	// Step 2 第 1 轮：摘要 → 初始叙事阶段块
	e.emit(*input, Progress{
		Stage:   StageInitialChunks,
		Message: "正在生成初始叙事阶段块...",
		Round:   1,
	})
	chunks, roundTrace, err := e.initialChunks(ctx, *input, summaries, budget)
	if err != nil {
		return nil, err
	}
	trace.ChunkRounds = append(trace.ChunkRounds, roundTrace)
	e.emit(*input, Progress{
		Stage:   StageInitialChunks,
		Message: fmt.Sprintf("第 1 轮生成 %d 个叙事阶段块", len(chunks)),
		Round:   1,
		Tokens:  tokensOfChunks(chunks),
		Chunks:  chunks,
	})
	if len(chunks) == 0 {
		return nil, fmt.Errorf("LLM 未返回任何叙事阶段块")
	}

	// Step 2 后续轮：递归压缩，直到总 token ≤ 阈值
	stallCount := 0
	const minReductionRatio = 0.05 // 每轮至少减少5%才视为有效收敛
	const maxStallRounds = 2       // 连续2轮收敛不足则终止
	for round := 2; tokensOfChunks(chunks) > finalChunkBudget; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		e.emit(*input, Progress{
			Stage:   StageCompressChunks,
			Message: fmt.Sprintf("第 %d 轮压缩中...", round),
			Round:   round,
			Tokens:  tokensOfChunks(chunks),
		})
		prevTokens := tokensOfChunks(chunks)
		next, roundTrace, err := e.compressChunks(ctx, *input, chunks, readingNumbers, budget, round)
		if err != nil {
			return nil, err
		}
		nextTokens := tokensOfChunks(next)
		if len(next) == 0 {
			// 退避 30s 后重试一次，LLM 偶尔可能返回空结果
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			next, roundTrace, err = e.compressChunks(ctx, *input, chunks, readingNumbers, budget, round)
			if err != nil {
				return nil, fmt.Errorf("第%d轮压缩未返回任何叙事阶段块，重试也失败: %w", round, err)
			}
			if len(next) == 0 {
				return nil, fmt.Errorf("第%d轮压缩重试后仍未返回任何叙事阶段块，无法继续生成最终技能", round)
			}
			nextTokens = tokensOfChunks(next)
			e.emit(*input, Progress{
				Stage:   StageCompressChunks,
				Message: fmt.Sprintf("第 %d 轮重试后得到 %d 个叙事阶段块", round, len(next)),
				Round:   round,
				Tokens:  nextTokens,
				Chunks:  next,
			})
		} else {
			e.emit(*input, Progress{
				Stage:   StageCompressChunks,
				Message: fmt.Sprintf("第 %d 轮压缩为 %d 个叙事阶段块", round, len(next)),
				Round:   round,
				Tokens:  nextTokens,
				Chunks:  next,
			})
		}
		trace.ChunkRounds = append(trace.ChunkRounds, roundTrace)
		if nextTokens >= prevTokens {
			// 压缩未降低 token 量：若当前 chunks 仍在模型上下文窗口 80% 以内，容忍并退出循环
			if prevTokens < int(float64(contextWindow)*0.8) {
				break
			}
			return nil, fmt.Errorf("第%d轮压缩未降低 token 量（%d -> %d），最终输入仍超过预算 %d", round, prevTokens, nextTokens, finalChunkBudget)
		}
		reduction := float64(prevTokens-nextTokens) / float64(prevTokens)
		if reduction < minReductionRatio {
			stallCount++
			if stallCount >= maxStallRounds {
				if tokensOfChunks(next) < int(float64(contextWindow)*0.8) {
					chunks = next
					break
				}
				return nil, fmt.Errorf("连续%d轮压缩收敛不足（降幅<%.0f%%），最终输入仍超过预算 %d", maxStallRounds, minReductionRatio*100, finalChunkBudget)
			}
		} else {
			stallCount = 0
		}
		chunks = next
	}

	trace.FinalTokens = tokensOfChunks(chunks)
	return &prefixResult{chunks: chunks, trace: trace, chapterCount: len(chapters)}, nil
}

// runFinal 执行 Step 3（final 技能生成）并将产物解析、净化文件名、组装为 ExtractPatternResult。
// flavor 决定走套路模板（struct）还是创作心智（mental）。
func (e *Extractor) runFinal(ctx context.Context, input ExtractPatternInput, pr *prefixResult, flavor string) (*ExtractPatternResult, error) {
	finalInput := input
	finalInput.Flavor = flavor
	e.emit(finalInput, Progress{
		Stage:   StageFinalizing,
		Message: "正在生成最终技能...",
		Tokens:  pr.trace.FinalTokens,
		Chunks:  pr.chunks,
	})
	outcome, err := e.finalSkill(ctx, finalInput, pr.chunks, pr.chapterCount)
	if err != nil {
		return nil, err
	}
	sk, err := skill.ParseBytes([]byte(outcome.Raw), "ai")
	if err != nil {
		return nil, fmt.Errorf("解析生成的技能失败: %w", err)
	}

	safeName := skill.SanitizeFileName(sk.Name)
	e.emit(finalInput, Progress{
		Stage:   StageDone,
		Message: fmt.Sprintf("已生成「%s」", sk.Name),
		Tokens:  pr.trace.FinalTokens,
	})
	// P1 双写（WBS §0.3）：结构化维度经 VoiceSink 并行落库；skill md 产出路径不变。
	// LLM 未产出（空清单）不触发——降级不落库，不解析正文兜底猜测。
	if len(outcome.VoiceProfile) > 0 && e.VoiceSink != nil {
		e.VoiceSink(finalInput.NovelID, outcome.VoiceProfile)
	}
	return &ExtractPatternResult{
		TaskID:        finalInput.TaskID,
		Name:          sk.Name,
		Description:   sk.Description,
		RawContent:    sk.RawContent,
		FilePath:      fmt.Sprintf("~/.novelgo/skills/%s.md", safeName),
		Flavor:        flavor,
		Fidelity:      outcome.Fidelity,
		DemotedModels: outcome.DemotedModels,
		Trace:         pr.trace,
	}, nil
}

// ExtractAll 一次共享前缀、多份产物（套路 + 心智）。
// Flavors 非空时按其中每项生成对应 flavor 的技能；为空则退化为单 flavor（与 Extract 等价）。
// 套路与心智复用同一套边界/摘要/压缩结果，仅 final 阶段分别调用，省去对全书的多遍重读。
func (e *Extractor) ExtractAll(ctx context.Context, input ExtractPatternInput) (*ExtractPatternAllResult, error) {
	pr, err := e.runPrefix(ctx, &input)
	if err != nil {
		return nil, err
	}
	flavors := input.Flavors
	if len(flavors) == 0 {
		f := input.Flavor
		if f == "" {
			f = string(FlavorStruct)
		}
		flavors = []string{f}
	}

	out := &ExtractPatternAllResult{TaskID: input.TaskID}
	for _, fl := range flavors {
		r, err := e.runFinal(ctx, input, pr, fl)
		if err != nil {
			return nil, err
		}
		if Flavor(fl) == FlavorMental {
			out.Mental = r
		} else {
			out.Struct = r
		}
	}
	return out, nil
}

// loadChapters 加载指定 Novel 的章节，支持按 ID 过滤
func (e *Extractor) loadChapters(ctx context.Context, novelID int64, ids []int64) ([]ChapterSource, error) {
	dbChapters, err := e.Chapters.ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		return nil, err
	}
	idSet := map[int64]bool{}
	if len(ids) > 0 {
		for _, id := range ids {
			idSet[id] = true
		}
	}
	out := make([]ChapterSource, 0, len(dbChapters))
	for _, ch := range dbChapters {
		if len(idSet) > 0 && !idSet[ch.ID] {
			continue
		}
		content, err := git.ReadFile(novelID, git.ChapterPath(ch.ID))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("读取第%d章失败: %w", ch.ReadingNumber, err)
		}
		out = append(out, ChapterSource{
			ID:            ch.ID,
			ReadingNumber: ch.ReadingNumber,
			Title:         ch.Title,
			Summary:       strings.TrimSpace(ch.Summary),
			Content:       strings.TrimSpace(content),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReadingNumber < out[j].ReadingNumber })
	return out, nil
}

// preAnalyzeBoundaries Pre-step：通过章节标题推断叙事阶段边界
func (e *Extractor) preAnalyzeBoundaries(ctx context.Context, input ExtractPatternInput, chapters []ChapterSource) ([]BoundaryHint, error) {
	onStatus := func(s LLMStatus) {
		e.emit(input, Progress{Stage: StageBoundaries, LLMStatus: s})
	}
	raw, err := e.callTool(ctx, input, outputBoundariesTool, BoundaryHintsOutput{}, boundaryMessages(chapters), 1, 0, onStatus)
	if err != nil {
		return nil, fmt.Errorf("分析章节标题边界失败: %w", err)
	}
	var out BoundaryHintsOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析边界提示失败: %w", err)
	}
	return normalizeBoundaries(out.Boundaries, readingNumbersFromChapters(chapters)), nil
}

// ensureSummaries Step 1：增量生成章节摘要，已有摘要的章节作为上下文参考跳过
func (e *Extractor) ensureSummaries(ctx context.Context, input ExtractPatternInput, chapters []ChapterSource, boundaries []BoundaryHint, budget int) ([]ChapterSummaryItem, error) {
	batches := buildChapterBatches(chapters, boundaries, budget)
	readingNumbers := readingNumbersFromChapters(chapters)
	summaryByID := map[int64]string{}
	for _, ch := range chapters {
		if strings.TrimSpace(ch.Summary) != "" {
			summaryByID[ch.ID] = strings.TrimSpace(ch.Summary)
		}
	}

	for i, batch := range batches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var missing []ChapterSource
		for _, ch := range batch {
			if _, ok := summaryByID[ch.ID]; !ok {
				missing = append(missing, ch)
			}
		}
		if len(missing) == 0 {
			e.emit(input, Progress{
				Stage:      StageSummaries,
				Message:    fmt.Sprintf("摘要第 %d/%d 批已复用缓存", i+1, len(batches)),
				BatchIndex: i + 1,
				BatchTotal: len(batches),
			})
			continue
		}

		e.emit(input, Progress{
			Stage:      StageSummaries,
			Message:    fmt.Sprintf("摘要第 %d/%d 批...", i+1, len(batches)),
			BatchIndex: i + 1,
			BatchTotal: len(batches),
		})
		raw, err := e.callTool(ctx, input, outputChapterSummariesTool, ChapterSummariesOutput{}, summaryMessages(batch, boundaries), 2, 0, nil)
		if err != nil {
			return nil, fmt.Errorf("生成章节摘要失败: %w", err)
		}
		var out ChapterSummariesOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("解析章节摘要失败: %w", err)
		}
		for i := range out.Summaries {
			item := &out.Summaries[i]
			item.Summary = strings.TrimSpace(item.Summary)
			item.ReadingNumber = readingNumbers[item.ChapterID]
			if item.ChapterID == 0 || item.Summary == "" {
				continue
			}
			if !chapterInList(item.ChapterID, missing) {
				continue
			}
			summaryByID[item.ChapterID] = item.Summary
			if err := e.Chapters.DB.WithContext(ctx).
				Model(&chapter.Chapter{}).
				Where("novel_id = ? AND id = ?", input.NovelID, item.ChapterID).
				Update("summary", item.Summary).Error; err != nil {
				return nil, fmt.Errorf("保存第%d章摘要失败: %w", readingNumbers[item.ChapterID], err)
			}
		}
		e.emit(input, Progress{
			Stage:      StageSummaries,
			Message:    fmt.Sprintf("摘要第 %d/%d 批完成，新增 %d 条", i+1, len(batches), len(out.Summaries)),
			BatchIndex: i + 1,
			BatchTotal: len(batches),
			Summaries:  out.Summaries,
		})
	}

	result := make([]ChapterSummaryItem, 0, len(chapters))
	for _, ch := range chapters {
		summary := summaryByID[ch.ID]
		if summary == "" && ch.Content == "" {
			summary = "无内容。"
		}
		if summary == "" {
			continue
		}
		result = append(result, ChapterSummaryItem{ChapterID: ch.ID, ReadingNumber: ch.ReadingNumber, Summary: summary})
	}
	return result, nil
}

// initialChunks Step 2 第 1 轮：将章节摘要压缩为初始叙事阶段块
func (e *Extractor) initialChunks(ctx context.Context, input ExtractPatternInput, summaries []ChapterSummaryItem, budget int) ([]Chunk, ChunkRoundTrace, error) {
	var all []Chunk
	batches := buildSummaryBatches(summaries, budget)
	trace := ChunkRoundTrace{Round: 1, InputCount: len(summaries)}
	for i, batch := range batches {
		if err := ctx.Err(); err != nil {
			return nil, trace, err
		}
		e.emit(input, Progress{
			Stage:      StageInitialChunks,
			Message:    fmt.Sprintf("第 1 轮第 %d/%d 批...", i+1, len(batches)),
			Round:      1,
			BatchIndex: i + 1,
			BatchTotal: len(batches),
		})
		raw, err := e.callTool(ctx, input, outputChunksTool, ChunksOutput{}, initialChunkMessages(batch), 2, 0, nil)
		if err != nil {
			return nil, trace, fmt.Errorf("生成初始阶段块失败: %w", err)
		}
		var out ChunksOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, trace, fmt.Errorf("解析初始阶段块失败: %w", err)
		}
		normalized := normalizeChunks(out.Chunks, readingNumbersFromSummaries(batch))
		all = append(all, normalized...)
		trace.Batches = append(trace.Batches, BatchTrace{
			Index:        i + 1,
			InputCount:   len(batch),
			OutputCount:  len(normalized),
			ApproxTokens: tokensOfSummaries(batch),
		})
		e.emit(input, Progress{
			Stage:      StageInitialChunks,
			Message:    fmt.Sprintf("第 1 轮第 %d/%d 批生成 %d 个阶段块", i+1, len(batches), len(normalized)),
			Round:      1,
			BatchIndex: i + 1,
			BatchTotal: len(batches),
			Chunks:     normalized,
		})
	}
	trace.Chunks = all
	trace.OutputCount = len(all)
	trace.TokenCount = tokensOfChunks(all)
	return all, trace, nil
}

// compressChunks Step 2 后续轮：递归合并叙事阶段块
func (e *Extractor) compressChunks(ctx context.Context, input ExtractPatternInput, chunks []Chunk, readingNumbers map[int64]int, budget int, round int) ([]Chunk, ChunkRoundTrace, error) {
	var all []Chunk
	batches := buildChunkBatches(chunks, budget)
	trace := ChunkRoundTrace{Round: round, InputCount: len(chunks)}
	for i, batch := range batches {
		if err := ctx.Err(); err != nil {
			return nil, trace, err
		}
		e.emit(input, Progress{
			Stage:      StageCompressChunks,
			Message:    fmt.Sprintf("第 %d 轮第 %d/%d 批...", round, i+1, len(batches)),
			Round:      round,
			BatchIndex: i + 1,
			BatchTotal: len(batches),
		})
		batchReadingNumbers := readingNumbersForChunks(batch, readingNumbers)
		raw, err := e.callTool(ctx, input, outputChunksTool, ChunksOutput{}, compressChunkMessages(batch, batchReadingNumbers, round), 2, 0, nil)
		if err != nil {
			return nil, trace, fmt.Errorf("第%d轮压缩失败: %w", round, err)
		}
		var out ChunksOutput
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, trace, fmt.Errorf("解析第%d轮压缩结果失败: %w", round, err)
		}
		normalized := normalizeChunks(out.Chunks, batchReadingNumbers)
		all = append(all, normalized...)
		trace.Batches = append(trace.Batches, BatchTrace{
			Index:        i + 1,
			InputCount:   len(batch),
			OutputCount:  len(normalized),
			ApproxTokens: tokensOfChunks(batch),
		})
		e.emit(input, Progress{
			Stage:      StageCompressChunks,
			Message:    fmt.Sprintf("第 %d 轮第 %d/%d 批压缩为 %d 个阶段块", round, i+1, len(batches), len(normalized)),
			Round:      round,
			BatchIndex: i + 1,
			BatchTotal: len(batches),
			Chunks:     normalized,
		})
	}
	trace.Chunks = all
	trace.OutputCount = len(all)
	trace.TokenCount = tokensOfChunks(all)
	return all, trace, nil
}

// finalSkillOutcome 是 Step 3 的最终产出；mental flavor 会额外携带保真度自测与降级信息。
type finalSkillOutcome struct {
	Raw           string
	Fidelity      *FidelityScorecard
	DemotedModels []string
	// VoiceProfile 是 P1 双写的结构化维度（仅 mental），由 runFinal 转交 VoiceSink。
	VoiceProfile []voice.ProfileItem
}

// finalSkill Step 3：所有阶段块 → 通过 output_skill 工具调用生成最终技能。
// 按 flavor 分流：struct 走套路模板（buildSkillMarkdown），mental 走创作心智（buildMentalSkillMarkdown）。
func (e *Extractor) finalSkill(ctx context.Context, input ExtractPatternInput, chunks []Chunk, chapterCount int) (finalSkillOutcome, error) {
	if Flavor(input.Flavor) == FlavorMental {
		return e.finalMentalSkill(ctx, input, chunks, chapterCount)
	}
	out, err := e.finalStructSkill(ctx, input, chunks)
	if err != nil {
		return finalSkillOutcome{}, err
	}
	return finalSkillOutcome{Raw: out}, nil
}

// finalStructSkill 生成套路模板技能（原 finalSkill 逻辑）。
func (e *Extractor) finalStructSkill(ctx context.Context, input ExtractPatternInput, chunks []Chunk) (string, error) {
	onStatus := func(s LLMStatus) {
		e.emit(input, Progress{Stage: StageFinalizing, LLMStatus: s})
	}
	raw, err := e.callTool(ctx, input, "output_skill", SkillOutput{}, finalSkillMessages(chunks), 2, 64000, onStatus)
	if err != nil {
		return "", err
	}
	var out SkillOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("解析生成的技能失败: %w", err)
	}
	if strings.TrimSpace(out.Name) == "" || strings.TrimSpace(out.Content) == "" {
		return "", fmt.Errorf("LLM 返回的技能内容不完整")
	}
	return buildSkillMarkdown(out.Name, out.Description, out.Content), nil
}

// finalMentalSkill 生成创作心智技能：复用同一编排，仅替换 final 提示词与 frontmatter。
// 流水线：①（V2-D 可选）向量检索证据前置；② 生成；③（V2-B）确定性跨弧复现门禁；
// ④（V1）低置信度提示；⑤（V2-C）产出后保真度自测。
func (e *Extractor) finalMentalSkill(ctx context.Context, input ExtractPatternInput, chunks []Chunk, chapterCount int) (finalSkillOutcome, error) {
	// V3 P1：深度多镜头精提（opt-in）。开启后 final 阶段改用三路聚焦镜头分别生成再合并，
	// 合计 token ≤7k，低于单轮 8k 护栏，提升五层结构的独立保真度。
	if input.DeepMultiLens {
		return e.finalMentalMultiLens(ctx, input, chunks, chapterCount)
	}

	onStatus := func(s LLMStatus) {
		e.emit(input, Progress{Stage: StageFinalizing, LLMStatus: s})
	}
	lowConfidence := chapterCount < 20

	// ①（V2-D）向量检索证据前置（可选，缺失则跳过）
	evidencePassages := e.retrieveEvidence(ctx, input.NovelID)

	raw, err := e.callTool(ctx, input, "output_skill", MentalSkillOutput{},
		mentalFinalSkillMessages(chunks, chapterCount, lowConfidence, evidencePassages), 2, mentalFinalMaxTokens, onStatus)
	if err != nil {
		return finalSkillOutcome{}, err
	}
	var out MentalSkillOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return finalSkillOutcome{}, fmt.Errorf("解析生成的心智技能失败: %w", err)
	}
	if strings.TrimSpace(out.Name) == "" || strings.TrimSpace(out.Content) == "" {
		return finalSkillOutcome{}, fmt.Errorf("LLM 返回的心智技能内容不完整")
	}

	// ③（V2-B）确定性跨弧复现门禁：独立章节证据 <2 的心智模型降级为启发式
	demoted, gateNote := gateMentalEvidence(out.Content, out.Evidence)

	// 组装正文：原始内容 + 门禁降级说明 + 低置信度提示
	body := strings.TrimSpace(out.Content) + gateNote
	if lowConfidence {
		body += "\n\n> ⚠️ 低置信度提示：本次提取样本章节较少（少于 20 章），上述心智模型与启发式可能不稳定，建议积累更多章节后重新提取。\n"
	}
	md := buildMentalSkillMarkdown(out.Name, out.Description, body, input.SourceAuthor)

	// ⑤（V2-C）产出后保真度自测（失败不阻断主流程，仅记录）
	fidelity, ferr := e.fidelityTest(ctx, input, chunks, out.Content)
	if ferr != nil {
		e.emit(input, Progress{Stage: StageFinalizing, Message: "保真度自测跳过：" + ferr.Error()})
	} else if fidelity != nil {
		md = appendFidelitySection(md, fidelity)
	}

	return finalSkillOutcome{
		Raw:           md,
		Fidelity:      fidelity,
		DemotedModels: demoted,
		VoiceProfile:  out.VoiceProfile,
	}, nil
}

// finalMentalMultiLens 多镜头精提（V3 P1）：boundary→summary→chunk 已单遍完成，复用 chunks。
// 三路聚焦 final（心智/表达/决策）分别生成，最后拼装五层并照常运行 V2 门禁与保真度。
func (e *Extractor) finalMentalMultiLens(ctx context.Context, input ExtractPatternInput, chunks []Chunk, chapterCount int) (finalSkillOutcome, error) {
	lowConfidence := chapterCount < 20
	evidencePassages := e.retrieveEvidence(ctx, input.NovelID)

	emitStep := func(lens, msg string) {
		e.emit(input, Progress{Stage: StageFinalizing, SubStep: lens, Message: msg})
	}
	onStatus := func(lens string) func(LLMStatus) {
		return func(s LLMStatus) {
			e.emit(input, Progress{Stage: StageFinalizing, SubStep: lens, LLMStatus: s})
		}
	}

	emitStep("mental", "多镜头精提·叙事心智模型（≤3k）...")
	mental, err := e.finalMentalLens(ctx, input, chunks, "mental", lensMentalMaxTokens, lowConfidence, evidencePassages, onStatus("mental"))
	if err != nil {
		return finalSkillOutcome{}, err
	}
	if strings.TrimSpace(mental.Content) == "" {
		return finalSkillOutcome{}, fmt.Errorf("多镜头：心智镜头未返回正文")
	}

	emitStep("expression", "多镜头精提·表达DNA（≤2k）...")
	expr, err := e.finalMentalLens(ctx, input, chunks, "expression", lensExprMaxTokens, lowConfidence, evidencePassages, onStatus("expression"))
	if err != nil {
		return finalSkillOutcome{}, err
	}
	if strings.TrimSpace(expr.Content) == "" {
		return finalSkillOutcome{}, fmt.Errorf("多镜头：表达镜头未返回正文")
	}

	emitStep("decision", "多镜头精提·创作决策启发式/价值观/诚实边界（≤2k）...")
	decision, err := e.finalMentalLens(ctx, input, chunks, "decision", lensDecisionMaxTokens, lowConfidence, evidencePassages, onStatus("decision"))
	if err != nil {
		return finalSkillOutcome{}, err
	}
	if strings.TrimSpace(decision.Content) == "" {
		return finalSkillOutcome{}, fmt.Errorf("多镜头：决策镜头未返回正文")
	}

	// 按五层 canonical 顺序拼装正文：叙事心智模型(mental) → 创作决策启发式/价值观(decision 前段)
	// → 表达DNA(expr) → 诚实边界(decision 后段)。
	body := mergeLensBodies(mental, expr, decision)

	// ③（V2-B）确定性跨弧复现门禁：使用心智镜头的证据，独立章节<2 的模型降级为启发式。
	demoted, gateNote := gateMentalEvidence(body, mental.Evidence)

	body = strings.TrimSpace(body) + gateNote
	if lowConfidence {
		body += "\n\n> ⚠️ 低置信度提示：本次提取样本章节较少（少于 20 章），上述心智模型与启发式可能不稳定，建议积累更多章节后重新提取。\n"
	}

	name := strings.TrimSpace(mental.Name)
	if name == "" {
		name = "创作心智"
		if a := strings.TrimSpace(input.SourceAuthor); a != "" {
			name = a + " 的创作心智"
		}
	}
	description := strings.TrimSpace(mental.Description)
	if description == "" {
		description = "以该作者的创作心智与表达习惯写新章节时"
	}
	md := buildMentalSkillMarkdown(name, description, body, input.SourceAuthor)

	// ⑤（V2-C）产出后保真度自测（失败不阻断主流程，仅记录）。
	fidelity, ferr := e.fidelityTest(ctx, input, chunks, body)
	if ferr != nil {
		e.emit(input, Progress{Stage: StageFinalizing, Message: "保真度自测跳过：" + ferr.Error()})
	} else if fidelity != nil {
		md = appendFidelitySection(md, fidelity)
	}

	return finalSkillOutcome{
		Raw:           md,
		Fidelity:      fidelity,
		DemotedModels: demoted,
		VoiceProfile:  mergeVoiceProfiles(mental.VoiceProfile, expr.VoiceProfile, decision.VoiceProfile),
	}, nil
}

// finalMentalLens 多镜头精提的单个聚焦镜头：调用 output_skill 生成仅含本镜头指定章节的
// 正文，返回 LensOutput（Content + Evidence）。主镜头（mental）额外产出 name/description。
func (e *Extractor) finalMentalLens(ctx context.Context, input ExtractPatternInput, chunks []Chunk, lens string, maxTokens int, lowConfidence bool, evidence []string, onStatus func(LLMStatus)) (LensOutput, error) {
	raw, err := e.callTool(ctx, input, "output_skill", LensOutput{}, lensMentalSkillMessages(chunks, lowConfidence, evidence, lens), 2, maxTokens, onStatus)
	if err != nil {
		return LensOutput{}, err
	}
	var out LensOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return LensOutput{}, fmt.Errorf("解析多镜头产出失败: %w", err)
	}
	return out, nil
}

// mergeVoiceProfiles 合并多镜头的 Voice 画像维度（按传入顺序首次出现优先、同维度去重），
// 全部为空时返回 nil。去重后仍由 voice.Store.Upsert 幂等兜底。
func mergeVoiceProfiles(profiles ...[]voice.ProfileItem) []voice.ProfileItem {
	seen := make(map[string]bool)
	var out []voice.ProfileItem
	for _, items := range profiles {
		for _, it := range items {
			if seen[it.Dimension] {
				continue
			}
			seen[it.Dimension] = true
			out = append(out, it)
		}
	}
	return out
}

// mergeLensBodies 按五层 canonical 顺序拼装三路镜头正文：
// 叙事心智模型(mental) → 创作决策启发式/价值观(decision 前段) → 表达DNA(expr) → 诚实边界(decision 后段)。
// decision 镜头一次产出三章（启发式/价值观/诚实边界），此处把「诚实边界」拆出放到表达DNA之后，
// 以满足 canonical 顺序（表达DNA 在诚实边界之前）。不做二次 LLM 调用。
func mergeLensBodies(mental, expr, decision LensOutput) string {
	decisionHead, honesty := splitHonestySection(decision.Content)
	var b strings.Builder
	b.WriteString(strings.TrimSpace(mental.Content))
	if strings.TrimSpace(decisionHead) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(decisionHead))
	}
	if strings.TrimSpace(expr.Content) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(expr.Content))
	}
	if strings.TrimSpace(honesty) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(honesty))
	}
	return b.String()
}

// splitHonestySection 将决策镜头正文按「## 诚实边界」（兼容「##诚实边界」）拆分为前段
// （启发式+价值观）与诚实边界段。若不含诚实边界标记，则整体作为前段返回，honesty 为空。
func splitHonestySection(content string) (head, honesty string) {
	idx := strings.Index(content, "## 诚实边界")
	if idx < 0 {
		// 兼容 LLM 偶发的「##诚实边界」（无空格）。
		if alt := strings.Index(content, "##诚实边界"); alt >= 0 {
			return content[:alt], content[alt:]
		}
		return content, ""
	}
	return content[:idx], content[idx:]
}

// buildSkillMarkdown 组装完整的 skill markdown：固定 frontmatter + 正文。
func buildSkillMarkdown(name, description, content string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", description)
	b.WriteString("category: 套路模板\n")
	b.WriteString("mode: auto\n")
	b.WriteString("author: ai\n")
	b.WriteString("version: 1\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n")
	return b.String()
}

// buildMentalSkillMarkdown 组装创作心智 skill markdown：frontmatter 标注 kind: mental，
// 并写入 source_author（V3 跨书聚合归并键）。低置信度提示与门禁降级说明由调用方追加到 content。
func buildMentalSkillMarkdown(name, description, content, sourceAuthor string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", description)
	b.WriteString("category: 创作心智\n")
	b.WriteString("kind: mental\n")
	b.WriteString("mode: auto\n")
	b.WriteString("author: ai\n")
	if strings.TrimSpace(sourceAuthor) != "" {
		fmt.Fprintf(&b, "source_author: %s\n", strings.TrimSpace(sourceAuthor))
	}
	b.WriteString("version: 1\n")
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n")
	return b.String()
}

// gateMentalEvidence 执行 V2-B 确定性跨弧复现门禁：统计每个心智模型的独立章节证据数，
// 独立章节 < 2 的模型标记为"证据不足，降级为启发式"。返回被降级的模型名与待追加的说明段。
// 若 LLM 未返回 Evidence（向前兼容 V1 行为），返回空，交由提示词中的 LLM 自评兜底。
func gateMentalEvidence(content string, evidence []ModelEvidence) (demoted []string, note string) {
	if len(evidence) == 0 {
		return nil, ""
	}
	var lines []string
	for _, ev := range evidence {
		name := strings.TrimSpace(ev.Model)
		if name == "" {
			continue
		}
		seen := make(map[int64]bool, len(ev.ChapterIDs))
		for _, id := range ev.ChapterIDs {
			seen[id] = true
		}
		if len(seen) < 2 {
			demoted = append(demoted, name)
			lines = append(lines, fmt.Sprintf("- 「%s」：独立章节证据仅 %d 处（门禁要求 ≥2），已降级为创作决策启发式。", name, len(seen)))
		}
	}
	if len(lines) == 0 {
		return nil, ""
	}
	note = "\n\n## 验证与降级说明\n\n" +
		"> 确定性跨弧复现门禁（V2）：以下心智模型因独立章节证据不足，已降级为启发式，建议作为「创作决策启发式」而非稳定的「叙事心智模型」使用：\n\n" +
		strings.Join(lines, "\n")
	return demoted, note
}

// retrieveEvidence 调用可选的向量检索器（V2-D）获取全书证据片段；缺失或出错时返回 nil（无操作）。
func (e *Extractor) retrieveEvidence(ctx context.Context, novelID int64) []string {
	if e.EvidenceRetriever == nil {
		return nil
	}
	passages, err := e.EvidenceRetriever.RetrieveEvidence(ctx, novelID, mentalEvidenceQueries, 4)
	if err != nil {
		e.emit(ExtractPatternInput{NovelID: novelID}, Progress{Stage: StageFinalizing, Message: "向量证据检索不可用，跳过：" + err.Error()})
		return nil
	}
	return passages
}

// fidelityTest 执行 V2-C 产出后保真度自测：用本书作者已明确处理过的冲突测方向一致性，
// 用作者从未涉及的题材测"适度不确定而非斩钉截铁"。返回评分卡；失败返回 error（由调用方决定是否阻断）。
func (e *Extractor) fidelityTest(ctx context.Context, input ExtractPatternInput, chunks []Chunk, skillContent string) (*FidelityScorecard, error) {
	onStatus := func(s LLMStatus) {
		e.emit(input, Progress{Stage: StageFinalizing, Message: "正在执行产出后保真度自测...", LLMStatus: s})
	}
	raw, err := e.callTool(ctx, input, outputFidelityTool, FidelityScorecard{}, fidelityTestMessages(chunks, skillContent), 2, 2000, onStatus)
	if err != nil {
		return nil, err
	}
	var sc FidelityScorecard
	if err := json.Unmarshal(raw, &sc); err != nil {
		return nil, fmt.Errorf("解析保真度自测结果失败: %w", err)
	}
	sc.Passed = sc.KnownConsistency >= 3 && sc.UnknownUncertainty >= 3
	return &sc, nil
}

// appendFidelitySection 将保真度自测结果追加到 skill markdown 末尾，供审阅者直接可见。
func appendFidelitySection(md string, f *FidelityScorecard) string {
	status := "通过"
	if !f.Passed {
		status = "未通过"
	}
	var b strings.Builder
	b.WriteString("\n\n## 保真度自测\n\n")
	fmt.Fprintf(&b, "> 已知立场一致性：%d/5 ｜ 未知题材适度不确定：%d/5 ｜ 结论：**%s**\n\n", f.KnownConsistency, f.UnknownUncertainty, status)
	if len(f.Issues) > 0 {
		b.WriteString("发现的问题：\n")
		for _, iss := range f.Issues {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(iss))
		}
		b.WriteString("\n")
	}
	b.WriteString("> 保真度自测由 V2 产出后测试生成，仅作质量参考，不构成绝对评判。\n")
	return md + b.String()
}

// callTool 请求 LLM 调用指定工具，返回结构化 JSON。
// 薄封装：组装单工具 tools 数组 + 适配 llm.CallToolOptions，核心流式接收/重试/错误聚合在 llm.CallTool。
func (e *Extractor) callTool(ctx context.Context, input ExtractPatternInput, toolName string, schema any, messages []map[string]any, attempts int, maxTokens int, onStatus func(LLMStatus)) (json.RawMessage, error) {
	tools := []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":        toolName,
			"description": "返回模式提取的结构化输出。",
			"parameters":  mcp_tools.SchemaOf(schema),
		},
	}}
	opts := &llm.CallToolOptions{
		Attempts: attempts,
		OnStatus: func(s llm.CallToolStatus) {
			if onStatus != nil {
				onStatus(s)
			}
		},
	}
	if maxTokens > 0 {
		opts.MaxTokens = maxTokens
	}
	if input.ReasoningEffort != "" {
		opts.ReasoningEffort = input.ReasoningEffort
	}
	return e.LLMClient.CallTool(ctx, input.ProviderName, input.ModelID, messages, tools, toolName, opts)
}

// batchBudget 根据模型上下文窗口计算每批 token 预算：≥200K 用 100K，否则取 70%
func batchBudget(contextWindow int) int {
	if contextWindow >= 200_000 {
		return 100_000
	}
	return int(float64(contextWindow) * 0.7)
}

func (e *Extractor) modelContextWindow(providerName, modelID string) int {
	if e.LLMClient != nil {
		if m, ok := e.LLMClient.ProviderModel(providerName, modelID); ok && m.ContextWindow > 0 {
			return m.ContextWindow
		}
	}
	return defaultContextWindow
}

// buildChapterBatches 按 token 预算分批章节正文，切分时尽量对齐 Pre-step 的疑似边界
func buildChapterBatches(chapters []ChapterSource, boundaries []BoundaryHint, budget int) [][]ChapterSource {
	var batches [][]ChapterSource
	var batch []ChapterSource
	tokens := 0
	for i := 0; i < len(chapters); i++ {
		ch := chapters[i]
		t := approxTokens(ch.Content)
		if tokens+t > budget && len(batch) > 0 {
			cut := maybeExtendToBoundary(chapters, i, boundaries, budget-tokens)
			if cut > i {
				batch = append(batch, chapters[i:cut]...)
				i = cut - 1
				batches = append(batches, batch)
				batch = nil
				tokens = 0
				continue
			}
			batches = append(batches, batch)
			batch = nil
			tokens = 0
		}
		batch = append(batch, ch)
		tokens += t
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// maybeExtendToBoundary 向前最多看 10 章，尝试在疑似边界处切分
func maybeExtendToBoundary(chapters []ChapterSource, start int, boundaries []BoundaryHint, remaining int) int {
	limit := min(start+10, len(chapters))
	extraTokens := 0
	for i := start; i < limit; i++ {
		extraTokens += approxTokens(chapters[i].Content)
		if extraTokens > remaining {
			return start
		}
		for _, b := range boundaries {
			if b.EndChapterID == chapters[i].ID {
				return i + 1
			}
		}
	}
	return start
}

func buildSummaryBatches(items []ChapterSummaryItem, budget int) [][]ChapterSummaryItem {
	var batches [][]ChapterSummaryItem
	var batch []ChapterSummaryItem
	tokens := 0
	for _, item := range items {
		t := approxTokens(item.Summary) + 16
		if tokens+t > budget && len(batch) > 0 {
			batches = append(batches, batch)
			batch = nil
			tokens = 0
		}
		batch = append(batch, item)
		tokens += t
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func buildChunkBatches(chunks []Chunk, budget int) [][]Chunk {
	var batches [][]Chunk
	var batch []Chunk
	tokens := 0
	for _, ch := range chunks {
		t := approxTokens(ch.Name+ch.Content) + 32
		if tokens+t > budget && len(batch) > 0 {
			batches = append(batches, batch)
			batch = nil
			tokens = 0
		}
		batch = append(batch, ch)
		tokens += t
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

func tokensOfChunks(chunks []Chunk) int {
	total := 0
	for _, ch := range chunks {
		total += approxTokens(ch.Name+ch.Content) + 32
	}
	return total
}

func tokensOfSummaries(items []ChapterSummaryItem) int {
	total := 0
	for _, item := range items {
		total += approxTokens(item.Summary) + 16
	}
	return total
}

func (e *Extractor) emit(input ExtractPatternInput, p Progress) {
	if e.Progress == nil {
		return
	}
	p.TaskID = input.TaskID
	p.NovelID = input.NovelID
	e.Progress(p)
}

// approxTokens 估算文本 token 数：优先用 tiktoken 精确计算，失败时退化为字符数/2
func approxTokens(text string) int {
	n, err := llm.CountTokens(text)
	if err == nil {
		return n
	}
	runes := len([]rune(text))
	if runes == 0 {
		return 0
	}
	return runes / 2
}

func readingNumbersFromChapters(chapters []ChapterSource) map[int64]int {
	numbers := make(map[int64]int, len(chapters))
	for _, ch := range chapters {
		numbers[ch.ID] = ch.ReadingNumber
	}
	return numbers
}

func readingNumbersFromSummaries(summaries []ChapterSummaryItem) map[int64]int {
	numbers := make(map[int64]int, len(summaries))
	for _, summary := range summaries {
		numbers[summary.ChapterID] = summary.ReadingNumber
	}
	return numbers
}

func readingNumbersForChunks(chunks []Chunk, all map[int64]int) map[int64]int {
	if len(chunks) == 0 {
		return nil
	}
	start, end := chunks[0].StartReadingNumber, chunks[0].EndReadingNumber
	for _, chunk := range chunks[1:] {
		start = min(start, chunk.StartReadingNumber)
		end = max(end, chunk.EndReadingNumber)
	}
	numbers := make(map[int64]int)
	for id, number := range all {
		if number >= start && number <= end {
			numbers[id] = number
		}
	}
	return numbers
}

// normalizeBoundaries 清洗并排序边界提示：按阅读顺序纠正端点、过滤无效条目。
func normalizeBoundaries(items []BoundaryHint, readingNumbers map[int64]int) []BoundaryHint {
	out := make([]BoundaryHint, 0, len(items))
	for _, it := range items {
		start, startOK := readingNumbers[it.StartChapterID]
		end, endOK := readingNumbers[it.EndChapterID]
		if !startOK || !endOK {
			continue
		}
		if start > end {
			it.StartChapterID, it.EndChapterID = it.EndChapterID, it.StartChapterID
			start, end = end, start
		}
		it.StartReadingNumber = start
		it.EndReadingNumber = end
		it.Hint = strings.TrimSpace(it.Hint)
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartReadingNumber < out[j].StartReadingNumber })
	return out
}

// normalizeChunks 清洗并排序叙事阶段块：按阅读顺序纠正端点、过滤无效条目。
func normalizeChunks(items []Chunk, readingNumbers map[int64]int) []Chunk {
	out := make([]Chunk, 0, len(items))
	for _, it := range items {
		it.Name = strings.TrimSpace(it.Name)
		it.Content = strings.TrimSpace(it.Content)
		start, startOK := readingNumbers[it.StartChapterID]
		end, endOK := readingNumbers[it.EndChapterID]
		if it.Name == "" || it.Content == "" || !startOK || !endOK {
			continue
		}
		if start > end {
			it.StartChapterID, it.EndChapterID = it.EndChapterID, it.StartChapterID
			start, end = end, start
		}
		it.StartReadingNumber = start
		it.EndReadingNumber = end
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartReadingNumber == out[j].StartReadingNumber {
			return out[i].EndReadingNumber < out[j].EndReadingNumber
		}
		return out[i].StartReadingNumber < out[j].StartReadingNumber
	})
	return out
}

func chapterInList(chapterID int64, chapters []ChapterSource) bool {
	for _, ch := range chapters {
		if ch.ID == chapterID {
			return true
		}
	}
	return false
}

func NovelSkillPath(novelID int64, name string) string {
	return filepath.Join(config.NovelSkillsDir(novelID), skill.SanitizeFileName(name)+".md")
}
