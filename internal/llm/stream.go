package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Client 是 LLM 流式调用的传输层。
// 持有多 provider，Run 时根据 providerName 选择合适的后端。
type Client struct {
	mu        sync.RWMutex
	providers map[string]Provider // providerName → 完整配置（由 Merge 产出）
	http      *http.Client
	logger    *slog.Logger
}

// NewClient 创建 LLM 客户端。providers 应为 Merge 产出的已组装配置。
func NewClient(providers map[string]Provider, log *slog.Logger) *Client {
	return &Client{
		providers: providers,
		http:      newHTTPClient(),
		logger:    log,
	}
}

// newHTTPClient 创建流式请求专用 http.Client：
//   - ResponseHeaderTimeout: 10m 首字节超时（不含 body 读取）
//   - DialContext: 10s TCP 连接超时
//   - Client.Timeout: 0（不限制整体超时，body 读取由 ctx 控制）
//
// 注意：绝不能用 http.Client.Timeout，会切断 DeepSeek 思考模型长输出
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).DialContext,
			ResponseHeaderTimeout: 10 * time.Minute,
		},
	}
}

// Reload 热更新 providers 配置。不影响正在进行的 ChatStream（值拷贝）。
func (c *Client) Reload(providers map[string]Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers = providers
}

// streamIdleTimeout 是 SSE 流式响应「chunk 间最大空闲时长」：
// 超过该时长未从响应 body 读到任何字节，即判定流已静默断开
// （服务端挂起 / 网络设备静默丢流，无 FIN/RST），主动取消请求。
// 必须远大于思考模型相邻 chunk 的正常间隔（思考 delta 通常秒级到达），
// 又要明显短于「永远卡死」。ResponseHeaderTimeout 只覆盖首字节前的等待，
// body 读取阶段此前没有任何超时，是 S5 修复等长任务卡死的根因。
const streamIdleTimeout = 3 * time.Minute

// idleTimeoutError 标记由空闲 watchdog 触发的中断，供 parseSSE 生成可读错误文案。
type idleTimeoutError struct{ timeout time.Duration }

func (e *idleTimeoutError) Error() string {
	return fmt.Sprintf("流式响应空闲超时（%s 内无任何数据）", e.timeout)
}

// idleWatchdog 监视 SSE body 读取活动：每收到字节就刷新 lastActivity，
// 监视 goroutine 每 5s 轮询一次，发现空闲超过 streamIdleTimeout 即 cancel(ctx)。
// 相比 time.Timer.Reset 轮询方案无竞态（CAS/双检查都不需要），代价可忽略。
type idleWatchdog struct {
	last    atomic.Int64 // 最近一次收到字节的 UnixNano
	fired   atomic.Bool
	timeout time.Duration
	cancel  context.CancelFunc
	logger  *slog.Logger
}

func newIdleWatchdog(cancel context.CancelFunc, logger *slog.Logger) *idleWatchdog {
	wd := &idleWatchdog{timeout: streamIdleTimeout, cancel: cancel, logger: logger}
	wd.last.Store(time.Now().UnixNano())
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if wd.fired.Load() {
				return
			}
			last := time.Unix(0, wd.last.Load())
			if time.Since(last) > wd.timeout {
				if wd.fired.CompareAndSwap(false, true) {
					if wd.logger != nil {
						wd.logger.Warn("llm stream idle watchdog fired, canceling request",
							"idle", time.Since(last).String(), "timeout", wd.timeout.String())
					}
					recordIdleFire()
					wd.cancel()
				}
				return
			}
		}
	}()
	return wd
}

// wrapBody 把 resp.Body 包一层「读到字节即刷新活动时间」的 reader；
// 读取报错且 watchdog 已触发时，返回 idleTimeoutError 供上层识别。
// 注意：包装层不实现 Close，底层 body 的 Close 由 ChatStream 的 defer 负责。
func (wd *idleWatchdog) wrapBody(r io.Reader) io.Reader {
	return &kickReader{r: r, wd: wd}
}

type kickReader struct {
	r  io.Reader
	wd *idleWatchdog
}

func (k *kickReader) Read(p []byte) (int, error) {
	n, err := k.r.Read(p)
	if n > 0 {
		k.wd.last.Store(time.Now().UnixNano())
	}
	if err != nil && k.wd.fired.Load() {
		return n, &idleTimeoutError{timeout: k.wd.timeout}
	}
	return n, err
}

