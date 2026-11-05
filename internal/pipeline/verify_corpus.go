package pipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/chapter"
)

// ── 复验语料预取（复验终极提速：取数与推理分离）────────────────────────────
//
// 旧设计：signability-verify 靠 agent 多轮工具往返自取证据（read 逐章读 + get_* 读台账），
// MaxTurns=12，思考模型单工单 2~10min，N 张工单串行叠加 → 复验动辄半小时以上。
// 取数本是后端对 SQLite/磁盘的毫秒级本地读，却让最贵的 LLM 环节用 12 轮工具往返去拿。
//
// 新设计（2026-11-05）：派发前后端批量取数——工单作用域章节正文 + 台账快照（紧凑版）——
// 整块内联进提示词（【本次复验语料】），明确禁止工具往返，一轮推理直接出复验回执。
// 与巡检 buildPatrolCorpus 同一教训同一药方：让 LLM 自己取数是最贵的取数方式；
// 复验证据完全可预知（修过的作用域章节 + 本轮修复日志），后端取好直接喂。
// 体量纪律（巡检超时复盘）：单批语料远小于巡检成功线（44k 字符实测 16.8min），
// 多数批次 1~3 章 ≈ 万级字符，单调用分钟内收口。

const (
	// verifyChapterCap 是单章正文注入上限（字符）：复验要核对修复点，作用域章尽量全量
	// 纳入；超长章只掐头尾（头 2/3 + 尾 1/3，保章尾钩子类修复点），中段截断并标注。
	verifyChapterCap = 8000
	// verifyCorpusCap 是单批章正文总字符帽：批次作用域通常 1~6 章，40k 覆盖绝大多数
	// 批次且单调用上下文远低于巡检收紧后的实测成功线。
	verifyCorpusCap = 40000
	// verifyLedgerCap 是复验台账快照字符帽：台账仅作交叉核对背景（主证据是作用域正文），
	// 8k 紧凑版足够，避免每批重复携带大台账拖慢单调用。
	verifyLedgerCap = 8000
)

// verifyCorpusSource 是一轮复验的共享取数缓存：章节索引（阅读序号→章行）与台账快照
// 在批间共享（每批只重渲染作用域正文，避免逐批重复查表/读盘）。ledger 保留 dumpLedgers
// 全量（内含 patrolLedgerCap 自帽），RAG-Verify 按引用定点过滤与窗口渲染各自在渲染端截断。
type verifyCorpusSource struct {
	byReading map[int64]chapter.Chapter // 阅读序号（工单章号）→ 章节行（含 ID/标题）
	ledger    string                    // 台账快照（dumpLedgers 全量，渲染端各自截断）
}

// loadVerifyCorpusSource 一次性取本轮复验的共享语料源（本地读，无 LLM 往返）。
// 任何单步失败仅降级（空索引/空台账），绝不阻断复验——语料缺失的批次退化为
// 「证据不全」提示，由回执未提及/留待下轮语义兜底，绝不臆测修复状态。
func (s *Service) loadVerifyCorpusSource(ctx context.Context, novelID int64) verifyCorpusSource {
	src := verifyCorpusSource{byReading: map[int64]chapter.Chapter{}}
	if s.chapterStore != nil {
		if chapters, err := s.chapterStore.ListAllByNovel(ctx, nil, novelID); err == nil {
			for _, ch := range chapters {
				src.byReading[int64(ch.ReadingNumber)] = ch
			}
		} else {
			s.logger.Warn("复验语料：列章失败，本批章正文降级为空", "novel_id", novelID, "err", err)
		}
	}
	src.ledger = s.dumpLedgers(ctx, novelID)
	return src
}

// buildVerifyCorpus 渲染单批的窗口式【本次复验语料】块：作用域章节正文（去重按阅读序）+
// 台账快照 + 「证据已内联、禁止工具」引导。chapterNums 为工单章号（全局阅读序号，
// 与 pipeline_issues.chapter_ids 同口径，经 byReading 映射回 chapter_id 读正文）。
// RAG-Verify（verify_retrieval.go）生效时本函数仅作向量库缺失的整体降级路径。
func (s *Service) buildVerifyCorpus(ctx context.Context, novelID int64, src verifyCorpusSource, chapterNums []int64) string {
	var sb strings.Builder
	sb.WriteString("\n\n【本次复验语料（后端已批量取好，本批所需证据已全部内联）】\n")
	sb.WriteString("**复验方法：直接通读以下语料逐项核对，禁止调用任何工具（read / get_* / search）；" +
		"核对完成后直接在最终回复末尾输出复验回执 JSON。**\n")

	if s.appendChapterWindows(&sb, ctx, novelID, src, chapterNums, verifyChapterCap, verifyCorpusCap) {
		sb.WriteString("\n（因单批语料预算，其余作用域章正文已省略；拿不准的项不要计入 ok=true，留待下轮复核）\n")
	}
	if src.ledger != "" {
		sb.WriteString("\n### 台账快照（紧凑，交叉核对背景）\n")
		sb.WriteString(truncateToBudget(src.ledger, verifyLedgerCap))
	}
	return sb.String()
}

// appendChapterWindows 渲染作用域章正文窗口（去重按阅读序）：每章 capHeadTail(章帽)，
// 累计超过 totalCap 的章省略并返回 omitted=true；章缺失/正文不可读只标注不阻断。
// 窗口渲染共用件：窗口语料主路径与 RAG-Verify 零命中兜底共用。
func (s *Service) appendChapterWindows(sb *strings.Builder, ctx context.Context, novelID int64, src verifyCorpusSource, chapterNums []int64, chapterCap, totalCap int) bool {
	seen := map[int64]bool{}
	omitted := false
	base := sb.Len()
	for _, n := range chapterNums {
		if seen[n] {
			continue
		}
		seen[n] = true
		if sb.Len()-base > totalCap {
			omitted = true
			continue
		}
		ch, ok := src.byReading[n]
		if !ok {
			fmt.Fprintf(sb, "\n### 第%d章（章节不存在或列表缺失，无法内联）\n", n)
			continue
		}
		title := ch.Title
		if title == "" {
			title = fmt.Sprintf("第%d章", n)
		}
		text := ""
		if s.chapterTextReader != nil {
			if t, rerr := s.chapterTextReader(ctx, novelID, ch.ID); rerr == nil {
				text = t
			}
		}
		if strings.TrimSpace(text) == "" {
			fmt.Fprintf(sb, "\n### 第%d章《%s》（正文不可读，无法内联）\n", n, title)
			continue
		}
		fmt.Fprintf(sb, "\n### 第%d章《%s》\n%s\n", n, title, capHeadTail(text, chapterCap))
	}
	return omitted
}

// capHeadTail 超长文本掐头尾保留：头 2/3 + 尾 1/3（保章尾钩子等尾部修复点），中段标注截断。
// 符文边界截断，避免截断半个多字节字符。
func capHeadTail(text string, cap int) string {
	if cap <= 0 {
		return ""
	}
	r := []rune(text)
	if len(r) <= cap {
		return text
	}
	head := cap * 2 / 3
	tail := cap - head
	return string(r[:head]) + "\n…（中段已截断）…\n" + string(r[len(r)-tail:])
}
