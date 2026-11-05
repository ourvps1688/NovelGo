package pipeline

// ── 复验向量检索证据装配（RAG-Verify，2026-11-05）──────────────────────────
//
// 窗口式语料内联（verify_corpus）把整章原文喂给模型自行找修复点，思考模型面对
// 30~40k 字符大语料必然长链思维——慢速供应商下单批 15 分钟预算屡屡烧满（DB 实据
// I7/I9~I12「单批 15 分钟预算耗尽」）。减预算只是止损，减语料才是根治。
//
// RAG-Verify：用项目 ONNX 向量索引（bge-small-zh 512 维，rag.VectorStore 按章
// chunk 检索）把「与工单相关的段落」定位出来，只喂证据切片——证据密度高、思维量
// 数量级下降；且 topK 固定，输入体量与书长无关（百万字小说复验不比 10 章慢）。
//
// 每张工单三层证据：
//  ① 作用域章内向量命中段（每章保底命中，修复点所在章必有证据）；
//  ② 台账引用定点行（desc/note 的 #\d+ 引用 → 只挑被引用条目，替代整表快照）；
//  ③ 零命中/无向量库 → 回退既有窗口语料（章头尾+紧凑台账），绝不阻断复验、
//     绝不因证据缺失瞎判（拿不准不得计 ok=true，留待下轮）。
//
// 与巡检预筛（patrol_prescreen）同一惯例：embedder/向量库全局单例，测试经
// verifySearcherProvider 注入 stub，不依赖真实 ONNX/sqlite-vec。

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/novelgo/novelgo/internal/rag"
)

// verifyEvidenceSearcher 是证据检索所需的向量库最小接口（*rag.VectorStore 满足；
// 测试注入 stub，避免依赖真实 ONNX 模型与 sqlite-vec）。
type verifyEvidenceSearcher interface {
	SearchBatch(ctx context.Context, novelID int64, queries []string, topK int, filter *rag.SearchFilter) ([][]rag.SearchResult, error)
}

// verifySearcherProvider 取向量检索器；测试可覆盖注入 stub（patrolEmbedderProvider 同模式）。
// 向量库未初始化（rag.InitVectorStore 未跑/单测环境）时返回 nil → 整体降级窗口语料。
var verifySearcherProvider = func() verifyEvidenceSearcher {
	if vs := rag.GetVectorStore(); vs != nil {
		return vs
	}
	return nil
}

const (
	// verifyEvidenceTopKPerChapter 是每个作用域章的检索命中段数（主张查询+修复查询
	// 合并去重后）；作用域章多时降为 1，保证每章保底又不膨胀证据。
	verifyEvidenceTopKPerChapter = 2
	// verifyEvidenceGlobalTopK 是无作用域（台账/全书级工单）时的全局命中段数。
	verifyEvidenceGlobalTopK = 6
	// verifyEvidenceChunkCap 是单个证据段渲染上限（字符）：chunk 原文 ~420 token，
	// 截断只掐中段保头尾（尾部常是修复点）。
	verifyEvidenceChunkCap = 900
	// verifyEvidencePerIssueCap 是单工单证据切片总字符帽（符文）。
	verifyEvidencePerIssueCap = 4500
	// verifyEvidenceBatchCap 是单批证据总字符帽（符文）：4 工单合验提示词总量
	// ≈1.5 万字符，约为旧窗口语料（40k 字节+8k 字节）有效体量的 1/3 且证据密度更高。
	verifyEvidenceBatchCap = 12000
	// verifyLedgerRefCap 是台账定点行渲染总帽（被引用行通常个位数）。
	verifyLedgerRefCap = 4000
	// verifyLedgerBackgroundCap 是无显式引用时的台账背景帽（仅交叉核对用，从简）。
	verifyLedgerBackgroundCap = 2500
	// verifyFallbackChapterWindow 是零命中兜底窗口的单章帽（头尾保留，符文）。
	verifyFallbackChapterWindow = 2500
	// verifyEvidenceQueryCap 是单条检索查询文本上限（符文）：查询只需问题/修复主张。
	verifyEvidenceQueryCap = 400
)

// buildVerifyQueries 生成单工单的检索查询：①问题主张（area+desc）②修复说明（fix note，
// 非空时）。两条查询分别命中「问题证据段」与「修复落点段」，合并去重。
func buildVerifyQueries(t verifyTarget) []string {
	q1 := truncateToBudget(strings.TrimSpace(t.Area+"："+t.Desc), verifyEvidenceQueryCap)
	queries := []string{q1}
	if q2 := truncateToBudget(strings.TrimSpace(t.FixNote), verifyEvidenceQueryCap); q2 != "" && q2 != q1 {
		queries = append(queries, q2)
	}
	return queries
}

// verifyEvidenceChunk 是一条检索命中的证据段（携带展示定位）。
type verifyEvidenceChunk struct {
	order int    // 作用域章序（排序用）
	num   int64  // 章全局阅读序号（0=未知）
	title string // 章标题
	pos   int    // chunk 在章正文中的 rune 偏移
	text  string // chunk 原文
	id    string // chunk ID（去重用）
}

