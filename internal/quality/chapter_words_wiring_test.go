package quality_test

// chapter_words_wiring_test.go —— #113 字数口径单源接线契约（静态闸）。
//
// 防止回潮：检测侧 L0 三个生产调用点、写作侧 system、评分判官必须全部消费
// ChapterWords；任何一处漏掉都会重新制造「体检 1000–1500、修复对比 2000」。
// 与 issue_no_auto_fix_test.go 同风格：读源码断言，零运行时开销。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWordsWiringSource(t *testing.T, rel string) string {
	t.Helper()
	// 本测试位于 internal/quality 下（外部包），相对仓库根用 ../..。
	p := filepath.Join("..", "..", filepath.FromSlash(rel))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestChapterWordsWiredIntoAllL0Callers(t *testing.T) {
	callers := []string{
		"internal/mcp_tools/chapter_ci.go",
		"internal/pipeline/propagation.go",
		"internal/pipeline/l0_verify.go",
	}
	for _, rel := range callers {
		src := readWordsWiringSource(t, rel)
		if !strings.Contains(src, "ChapterWords(ctx") && !strings.Contains(src, "ChapterWords:") {
			t.Errorf("%s 未装配 ChapterWords 单源", rel)
		}
		if !strings.Contains(src, "ChapterWords:") {
			t.Errorf("%s 的 RunL0Checks 未注入 ChapterWords", rel)
		}
	}
}

func TestChapterWordsWiredIntoWritingSide(t *testing.T) {
	checks := []struct {
		rel  string
		want string
	}{
		{"internal/agentcfg/novel_profile.go", "ChapterWords(ctx, novelID)"},
		{"internal/agentcfg/effective_constraint.go", "quality.ChapterWordsTarget"},
		{"internal/review/score.go", "wordsLine"},
		{"internal/mcp_tools/rw_tools.go", "chapterWords.SystemLine()"},
		{"internal/pipeline/detect_status.go", "WordsTargetLabel"},
	}
	for _, c := range checks {
		src := readWordsWiringSource(t, c.rel)
		if !strings.Contains(src, c.want) {
			t.Errorf("%s 缺少 %q", c.rel, c.want)
		}
	}
}

func TestChapterDetectionRerunRecalcsL0(t *testing.T) {
	// #113：显式「重新检测本章」必须在 L1 之前按当前正文重算 L0，否则改稿后
	// 卡片仍显示修改前的字数/开篇发现，闭环看不到结果。
	app := readWordsWiringSource(t, "app/handler.go")
	if !strings.Contains(app, "RecalcChapterL0FromDisk(ctx, a.db, a.logger, novelID, chapterID)") ||
		!strings.Contains(app, "RunChapterCIForChapter(ctx,") {
		t.Error("app/handler.go 的章节检测执行器未按「先重算 L0、再跑 L1」装配")
	}
	ci := readWordsWiringSource(t, "internal/mcp_tools/chapter_ci.go")
	for _, want := range []string{
		"func RecalcChapterL0FromDisk(",
		"func recalcChapterL0(",
		"git.ReadFile(novelID, git.ChapterPath(chapterID))",
		"recalcChapterL0(ctx, tc, chapterID, content)",
	} {
		if !strings.Contains(ci, want) {
			t.Errorf("chapter_ci.go 缺少 %q", want)
		}
	}
	// L0 计算只允许一处实现，写后链路与显式重跑必须共用（防双口径回潮）。
	if n := strings.Count(ci, "quality.RunL0Checks("); n != 1 {
		t.Errorf("chapter_ci.go 的 RunL0Checks 调用点应为 1 处，实际 %d", n)
	}
	// 重跑检测不得重复落情绪账工单（人工已收口的 E 系工单不能被唤醒）。
	if n := strings.Count(ci, "tensionTicketLander(ctx"); n != 1 {
		t.Errorf("chapter_ci.go 的 tensionTicketLander 应只在写后链路出现 1 次，实际 %d", n)
	}
}

func TestNoHardcodedDualWordTargets(t *testing.T) {
	// 写作侧不得再自行写死 2000 字；唯一常量在 quality/chapter_words.go。
	files := []string{
		"internal/agentcfg/effective_constraint.go",
		"internal/agentcfg/identity.go",
	}
	for _, rel := range files {
		src := readWordsWiringSource(t, rel)
		if strings.Contains(src, "默认 2000 字") || strings.Contains(src, "系统默认 2000 字") {
			t.Errorf("%s 仍硬编码 2000 字默认口径（应引用 quality.DefaultTargetWords/口径块）", rel)
		}
	}
	// 单源常量只允许定义一次。
	l0 := readWordsWiringSource(t, "internal/quality/l0.go")
	if strings.Contains(l0, "const DefaultTargetWords") {
		t.Error("DefaultTargetWords 应只在 chapter_words.go 定义一次")
	}
	cw := readWordsWiringSource(t, "internal/quality/chapter_words.go")
	if !strings.Contains(cw, "const DefaultTargetWords = 2000") {
		t.Error("唯一真相源常量缺失")
	}
	// 七猫笔误 1000–1800 不得回潮。
	md := readWordsWiringSource(t, "internal/skill/builtin/opening-chapters.md")
	if strings.Contains(md, "七猫 1000–1800") {
		t.Error("opening-chapters.md 七猫区间笔误回潮（应为 1000–1500）")
	}
}
