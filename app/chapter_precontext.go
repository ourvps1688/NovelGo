package app

// B4：写章自动前情注入。
//
// 审计结论（docs/CODE_AUDIT_OVERVIEW_2026-10-29.md §3.1）：「写章无自动前情注入」
// 是写作上下文智能缺口——用户让 AI 写/续写章节时，模型拿不到上一章正文，难以保持
// 情节、人物、文风、设定的连贯。本文件复用 chat 既有的 <system-reminder> 注入通道
// （与 autoInject/slashInject 同源、同持久化语义：ToAPI=true/ToFrontend=false），
// 在「判定为写章意图」的轮次自动注入「最新一章正文」作为前情。
//
// 与 contextasm 纪律一致（docs/…/contextasm.go）：
//  1. 有则注入：命中写章意图且存在可读的前文 → 注入；
//  2. 无则降级：无前文 / 读失败 / 空正文 → 返回空串，绝不阻塞、不改主流程；
//  3. 预算截断：前文超预算时尾部截断并标注「（已截断）」，按 rune 边界，绝不注入半截；
//  4. 坏数据宁缺勿错：非法 UTF-8 整块放弃。

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/novelgo/novelgo/internal/git"
)

// preContextBudgetRunes 是单轮注入前情的最大 rune 数（约 3000 中文字）。
// 预算纪律（contextasm §0.2）：超限尾部截断，避免单章正文撑爆上下文、稀释注意力。
const preContextBudgetRunes = 3000

// chapterWritingCues 是「写章意图」的确定性 cue 集合（中文网文写作常见触发词）。
// 采用确定性（非 LLM）匹配：零额外往返、可预测、可测。仅作「是否注入前情」的粗判，
// 真正的创作内容仍交给模型；漏判顶多不注入（无副作用），误判最多多带一段前文（无害）。
var chapterWritingCues = []string{
	"写章", "写第", "写一章", "写下一章", "写新章", "写正文", "写草稿",
	"续写", "接着写", "继续写", "接着上文", "接着前文", "接着往下写", "继续往下写",
	"下章", "下一章", "本章", "这章", "开篇", "写开头", "写下去", "写一段",
	"展开这一章", "展开本章", "创作这一章", "创作本章",
}

// isChapterWritingIntent 依据确定性 cue 判定用户消息是否为写章/续写意图。
func isChapterWritingIntent(message string) bool {
	msg := strings.ToLower(message)
	for _, c := range chapterWritingCues {
		if strings.Contains(msg, strings.ToLower(c)) {
			return true
		}
	}
	return false
}

// truncateRunes 在 rune 边界截断 s 至至多 max 个 rune；超出则在尾部追加「（已截断）」标注。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 6 {
		return string(runes[:max])
	}
	return string(runes[:max-6]) + "（已截断）"
}

// buildPreContextInject 构建写章前情回顾块（contextasm.TextProvider 精神：只回正文，
// 块头/尾注在此处渲染）。返回空串表示不注入（无写章意图 / 无前文 / 读失败均降级）。
// 返回的 error 仅用于调用方记日志，不影响主流程（降级与「不注入」等价）。
func (a *App) buildPreContextInject(ctx context.Context, novelID int64, message string) (string, error) {
	// 仅对非斜杠消息注入（与 autoInject 同口径：斜杠命令走 skill 自身正文，不重复叠加前情）。
	if strings.HasPrefix(message, "/") {
		return "", nil
	}
	if !isChapterWritingIntent(message) {
		return "", nil
	}

	// 取阅读顺序最新的一章作为前情源（chapterOrderDesc 保证是「当前写到哪儿」）。
	recents, err := a.chapter.GetRecent(ctx, a.db, novelID, 1)
	if err != nil {
		return "", fmt.Errorf("precontext: 取最新章失败: %w", err)
	}
	if len(recents) == 0 {
		return "", nil // 尚无任何章节，无前情可注入
	}
	ch := recents[0]

	content, err := git.ReadFile(novelID, ch.FilePath)
	if err != nil {
		// 文件缺失/读失败：宁缺勿错，降级为不注入。
		return "", fmt.Errorf("precontext: 读章节正文失败(%s): %w", ch.FilePath, err)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", nil
	}
	if !utf8.ValidString(content) {
		return "", fmt.Errorf("precontext: 章节正文非法 UTF-8，整块放弃(%s)", ch.FilePath)
	}

	body := truncateRunes(content, preContextBudgetRunes)
	title := strings.TrimSpace(ch.Title)
	if title == "" {
		title = "未命名章节"
	}
	block := fmt.Sprintf("<system-reminder>\n【前情回顾】以下是你正在续写的这本小说中最新一章《%s》的正文，"+
		"供你保持情节、人物、文风与设定的连贯。若用户要求另起新章或续写，请在此前情基础上自然衔接：\n\n---\n%s\n---\n</system-reminder>",
		title, body)
	return block, nil
}
