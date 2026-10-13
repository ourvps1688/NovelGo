package pattern

import (
	"fmt"
	"slices"
	"strings"
)

func boundaryMessages(chapters []ChapterSource) []map[string]any {
	var b strings.Builder
	for _, ch := range chapters {
		title := strings.TrimSpace(ch.Title)
		if title == "" {
			title = fmt.Sprintf("第%d章（无标题）", ch.ReadingNumber)
		}
		fmt.Fprintf(&b, "第%d章（chapter_id: %d）：%s\n", ch.ReadingNumber, ch.ID, title)
	}
	return []map[string]any{
		{"role": "system", "content": "你是一个长篇小说结构分析师。根据章节标题推断可能的叙事阶段边界。章节号只用于理解阅读顺序；返回边界端点时必须使用对应的 chapter_id。只能通过调用 output_boundary_hints 返回结果。"},
		{"role": "user", "content": "请找出可能的叙事阶段边界。边界是参考线索，不是最终定论。每条提示保持简短。\n\n" + b.String()},
	}
}

func summaryMessages(chapters []ChapterSource, boundaries []BoundaryHint) []map[string]any {
	var b strings.Builder
	if len(boundaries) > 0 {
		b.WriteString("疑似阶段边界参考：\n")
		for _, h := range boundaries {
			fmt.Fprintf(&b, "- 第%d-%d章（chapter_id: %d-%d）：%s\n", h.StartReadingNumber, h.EndReadingNumber, h.StartChapterID, h.EndChapterID, h.Hint)
		}
		b.WriteString("\n")
	}
	b.WriteString("章节列表：\n")
	for _, ch := range chapters {
		title := strings.TrimSpace(ch.Title)
		if title == "" {
			title = fmt.Sprintf("第%d章（无标题）", ch.ReadingNumber)
		}
		fmt.Fprintf(&b, "\n## 第%d章（chapter_id: %d）：%s\n", ch.ReadingNumber, ch.ID, title)
		if strings.TrimSpace(ch.Summary) != "" {
			fmt.Fprintf(&b, "[上下文参考，无需重新生成]\n%s\n", strings.TrimSpace(ch.Summary))
			continue
		}
		if strings.TrimSpace(ch.Content) == "" {
			b.WriteString("[需提取]\n无内容。\n")
			continue
		}
		fmt.Fprintf(&b, "[需提取]\n%s\n", ch.Content)
	}
	return []map[string]any{
		{"role": "system", "content": "你为叙事模式提取生成章节摘要。标记为[需提取]的章节需要各生成一条 80-150 字的叙事摘要，覆盖核心事件、关键人物行为与决策、情感或关系转折。保留足够信息量以供后续判断叙事阶段边界。标记为[上下文参考]的章节仅作叙事背景参考，除非同时标记了[需提取]，否则不要包含在输出中。章节号只用于理解阅读顺序；返回摘要时必须使用对应的 chapter_id。只能通过调用 output_chapter_summaries 返回结果。"},
		{"role": "user", "content": b.String()},
	}
}

func initialChunkMessages(summaries []ChapterSummaryItem) []map[string]any {
	var b strings.Builder
	for _, summary := range summaries {
		fmt.Fprintf(&b, "第%d章（chapter_id: %d）：%s\n", summary.ReadingNumber, summary.ChapterID, summary.Summary)
	}
	return []map[string]any{
		{"role": "system", "content": "你将章节级摘要压缩为叙事阶段块（chunk）。以叙事阶段转折为分界标准：当摘要显示叙事方向、情感基调或核心冲突发生明显变化时，应在该处划分边界。相邻且属于同一叙事阶段的章节应合并为一个块。每个块的 content 应概括该阶段的核心事件、关键人物行动与转折，约 100-200 字。章节号只用于理解阅读顺序；返回块端点时必须使用对应的 start_chapter_id 和 end_chapter_id。只能通过调用 output_chunks 返回结果。"},
		{"role": "user", "content": "根据以下章节摘要生成第一轮叙事阶段块：\n\n" + b.String()},
	}
}

