package llm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- parseRetryAfter ---

func TestParseRetryAfter_Empty(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty string: got %v, want 0", got)
	}
}

func TestParseRetryAfter_WhitespaceOnly(t *testing.T) {
	if got := parseRetryAfter("   "); got != 0 {
		t.Errorf("whitespace only: got %v, want 0", got)
	}
}

func TestParseRetryAfter_Seconds(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"0", 0},
		{"1", time.Second},
		{"120", 120 * time.Second},
	}
	for _, c := range cases {
		if got := parseRetryAfter(c.input); got != c.want {
			t.Errorf("parseRetryAfter(%q): got %v, want %v", c.input, got, c.want)
		}
	}
}

func TestParseRetryAfter_SecondsWithSurroundingSpaces(t *testing.T) {
	// TrimSpace 后等价于 "120"
	if got := parseRetryAfter("  120  "); got != 120*time.Second {
		t.Errorf("got %v, want 120s", got)
	}
}

func TestParseRetryAfter_NegativeSeconds(t *testing.T) {
	// secs >= 0 检查拒绝负数；HTTP-date 解析也失败 → 0
	if got := parseRetryAfter("-5"); got != 0 {
		t.Errorf("negative seconds: got %v, want 0", got)
	}
}

func TestParseRetryAfter_InvalidString(t *testing.T) {
	if got := parseRetryAfter("abc"); got != 0 {
		t.Errorf("invalid string: got %v, want 0", got)
	}
}

func TestParseRetryAfter_FutureHTTPDate(t *testing.T) {
	// 未来日期 → 正 duration
	future := time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	if got <= 0 {
		t.Errorf("future date %q: got %v, want > 0", future, got)
	}
	// 应接近 2 小时（容忍漂移）
	if got > 2*time.Hour+10*time.Second {
		t.Errorf("future date %q: got %v, want <= ~2h", future, got)
	}
}

func TestParseRetryAfter_PastHTTPDate(t *testing.T) {
	// 过去日期 → time.Until 负数 → 不返回 → 0
	past := time.Now().Add(-2 * time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past date %q: got %v, want 0", past, got)
	}
}

// --- statusRetryable ---

func TestStatusRetryable(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{429, true}, // 限流
		{408, true}, // 超时
		{500, true}, // 服务端错误
		{502, true},
		{503, true},
		{599, true},  // 5xx 上界
		{400, false}, // 客户端错误
		{401, false},
		{404, false},
		{422, false},
		{600, false}, // 超出范围
		{200, false}, // 成功
		{0, false},   // 网络错误（无状态码）
	}
	for _, c := range cases {
		if got := statusRetryable(c.code); got != c.want {
			t.Errorf("statusRetryable(%d): got %v, want %v", c.code, got, c.want)
		}
	}
}

// --- parseDefaultError ---

func TestParseDefaultError_StandardFormat(t *testing.T) {
	body := []byte(`{"error":{"message":"rate limit exceeded"}}`)
	err := parseDefaultError(body)
	if err.Error() != "rate limit exceeded" {
		t.Errorf("got %q, want %q", err.Error(), "rate limit exceeded")
	}
}

