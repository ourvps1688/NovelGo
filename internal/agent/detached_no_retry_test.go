package agent

// #122 契约：Detached 写后维护子 Agent 一次失败即收口，不做 9 分钟 ×N 重试；
// 主 chat / 前台 run_subagent 的 #118 重试判定与重试矩阵逐字不动。

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/llm"
)

func TestDetachedSubAgentNoRetryWiring(t *testing.T) {
	src, err := os.ReadFile("agent.go")
	require.NoError(t, err)
	s := string(src)
	for _, want := range []string{
		"stopRetry := opts.Detached || shouldStopRetrying(apiErr, idleTimeouts, time.Since(retryWindowStart))",
		"!opts.Detached && retryCount < maxRetries",
	} {
		assert.Contains(t, s, want, "agent.go 缺少 #122 接线 "+want)
	}
	// 零自动修复 / 零新增执行体 / 零上传纪律不受影响。
	for _, banned := range []string{
		"StartIssueFixTickets", "StartPipeline", "Upload", "http.Post",
	} {
		assert.NotContains(t, s, banned)
	}
}

func TestShouldStopRetryingUnchangedForForeground(t *testing.T) {
	// 前台仍按 #118 P1：连续空闲达上限、或累计耗时超预算才停止重试。
	idle := &llm.APIError{Kind: "idle_timeout", Retryable: true}
	assert.False(t, shouldStopRetrying(idle, 0, time.Minute))
	assert.True(t, shouldStopRetrying(idle, idleRetryCap, time.Minute))
	assert.True(t, shouldStopRetrying(&llm.APIError{Retryable: true}, 0, maxRetryWindow))
	assert.False(t, shouldStopRetrying(nil, 0, time.Minute))
}
