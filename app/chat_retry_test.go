package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/llm"
)

// newTestProvider 返回一个全局唯一的 provider 名，避免测试间复用 breaker 单例状态互相污染。
func newTestProvider(t *testing.T) string {
	t.Helper()
	return "test-provider-" + t.Name()
}

func TestClassifyTurnRetry_NonAPIError(t *testing.T) {
	ok, ra := classifyTurnRetry(errors.New("boom"), agent.AgentLoopResult{}, "p")
	if ok || ra != 0 {
		t.Fatalf("plain error should not retry: ok=%v ra=%v", ok, ra)
	}
}

func TestClassifyTurnRetry_ContextCanceled(t *testing.T) {
	ok, _ := classifyTurnRetry(context.Canceled, agent.AgentLoopResult{}, "p")
	if ok {
		t.Fatal("context.Canceled should not retry")
	}
}

func TestClassifyTurnRetry_NonRetryable(t *testing.T) {
	err := &llm.APIError{StatusCode: 400, Message: "bad request", Retryable: false}
	ok, _ := classifyTurnRetry(err, agent.AgentLoopResult{}, "p")
	if ok {
		t.Fatal("non-retryable 4xx should not retry")
	}
}

func TestClassifyTurnRetry_RetryableTransient(t *testing.T) {
	err := &llm.APIError{StatusCode: 503, Message: "unavailable", Retryable: true}
	ok, ra := classifyTurnRetry(err, agent.AgentLoopResult{}, "p")
	if !ok || ra != 0 {
		t.Fatalf("503 should retry with no retry-after: ok=%v ra=%v", ok, ra)
	}
}

func TestClassifyTurnRetry_RetryAfterRespected(t *testing.T) {
	err := &llm.APIError{StatusCode: 429, Message: "rate limit", Retryable: true, RetryAfter: 10 * time.Second}
	ok, ra := classifyTurnRetry(err, agent.AgentLoopResult{}, "p")
	if !ok || ra != 10*time.Second {
		t.Fatalf("429 should retry honoring Retry-After: ok=%v ra=%v", ok, ra)
	}
}

func TestClassifyTurnRetry_ToolArgsInvalid(t *testing.T) {
	err := &llm.APIError{Kind: "tool_args_invalid", Retryable: true}
	ok, _ := classifyTurnRetry(err, agent.AgentLoopResult{}, "p")
	if ok {
		t.Fatal("tool_args_invalid should not turn-retry (model-side, in-loop already retried)")
	}
}

func TestClassifyTurnRetry_ProviderBreakerOpen(t *testing.T) {
	p := newTestProvider(t)
	// 触发 5 次硬失败 → 熔断 Open（breaker 单例按 provider 隔离，用唯一名避免串扰）
	for i := 0; i < 5; i++ {
		llm.ProviderBreaker.RecordHardFailure(p)
	}
	if !llm.ProviderBreaker.IsOpen(p) {
		t.Fatalf("breaker should be open for %s", p)
	}
	err := &llm.APIError{StatusCode: 503, Retryable: true}
	ok, _ := classifyTurnRetry(err, agent.AgentLoopResult{}, p)
	if ok {
		t.Fatal("should not turn-retry while breaker is open for the provider")
	}
}

func TestClassifyTurnRetry_FinalTextPresent(t *testing.T) {
	err := &llm.APIError{StatusCode: 503, Retryable: true}
	ok, _ := classifyTurnRetry(err, agent.AgentLoopResult{FinalText: "partial"}, "p")
	if ok {
		t.Fatal("turn that produced content should not auto-replay (orphan partial)")
	}
}

func TestClassifyTurnRetry_ToolExecuted(t *testing.T) {
	err := &llm.APIError{StatusCode: 503, Retryable: true}
	ok, _ := classifyTurnRetry(err, agent.AgentLoopResult{ToolExecuted: true}, "p")
	if ok {
		t.Fatal("turn that executed tools should not auto-replay (double-write)")
	}
}

func TestClassifyTurnRetry_CleanRetryable(t *testing.T) {
	p := newTestProvider(t) // 全新 provider，breaker 未触发
	err := &llm.APIError{StatusCode: 503, Retryable: true}
	ok, _ := classifyTurnRetry(err, agent.AgentLoopResult{}, p)
	if !ok {
		t.Fatal("clean retryable transient failure should turn-retry")
	}
}

func TestTurnRetryBackoff(t *testing.T) {
	cases := []struct {
		attempt    int
		retryAfter time.Duration
		want       time.Duration
	}{
		{1, 0, turnBackoffBase},
		{2, 0, 2 * turnBackoffBase},
		{5, 0, 5 * turnBackoffBase},            // 25s，未触顶
		{1, 8 * time.Second, 8 * time.Second},  // 遵循 Retry-After
		{7, 0, turnBackoffMax},                 // 35s 触顶 30s
		{1, 999 * time.Second, turnBackoffMax}, // Retry-After 超限封顶
	}
	for _, c := range cases {
		got := turnRetryBackoff(c.attempt, c.retryAfter)
		if got != c.want {
			t.Fatalf("turnRetryBackoff(%d,%v)=%v want %v", c.attempt, c.retryAfter, got, c.want)
		}
	}
}

func TestCopyTokenMap(t *testing.T) {
	src := map[string]int{"system": 1, "user": 2}
	cp := copyTokenMap(src)
	cp["system"] = 99
	if src["system"] != 1 {
		t.Fatal("copyTokenMap must not alias the source map")
	}
	if copyTokenMap(nil) != nil {
		t.Fatal("nil map should copy to nil")
	}
}