func TestParseDefaultError_EmptyMessage(t *testing.T) {
	// JSON 合法但 message 为空 → 回退到 raw body
	body := []byte(`{"error":{"message":""}}`)
	err := parseDefaultError(body)
	want := `request failed: {"error":{"message":""}}`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestParseDefaultError_NoMessageField(t *testing.T) {
	body := []byte(`{"foo":"bar"}`)
	err := parseDefaultError(body)
	want := `request failed: {"foo":"bar"}`
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestParseDefaultError_InvalidJSON(t *testing.T) {
	body := []byte("not json at all")
	err := parseDefaultError(body)
	want := "request failed: not json at all"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

// --- mergeSystemMessages ---

func TestMergeSystemMessages_NoSystem(t *testing.T) {
	// 0 条 system：原样返回
	in := []map[string]any{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
	}
	got := mergeSystemMessages(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("no system: got %v, want %v", got, in)
	}
}

func TestMergeSystemMessages_SingleSystem(t *testing.T) {
	// 1 条 system：原样返回（避免不必要拷贝）
	in := []map[string]any{
		{"role": "system", "content": "sys"},
		{"role": "user", "content": "hi"},
	}
	got := mergeSystemMessages(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("single system: got %v, want %v", got, in)
	}
}

func TestMergeSystemMessages_ThreeSystemAgentPath(t *testing.T) {
	// 复现 agent.go 主路径：sysPrompt + profile + state 三条 system
	in := []map[string]any{
		{"role": "system", "content": "sysPrompt"},
		{"role": "system", "content": "profile"},
		{"role": "system", "content": "state"},
		{"role": "user", "content": "instruction"},
	}
	got := mergeSystemMessages(in)

	if len(got) != 2 {
		t.Fatalf("len: got %d, want 2 (1 merged system + 1 user)", len(got))
	}
	role, _ := got[0]["role"].(string)
	if role != "system" {
		t.Errorf("got[0] role: got %q, want %q", role, "system")
	}
	content, _ := got[0]["content"].(string)
	wantContent := "sysPrompt\n\nprofile\n\nstate"
	if content != wantContent {
		t.Errorf("merged content: got %q, want %q", content, wantContent)
	}
	// user 消息保持原序
	if got[1]["role"] != "user" || got[1]["content"] != "instruction" {
		t.Errorf("got[1]: got %v, want {role:user content:instruction}", got[1])
	}
}

func TestMergeSystemMessages_PreservesOtherRolesOrder(t *testing.T) {
	// 多条 system + tool/assistant 顺序保持不变
	in := []map[string]any{
		{"role": "system", "content": "s1"},
		{"role": "user", "content": "u1"},
		{"role": "assistant", "content": "a1"},
		{"role": "tool", "content": "t1"},
		{"role": "system", "content": "s2"},
		{"role": "user", "content": "u2"},
	}
	got := mergeSystemMessages(in)

	want := []struct {
		role    string
		content string
	}{
		{"system", "s1\n\ns2"},
		{"user", "u1"},
		{"assistant", "a1"},
		{"tool", "t1"},
		{"user", "u2"},
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i]["role"] != w.role {
			t.Errorf("got[%d] role: got %v, want %s", i, got[i]["role"], w.role)
		}
		if got[i]["content"] != w.content {
			t.Errorf("got[%d] content: got %v, want %s", i, got[i]["content"], w.content)
		}
	}
}

func TestMergeSystemMessages_NonStringContentSystemPreserved(t *testing.T) {
	// content 非 string 的 system（多模态场景）保持原样，不参与合并
	in := []map[string]any{
		{"role": "system", "content": "text-sys"},
		{"role": "system", "content": []any{map[string]any{"type": "text", "text": "multi-sys"}}},
		{"role": "user", "content": "hi"},
	}
	got := mergeSystemMessages(in)
	// 1 条 string system + 1 条非 string system：len(sysParts)==1，原样返回
	if !reflect.DeepEqual(got, in) {
		t.Errorf("non-string system should be preserved as-is: got %v, want %v", got, in)
	}
}

func TestMergeSystemMessages_MixedStringAndNonStringSystem(t *testing.T) {
	// 2+ 条 string system + 1 条非 string system：string system 合并，非 string system 保留原序
	in := []map[string]any{
		{"role": "system", "content": "s1"},
		{"role": "system", "content": []any{map[string]any{"type": "text", "text": "multi"}}},
		{"role": "system", "content": "s2"},
		{"role": "user", "content": "hi"},
	}
	got := mergeSystemMessages(in)

	// 期望：merged string system + 非 string system + user
	if len(got) != 3 {
		t.Fatalf("len: got %d, want 3", len(got))
	}
	// 头部是合并后的 string system
	if got[0]["role"] != "system" || got[0]["content"] != "s1\n\ns2" {
		t.Errorf("got[0]: got %v, want merged string system", got[0])
	}
	// 接下来是非 string system（保持原序）
	if got[1]["role"] != "system" {
		t.Errorf("got[1] role: got %v, want system", got[1]["role"])
	}
	if _, ok := got[1]["content"].([]any); !ok {
		t.Errorf("got[1] content: should be []any (non-string), got %T", got[1]["content"])
	}
	// 最后是 user
	if got[2]["role"] != "user" || got[2]["content"] != "hi" {
		t.Errorf("got[2]: got %v, want user", got[2])
	}
}

func TestMergeSystemMessages_EmptyContentSystemSkipped(t *testing.T) {
	// 空 content 的 system 跳过合并，保持原样
	in := []map[string]any{
		{"role": "system", "content": ""},
		{"role": "system", "content": "s2"},
		{"role": "user", "content": "hi"},
	}
	got := mergeSystemMessages(in)
	// 只有 1 条非空 string system，sysParts 长度为 1，原样返回
	if !reflect.DeepEqual(got, in) {
		t.Errorf("empty content skipped: got %v, want %v", got, in)
	}
}

func TestMergeSystemMessages_TwoEmptyOneNonEmpty(t *testing.T) {
	// 2 条空 content + 1 条非空：sysParts 长度 1，原样返回
	in := []map[string]any{
		{"role": "system", "content": ""},
		{"role": "system", "content": "real"},
		{"role": "system", "content": ""},
		{"role": "user", "content": "hi"},
	}
	got := mergeSystemMessages(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("only one non-empty: got %v, want %v", got, in)
	}
}

// --- clampMaxTokens ---

func TestClampMaxTokens(t *testing.T) {
	cases := []struct {
		name          string
		maxTokens     int
		contextWindow int
		promptTokens  int
		want          int
	}{
		// 正常：prompt + maxTokens 不超窗口，原值返回
		{"fits", 384000, 1048576, 100000, 384000},
		// 触发 400 的真实场景：665042 + 384000 > 1048576，应收缩到可用余量
		{"overflow", 384000, 1048576, 665042, 1048576 - 665042 - 2048},
		// 即使模型无上限，也不得超过窗口
		{"hugeMax", 1_000_000_000, 200000, 10000, 200000 - 10000 - 2048},
		// 留白不足时兜底到最小输出预算
		{"tinyAvail", 64000, 5000, 1000, 4096},
		// 完全没prompt：按窗口收缩
		{"noPrompt", 384000, 128000, 0, 128000 - 2048},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := clampMaxTokens(c.maxTokens, c.contextWindow, c.promptTokens); got != c.want {
				t.Errorf("clampMaxTokens(%d,%d,%d) = %d, want %d",
					c.maxTokens, c.contextWindow, c.promptTokens, got, c.want)
			}
		})
	}
}