// buildVerifyEvidenceBatch 渲染单批的 RAG-Verify【本次复验语料】块：逐工单证据切片
// （向量检索命中段 + 引用台账行），单批总帽 verifyEvidenceBatchCap。
// 检索器缺失（向量库未初始化）→ 整体降级既有窗口语料（与旧行为逐字一致）；
// 单工单零命中 → 该工单窗口兜底，绝不因检索失败阻断或瞎判。
func (s *Service) buildVerifyEvidenceBatch(ctx context.Context, novelID int64, src verifyCorpusSource, targets []verifyTarget) string {
	searcher := verifySearcherProvider()
	if searcher == nil {
		nums := make([]int64, 0, len(targets))
		for _, t := range targets {
			nums = append(nums, t.Chapters...)
		}
		return s.buildVerifyCorpus(ctx, novelID, src, nums)
	}

	var sb strings.Builder
	sb.WriteString("\n\n【本次复验语料（后端向量检索已定位证据切片，本批所需证据已全部内联）】\n")
	sb.WriteString("**复验方法：直接通读以下证据切片逐项核对，禁止调用任何工具（read / get_* / search）；" +
		"核对完成后直接在最终回复末尾输出复验回执 JSON。**\n")
	sb.WriteString("（以下为与各工单最相关的正文片段与台账条目；未覆盖到的改动拿不准时" +
		"不要计入 ok=true，留待下轮复核）\n")

	budget := verifyEvidenceBatchCap
	for _, t := range targets {
		if budget <= 0 {
			fmt.Fprintf(&sb, "\n### 工单 %s 的证据（单批证据预算已满，本工单证据省略；不得计入 ok=true）\n", t.ID)
			continue
		}
		sec := s.buildIssueEvidence(ctx, novelID, src, searcher, t)
		if sec == "" {
			continue
		}
		if utf8.RuneCountInString(sec) > budget {
			sec = truncateToBudget(sec, budget) + "\n（本工单证据预算已截断；拿不准的项不要计入 ok=true）\n"
			budget = 0
		} else {
			budget -= utf8.RuneCountInString(sec)
		}
		sb.WriteString(sec)
	}
	return sb.String()
}

// buildIssueEvidence 渲染单张工单的证据切片小节：向量命中段（按作用域章序+章内位置
// 排序，chunk 去重）+ 台账证据（显式 #id 引用定点行优先，无引用给小背景）。
// 零命中回退作用域章窗口兜底；整节受 verifyEvidencePerIssueCap 帽约束。
func (s *Service) buildIssueEvidence(ctx context.Context, novelID int64, src verifyCorpusSource, searcher verifyEvidenceSearcher, t verifyTarget) string {
	queries := buildVerifyQueries(t)
	hits := s.searchIssueChunks(ctx, novelID, src, searcher, queries, t)

	var sb strings.Builder
	if len(hits) == 0 {
		fmt.Fprintf(&sb, "\n### 工单 %s 的证据（向量检索零命中，作用域章窗口兜底）\n", t.ID)
		s.appendChapterWindows(&sb, ctx, novelID, src, t.Chapters, verifyFallbackChapterWindow, verifyEvidencePerIssueCap)
	} else {
		fmt.Fprintf(&sb, "\n### 工单 %s 的证据切片（向量检索命中 %d 段）\n", t.ID, len(hits))
		for _, h := range hits {
			num := "未知"
			if h.num > 0 {
				num = fmt.Sprintf("%d", h.num)
			}
			title := h.title
			if title == "" {
				title = "未知标题"
			}
			fmt.Fprintf(&sb, "\n【第%s章《%s》片段 @字符%d】\n%s\n", num, title, h.pos, capHeadTail(h.text, verifyEvidenceChunkCap))
		}
	}

	// 台账证据：显式 #id 引用 → 定点行；无引用 → 小帽背景（交叉核对用）。
	if refs := extractLedgerRefIDs(t.Area, t.Desc, t.FixNote); len(refs) > 0 {
		if led := filterLedgerByRefs(src.ledger, refs); led != "" {
			sb.WriteString("\n（工单引用的台账条目）\n")
			sb.WriteString(truncateToBudget(led, verifyLedgerRefCap))
		}
	} else if src.ledger != "" {
		sb.WriteString("\n（台账背景，已截断，仅交叉核对用）\n")
		sb.WriteString(truncateToBudget(src.ledger, verifyLedgerBackgroundCap))
	}

	out := sb.String()
	if utf8.RuneCountInString(out) > verifyEvidencePerIssueCap {
		out = truncateToBudget(out, verifyEvidencePerIssueCap) + "\n（本工单证据已截断；拿不准的项不要计入 ok=true）\n"
	}
	return out
}