// HTTPClient 返回底层共享的 HTTP 客户端，供非流式请求（如图像生成）复用。
func (c *Client) HTTPClient() *http.Client { return c.http }

// ChatStream 发起流式对话，返回 SSE 事件 channel。
//
// providerName 指明使用哪个 Provider，messages/tools 由调用方按 OpenAI Function Calling 格式组装。
// Client 负责拼装完整 payload 并注入流式参数和钩子处理。
//
// ctx 取消时底层 HTTP 连接被中止，channel 随之关闭。
func (c *Client) ChatStream(
	ctx context.Context,
	providerName string,
	messages []map[string]any,
	tools []map[string]any,
	model string,
	opts *CallOptions,
) <-chan StreamEvent {
	c.mu.RLock()
	p, ok := c.providers[providerName]
	c.mu.RUnlock()
	if !ok {
		ch := make(chan StreamEvent, 1)
		ch <- StreamEvent{Type: EventError, Error: fmt.Errorf("unknown provider: %s", providerName)}
		close(ch)
		return ch
	}

	ch := make(chan StreamEvent, 32)
	go func() {
		defer close(ch)

		payload := c.buildPayload(&p, messages, tools, model, opts)

		body, err := json.Marshal(payload)
		if err != nil {
			ch <- StreamEvent{Type: EventError, Error: fmt.Errorf("failed to marshal request body: %w", err)}
			return
		}

		// 空闲 watchdog：派生可取消 ctx，body 读取空闲超过 streamIdleTimeout 即取消请求，
		// 防止 SSE 流静默断开时 streamLoop 永久阻塞（调用方 ctx 在长任务中永不取消）。
		streamCtx, cancelStream := context.WithCancel(ctx)
		defer cancelStream()

		req, err := http.NewRequestWithContext(streamCtx, http.MethodPost, p.ChatURL, bytes.NewReader(body))
		if err != nil {
			ch <- StreamEvent{Type: EventError, Error: fmt.Errorf("failed to create HTTP request: %w", err)}
			return
		}

		// 组装请求头
		headers := map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + p.APIKey,
		}
		if p.BuildHeaders != nil {
			headers = p.BuildHeaders(opts, headers)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			// 网络错误分类走重试矩阵单源（retry_matrix.go），此处只负责日志。
			if isTimeoutErr(err) {
				c.logger.Warn("llm first byte timeout", "err", err, "url", req.URL.String())
			} else {
				c.logger.Warn("llm network error", "err", err, "url", req.URL.String())
			}

			ch <- StreamEvent{Type: EventError, Error: apiErrorFromNetErr(err)}
			return
		}
		defer resp.Body.Close()

		// HTTP 错误
		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
			// 关键节点日志：HTTP 4xx/5xx
			bodyStr := string(errBody)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500]
			}
			c.logger.Warn("llm http error",
				"status", resp.StatusCode,
				"body", bodyStr,
				"url", req.URL.String())
			// 错误分类（Retryable/Retry-After/消息解析）走重试矩阵单源（retry_matrix.go）。
			ch <- StreamEvent{Type: EventError, Error: apiErrorFromHTTP(
				resp.StatusCode, errBody, p.ParseError, resp.Header.Get("Retry-After"))}
			return
		}

		// SSE 逐行解析（body 空闲超过 streamIdleTimeout 时 watchdog 取消请求 → 读报错 → EventError）。
		// 把 resp.Body 换成带活动刷新的包装 reader；defer 关闭的是包装前捕获的原始 body。
		wd := newIdleWatchdog(cancelStream, c.logger)
		resp.Body = io.NopCloser(wd.wrapBody(resp.Body))
		c.parseSSE(ch, resp)
	}()

	return ch
}

