package pattern

import "github.com/novelgo/novelgo/internal/llm"

type ExtractPatternInput struct {
	TaskID          string  `json:"task_id,omitempty"`
	NovelID         int64   `json:"novel_id"`
	ProviderName    string  `json:"provider_name"`
	ModelID         string  `json:"model_id"`
	ReasoningEffort string  `json:"reasoning_effort"`
	ChapterIDs      []int64 `json:"chapter_ids,omitempty"`
	// Flavor 选择提取产物类型："struct"（默认，套路模板）或 "mental"（创作心智）。
	Flavor string `json:"flavor,omitempty"`
	// SourceAuthor 仅 mental 有意义：被蒸馏的心智所属的作者（用于 V3 跨书聚合）。
	// 由前端「来源作者」输入框传入，写入 skill frontmatter 的 source_author。
	SourceAuthor string `json:"source_author,omitempty"`
	// DeepMultiLens 仅 mental 有意义（opt-in）：开启后 final 阶段改用三路聚焦镜头
	// （叙事心智模型 / 表达DNA / 创作决策启发式+价值观+诚实边界）分别生成再合并，
	// 合计 token ≤7k，低于单轮 8k 护栏，提升五层结构的独立保真度（V3 P1）。
	DeepMultiLens bool `json:"deep_multi_lens,omitempty"`
}