func compressChunkMessages(chunks []Chunk, readingNumbers map[int64]int, round int) []map[string]any {
	var b strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "第%d-%d章（chapter_id: %d-%d） %s：%s\n", chunk.StartReadingNumber, chunk.EndReadingNumber, chunk.StartChapterID, chunk.EndChapterID, chunk.Name, chunk.Content)
	}
	references := chapterReferences(chunks, readingNumbers)
	return []map[string]any{
		{"role": "system", "content": "你将多个叙事阶段块合并为更少、更大的阶段块。规则如下：\n1. 优先合并相邻且属于同一叙事阶段的块（叙事方向一致、情感基调相同）。\n2. 若一个块的内部包含两个明显不同的叙事子阶段（如铺垫→爆发），则可以拆分，但应尽量减少拆分。\n3. 每个输出块的 content 应重新概括该合并后阶段的核心事件与转折，约 100-200 字。\n4. 必须保留准确的 start_chapter_id 和 end_chapter_id；章节号只用于理解阅读顺序。可用章节定位表列出了可作为端点的全部 ID。只能通过调用 output_chunks 返回结果。"},
		{"role": "user", "content": fmt.Sprintf("第 %d 轮压缩。将以下阶段块合并为更少的叙事阶段块：\n\n%s\n可用章节定位表：\n%s", round, b.String(), references)},
	}
}

func chapterReferences(chunks []Chunk, readingNumbers map[int64]int) string {
	if len(chunks) == 0 || len(readingNumbers) == 0 {
		return ""
	}
	type chapterReference struct {
		id     int64
		number int
	}
	refs := make([]chapterReference, 0, len(readingNumbers))
	for id, number := range readingNumbers {
		refs = append(refs, chapterReference{id: id, number: number})
	}
	slices.SortFunc(refs, func(a, b chapterReference) int { return a.number - b.number })
	var b strings.Builder
	for _, ref := range refs {
		fmt.Fprintf(&b, "第%d章=%d\n", ref.number, ref.id)
	}
	return b.String()
}

func finalSkillMessages(chunks []Chunk) []map[string]any {
	var b strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "第%d-%d章（chapter_id: %d-%d） %s：%s\n", chunk.StartReadingNumber, chunk.EndReadingNumber, chunk.StartChapterID, chunk.EndChapterID, chunk.Name, chunk.Content)
	}
	return []map[string]any{
		{"role": "system", "content": finalSkillSystemPrompt},
		{"role": "user", "content": "根据以下叙事阶段块生成最终的可复用叙事模式技能：\n\n" + b.String()},
	}
}

const finalSkillSystemPrompt = `你是一个专业的小说结构分析师。

根据提供的叙事阶段块，创建一个可复用的叙事模式技能（套路模板）。不要仅仅描述原书内容，而要抽象出可复用的模板。

只能通过调用 output_skill 工具返回结果，工具参数要求：
- name：中文模式名称（如"逆袭打脸流"）。
- description：一句话描述何时使用此叙事模式。
- content：技能正文 markdown，不含 frontmatter（frontmatter 由系统自动生成）。必须包含以下章节：

  ## 套路概览
  总结可复用的叙事结构。
  ## 阶段拆解
  列出每个阶段，包含源文章节范围、结构功能、常见信号以及如何复用。
  ## 爽点节奏
  说明压制、释放、揭示、挫折和高潮的分布规律。
  ## 角色功能模板
  抽象主角、盟友、对手、反派、导师、工具角色以及牺牲/对比角色的功能定位。
  ## 可复用叙事规律
  给出可以指导新作品创作的具体写作规则。
  ## 使用注意
  说明哪些地方必须改编，避免成品显得雷同。

再次强调：content 中应抽象出可复用模板，不要仅仅描述原书内容；content 中不要包含 YAML frontmatter。`