// buildPayload 组装 LLM API 请求体。
func (c *Client) buildPayload(
	p *Provider,
	messages []map[string]any,
	tools []map[string]any,
	model string,
	opts *CallOptions,
) map[string]any {
	// 出站归一化：合并多条 system 为单条置于头部（参见 mergeSystemMessages）。
	messages = mergeSystemMessages(messages)
	payload := map[string]any{
		"model":          model,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}

	if len(tools) > 0 {
		payload["tools"] = tools
	}
	if opts != nil && opts.ToolChoice != nil {
		payload["tool_choice"] = opts.ToolChoice
	}

	// 从 ModelInfo 取模型默认值
	var um *ModelInfo
	for i := range p.Models {
		if p.Models[i].ID == model {
			um = &p.Models[i]
			break
		}
	}

	temperature := 0.7
	if p.Temperature != nil {
		temperature = *p.Temperature
	}
	// 自定义模型未配 MaxOutputTokens 时的安全默认值：64K。
	// 足够 thinking 模型的推理 + 输出，又不至于超出多数模型限制触发 400。
	maxTokens := 64000
	if opts != nil && opts.Temperature != nil {
		temperature = *opts.Temperature
	}
	if opts != nil && opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	} else if um != nil && um.MaxOutputTokens > 0 {
		maxTokens = um.MaxOutputTokens
	}

	// 上下文安全收缩：模型拥有上下文窗口时，确保 prompt + max_tokens 不超窗口，
	// 直接避免触发「max context length」HTTP 400。用 o200k BPE 精确估计 prompt
	// token，留 2K 安全余量，并兜底到最小可用输出预算。
	if um != nil && um.ContextWindow > 0 {
		promptTokens, _, _ := CountMessagesTokens(messages)
		maxTokens = clampMaxTokens(maxTokens, um.ContextWindow, promptTokens)
	}
	payload["temperature"] = temperature
	payload["max_tokens"] = maxTokens

	// 推理/思考参数：支持思考的模型默认开启，opts 可覆盖
	thinkingEnabled := false
	reasoningEffort := ""
	if um != nil && um.SupportsThinking {
		thinkingEnabled = true
		if len(um.ReasoningLevels) > 0 {
			reasoningEffort = um.ReasoningLevels[0]
		}
	}
	if opts != nil {
		if opts.ThinkingEnabled != nil {
			thinkingEnabled = *opts.ThinkingEnabled
		}
		if opts.ReasoningEffort != nil {
			reasoningEffort = *opts.ReasoningEffort
		}
	}

	if thinkingEnabled {
		payload["thinking"] = map[string]string{"type": "enabled"}
		if reasoningEffort != "" {
			payload["reasoning_effort"] = reasoningEffort
		}
	}

	// Provider 钩子改造
	if p.BuildRequest != nil {
		payload = p.BuildRequest(payload)
	}

	return payload
}

// mergeSystemMessages 将 messages 数组中所有 role=system 的消息合并成单条置于头部。
//
// 背景：OpenAI 协议允许多条 system 消息，但大量本地推理后端（llama.cpp 系列、
// 部分 ollama 后端等）只支持头部最多一条 system，多余会被静默丢弃或直接 400。
// OpenAI 官方社区确认：多条 system ≈ 合并成一条（推理效果几乎等价），所以
// 客户端主动合并是无损的，且能最大化跨后端兼容性。
//
// 行为：
//   - 收集所有 role=system 且 content 为非空 string 的消息内容
//   - 用空行分隔拼成单条 system，置于 messages 头部
//   - 其余消息（含 content 非 string 的 system，如多模态）保持原有顺序
//   - 0 或 1 条 string system 时直接返回原 messages，避免不必要的拷贝
func mergeSystemMessages(messages []map[string]any) []map[string]any {
	var sysParts []string
	var others []map[string]any
	for _, m := range messages {
		if role, _ := m["role"].(string); role == "system" {
			if content, ok := m["content"].(string); ok && content != "" {
				sysParts = append(sysParts, content)
				continue
			}
		}
		others = append(others, m)
	}
	if len(sysParts) <= 1 {
		return messages
	}
	merged := map[string]any{"role": "system", "content": strings.Join(sysParts, "\n\n")}
	out := make([]map[string]any, 0, len(others)+1)
	out = append(out, merged)
	out = append(out, others...)
	return out
}