// --- parseSSE / finish_reason=length 截断提示 ---

// runParseSSE 用给定 HTTP 响应体驱动 parseSSE，收集全部 StreamEvent（parseSSE 自身不关闭 channel）。
func runParseSSE(t *testing.T, body string) []StreamEvent {
	t.Helper()
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	c := NewClient(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch := make(chan StreamEvent, 64)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.parseSSE(ch, resp, "test-provider")
		close(ch)
	}()

	var events []StreamEvent
	for ev := range ch {
		events = append(events, ev)
	}
	wg.Wait()
	return events
}

// TestParseSSE_FinishReasonLength_TruncatedJSON_ReportsTruncation
// 流式返回 finish_reason=length，且工具参数 JSON 不完整 → 错误信息应明确包含「截断」字样，
// 而非笼统的 "unexpected end of JSON input"。
func TestParseSSE_FinishReasonLength_TruncatedJSON_ReportsTruncation(t *testing.T) {
	// tool call arguments 为不完整的 JSON（缺闭合大括号），末尾 chunk 标记 finish_reason=length。
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"output_skill","arguments":"{\"name\":\"x\""}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	events := runParseSSE(t, body)

	var errEvent *StreamEvent
	for i := range events {
		if events[i].Type == EventError {
			errEvent = &events[i]
			break
		}
	}
	if errEvent == nil {
		t.Fatalf("expected a tool_args_invalid EventError, got events: %+v", events)
	}
	apiErr, ok := errEvent.Error.(*APIError)
	if !ok {
		t.Fatalf("EventError.Error should be *APIError, got %T", errEvent.Error)
	}
	if apiErr.Kind != "tool_args_invalid" {
		t.Errorf("Kind: got %q, want %q", apiErr.Kind, "tool_args_invalid")
	}
	if !strings.Contains(apiErr.Message, "截断") {
		t.Errorf("truncation hint missing from message: %q", apiErr.Message)
	}
	if !strings.Contains(apiErr.Message, "max_tokens") {
		t.Errorf("message should mention max_tokens: %q", apiErr.Message)
	}
}

// TestParseSSE_FinishReasonLength_NoToolArgs_NotTruncationError
// 正常 length 截断但零产出（无任何 tool call 参数）场景：走 empty_stream 分支，
// 不应被误升级为「截断」提示（该提示仅针对「参数存在但 JSON 无效」）。
func TestParseSSE_FinishReasonLength_NoToolArgs_NotTruncationError(t *testing.T) {
	// 只有 reasoning_content，无 tool call，末尾 finish_reason=length（思考被预算截断）。
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"让我想想..."}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	events := runParseSSE(t, body)

	var errEvent *StreamEvent
	for i := range events {
		if events[i].Type == EventError {
			errEvent = &events[i]
			break
		}
	}
	if errEvent == nil {
		t.Fatalf("expected empty_stream EventError, got events: %+v", events)
	}
	apiErr, ok := errEvent.Error.(*APIError)
	if !ok {
		t.Fatalf("EventError.Error should be *APIError, got %T", errEvent.Error)
	}
	if apiErr.Kind != "empty_stream" {
		t.Errorf("Kind: got %q, want %q", apiErr.Kind, "empty_stream")
	}
	// 此处不应出现「截断」字样（那是 tool_args_invalid 分支的专属提示）。
	if strings.Contains(apiErr.Message, "截断") {
		t.Errorf("zero-output empty_stream should not contain 截断: %q", apiErr.Message)
	}
}

