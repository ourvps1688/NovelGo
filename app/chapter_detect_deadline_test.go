package app

// #123：手动「重新检测本章」自带 8min 硬超时，但链路不设 Detached，
// #121 只给 Detached 换了后台文案，导致检测超时被 FriendlyError 套成主 chat
// 「30/60 分钟…正文未落盘」的误导文案（15:28 实测）。
// 本文件锁住：专属文案准确、分类码与前端完成事件同源、三条既有文案逐字不动、
// 只注入检测链路、零自动修复。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/agent"
)

func TestChapterDetectDeadlineMessageIsAccurate(t *testing.T) {
	msg := chapterDetectDeadlineFriendlyMessage
	assert.NotEqual(t, agent.FriendlyError(context.DeadlineExceeded), msg,
		"检测超时不得借用主 chat 的 30/60 分钟文案")
	for _, want := range []string{
		"本章检测", "正文", "已落盘", "上一轮检测结果", "重新检测本章",
	} {
		assert.Contains(t, msg, want, "检测超时文案缺少可执行信息 %q", want)
	}
	for _, banned := range []string{
		"30 分钟", "60 分钟", "普通对话", "本轮对话已超时", "草稿", "未落盘为章节正文",
		"后台写后维护", "独立预算",
	} {
		assert.NotContains(t, msg, banned, "检测超时文案不得含误导措辞 %q", banned)
	}
}

func TestClassifyChapterDetectTimeout(t *testing.T) {
	inner := errors.New(chapterDetectDeadlineFriendlyMessage)
	got := classifyChapterDetectError(fmt.Errorf("%w: %s", errChapterDetectTimeout, inner))
	require.Error(t, got)
	var reasoner interface{ DetectReason() string }
	require.True(t, errors.As(got, &reasoner), "分类错误必须实现 DetectReason")
	assert.Equal(t, "timeout", reasoner.DetectReason(),
		"日志 reason 必须与前端完成事件的 reason=timeout 同源")
	assert.True(t, errors.Is(got, errChapterDetectTimeout), "Unwrap 必须保留超时哨兵")
	assert.Contains(t, got.Error(), "已落盘")

	// 未超时的普通失败仍按 #115 P0 原分类，不受新增 case 影响。
	plain := classifyChapterDetectError(errors.New("对话出错，请重试"))
	require.True(t, errors.As(plain, &reasoner))
	assert.Equal(t, "llm", reasoner.DetectReason())
}

func TestChapterDetectDeadlineWiring(t *testing.T) {
	handler := readAppSource(t, "handler.go")
	for _, want := range []string{
		"req.DeadlineMessage = chapterDetectDeadlineFriendlyMessage",
		"a.runBackgroundSubAgent(c, req)",
		"errors.Is(ctx.Err(), context.DeadlineExceeded)",
		`fmt.Errorf("%w: %s", errChapterDetectTimeout, err)`,
		"classifyChapterDetectError(err)",
	} {
		assert.Contains(t, handler, want, "handler.go 缺少检测超时接线 %q", want)
	}

	bg := readAppSource(t, "background_agent.go")
	for _, want := range []string{
		"chapterDetectDeadlineFriendlyMessage", "errChapterDetectTimeout",
		"case errors.Is(err, errChapterDetectTimeout):", `code = "timeout"`,
	} {
		assert.Contains(t, bg, want, "background_agent.go 缺少 %q", want)
	}

	agentSrc := readAppSource(t, "../internal/agent/agent.go")
	detectBranch := `if req.DeadlineMessage != "" && errors.Is(err, context.DeadlineExceeded) {`
	detachedBranch := "} else if subOpts.Detached && errors.Is(err, context.DeadlineExceeded) {"
	for _, want := range []string{
		detectBranch, "friendly = req.DeadlineMessage",
		detachedBranch, "friendly = detachedDeadlineFriendlyMessage",
	} {
		assert.Contains(t, agentSrc, want, "RunSubAgent 缺少检测/写后维护文案分支 %q", want)
	}
	// 两个分支都必须自带 DeadlineExceeded 判定，且专属文案优先于 Detached。
	assert.Less(t,
		strings.Index(agentSrc, detectBranch), strings.Index(agentSrc, detachedBranch),
		"DeadlineMessage 必须优先，避免检测链路回落到写后维护文案")
	assert.Equal(t, 1, strings.Count(agentSrc, detectBranch), "检测专属分支只出现一次")

	base := readAppSource(t, "../internal/mcp_tools/base.go")
	assert.Contains(t, base, "DeadlineMessage string")
	assert.Equal(t, 1, strings.Count(base, "DeadlineMessage string"), "字段只声明一次")

	// #121/#118 两条既有文案逐字不动。
	errs := readAppSource(t, "../internal/agent/errors.go")
	assert.Contains(t, errs, "本轮对话已超时（普通对话 30 分钟 / 写章 60 分钟上限）")
	assert.Contains(t, errs, "后台写后维护（台账同步 / 章节检测）已到独立预算上限")
}

func TestChapterDetectDeadlineNeverAutoFixes(t *testing.T) {
	src := readAppSource(t, "handler.go") +
		readAppSource(t, "background_agent.go") +
		readAppSource(t, "../internal/pipeline/detect.go")
	for _, banned := range []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets", "autoFix",
		"StartPipeline", "PipelineWizard", "SignabilityAuditView",
	} {
		assert.NotContains(t, src, banned, "检测超时链路不得复活自动修复/自动派发：%s", banned)
	}
}