// parseSSE 解析 SSE 流，产出 StreamEvent。
func (c *Client) parseSSE(ch chan<- StreamEvent, resp *http.Response) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024) // 行最长 2MB

	// 工具调用累积缓冲区：按 index 对齐
	type accToolCall struct {
		id        string
		name      string
		arguments strings.Builder
	}
	accumulated := make([]accToolCall, 0, 4)
	var diag sseDiagnostics // 诊断追踪（hasContent/hasToolCalls/hasReason/headLines/tailLines 等）
	// usage 缓冲（diag.lastUsage）：部分 provider（如硅基流动）在每个 SSE chunk 都带 usage，
	// 而非只在末尾。这里只保留最后一个，流结束后统一发一次，避免每 chunk
	// 触发一次 DB 持久化（tokens.go updateUsage）和前端事件刷屏。

	for scanner.Scan() {
		line := scanner.Text()

		// 累积原始行样本用于 empty 诊断（前 10 行 + 后 10 行环形）
		if len(diag.headLines) < 10 {
			diag.headLines = append(diag.headLines, line)
		}
		diag.tailLines = append(diag.tailLines, line)
		if len(diag.tailLines) > 10 {
			diag.tailLines = diag.tailLines[1:]
		}

		// SSE data 行
		const prefix = "data: "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		diag.dataLineCount++
		data := line[len(prefix):]

		// 流结束标记
		if data == "[DONE]" {
			diag.gotDone = true
			break
		}

		// 解析 JSON chunk
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			c.logger.Warn("SSE JSON parse failed", "line", data, "error", err)
			continue
		}

		// 提取 usage：标准 provider 只在末尾 chunk 发一次，硅基流动等会每个 chunk 都发。
		// 只缓存最后一个，流结束后统一发一次（见循环外）。
		if usage, ok := chunk["usage"].(map[string]any); ok && usage != nil {
			diag.lastUsage = usage
		}

		// 提取 choices
		choices, ok := chunk["choices"].([]any)
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]any)
		if !ok {
			continue
		}
		// 追踪 finish_reason：标准 provider 在最后一个 chunk 的 choice 上置 finish_reason。
		// 思考模型（SupportsThinking）的推理 token 也计入 max_tokens 预算，
		// finish_reason=="length" 表示输出被预算截断（可能致 tool arguments JSON 不完整）。
		// 放在 delta 提取之前，确保无 delta 的收尾 chunk 也能被追踪到。
		if fr, ok := choice["finish_reason"].(string); ok && fr == "length" {
			diag.truncatedByLength = true
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}

		// reasoning_content → EventThinking
		if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
			ch <- StreamEvent{Type: EventThinking, Data: reasoning}
			diag.hasReason = true
		}

		// content → EventContent
		if content, ok := delta["content"].(string); ok && content != "" {
			ch <- StreamEvent{Type: EventContent, Data: content}
			diag.hasContent = true
		}

		// tool_calls delta → 按 index 累积
		toolCalls, ok := delta["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, tcRaw := range toolCalls {
			tc, _ := tcRaw.(map[string]any)
			if tc == nil {
				continue
			}
			idxFloat, ok := tc["index"].(float64)
			if !ok || idxFloat < 0 {
				c.logger.Warn("tool call delta missing valid index", "delta", tc)
				continue
			}
			idx := int(idxFloat)

			// 扩展累积缓冲区
			for len(accumulated) <= idx {
				accumulated = append(accumulated, accToolCall{})
			}
			acc := &accumulated[idx]

			// ID（首次出现）
			if id, ok := tc["id"].(string); ok && id != "" && acc.id == "" {
				acc.id = id
				ch <- StreamEvent{
					Type:  EventToolCallStart,
					Delta: &ToolCallDelta{ToolID: id},
				}
			}

			// function 子对象
			fn, ok := tc["function"].(map[string]any)
			if !ok {
				continue
			}

			// 名称（首次出现）
			if name, ok := fn["name"].(string); ok && name != "" && acc.name == "" {
				acc.name = name
				ch <- StreamEvent{
					Type:  EventToolCallStart,
					Delta: &ToolCallDelta{ToolName: name, ToolID: acc.id},
				}
			}

			// arguments 增量追加
			if args, ok := fn["arguments"].(string); ok && args != "" {
				acc.arguments.WriteString(args)
				ch <- StreamEvent{
					Type: EventToolCallArguments,
					Delta: &ToolCallDelta{
						ToolName:      acc.name,
						ToolID:        acc.id,
						ArgumentsText: acc.arguments.String(),
					},
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		msg := fmt.Sprintf("SSE stream read error: %s", err)
		var idleErr *idleTimeoutError
		if errors.As(err, &idleErr) {
			msg = fmt.Sprintf("流式响应空闲超时：%s 内未收到任何数据，疑似网络中断或服务端挂起，已自动中断待重试",
				idleErr.timeout.String())
		}
		c.logger.Warn("sse stream interrupted", "err", err)
		ch <- StreamEvent{Type: EventError, Error: &APIError{
			StatusCode: 0,
			Message:    msg,
			Retryable:  true,
		}}
		// 流异常中断：不再做 tool call 收尾，避免与 invalidToolErrors 分支叠加
		// 产生第二个 EventError（scanner.Err 后 accumulated 数据不可信，保守丢弃）
		return
	}

	// 流正常结束：发送本次 stream 的最终 usage（只发一次，见循环内缓冲说明）。
	// 放在 tool call 收尾之前，与标准 provider「末尾 chunk 发 usage」的时序一致。
	if diag.lastUsage != nil {
		ch <- StreamEvent{Type: EventUsage, Usage: diag.lastUsage}
	}

	// 流结束后，发送完整工具调用。参数保留原始 JSON，由 Registry 按目标类型反序列化。
	// 按 id 去重：DeepSeek 偶发会在不同 index 上发相同 id 的 tool_call，
	// 这里保留首次出现的 slot，跳过后续重复 id，避免主 agent 重复执行 tool（特别是 run_subagent 等耗时工具）。
	//
	// 全丢弃策略：如果本轮有任意 tool arguments 解析失败，不发任何 EventToolCallEnd
	// （成功的 tool call 也一起丢弃），直接发 EventError（Kind="tool_args_invalid"）。
	// agent 层收到后丢弃 responseBuffer/thinkingBuffer + appendMsg reminder + 走自动重试。
	// 这避免 "部分 tool call 执行了，部分没有" 导致的 assistant.tool_calls 协议矛盾。
	type validToolCall struct {
		idx int
		raw string
	}
	var validToolCalls []validToolCall
	var invalidToolErrors []string

	seenToolIDs := make(map[string]bool, len(accumulated))
	for i := range accumulated {
		acc := &accumulated[i]
		if acc.name == "" || acc.arguments.Len() == 0 {
			continue
		}
		// 按 id 去重（空 id 不去重，理论上不应出现）
		if acc.id != "" {
			if seenToolIDs[acc.id] {
				c.logger.Warn("duplicate tool_call_id from provider, skipping",
					"tool", acc.name, "tool_call_id", acc.id, "index", i)
				continue
			}
			seenToolIDs[acc.id] = true
		}
		raw := acc.arguments.String()
		if !json.Valid([]byte(raw)) {
			// 先 valid，不通过再 unmarshal 拿具体错误信息
			var invalidErr error
			var probe any
			if invalidErr = json.Unmarshal([]byte(raw), &probe); invalidErr == nil {
				// 理论不会走到这里（Valid 已返回 false），防御性代码
				invalidErr = fmt.Errorf("invalid JSON")
			}
			c.logger.Warn("tool arguments JSON invalid", "tool", acc.name, "raw", raw, "err", invalidErr)
			invalidToolErrors = append(invalidToolErrors, fmt.Sprintf("tool=%s: %s", acc.name, invalidErr.Error()))
			continue
		}
		validToolCalls = append(validToolCalls, validToolCall{idx: i, raw: raw})
	}

	// tool arguments 解析失败：全丢弃（不发送成功的 tool call），发 EventError（tool_args_invalid）
	if len(invalidToolErrors) > 0 {
		errMsg := buildToolArgsInvalidMsg(invalidToolErrors)
		// 若工具参数存在但 JSON 校验失败，且流被 max_tokens 截断
		// （finish_reason=="length"，思考模型的推理 token 也计入预算），升级为明确提示，
		// 替代笼统的 "unexpected end of JSON input"，便于定位为预算不足而非协议错误。
		if diag.truncatedByLength {
			errMsg = "输出因 max_tokens 预算被截断（思考模型的推理 token 也计入预算），" +
				"建议提高输出预算或换模型。原错误：" + errMsg
		}
		ch <- StreamEvent{Type: EventError, Error: &APIError{
			StatusCode: 0,
			Message:    errMsg,
			Retryable:  true,
			Kind:       "tool_args_invalid",
		}}
	} else {
		// 没有失败，发送完整的 tool call
		for _, vtc := range validToolCalls {
			acc := &accumulated[vtc.idx]
			ch <- StreamEvent{
				Type: EventToolCallEnd,
				Delta: &ToolCallDelta{
					ToolName:      acc.name,
					ToolID:        acc.id,
					ArgumentsText: vtc.raw,
					ArgumentsJSON: json.RawMessage(vtc.raw),
				},
			}
			diag.hasToolCalls = true
		}

		// 零产出检测：流结束但未收到任何有效内容（content 或 tool call 收尾）。
		// 填充 HTTP 快照后通过 LogValuer 输出结构化诊断，区分限流软拒绝 / 推理失控 / 非标准格式。
		if !diag.hasContent && !diag.hasToolCalls {
			diag.statusCode = resp.StatusCode
			diag.contentType = resp.Header.Get("Content-Type")
			diag.contentLength = resp.Header.Get("Content-Length")
			diag.retryAfter = resp.Header.Get("Retry-After")
			diag.rateLimitRemain = resp.Header.Get("X-RateLimit-Remaining")
			c.logger.Warn("empty sse response", "diag", diag)
			// Kind="empty_stream"：CallTool 层据此触发非流式回退（部分服务商不支持流式+tools）。
			msg := "流式响应为空，服务商可能不支持流式请求或返回了非标准格式"
			if diag.hasReason {
				msg = "流式响应只有推理内容，未生成最终结果（可能 max_tokens 不足或推理失控）"
			}
			ch <- StreamEvent{Type: EventError, Error: &APIError{
				StatusCode: 0,
				Message:    msg,
				Retryable:  true,
				Kind:       "empty_stream",
			}}
		}
	}
}

// buildToolArgsInvalidMsg 构造 tool arguments 解析失败的错误信息。
// 格式：tool arguments 解析失败（N 个）：tool=xxx: err; tool=yyy: err
// 截断到 200 字符，避免 reminder 过长。
func buildToolArgsInvalidMsg(errs []string) string {
	const maxLen = 200
	prefix := fmt.Sprintf("tool arguments 解析失败（%d 个）", len(errs))
	detail := strings.Join(errs, "; ")
	msg := prefix + "：" + detail
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "..."
	}
	return msg
}

