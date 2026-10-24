package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
)

// --- apiErrorFromNetErr：矩阵网络错误三行 ---

func TestAPIErrorFromNetErrTimeoutRetryable(t *testing.T) {
	// net.Error 超时（首字节 10m / 读超时）→ 可重试
	got := apiErrorFromNetErr(&net.DNSError{Err: "i/o timeout", IsTimeout: true})
	if !got.Retryable {
		t.Fatalf("timeout should be retryable, got %+v", got)
	}
	if got.StatusCode != 0 {
		t.Fatalf("network error should have StatusCode 0, got %d", got.StatusCode)
	}
	if want := "服务器响应超时（首字节超过 10m），请稍后重试"; got.Message != want {
		t.Fatalf("message = %q, want %q", got.Message, want)
	}
}

func TestAPIErrorFromNetErrPermanentNotRetryable(t *testing.T) {
	// DNS NXDOMAIN → 永久性，不可重试
	dns := &net.DNSError{Err: "no such host", Name: "nope.invalid", IsNotFound: true}
	if got := apiErrorFromNetErr(dns); got.Retryable {
		t.Fatalf("DNS NXDOMAIN should not be retryable, got %+v", got)
	}

	// ECONNREFUSED → 永久性，不可重试
	refused := &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	if got := apiErrorFromNetErr(refused); got.Retryable {
		t.Fatalf("ECONNREFUSED should not be retryable, got %+v", got)
	}
	if want := "网络连接被拒绝或域名无法解析，请检查网络配置"; apiErrorFromNetErr(refused).Message != want {
		t.Fatalf("permanent message mismatch")
	}
}

func TestAPIErrorFromNetErrTransientRetryable(t *testing.T) {
	// 其余瞬时性网络错误（EOF / reset / 未知）→ 可重试
	for _, err := range []error{
		fmt.Errorf("read: connection reset by peer"),
		errors.New("EOF"),
		&net.OpError{Op: "read", Err: errors.New("broken pipe")},
	} {
		got := apiErrorFromNetErr(err)
		if !got.Retryable {
			t.Fatalf("transient error %v should be retryable, got %+v", err, got)
		}
		if want := "网络连接失败，请检查网络后重试"; got.Message != want {
			t.Fatalf("transient message = %q, want %q", got.Message, want)
		}
	}
}

// --- apiErrorFromHTTP：矩阵 HTTP 行 ---

func TestAPIErrorFromHTTPRetryableStatuses(t *testing.T) {
	for _, code := range []int{429, 408, 500, 502, 503} {
		got := apiErrorFromHTTP(code, []byte(`{"error":{"message":"x"}}`), nil, "")
		if !got.Retryable {
			t.Fatalf("status %d should be retryable, got %+v", code, got)
		}
	}
}

func TestAPIErrorFromHTTPNonRetryableStatuses(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 422} {
		got := apiErrorFromHTTP(code, []byte(`{"error":{"message":"x"}}`), nil, "")
		if got.Retryable {
			t.Fatalf("status %d should not be retryable, got %+v", code, got)
		}
	}
}

func TestAPIErrorFromHTTPRetryAfterParsed(t *testing.T) {
	// Retry-After 秒数格式
	got := apiErrorFromHTTP(429, []byte(`{"error":{"message":"rate"}}`), nil, "120")
	if got.RetryAfter != 120*time.Second {
		t.Fatalf("RetryAfter = %v, want 120s", got.RetryAfter)
	}
	// 无 Retry-After 头 → 0
	if got := apiErrorFromHTTP(429, []byte(`{"error":{"message":"r"}}`), nil, ""); got.RetryAfter != 0 {
		t.Fatalf("RetryAfter = %v, want 0", got.RetryAfter)
	}
}

func TestAPIErrorFromHTTPParseErrorHook(t *testing.T) {
	custom := func(body []byte) error { return fmt.Errorf("custom: %s", string(body)) }
	got := apiErrorFromHTTP(500, []byte("boom"), custom, "")
	if got.Message != "custom: boom" {
		t.Fatalf("message = %q, want custom parse", got.Message)
	}
	// nil hook → OpenAI 标准格式解析
	got = apiErrorFromHTTP(500, []byte(`{"error":{"message":"std"}}`), nil, "")
	if got.Message != "std" {
		t.Fatalf("message = %q, want std", got.Message)
	}
}

// --- 三路径回归：GenerateText 与 ChatStream 同错误同判定 ---

// TestRetryMatrixConsistentAcrossPaths 用同一 429+Retry-After 端点验证
// 非流式（GenerateText）与流式（ChatStream）产出的 *APIError 判定一致
// （Retryable=true 且 RetryAfter 同值）——单源决策函数是唯一分类点的回归锁定。
func TestRetryMatrixConsistentAcrossPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	prov := Provider{
		Name:    "p",
		ChatURL: server.URL,
		APIKey:  "k",
		Models:  []ModelInfo{{ID: "m", ContextWindow: 128000, MaxOutputTokens: 8192}},
	}
	client := NewClient(map[string]Provider{"p": prov}, slog.Default())
	messages := []map[string]any{{"role": "user", "content": "hi"}}

	// GenerateText 路径
	_, err := client.GenerateText(context.Background(), "p", messages, "m")
	var genErr *APIError
	if !errors.As(err, &genErr) {
		t.Fatalf("GenerateText should return *APIError, got %v", err)
	}

	// ChatStream 路径
	var streamErr *APIError
	for evt := range client.ChatStream(context.Background(), "p", messages, nil, "m", nil) {
		if evt.Type == EventError && evt.Error != nil {
			if !errors.As(evt.Error, &streamErr) {
				t.Fatalf("ChatStream should emit *APIError, got %v", evt.Error)
			}
		}
	}
	if streamErr == nil {
		t.Fatal("ChatStream should emit EventError for 429")
	}

	// 同错误同判定
	if genErr.Retryable != streamErr.Retryable {
		t.Fatalf("Retryable mismatch: generate=%v stream=%v", genErr.Retryable, streamErr.Retryable)
	}
	if genErr.RetryAfter != streamErr.RetryAfter {
		t.Fatalf("RetryAfter mismatch: generate=%v stream=%v", genErr.RetryAfter, streamErr.RetryAfter)
	}
	if !genErr.Retryable || genErr.RetryAfter != 7*time.Second {
		t.Fatalf("want retryable with RetryAfter 7s, got %+v", genErr)
	}
	if genErr.Message != "rate limited" || streamErr.Message != "rate limited" {
		t.Fatalf("message mismatch: generate=%q stream=%q", genErr.Message, streamErr.Message)
	}
}
