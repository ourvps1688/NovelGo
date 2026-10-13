package llm

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// calltoolTestServer 构造一个按请求 payload.stream 分流的测试端点：
//   - stream=true：返回零产出 SSE（只有空 delta + [DONE]），触发 empty_stream
//   - stream=false：返回非流式 tool_calls 响应（arguments 编码由 nonStreamArgs 决定）
func calltoolTestServer(t *testing.T, nonStreamArgs string, streamCalls *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)

		if isStream, _ := payload["stream"].(bool); isStream {
			*streamCalls++
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{}}]}\n\ndata: [DONE]\n\n"))
			return
		}

		resp := map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "extract",
							"arguments": nonStreamArgs,
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func calltoolTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	prov := Provider{
		Name:    "p",
		ChatURL: serverURL,
		APIKey:  "k",
		Models:  []ModelInfo{{ID: "m", ContextWindow: 128000, MaxOutputTokens: 8192}},
	}
	return NewClient(map[string]Provider{"p": prov}, slog.Default())
}

func calltoolTestInput() ([]map[string]any, []map[string]any) {
	messages := []map[string]any{{"role": "user", "content": "hi"}}
	tools := []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":       "extract",
			"parameters": map[string]any{"type": "object"},
		},
	}}
	return messages, tools
}

// 流式零产出 → 自动回退非流式 → 拿到工具调用结果。
func TestCallToolFallbackToNonStreamOnEmptyStream(t *testing.T) {
	var streamCalls int
	server := calltoolTestServer(t, `{"a":1}`, &streamCalls)
	defer server.Close()
	client := calltoolTestClient(t, server.URL)
	messages, tools := calltoolTestInput()

	raw, err := client.CallTool(context.Background(), "p", "m", messages, tools, "extract", &CallToolOptions{Attempts: 1})
	if err != nil {
		t.Fatalf("CallTool fallback failed: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if got["a"] != float64(1) {
		t.Fatalf("unexpected arguments: %s", raw)
	}
	if streamCalls == 0 {
		t.Fatal("expected stream request to have been made before fallback")
	}
}

// 非标准后端直接返回 JSON 对象形式的 arguments（而非字符串）也应兼容。
func TestCallToolFallbackAcceptsObjectArguments(t *testing.T) {
	var streamCalls int
	server := calltoolTestServer(t, `{"items":["x","y"],"n":2}`, &streamCalls)
	defer server.Close()
	client := calltoolTestClient(t, server.URL)
	messages, tools := calltoolTestInput()

	raw, err := client.CallTool(context.Background(), "p", "m", messages, tools, "extract", &CallToolOptions{Attempts: 1})
	if err != nil {
		t.Fatalf("CallTool fallback failed: %v", err)
	}
	var got struct {
		Items []string `json:"items"`
		N     int      `json:"n"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("arguments not valid JSON: %v", err)
	}
	if len(got.Items) != 2 || got.N != 2 {
		t.Fatalf("unexpected arguments: %s", raw)
	}
}

// 流式正常产出时不触发回退（无非流式请求）。
func TestCallToolNoFallbackWhenStreamWorks(t *testing.T) {
	var streamCalls, nonStreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if isStream, _ := payload["stream"].(bool); isStream {
			streamCalls++
			chunk := func(args string) []byte {
				b, _ := json.Marshal(map[string]any{
					"choices": []map[string]any{{
						"delta": map[string]any{
							"tool_calls": []map[string]any{{
								"index":    0,
								"id":       "c1",
								"function": map[string]any{"name": "extract", "arguments": args},
							}},
						},
					}},
				})
				return append(b, '\n', '\n')
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(chunk(`{"a":1`))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(chunk(`}`))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		nonStreamCalls++
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	client := calltoolTestClient(t, server.URL)
	messages, tools := calltoolTestInput()

	raw, err := client.CallTool(context.Background(), "p", "m", messages, tools, "extract", &CallToolOptions{Attempts: 1})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if string(raw) != `{"a":1}` {
		t.Fatalf("unexpected arguments: %s", raw)
	}
	if nonStreamCalls != 0 {
		t.Fatalf("non-stream fallback should not fire, got %d calls", nonStreamCalls)
	}
	if streamCalls == 0 {
		t.Fatal("stream request never made")
	}
}

// 流式零产出且非流式回退也失败时，两个错误都应聚合返回，不静默。
func TestCallToolFallbackFailureAggregates(t *testing.T) {
	var streamCalls int
	// 非流式返回无 tool_calls 的纯文本，回退应失败。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if isStream, _ := payload["stream"].(bool); isStream {
			streamCalls++
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{}}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"just text"}}]}`))
	}))
	defer server.Close()
	client := calltoolTestClient(t, server.URL)
	messages, tools := calltoolTestInput()

	_, err := client.CallTool(context.Background(), "p", "m", messages, tools, "extract", &CallToolOptions{Attempts: 1})
	if err == nil {
		t.Fatal("expected error when both stream and non-stream fail")
	}
}
