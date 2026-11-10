package pipeline

// #270 后续收口（2026-11-10）：后台 LLM 自动修复执行体已整体下线，
// **所有修复都走 chat**——「去修复」把工单上下文发到聊天窗口人工/AI 协作完成，
// 「标记已修复」收口，再显式「复验」。本文件是开发期静态契约断言，防止
// 入口 / 任务 kind / 派发器 / 互斥标记 / 退役技能以任何形式回潮。
//
// 边界：p0 golden 五构造器（fixPrompt / chapterRewritePrompt / blueprintFixPrompt 等）
// 按 AGENTS.md 红线「函数体逐字不动」保留为**仅测试消费的提示词构造器**，
// 零生产调用，不构成本禁令的例外；#100 状态继承与 #106 终态保护 likewise 保留。
// 因此 p0 golden 文案、历史 exec_run_stats fixture、退役技能断言清单里仍会出现
// 4 个已退役技能名，属预期残留，**不是回潮**，不要为了「grep 干净」去改契约单源。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// autoFixBanned 是后台自动修复链路的运行时符号清单。任一符号重新出现在
// internal/pipeline 或 app 的生产源码里，即视为「自动修复回潮」，构建失败。
var autoFixBanned = []string{
	"StartIssueFixTickets", "runIssueFixTickets", "runIssueFixPass",
	"runLedgerFixTickets", "runBodyRewriteTickets",
	"issueFixActive", "issueFixMu", "issueFixRunning",
	"issueFixDrainPasses", "releaseIssueFix",
	"taskKindIssueFix", `"issue_fix"`, "dispatchExecutorUnique",
	"rejectFixInFlight", "planFixGroups", "planFixBatches",
	"applyFixReceipt", "parseFixReceipt",
	"ledgerChangeWatermark", "appendLedgerChangeLog",
	"executeChapterRewrite", "runBlueprintDriftPatrol", "driftPatrolPrompt",
	"fixReachabilityGuard", "filterUnreachableFixIssues",
}

// readProdGoSources 读取一个包目录下的非测试 Go 源码，并归一 CRLF
// （GitHub Windows runner 默认 autocrlf=true，静态扫描不得受行尾影响）。
func readProdGoSources(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		out[name] = strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	return out
}

func TestNoAutoFixRuntimeSymbols(t *testing.T) {
	dirs := map[string]string{
		"internal/pipeline": ".",
		"app":               filepath.Join("..", "..", "app"),
	}
	for label, dir := range dirs {
		for file, src := range readProdGoSources(t, dir) {
			for _, sym := range autoFixBanned {
				require.NotContains(t, src, sym,
					"%s/%s 不得出现已下线的自动修复符号 %q（修复一律走 chat）", label, file, sym)
			}
		}
	}
}

func TestNoAutoFixFrontendEntry(t *testing.T) {
	panel, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src",
		"components", "pipeline", "IssueTicketsPanel.tsx"))
	require.NoError(t, err)
	src := strings.ReplaceAll(string(panel), "\r\n", "\n")
	for _, sym := range []string{"StartIssueFixTickets", "onAutoFix", "autoFix"} {
		require.NotContains(t, src, sym, "工单面板不得再有自动修复入口 %q", sym)
	}
	for _, locale := range []string{"zh-CN", "en"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src",
			"i18n", "locales", locale+".json"))
		require.NoError(t, err)
		require.NotContains(t, string(b), `"autoFix"`, "%s 不得再暴露自动修复文案键", locale)
	}
}

func TestRetiredFixSkillsGone(t *testing.T) {
	for _, name := range []string{
		"signability-fix", "blueprint-fix", "chapter-rewrite", "blueprint-drift-check",
	} {
		_, err := os.Stat(filepath.Join("..", "skill", "builtin", name+".md"))
		require.True(t, os.IsNotExist(err),
			"退役修复技能 %s 不应再存在（其唯一消费方是已下线的后台执行体）", name)
	}
}

func TestManualFixChainAndGoldenConstructorsIntact(t *testing.T) {
	app := readProdGoSources(t, filepath.Join("..", "..", "app"))["issue_tickets.go"]
	for _, fn := range []string{
		"StartManualIssueFixTickets", "CloseIssueFixTickets",
		"CancelIssueFixTickets", "VerifyIssueTickets",
	} {
		require.Contains(t, app, fn, "人工修复/复验入口 %s 必须保留", fn)
	}
	// p0 golden 契约单源：函数体逐字不动，删除后台执行体不得连带改动它们。
	var svc strings.Builder
	for _, src := range readProdGoSources(t, ".") {
		svc.WriteString(src)
	}
	all := svc.String()
	for _, fn := range []string{
		"func fixPrompt(", "func chapterRewritePrompt(", "func blueprintFixPrompt(",
		"func fixLogBlock(", "func fixReceiptFormatLine(",
	} {
		require.Contains(t, all, fn, "p0 golden 契约构造器 %s 必须保留", fn)
	}
}
