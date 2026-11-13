package agent

// #118 契约：超时文案、重试预算、压缩提前、中断草稿保底。
// 静态全链锁 + 纯函数单测，防止「修一次又回退」。
//
// 红线：本文件只锁既有链路，不引入任何自动修复 / 执行体 / 上传语义。

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/llm"
)

func TestFriendlyErrorDeadlineExceededIsActionable(t *testing.T) {
	got := FriendlyError(context.DeadlineExceeded)
	assert.Contains(t, got, "超时")
	assert.Contains(t, got, "草稿")
	assert.Contains(t, got, "未落盘")
	assert.NotEqual(t, "对话出错，请重试", got, "超时不能再落无信息兜底")
	assert.Empty(t, FriendlyError(context.Canceled), "用户主动停止仍不弹文案")
	// 上层常用 fmt.Errorf 包装，errors.Is 必须仍能识别。
	assert.Equal(t, got, FriendlyError(fmt.Errorf("run: %w", context.DeadlineExceeded)))
}

func TestShouldStopRetryingCoversDropAfterBytes(t *testing.T) {
	// 旧实现 idleExhausted 只认 Kind=="idle_timeout"，而 agent.go 已把
	// 「吐字节后掉线」（StatusCode==0 且 Retryable）也计入 idleTimeouts，
	// 该分支会一路重试到主 ctx 硬超时。
	dropped := &llm.APIError{StatusCode: 0, Retryable: true}
	assert.False(t, shouldStopRetrying(dropped, 1, time.Second))
	assert.True(t, shouldStopRetrying(dropped, idleRetryCap, time.Second))

	idle := &llm.APIError{Kind: "idle_timeout", Retryable: true}
	assert.True(t, shouldStopRetrying(idle, idleRetryCap, time.Second))

	// 429/5xx 等带 HTTP 状态码的错误不受 idleTimeouts 影响，保留既有退避。
	rate := &llm.APIError{StatusCode: 429, Retryable: true}
	assert.False(t, shouldStopRetrying(rate, idleRetryCap+2, time.Second))

	// 累计耗时预算到点即停（避免 9 分钟 × N 吃满 30 分钟）。
	assert.True(t, shouldStopRetrying(rate, 0, maxRetryWindow))
	assert.False(t, shouldStopRetrying(rate, 0, maxRetryWindow-time.Second))
	assert.False(t, shouldStopRetrying(nil, idleRetryCap, maxRetryWindow))
}

func TestCanSaveInterruptedDraftSkipsSubAgent(t *testing.T) {
	// 子 Agent 复用主 turn 的 SessionID，其中间报告不是给用户看的章节正文，
	// 绝不能写成 ToFrontend 草稿混进主会话视图。
	main := &RunOptions{SessionID: "sess_main", TurnID: 7}
	assert.True(t, canSaveInterruptedDraft(main, "半章正文"))
	assert.False(t, canSaveInterruptedDraft(main, ""), "零字节不落空草稿")
	assert.False(t, canSaveInterruptedDraft(main, "  "), "空白占位也不落")

	sub := &RunOptions{SessionID: "sess_main", TurnID: 7, SubTaskID: "tool_42"}
	assert.False(t, canSaveInterruptedDraft(sub, "检测中间报告"))
}

func TestChatTimeoutBudgets(t *testing.T) {
	assert.Equal(t, 0.60, compressionBudgetRatio,
		"0.85 会让 1M 窗口撑到 used≈73.9 万 tokens 才压缩（00:38 实测）")
	assert.Equal(t, 8*time.Minute, maxRetryWindow)
	assert.Equal(t, 2, idleRetryCap)
}

func TestChatTimeoutGuardWiring(t *testing.T) {
	checks := []struct {
		file  string
		wants []string
	}{
		{"agent.go", []string{
			"compressionBudgetRatio", "shouldStopRetrying", "retryWindowStart",
			"saveInterruptedDraft", "draftSaved", "exitDraftHandled", "ctx.Err()",
			"超时出口保底保存草稿",
		}},
		{"errors.go", []string{"deadlineFriendlyMessage", "context.DeadlineExceeded"}},
		{"retry.go", []string{"compressionBudgetRatio", "maxRetryWindow", "shouldStopRetrying"}},
		{"draft.go", []string{
			"interruptedDraftMarker", "a.db.Create(msg)", "saveInterruptedDraft",
			"canSaveInterruptedDraft", "SubTaskID", "strings.TrimSpace",
		}},
	}
	for _, c := range checks {
		b, err := os.ReadFile(c.file)
		require.NoError(t, err)
		s := string(b)
		for _, w := range c.wants {
			assert.Contains(t, s, w, c.file+" 缺少接线 "+w)
		}
	}

	draft, err := os.ReadFile("draft.go")
	require.NoError(t, err)
	// 草稿绝不进 API 上下文（否则重试时半章会被当成已成稿正文）。
	assert.Regexp(t, `ToAPI:\s+false`, string(draft))
	assert.Regexp(t, `ToFrontend:\s+true`, string(draft))
	for _, banned := range []string{
		"StartIssueFixTickets", "autoFix", "RunSubAgent", "Dispatch", "Upload",
	} {
		assert.NotContains(t, string(draft), banned, "草稿链路不得含自动修复/执行体/上传")
	}

	// 压缩阈值旧值不得回潮。
	agentSrc, err := os.ReadFile("agent.go")
	require.NoError(t, err)
	src := string(agentSrc)
	assert.NotContains(t, src, ">= 0.85")
	assert.NotContains(t, src, `Kind == "idle_timeout" && idleTimeouts`)

	// 重试前保存草稿必须发生在 responseBuffer.Reset() 之前：Reset 之后
	// 半章正文就没了，这正是 00:38 丢稿的根因。从「重试护栏」起截取，
	// 避免误命中统一出口 defer 里的同名调用。
	guardAt := strings.Index(src, "if !draftSaved && responseBuffer.Len() > 0 {")
	require.GreaterOrEqual(t, guardAt, 0, "重试前草稿护栏缺失")
	seg := src[guardAt:]
	saveAt := strings.Index(seg, "a.saveInterruptedDraft(&opts, responseBuffer.String(), thinkingBuffer.String())")
	require.GreaterOrEqual(t, saveAt, 0, "重试前的草稿保存点缺失")
	resetAt := strings.Index(seg, "responseBuffer.Reset()")
	require.Greater(t, resetAt, saveAt, "草稿必须在 buffer Reset 之前保存")
}

// #119 P0：写章档预算放宽后文案不再写死 30；写后维护子 Agent 不进前端可见流。
func TestDeadlineMessageCoversBothTiersAndDetachedSubAgent(t *testing.T) {
	msg := FriendlyError(context.DeadlineExceeded)
	assert.Contains(t, msg, "30 分钟")
	assert.Contains(t, msg, "60 分钟")
	assert.Contains(t, msg, "章节列表", "正文可能已落盘，须提示先确认再重试")

	b, err := os.ReadFile("agent.go")
	require.NoError(t, err)
	s := string(b)
	for _, want := range []string{
		"Detached bool", "req.Detached", "!opts.Detached", "background:",
		`role == "assistant" && !opts.Detached`,
	} {
		assert.Contains(t, s, want, "agent.go 缺少后台子 Agent 接线 "+want)
	}
}
