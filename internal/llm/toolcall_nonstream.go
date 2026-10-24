package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// callToolNonStream 以非流式方式请求工具调用，返回目标工具的 arguments JSON。
//
// 作为流式零产出（empty_stream）的回退路径：部分第三方服务商/中转不支持
// 「流式 + tools」组合，流式返回零产出或非标准格式，但非流式可正常返回 tool_calls。
//
// payload 复用 buildPayload（temperature/max_tokens/thinking/Provider 钩子一致），
// 仅关闭 stream 与 stream_options。
func (c *Client) callToolNonStream(
	ctx context.Context,
	providerName, modelID string,
	messages []map[string]any,
	tools []map[string]any,
	callOpts *CallOptions,
) (json.RawMessage, error) {
	c.mu.RLock()
	p, ok := c.providers[providerName]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	payload := c.buildPayload(&p, messages, tools, modelID, callOpts)
	payload["stream"] = false
	delete(payload, "stream_options")

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.ChatURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.APIKey,
	}
	if p.BuildHeaders != nil {
		headers = p.BuildHeaders(callOpts, headers)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// 网络错误分类走重试矩阵单源（retry_matrix.go）：永久性（DNS/连接拒绝）判不可重试。
		return nil, apiErrorFromNetErr(err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// 错误分类（Retryable/Retry-After/消息解析）走重试矩阵单源（retry_matrix.go）。
		return nil, apiErrorFromHTTP(resp.StatusCode, respBody, p.ParseError, resp.Header.Get("Retry-After"))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析非流式响应失败: %w（body: %.200s）", err, string(respBody))
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("非流式响应没有 choices（body: %.200s）", string(respBody))
	}

	msg := result.Choices[0].Message
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "" || len(tc.Function.Arguments) == 0 {
			continue
		}
		// OpenAI 规范 arguments 是字符串；部分非标准后端直接返回 JSON 对象，两种都兼容。
		raw, err := unquoteIfNeeded(tc.Function.Arguments)
		if err != nil {
			return nil, fmt.Errorf("非流式工具调用参数 JSON 无效（tool=%s）: %w", tc.Function.Name, err)
		}
		return raw, nil
	}

	// 没有 tool_calls：带出部分文本便于诊断（如模型把 JSON 写进了 content）。
	snippet := msg.Content
	if len(snippet) > 200 {
		snippet = snippet[:200] + "..."
	}
	if snippet == "" {
		return nil, fmt.Errorf("非流式响应未包含工具调用（模型可能只输出了推理内容）")
	}
	return nil, fmt.Errorf("模型返回了文本而非工具调用: %s", snippet)
}

// unquoteIfNeeded 兼容两种 arguments 编码：
//   - OpenAI 规范：字符串（JSON 编码后的 JSON 文本）→ 先反引出内层文本
//   - 非标准后端：直接的 JSON 对象 → 原样返回
//
// 返回值保证是合法 JSON 文本，否则报错。
func unquoteIfNeeded(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		trimmed = []byte(s)
	}
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("invalid JSON")
	}
	return json.RawMessage(trimmed), nil
}