// TestParseSSE_NormalStop_CompleteJSON_NoTruncation
// 正常结束（finish_reason=stop）且工具参数 JSON 完整：成功收尾，不应有错误，消息不含「截断」。
func TestParseSSE_NormalStop_CompleteJSON_NoTruncation(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"output_skill","arguments":"{\"name\":\"x\"}"}}]}}]}`,
		``,
		`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	events := runParseSSE(t, body)

	for i := range events {
		if events[i].Type == EventError {
			t.Fatalf("unexpected EventError: %+v", events[i].Error)
		}
	}
	var gotEnd bool
	for i := range events {
		if events[i].Type == EventToolCallEnd {
			gotEnd = true
			break
		}
	}
	if !gotEnd {
		t.Errorf("expected EventToolCallEnd for complete JSON, got events: %+v", events)
	}
}

// --- P1：LLM 调用入口熔断拦截 + 空闲预算按模型放宽 ---

// TestChatStream_BreakerBlocked 命中熔断时 ChatStream 应立即发出 provider_breaker_open
// 的 EventError，且 Retryable=false（agent 层不重试、干净快速失败）。通过替换接缝
// breakerBlockedProvider 确定性驱动，不污染全局 ProviderBreaker。
func TestChatStream_BreakerBlocked(t *testing.T) {
	orig := breakerBlockedProvider
	defer func() { breakerBlockedProvider = orig }()
	breakerBlockedProvider = func(string) bool { return true }

	c := NewClient(
		map[string]Provider{"test-provider": {ChatURL: "http://example.com"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	ch := c.ChatStream(context.Background(), "test-provider",
		[]map[string]any{{"role": "user", "content": "hi"}}, nil, "m", nil)

	var gotErr error
	for ev := range ch {
		if ev.Type == EventError {
			gotErr = ev.Error
			break
		}
	}
	if gotErr == nil {
		t.Fatalf("expected EventError from breaker guard")
	}
	apiErr, ok := gotErr.(*APIError)
	if !ok {
		t.Fatalf("EventError.Error should be *APIError, got %T", gotErr)
	}
	if apiErr.Kind != "provider_breaker_open" {
		t.Errorf("Kind: got %q, want %q", apiErr.Kind, "provider_breaker_open")
	}
	if apiErr.Retryable {
		t.Errorf("breaker-open error must be non-retryable (agent should fast-fail, not retry)")
	}
}

// TestGenerateText_BreakerBlocked 命中熔断时 GenerateText 应返回 provider_breaker_open 错误，
// 且 Retryable=false。同样经接缝替换确定性驱动。
func TestGenerateText_BreakerBlocked(t *testing.T) {
	orig := breakerBlockedProvider
	defer func() { breakerBlockedProvider = orig }()
	breakerBlockedProvider = func(string) bool { return true }

	c := NewClient(
		map[string]Provider{"test-provider": {ChatURL: "http://example.com"}},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	_, err := c.GenerateText(context.Background(), "test-provider",
		[]map[string]any{{"role": "user", "content": "hi"}}, "m")
	if err == nil {
		t.Fatalf("expected error from breaker guard")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Kind != "provider_breaker_open" {
		t.Errorf("Kind: got %q, want %q", apiErr.Kind, "provider_breaker_open")
	}
	if apiErr.Retryable {
		t.Errorf("breaker-open error must be non-retryable (agent should fast-fail, not retry)")
	}
}

// TestIdleTimeoutFor 验证空闲预算按模型能力放宽：思考模型取 thinkingIdleTimeout，其余取
// streamIdleTimeout（提案 §7 Step5，P1）。
func TestIdleTimeoutFor(t *testing.T) {
	thinking := Provider{Models: []ModelInfo{{ID: "m", SupportsThinking: true}}}
	plain := Provider{Models: []ModelInfo{{ID: "m", SupportsThinking: false}}}
	if got := idleTimeoutFor(thinking, "m"); got != thinkingIdleTimeout {
		t.Errorf("thinking model: got %v, want %v", got, thinkingIdleTimeout)
	}
	if got := idleTimeoutFor(plain, "m"); got != streamIdleTimeout {
		t.Errorf("non-thinking model: got %v, want %v", got, streamIdleTimeout)
	}
	// 未知模型 → 回落到 streamIdleTimeout（不误放宽）
	if got := idleTimeoutFor(thinking, "unknown"); got != streamIdleTimeout {
		t.Errorf("unknown model should fall back to streamIdleTimeout, got %v", got)
	}
}