const mentalFinalSkillSystemPrompt = `你是一个小说创作心智分析师。

你的任务不是总结剧情，而是从这本小说的已写章节中，蒸馏出**作者/叙事者的"创作心智"**——即指导 AI 未来写作新章节时可复用的思维与表达操作系统。这是给写作系统使用的"创作指南"，不是人物小传，也不是剧情摘要。

只能通过调用 output_skill 工具返回结果，工具参数要求：
- name：中文名称，体现该作者的创作心智（如"X 的创作心智"）。
- description：一句话描述何时启用此创作心智技能（如"用 X 的叙事与表达习惯写新章节时"）。
- content：技能正文 markdown，不含 frontmatter（frontmatter 由系统自动生成）。必须包含以下五章，顺序固定：

  ## 叙事心智模型
  3–7 个作者构造叙事的核心信念/框架（如冲突如何生成、信息何时收放、对节奏/命运/agent 的信念）。**每个模型必须附章节锚点**（如"见第 N 章"或"第 N–M 章"），证明它真实出现在书中而非你的猜测。
  ## 创作决策启发式
  5–10 条面临写作抉择时的快速规则，每条附书中案例章节锚点，且须说明"遇到什么新情境会触发该规则"。
  ## 价值观与反模式
  作者追求什么、明确拒绝什么、以及作者内部张力（价值观冲突）。
  ## 表达 DNA
  句式指纹（长短句偏好、疑问/陈述比例）、高频词与禁忌词、节奏（先结论还是先铺垫、转折方式）、幽默与确定性语气。可复用 internal/style 的六维画像思路，并补充小说专属维度：POV 与视角处理、时态、意象密度、对话-叙述比。
  ## 诚实边界
  明确声明：本技能是从**虚构文本**推断的创作人格，**非作者真人三观**；可能混有角色个体声音而非作者稳定技法；并附推断依据（章节数 / 提取时间）。

**小说适配三重验证（防止臆造的关键）**：
1. 跨章节/跨情节弧复现：一个观点要成为"心智模型"，必须在本书中**至少 2 条相互独立的情节线/章节段**都出现；只出现一次的，降级为"创作决策启发式"；0 处印证的，不收录。
2. 有生成力：该模型必须能用于指导写出新章节（而非仅描述原书）；否则只是观察，不是心智模型。
3. 有排他性：区别于"show don't tell"等通用写作建议——只收录真正体现**这位作者独特性**的特质。

**质量自检（生成前逐项确认）**：每个心智模型是否都有≥2 章证据？模型数是否在 3–7？表达 DNA 是否有辨识度（不像通用 AI 腔）？诚实边界是否填了来源与时间？

**token 护栏（V2-A）**：「叙事心智模型」一节控制在约 1.5k–2.5k token（约 600–1000 字）内；整体 content 不超过 ~8k token。过细则合并相邻模型，过简则补证据。

{{LOW_CONFIDENCE_NOTE}}

再次强调：content 是给写作系统用的创作指南，要抽象出可复用规律；每个断言都要有章节锚点；content 中不要包含 YAML frontmatter。`

// mentalFinalSkillMessages 组装 mental flavor 的最终提示词。
// chapterCount 与 lowConfidence 用于注入样本量提示：章节少于 20 时要求缩减模型数量并声明样本不足。
// evidence 是 V2-D 可选的全书向量检索证据片段；非空时前置到 user 消息，减少 LLM 重读全本。
func mentalFinalSkillMessages(chunks []Chunk, chapterCount int, lowConfidence bool, evidence []string) []map[string]any {
	var b strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "第%d-%d章（chapter_id: %d-%d） %s：%s\n", chunk.StartReadingNumber, chunk.EndReadingNumber, chunk.StartChapterID, chunk.EndChapterID, chunk.Name, chunk.Content)
	}
	note := ""
	if lowConfidence {
		note = "**样本量提示**：本次提取仅基于 " + fmt.Sprintf("%d", chapterCount) + " 章，样本偏少。请将「叙事心智模型」缩减为 2–4 个最稳定的，并在「诚实边界」中明确声明样本不足、结论可能不稳定。"
	}
	prompt := strings.Replace(mentalFinalSkillSystemPrompt, "{{LOW_CONFIDENCE_NOTE}}", note, 1)

	userContent := "根据以下叙事阶段块，蒸馏该作者的创作心智技能：\n\n" + b.String()
	if len(evidence) > 0 {
		var eb strings.Builder
		eb.WriteString("\n\n## 全书证据片段（来自向量检索，供佐证，非必须全部采纳）\n")
		for i, p := range evidence {
			fmt.Fprintf(&eb, "\n证据%d：%s\n", i+1, strings.TrimSpace(p))
		}
		userContent += eb.String()
	}
	return []map[string]any{
		{"role": "system", "content": prompt},
		{"role": "user", "content": userContent},
	}
}