type ExtractPatternResult struct {
	TaskID      string `json:"task_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RawContent  string `json:"raw_content"`
	FilePath    string `json:"file_path"`
	Flavor      string `json:"flavor,omitempty"`
	// Fidelity 是 V2 产出后保真度自测结果，仅 mental flavor 非空。
	Fidelity *FidelityScorecard `json:"fidelity,omitempty"`
	// DemotedModels 是 V2 确定性跨弧复现门禁降级的心智模型名（独立章节证据<2），仅 mental flavor 非空。
	DemotedModels []string `json:"demoted_models,omitempty"`
	Trace         *Trace   `json:"trace,omitempty"`
}

// Flavor 是提取产物的类型标识。
type Flavor string

const (
	// FlavorStruct 套路模板（默认）。
	FlavorStruct Flavor = "struct"
	// FlavorMental 创作心智（作者心智模型 / 决策启发式 / 表达 DNA）。
	FlavorMental Flavor = "mental"
)

// MentalSkillOutput 是 mental flavor 下 LLM 返回的产出结构。
// 与 SkillOutput 不同，content 必须包含五层结构（叙事心智模型 / 创作决策启发式 /
// 价值观与反模式 / 表达DNA / 诚实边界），且心智模型与启发式须以章节锚点
// （如「见第 N 章」）佐证——锚点内联在 content 文本中，不做独立结构化字段，
// 以降低 LLM 输出不合规导致解析失败的风险。
// Evidence 是 V2 新增的确定性复现门禁证据：列出每个心智模型佐证的章节 chapter_id，
// 由 gateMentalEvidence 做"独立章节数 ≥2"的确定性判定（不依赖 LLM 自评）。
type MentalSkillOutput struct {
	Name        string          `json:"name" jsonschema:"required,description=技能名称（中文，体现该作者的创作心智，如「X 的创作心智」）"`
	Description string          `json:"description" jsonschema:"required,description=一句话描述何时使用此创作心智技能"`
	Content     string          `json:"content" jsonschema:"required,description=技能正文 markdown（不含 frontmatter），须含五章：叙事心智模型/创作决策启发式/价值观与反模式/表达DNA/诚实边界，且心智模型与启发式须附章节锚点"`
	Evidence    []ModelEvidence `json:"evidence,omitempty" jsonschema:"description=每个心智模型的章节证据锚点（chapter_id 列表），用于 V2 确定性跨弧复现门禁；要求每个模型≥2 个相互独立的章节"`
}

// ModelEvidence 是单个心智模型的证据锚点，供确定性门禁计数。
type ModelEvidence struct {
	// Model 必须与正文「## 叙事心智模型」中列出的模型名称一致，用于关联降级说明。
	Model string `json:"model" jsonschema:"required,description=心智模型名称，须与正文『## 叙事心智模型』中列出的模型名称一致"`
	// ChapterIDs 是佐证该模型的章节 chapter_id 列表；独立章节数 <2 将被确定性降级。
	ChapterIDs []int64 `json:"chapter_ids" jsonschema:"required,description=佐证该模型的章节 chapter_id 列表；确定性门禁要求≥2 个相互独立的章节"`
}

// LensOutput 是 V3 P1 多镜头精提中单个镜头（mental/expression/decision）的产出。
// 三路镜头各自聚焦五层结构的子集，最终由 mergeLensBodies 拼装为完整五层正文。
// Name/Description 仅主镜头（mental）填写，其余镜头留空；Evidence 仅 mental 镜头产出
// （用于确定性跨弧复现门禁），其余镜头留空。
type LensOutput struct {
	Name        string          `json:"name,omitempty" jsonschema:"description=技能名称（仅 mental 主镜头填写，其余镜头留空）"`
	Description string          `json:"description,omitempty" jsonschema:"description=一句话描述（仅 mental 主镜头填写，其余镜头留空）"`
	Content     string          `json:"content" jsonschema:"required,description=本镜头聚焦产出的技能正文 markdown（不含 frontmatter），仅含指定章节"`
	Evidence    []ModelEvidence `json:"evidence,omitempty" jsonschema:"description=（仅 mental 镜头）每个心智模型的章节证据锚点；其余镜头留空"`
}

// FidelityScorecard 是 V2 产出后保真度测试的结果（nuwa 保真度评分卡的 fiction 适配版）。
// 两项评分均 1–5：
//   - KnownConsistency：已知立场一致性——作者已明确处理过的冲突，技能指导方向是否一致；
//   - UnknownUncertainty：未知题材适度不确定——作者从未涉及的题材，技能是否避免过度断言、表达适度不确定。
type FidelityScorecard struct {
	KnownConsistency   int      `json:"known_consistency" jsonschema:"required,description=已知立场一致性评分 1-5：作者已明确处理过的冲突，技能指导方向是否一致"`
	UnknownUncertainty int      `json:"unknown_uncertainty" jsonschema:"required,description=未知题材适度不确定评分 1-5：作者未涉及的题材，技能是否避免过度断言、表达适度不确定"`
	Issues             []string `json:"issues,omitempty" jsonschema:"description=发现的问题清单（如过度断言、与书中处理矛盾）"`
	Passed             bool     `json:"passed" jsonschema:"required,description=是否通过保真度自测（两项均≥3 视为通过）"`
}

// Pipeline stage names
const (
	StageLoaded         = "loaded"
	StageBoundaries     = "boundaries"
	StageSummaries      = "summaries"
	StageInitialChunks  = "initial_chunks"
	StageCompressChunks = "compress_chunks"
	StageFinalizing     = "finalizing"
	StageDone           = "done"
)

// LLMStatus 是 llm.CallToolStatus 的别名，保持 pattern 包对外类型兼容。
type LLMStatus = llm.CallToolStatus

const (
	LLMThinking   = llm.CallToolThinking   // 模型正在推理/思考
	LLMGenerating = llm.CallToolGenerating // 模型正在输出结果
)

type Progress struct {
	TaskID     string               `json:"task_id,omitempty"`
	NovelID    int64                `json:"novel_id"`
	Stage      string               `json:"stage"`
	SubStep    string               `json:"sub_step,omitempty"`
	Message    string               `json:"message"`
	LLMStatus  LLMStatus            `json:"llm_status,omitempty"`
	Round      int                  `json:"round,omitempty"`
	BatchIndex int                  `json:"batch_index,omitempty"`
	BatchTotal int                  `json:"batch_total,omitempty"`
	Tokens     int                  `json:"tokens,omitempty"`
	Boundaries []BoundaryHint       `json:"boundaries,omitempty"`
	Summaries  []ChapterSummaryItem `json:"summaries,omitempty"`
	Chunks     []Chunk              `json:"chunks,omitempty"`
}

type Trace struct {
	TaskID        string               `json:"task_id,omitempty"`
	NovelID       int64                `json:"novel_id"`
	ChapterCount  int                  `json:"chapter_count"`
	ContextWindow int                  `json:"context_window"`
	BatchBudget   int                  `json:"batch_budget"`
	Boundaries    []BoundaryHint       `json:"boundaries"`
	Summaries     []ChapterSummaryItem `json:"summaries"`
	ChunkRounds   []ChunkRoundTrace    `json:"chunk_rounds"`
	FinalTokens   int                  `json:"final_tokens"`
}

type ChunkRoundTrace struct {
	Round       int          `json:"round"`
	InputCount  int          `json:"input_count"`
	OutputCount int          `json:"output_count"`
	TokenCount  int          `json:"token_count"`
	Batches     []BatchTrace `json:"batches"`
	Chunks      []Chunk      `json:"chunks"`
}

type BatchTrace struct {
	Index        int `json:"index"`
	InputCount   int `json:"input_count"`
	OutputCount  int `json:"output_count"`
	ApproxTokens int `json:"approx_tokens"`
}

type BoundaryHint struct {
	StartChapterID     int64  `json:"start_chapter_id" jsonschema:"required,description=起始章节的稳定ID"`
	EndChapterID       int64  `json:"end_chapter_id" jsonschema:"required,description=结束章节的稳定ID"`
	StartReadingNumber int    `json:"-"`
	EndReadingNumber   int    `json:"-"`
	Hint               string `json:"hint" jsonschema:"required,description=为什么这里可能是叙事阶段边界"`
}

type BoundaryHintsOutput struct {
	Boundaries []BoundaryHint `json:"boundaries" jsonschema:"required,description=可能的叙事阶段边界列表"`
}

type ChapterSummaryItem struct {
	ChapterID     int64  `json:"chapter_id" jsonschema:"required,description=章节的稳定ID"`
	ReadingNumber int    `json:"-"`
	Summary       string `json:"summary" jsonschema:"required,description=80-150字的章节叙事摘要，覆盖核心事件、人物行为与转折"`
}

type ChapterSummariesOutput struct {
	Summaries []ChapterSummaryItem `json:"summaries" jsonschema:"required,description=生成的章节摘要列表"`
}

type Chunk struct {
	Name               string `json:"name" jsonschema:"required,description=叙事阶段名称（如：崛起、转折）"`
	StartChapterID     int64  `json:"start_chapter_id" jsonschema:"required,description=起始章节的稳定ID"`
	EndChapterID       int64  `json:"end_chapter_id" jsonschema:"required,description=结束章节的稳定ID"`
	StartReadingNumber int    `json:"-"`
	EndReadingNumber   int    `json:"-"`
	Content            string `json:"content" jsonschema:"required,description=100-200字的阶段叙事概括，覆盖核心事件与转折"`
}

type ChunksOutput struct {
	Chunks []Chunk `json:"chunks" jsonschema:"required,description=压缩后的叙事阶段块列表"`
}

type SkillOutput struct {
	Name        string `json:"name" jsonschema:"required,description=技能名称（中文模式名）"`
	Description string `json:"description" jsonschema:"required,description=一句话描述何时使用此叙事模式"`
	Content     string `json:"content" jsonschema:"required,description=技能正文 markdown，含 ## 套路概览/阶段拆解/爽点节奏/角色功能模板/可复用叙事规律/使用注意 等章节，不含 frontmatter"`
}

// ChapterSource 是从 DB 元数据和 git 正文拼出的章节数据传递对象
type ChapterSource struct {
	ID            int64
	ReadingNumber int
	Title         string
	Summary       string
	Content       string
}
