package mcp_tools

// chapter_ci_corpus.go —— #117 P1 方案 C：章节 L1 质检语料内联（取数与推理分离）。
//
// 旧链路把取数交给 chapterci 子 Agent：指令让它自己 read / get_chapter_list /
// get_chapter_task_card / get_*，实测每次写稿后 6 轮工具循环并出现 8m13s 轮间空档，
// 而所有工具 elapsed_ms 都是 0–4ms —— 时间全花在 LLM 轮间推理/排队，纯提示词
// 约束（「一次读齐」）挡不住。取数本是后端对磁盘/SQLite 的毫秒级本地读，
// 与 pipeline/verify_corpus.go 同一药方：派发前一次性取齐内联，让模型只做一轮分析。
//
// 纪律：
//   - 纯本地读取，零 LLM、零上传、零新增执行体；不碰回执解析与 UpsertL1。
//   - 任何单步失败只降级（跳过该块 / 整体返回空串），零 WARN 零阻断；corpus 为空时
//     指令逐字保持旧文案，模型仍可自取 —— 不新增「跳过落盘」分支，#115 P0 不动。
//   - mcp_tools 不能反向 import pipeline（pipeline→agent→mcp_tools 成环），
//     故预算/截断/台账渲染在本文件自持一份，不复用 dumpLedgers / capHeadTail。

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/text"
)

const (
	// 分段预算按优先级分配：本章正文是主证据优先保，其次是邻居/任务卡/台账。
	// 合计 6000+600+600+1500+3000=11700，加标题噪声后由总帽兜底。
	// 巡检复盘（patrol_corpus.go）：44k 字符曾让思考模型 >45min 仍吐不完，
	// 故此处取 12000 rune 保守帽，配合「一轮出回执」把单调用上下文压到约 14k rune。
	chapterCIBodyCap       = 6000
	chapterCINeighborCap   = 600
	chapterCITaskCardCap   = 1500
	chapterCILedgerCap     = 3000
	chapterCICorpusCap     = 12000
	chapterCILedgerRows    = 60
	chapterCILedgerCellCap = 240
)

// chapterCILedgerTables 是与 L1 检查项直接相关的台账表（均含 novel_id 列）。
// 顺序即优先级：读者认知态/伏笔/任务卡兑现/一致性；表不存在时只跳过该表。
var chapterCILedgerTables = []string{
	"reader_perspectives",
	"time_entries",
	"chapter_task_cards",
	"characters",
	"chronology_anchors",
	"story_arcs",
	"arc_nodes",
	"pinned_invariants",
}

// chapterCITextReader 是正文读取的可替换入口（测试注入内存正文，不动全局 DataDir）。
// 与 RecalcChapterL0FromDisk 同源：git.ReadFile(novelID, git.ChapterPath(chapterID))。
var chapterCITextReader = func(_ context.Context, novelID, chapterID int64) (string, error) {
	return git.ReadFile(novelID, git.ChapterPath(chapterID))
}

// buildChapterCICorpus 在派发前一次性取齐本章质检证据，渲染为可注入提示词的语料块。
// 返回空串表示降级（db 未注入 / 本章正文不可读 / 列章失败），调用方回落旧的自取指令。
// 只做本地读，不写库、不派发、不告警。
func buildChapterCICorpus(ctx context.Context, db *gorm.DB, logger *slog.Logger, novelID, chapterID int64) string {
	if db == nil || chapterCITextReader == nil {
		return ""
	}
	if logger == nil {
		logger = slog.Default()
	}
	body, err := chapterCITextReader(ctx, novelID, chapterID)
	if err != nil || strings.TrimSpace(body) == "" {
		return "" // 正文是主证据：读不到就整体降级，绝不半截语料误导模型
	}
	chapters, err := chapter.NewStore(db, logger).ListAllByNovel(ctx, nil, novelID)
	if err != nil {
		return ""
	}

	idx := -1
	for i := range chapters {
		if chapters[i].ID == chapterID {
			idx = i
			break
		}
	}

	var b strings.Builder
	b.WriteString("\n\n【本章质检语料（后端已一次性取齐，直接通读核对，无需再自行取数）】\n")
	b.WriteString("**字数口径：凡涉及章长/字数，一律以本章标注的「权威字数（word_count）」为准——" +
		"纯汉字+英文词，不含标点/空格/换行；禁止自行数语料字符，禁止把邻居片段或台账快照计入本章正文。**\n")

	title := ""
	num := 0
	if idx >= 0 {
		title = chapters[idx].Title
		num = chapters[idx].ReadingNumber
	}
	if title == "" {
		title = fmt.Sprintf("第%d章", num)
	}
	fmt.Fprintf(&b, "\n### 本章正文（第%d章《%s》· chapter_id=%d · 权威字数 %d 字（word_count））\n%s\n",
		num, title, chapterID, text.ComputeStats(body).WordCount,
		chapterCICapHeadTail(body, chapterCIBodyCap))

	// 邻居只作连贯性/伏笔对照，明确标注不计入本章字数。
	if idx > 0 {
		if tail := chapterCINeighborText(ctx, novelID, chapters[idx-1].ID); tail != "" {
			fmt.Fprintf(&b, "\n### 前一章结尾片段（第%d章《%s》，仅作连贯性对照，不计入本章字数）\n%s\n",
				chapters[idx-1].ReadingNumber, chapterCINeighborTitle(&chapters[idx-1]),
				chapterCITailRunes(tail, chapterCINeighborCap))
		}
	}
	if idx >= 0 && idx+1 < len(chapters) {
		if head := chapterCINeighborText(ctx, novelID, chapters[idx+1].ID); head != "" {
			fmt.Fprintf(&b, "\n### 次一章开头片段（第%d章《%s》，仅作伏笔对照，不计入本章字数）\n%s\n",
				chapters[idx+1].ReadingNumber, chapterCINeighborTitle(&chapters[idx+1]),
				chapterCIHeadRunes(head, chapterCINeighborCap))
		}
	}

	if card := taskCardForChapter(ctx, ToolContext{DB: db, NovelID: novelID, Logger: logger}, chapterID); card != nil {
		fmt.Fprintf(&b, "\n### 本章计划卡（任务卡兑现核对）\n%s\n",
			chapterCICapRunes(FormatTaskCard(card), chapterCITaskCardCap))
	}

	if ledger := buildChapterCILedgerSnapshot(ctx, db, novelID); ledger != "" {
		b.WriteString("\n### 相关台账快照（紧凑，交叉核对背景，不计入本章字数）\n")
		b.WriteString(ledger)
	}
	return chapterCICapRunes(b.String(), chapterCICorpusCap)
}