// lensTitles 是三个镜头的中文名，用于 user 消息说明本次聚焦的章节。
var lensTitles = map[string]string{
	"mental":     "叙事心智模型",
	"expression": "表达DNA",
	"decision":   "创作决策启发式/价值观/诚实边界",
}

// lensMentalSystemPrompt 返回多镜头精提中某个镜头的聚焦系统提示词。
// 复用 mentalFinalSkillSystemPrompt 的"蒸馏创作心智、非剧情摘要"核心 framing，但仅要求本镜头
// 指定的章节，并给出 token 护栏，使三路合计 ≤7k（低于单轮 8k 护栏）。
func lensMentalSystemPrompt(lens string, lowConfidence bool) string {
	var b strings.Builder
	b.WriteString(`你是一个小说创作心智分析师。

你的任务不是总结剧情，而是从这本小说的已写章节中，蒸馏出**作者/叙事者的"创作心智"**——指导 AI 未来写作新章节时可复用的思维与表达操作系统。这是给写作系统使用的"创作指南"，不是人物小传，也不是剧情摘要。

只能通过调用 output_skill 工具返回结果。本镜头只聚焦下面指定的章节（其余章节不要产出），content 中不要包含 YAML frontmatter。`)

	switch lens {
	case "mental":
		b.WriteString(`

工具参数要求：
- name：中文名称，体现该作者的创作心智（如"X 的创作心智"）。
- description：一句话描述何时启用此创作心智技能。
- content：技能正文 markdown。**本镜头只产出一章**：
  ## 叙事心智模型
  3–7 个作者构造叙事的核心信念/框架（如冲突如何生成、信息何时收放、对节奏/命运/agent 的信念）。**每个模型必须附章节锚点**（"见第 N 章"或"第 N–M 章"），证明它真实出现在书中而非猜测。
- evidence：每个心智模型的章节证据锚点（chapter_id 列表），用于确定性门禁（独立章节<2 的模型将降级为启发式）。

**小说适配三重验证**：①跨章节/弧复现≥2 章才作"心智模型"，仅一次降级为启发式，0 处不收录；②有生成力（能指导写新章）；③有排他性（只收真正体现该作者独特性者）。
**token 护栏**：本镜头「叙事心智模型」≤3k token（约 1200 字），过细则合并相邻模型，过简则补证据。`)
		if lowConfidence {
			b.WriteString("\n\n**样本量提示**：本次提取样本偏少，请将「叙事心智模型」缩减为 2–4 个最稳定的，并在「诚实边界」相关说明中声明样本不足（诚实边界由决策镜头产出，请在其章节中明确标注样本不足、结论可能不稳定）。")
		}
	case "expression":
		b.WriteString(`

工具参数要求：
- content：技能正文 markdown。**本镜头只产出一章**，其余参数（name/description/evidence）留空：
  ## 表达 DNA
  句式指纹（长短句偏好、疑问/陈述比例）、高频词与禁忌词、节奏（先结论还是先铺垫、转折方式）、幽默与确定性语气。复用 style 六维画像思路并补小说专属维度：POV 与视角处理、时态、意象密度、对话-叙述比。

**token 护栏**：本镜头「表达DNA」≤2k token（约 800 字），要有辨识度，不像通用 AI 腔。`)
	case "decision":
		b.WriteString(`

工具参数要求：
- content：技能正文 markdown。**本镜头产出三章（用 ## 分隔）**，其余参数（name/description/evidence）留空：
  ## 创作决策启发式
  5–10 条面临写作抉择时的快速规则，每条附书中案例章节锚点，并说明"遇到什么新情境会触发该规则"。
  ## 价值观与反模式
  作者追求什么、明确拒绝什么、以及作者内部张力（价值观冲突）。
  ## 诚实边界
  明确声明：本技能是从**虚构文本**推断的创作人格，**非作者真人三观**；可能混有角色个体声音而非作者稳定技法；并附推断依据（章节数 / 提取时间）。

**token 护栏**：本镜头三章合计 ≤2k token（约 800 字）。`)
		if lowConfidence {
			b.WriteString("\n\n**样本量提示**：本次提取样本偏少，请在「诚实边界」中明确声明样本不足、结论可能不稳定。")
		}
	default:
		// 未知镜头退回单轮完整提示词（不应发生，仅作兜底）。
		return mentalFinalSkillSystemPrompt
	}
	return b.String()
}

