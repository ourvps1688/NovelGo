package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// GenerateText 发起非流式对话，返回模型生成的纯文本。
// messages 由调用方自行组装，格式与 ChatStream 一致。
func (c *Client) GenerateText(
	ctx context.Context,
	providerName string,
	messages []map[string]any,
	model string,
) (string, error) {
	c.mu.RLock()
	p, ok := c.providers[providerName]
	c.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("unknown provider: %s", providerName)
	}

	// 熔断拦截（P1）：与 ChatStream 共用同一 ProviderBreaker，pipeline 已熔断时同步跳过。
	if breakerBlockedProvider(providerName) {
		return "", breakerOpenError(providerName)
	}

	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}

	if p.BuildRequest != nil {
		payload = p.BuildRequest(payload)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// 空闲 watchdog：非流式请求同样可能因服务端挂起/网络静默断流而永久阻塞，
	// 派生可取消 ctx，body 读取空闲超过 streamIdleTimeout 即取消。
	genCtx, cancelGen := context.WithCancel(ctx)
	defer cancelGen()

	req, err := http.NewRequestWithContext(genCtx, http.MethodPost, p.ChatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.APIKey,
	}
	if p.BuildHeaders != nil {
		headers = p.BuildHeaders(nil, headers)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// 网络错误分类走重试矩阵单源（retry_matrix.go）：裸 error 升级为 *APIError，
		// agent 层才能按 Retryable 判定走既有退避重试（永久性网络错误仍判不可重试）。
		return "", apiErrorFromNetErr(err)
	}
	defer resp.Body.Close()

	wd := newIdleWatchdog(cancelGen, c.logger, idleTimeoutFor(p, model))
	respBody, err := io.ReadAll(wd.wrapBody(resp.Body))
	if err != nil {
		var idleErr *idleTimeoutError
		if errors.As(err, &idleErr) {
			return "", &APIError{StatusCode: 0, Message: "响应空闲超时：" + idleErr.Error() + "，疑似网络中断或服务端挂起", Retryable: true}
		}
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		// 错误分类（Retryable/Retry-After/消息解析）走重试矩阵单源（retry_matrix.go）。
		return "", apiErrorFromHTTP(resp.StatusCode, respBody, p.ParseError, resp.Header.Get("Retry-After"))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response from LLM")
	}
	return result.Choices[0].Message.Content, nil
}