// searchIssueChunks 执行单工单的分章向量检索并装配命中段：
//   - 有作用域章：逐章检索（filter 锁定该章，防止语义漂移命中无关章），每章保底
//     topK 命中（章多时降 1）；某章检索失败仅跳过（降级该章，绝不阻断）。
//   - 无作用域（台账/全书级）：全局检索 topK=verifyEvidenceGlobalTopK。
//
// 返回结果按（作用域章序, 章内 rune 位置）排序、chunk 去重。
func (s *Service) searchIssueChunks(ctx context.Context, novelID int64, src verifyCorpusSource, searcher verifyEvidenceSearcher, queries []string, t verifyTarget) []verifyEvidenceChunk {
	var out []verifyEvidenceChunk
	appendResults := func(order int, num int64, title string, res []rag.SearchResult) {
		for _, r := range res {
			out = append(out, verifyEvidenceChunk{
				order: order, num: num, title: title,
				pos: r.StartRunePos, text: r.Content, id: r.ChunkID,
			})
		}
	}

	if len(t.Chapters) == 0 {
		res, err := searcher.SearchBatch(ctx, novelID, queries, verifyEvidenceGlobalTopK, &rag.SearchFilter{ChunkTypes: []string{"content"}})
		if err != nil {
			s.logger.Warn("复验证据检索：全局检索失败，本工单回退窗口兜底", "issue", t.ID, "err", err)
			return nil
		}
		for _, rs := range res {
			for _, r := range rs {
				num, title := src.displayByChapterID(r.ChapterID)
				appendResults(0, num, title, []rag.SearchResult{r})
			}
		}
	} else {
		topK := verifyEvidenceTopKPerChapter
		if len(t.Chapters) > 3 {
			topK = 1
		}
		for order, n := range t.Chapters {
			ch, ok := src.byReading[n]
			if !ok {
				continue
			}
			res, err := searcher.SearchBatch(ctx, novelID, queries, topK, &rag.SearchFilter{
				ChapterIDs: []int64{ch.ID}, ChunkTypes: []string{"content"},
			})
			if err != nil {
				s.logger.Warn("复验证据检索：章检索失败，跳过该章", "issue", t.ID, "chapter", n, "err", err)
				continue
			}
			for _, rs := range res {
				appendResults(order, n, ch.Title, rs)
			}
		}
	}

	// chunk 去重（多查询可能命中同一段），保首次出现顺序，再按（章序, 位置）排序。
	seen := map[string]bool{}
	dedup := out[:0]
	for _, h := range out {
		if h.id != "" && seen[h.id] {
			continue
		}
		if h.id != "" {
			seen[h.id] = true
		}
		dedup = append(dedup, h)
	}
	sort.SliceStable(dedup, func(i, j int) bool {
		if dedup[i].order != dedup[j].order {
			return dedup[i].order < dedup[j].order
		}
		return dedup[i].pos < dedup[j].pos
	})
	return dedup
}

// displayByChapterID 反查 chapters.id → （全局阅读序号, 标题）；找不到返回 0/空。
func (src verifyCorpusSource) displayByChapterID(chapterID int64) (int64, string) {
	for num, ch := range src.byReading {
		if ch.ID == chapterID {
			return num, ch.Title
		}
	}
	return 0, ""
}

// ── 台账引用定点提取 ─────────────────────────────────────────────────────

// ledgerRefRe 提取裸 #\d+ 台账引用（契约：台账引用写 <type>#<id>，裸 #\d+ 一律视为
// 台账引用）。「章#N」是章节引用，提取前先剔除，避免误抓。
var (
	ledgerRefRe     = regexp.MustCompile(`#(\d{1,12})`)
	chapterRefStrip = regexp.MustCompile(`章#\d+`)
)

// extractLedgerRefIDs 从多段文本提取被引用的台账条目 id 集合（十进制字符串键）。
func extractLedgerRefIDs(texts ...string) map[string]bool {
	ids := map[string]bool{}
	for _, text := range texts {
		clean := chapterRefStrip.ReplaceAllString(text, "")
		for _, m := range ledgerRefRe.FindAllStringSubmatch(clean, -1) {
			ids[m[1]] = true
		}
	}
	return ids
}

// filterLedgerByRefs 从台账快照文本只挑被引用条目的行（`- id=N; ...` 字段行，id 精确
// 匹配）及其所属表头（`#### 表名`），其余全部丢弃——替代整表快照，压缩证据体量。
func filterLedgerByRefs(ledger string, refs map[string]bool) string {
	if ledger == "" || len(refs) == 0 {
		return ""
	}
	var sb strings.Builder
	headerPending := ""
	for _, line := range strings.Split(ledger, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "####"):
			headerPending = trimmed
		case strings.HasPrefix(trimmed, "- "):
			if id, ok := ledgerLineID(trimmed); ok && refs[id] {
				if headerPending != "" {
					sb.WriteString(headerPending + "\n")
					headerPending = ""
				}
				sb.WriteString(trimmed + "\n")
			}
		}
	}
	return sb.String()
}

// ledgerLineID 解析台账行（`- k=v; k=v; ...`）里 id 字段的值；无 id 字段返回 false。
// 只认字段级 `id=` 前缀，不匹配 `novel_id=` 等长键（键按分号字段切分后精确前缀比对）。
func ledgerLineID(line string) (string, bool) {
	for _, part := range strings.Split(strings.TrimPrefix(line, "- "), "; ") {
		if v, ok := strings.CutPrefix(part, "id="); ok && v != "" {
			return v, true
		}
	}
	return "", false
}