// lensMentalSkillMessages 组装多镜头精提中单个镜头的提示词。
// lowConfidence 透传给镜头系统提示词；evidence 是 V2-D 可选证据片段。
func lensMentalSkillMessages(chunks []Chunk, lowConfidence bool, evidence []string, lens string) []map[string]any {
	var b strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "第%d-%d章（chapter_id: %d-%d） %s：%s\n", chunk.StartReadingNumber, chunk.EndReadingNumber, chunk.StartChapterID, chunk.EndChapterID, chunk.Name, chunk.Content)
	}
	prompt := lensMentalSystemPrompt(lens, lowConfidence)

	title := lensTitles[lens]
	if title == "" {
		title = lens
	}
	userContent := "根据以下叙事阶段块，蒸馏该作者创作心智的「" + title + "」章节（仅产出于该镜头指定的章节）：\n\n" + b.String()
	if len(evidence) > 0 {
		var eb strings.Builder
		eb.WriteString("\n\n## 全书证据片段（来自向量检索，供佐证，非必须全部采纳）\n")
		for i, p := range evidence {
			fmt.Fprintf(&eb, "\n证据%d：%s\n", i+1, strings.TrimSpace(p))
		}
		userContent += eb.String()
	}
	return []map[string]any{
		{"role": "system", "content": prompt},
		{"role": "user", "content": userContent},
	}
}

const fidelityTestSystemPrompt = `你是一个严格的保真度审计员，负责核验一份"创作心智"写作技能是否忠实于它来源的小说，而非凭空臆造。

你不会修改技能，只做评判。请根据提供的"全书叙事阶段块"与"已生成的创作心智技能"，完成两项测试，并通过 output_fidelity 工具返回评分卡：

1. **已知立场一致性（known_consistency，1–5）**：从书中作者**已明确处理过**的冲突/抉择中挑选 1–2 个，判断：若严格按技能的指导去写新章节，是否会导向与该书实际处理**方向一致**的结局？一致给 4–5，部分一致给 3，矛盾或扭曲给 1–2。

2. **未知题材适度不确定（unknown_uncertainty，1–5）**：设想一个本书作者**从未涉及**的题材/类型（如慢热爱情副线、法庭戏、星际政治），判断：技能是否会**过度断言**它在这种题材下也成立（斩钉截铁、以偏概全）？还是会**诚实表达适度不确定**（"这位作者在该题材下如何表现尚不可知，需以真实文本核验"）？能避免过度断言给 4–5，照搬到未知领域给 1–2。

将发现的具体问题写入 issues 列表（若无则留空）。passed 在两项均 ≥3 时为 true。只通过 output_fidelity 工具返回，不要输出多余文字。`

// fidelityTestMessages 组装产出后保真度自测的提示词：把全书阶段块与已生成技能一并交给审计员。
func fidelityTestMessages(chunks []Chunk, skillContent string) []map[string]any {
	var b strings.Builder
	for _, chunk := range chunks {
		fmt.Fprintf(&b, "第%d-%d章（chapter_id: %d-%d） %s：%s\n", chunk.StartReadingNumber, chunk.EndReadingNumber, chunk.StartChapterID, chunk.EndChapterID, chunk.Name, chunk.Content)
	}
	return []map[string]any{
		{"role": "system", "content": fidelityTestSystemPrompt},
		{"role": "user", "content": fmt.Sprintf("以下是全书叙事阶段块：\n\n%s\n\n以下是已生成的创作心智技能正文：\n\n%s\n\n请按系统指令完成两项保真度测试并返回评分卡。", b.String(), skillContent)},
	}
}