// statusRetryable 判断 HTTP 状态码对应的错误是否可重试。
// 429（限流）、408（超时）、5xx（服务端错误）可重试。
func statusRetryable(code int) bool {
	if code == 429 || code == 408 {
		return true
	}
	if code >= 500 && code < 600 {
		return true
	}
	return false
}

// parseDefaultError 按 OpenAI 标准格式解析错误响应体。
func parseDefaultError(body []byte) error {
	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Error.Message == "" {
		return fmt.Errorf("request failed: %s", string(body))
	}
	return fmt.Errorf("%s", resp.Error.Message)
}

// parseRetryAfter 解析 Retry-After header。
// 支持两种格式：
//   - 秒数：如 "120" 表示 120 秒后重试
//   - HTTP-date：如 "Wed, 21 Oct 2026 07:28:00 GMT"
//
// 解析失败或为空返回 0。
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	// 优先尝试秒数
	if secs, err := strconv.Atoi(value); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	// 尝试 HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

// clampMaxTokens 在保证 prompt + maxTokens 不超出 contextWindow 的前提下，
// 收缩 maxTokens 避免触发服务端的 max context length 限制（HTTP 400）。
// safetyMargin 预留少量余量；available 低于 minOutputBudget 时仍给模型最小输出预算。
func clampMaxTokens(maxTokens, contextWindow, promptTokens int) int {
	const safetyMargin = 2048
	const minOutputBudget = 4096
	available := contextWindow - promptTokens - safetyMargin
	if available < minOutputBudget {
		available = minOutputBudget
	}
	if maxTokens > available {
		return available
	}
	return maxTokens
}