func chapterCINeighborText(ctx context.Context, novelID, id int64) string {
	s, err := chapterCITextReader(ctx, novelID, id)
	if err != nil || strings.TrimSpace(s) == "" {
		return ""
	}
	return strings.TrimSpace(s)
}

func chapterCINeighborTitle(ch *chapter.Chapter) string {
	if ch.Title != "" {
		return ch.Title
	}
	return fmt.Sprintf("第%d章", ch.ReadingNumber)
}

// buildChapterCILedgerSnapshot 渲染与 L1 检查项相关的台账紧凑快照；单表查询失败
// 只跳过该表，逐行按 rune 预算累加（与已发送文本同口径），绝不臆造条目。
func buildChapterCILedgerSnapshot(ctx context.Context, db *gorm.DB, novelID int64) string {
	var b strings.Builder
	used := 0
	for _, tbl := range chapterCILedgerTables {
		if used >= chapterCILedgerCap {
			break
		}
		// 先判表存在：某些迁移版本/测试库没有这些表，直接 Find 会让 GORM 默认
		// logger 每次派发刷一条 "no such table" 错误行，违背「降级零提示零报错」。
		if !db.WithContext(ctx).Migrator().HasTable(tbl) {
			continue
		}
		rows := []map[string]any{}
		if err := db.WithContext(ctx).Table(tbl).
			Where("novel_id = ?", novelID).
			Limit(chapterCILedgerRows).Find(&rows).Error; err != nil || len(rows) == 0 {
			continue
		}
		header := fmt.Sprintf("\n#### %s（%d 条）\n", tbl, len(rows))
		if used+len([]rune(header)) >= chapterCILedgerCap {
			break
		}
		b.WriteString(header)
		used += len([]rune(header))
		for _, row := range rows {
			line := chapterCILedgerRow(row) + "\n"
			n := len([]rune(line))
			if used+n > chapterCILedgerCap {
				b.WriteString("（台账快照因预算已截断）\n")
				return b.String()
			}
			b.WriteString(line)
			used += n
		}
	}
	return b.String()
}

// chapterCILedgerRow 渲染单行台账为「- 字段=值; …」；跳过时间戳与 novel_id 噪声列，
// key 排序保证输出确定性（map 无序会让语料在两次运行间漂移）。
func chapterCILedgerRow(row map[string]any) string {
	keys := make([]string, 0, len(row))
	for k := range row {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "novel_id" || k == "created_at" || k == "updated_at" || strings.HasSuffix(k, "_at") {
			continue
		}
		parts = append(parts, k+"="+chapterCILedgerCell(row[k]))
	}
	return "- " + strings.Join(parts, "; ")
}

func chapterCILedgerCell(v any) string {
	var s string
	switch x := v.(type) {
	case nil:
		return "∅"
	case []byte:
		s = string(x)
	case string:
		s = x
	default:
		s = fmt.Sprint(x)
	}
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > chapterCILedgerCellCap {
		return string(r[:chapterCILedgerCellCap]) + "…"
	}
	return s
}

// ── rune 预算截断（与 pipeline/truncateToBudget 同口径，本包自持一份避免成环）──

func chapterCICapRunes(s string, cap int) string {
	if cap <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= cap {
		return s
	}
	return string(r[:cap]) + "\n…（已按预算截断）"
}

// chapterCICapHeadTail 超长正文掐头尾保留（头 2/3 + 尾 1/3，保章尾钩子），
// 符文边界截断避免切半个多字节字符。
func chapterCICapHeadTail(s string, cap int) string {
	if cap <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= cap {
		return s
	}
	head := cap * 2 / 3
	tail := cap - head
	return string(r[:head]) + "\n…（中段已截断）…\n" + string(r[len(r)-tail:])
}

func chapterCIHeadRunes(s string, cap int) string {
	r := []rune(s)
	if len(r) <= cap {
		return s
	}
	return string(r[:cap]) + "\n…（后略）"
}

func chapterCITailRunes(s string, cap int) string {
	r := []rune(s)
	if len(r) <= cap {
		return s
	}
	return "…（前略）\n" + string(r[len(r)-cap:])
}