// idleFireLog 记录近期「流式空闲超时（idle watchdog 触发）」的发生时刻，用于向上层
// 暴露 Provider 健康度——长时间挂起会让修复/写作/重写等长任务反复失败。仅保留最近
// maxIdleFires 条，避免长时间运行的进程内存膨胀。
type idleFireLog struct {
	mu    sync.Mutex
	times []time.Time
}

const maxIdleFires = 200

var idleFires = &idleFireLog{}

func (l *idleFireLog) record() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.times = append(l.times, time.Now())
	if len(l.times) > maxIdleFires {
		l.times = l.times[len(l.times)-maxIdleFires:]
	}
}

// recordIdleFire 由 idle watchdog 在超时触发时调用，记录一次 Provider 疑似挂起事件。
func recordIdleFire() { idleFires.record() }

// RecentIdleTimeouts 返回最近 window 内发生的空闲超时次数（Provider 疑似挂起的信号）。
// 上层（如 S5 修复失败）可用它判断失败是否由供应商挂起而非代码逻辑导致，并据此给出提示。
func RecentIdleTimeouts(window time.Duration) int {
	idleFires.mu.Lock()
	defer idleFires.mu.Unlock()
	cutoff := time.Now().Add(-window)
	count := 0
	for _, t := range idleFires.times {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}
