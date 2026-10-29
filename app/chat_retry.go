package app

import (
	"context"
	"errors"
	"time"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/llm"
)

// ── 失败 turn 自动重试（维度 B · B6）────────────────────────────────────────
//
// 背景：agent.Run 内部已对「单个 LLM 调用」做至多 maxRetries(=8) 次退避重试
// （internal/agent/retry.go），能吸收绝大多数瞬时单调用失败。但若整轮 turn 仍失败
// （例如供应商持续不可用刚好越过该窗口、或首字节超时反复触发），旧逻辑只能写一条
// interrupted/error 事件，用户必须手动重发。B6 在 chat 层补一道「turn 级」自动重放：
// 同一 turnID、同一初始上下文再跑一次整轮，最多 maxTurnRetries 次。
//
// 与 breaker / Retryable 的协调（核心约束，避免与熔断冲突）：
//   - 只重试 *llm.APIError 且 Retryable==true 的错误（口径单源在 retry_matrix.go）；
//   - 排除 tool_args_invalid / provider_breaker_open 两类（模型侧/熔断拦截，重放无益甚至有害）；
//   - 若 ProviderBreaker 当前正拦截该 provider，不重试（重放必然立即再被熔断，纯属狂打）；
//   - 仅当本轮「未产出任何正文 且 未执行任何工具」才重放：已产出正文重放会产生孤儿
//     partial 消息；已执行工具重放会造成双写。两者都交回用户手动重发。
//   - ctx 取消 / 整体超时（非 *APIError）不重试，立即交回。

// maxTurnRetries 是 turn 级自动重放上限（与 agent 内部单调用 8 次重试正交）。
// 取 2：叠加 agent 内部重试后已能覆盖绝大多数「持续性中断后恢复」场景，又不至于在
// 供应商彻底挂起时无限空转（受 mainChatTimeout=30min 兜底）。
const maxTurnRetries = 2

const (
	turnBackoffBase = 5 * time.Second  // turn 级退避基数（随尝试次数线性增长）
	turnBackoffMax  = 30 * time.Second // turn 级退避上限
)

// turnRetryBackoff 计算 turn 级重放的退避时长。
// retryAfter>0 且不超过上限时优先遵循服务商 Retry-After；否则按 attempt(1-indexed) 线性退避。
func turnRetryBackoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		// 服务商指定的退避优先遵循，但封顶到 turnBackoffMax，避免异常/恶意长等待导致对话挂起。
		if retryAfter > turnBackoffMax {
			return turnBackoffMax
		}
		return retryAfter
	}
	d := turnBackoffBase * time.Duration(attempt)
	if d > turnBackoffMax {
		d = turnBackoffMax
	}
	if d <= 0 {
		d = time.Second
	}
	return d
}

// copyTokenMap 返回 token 预算映射的浅拷贝（值为 int，无需深拷贝）。
// 每次 turn 重放都用原始快照的副本，避免 agent.Run 原地累加导致 token 重复计数、
// 误触发上下文压缩。
func copyTokenMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	cp := make(map[string]int, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// classifyTurnRetry 判断整轮 runErr 是否值得 turn 级自动重放，并返回建议退避的
// Retry-After（无则为 0）。这是 B6 与 breaker / Retryable 语义协调的唯一决策点。
//
// 返回 (false, 0) 表示不重放（交回用户手动重发或按既定路径报错）。
func classifyTurnRetry(runErr error, result agent.AgentLoopResult, providerName string) (bool, time.Duration) {
	// 已产出正文：重放会产生孤儿 partial，交回手动重发。
	if result.FinalText != "" {
		return false, 0
	}
	// 已执行工具：重放会双写，交回手动重发。
	if result.ToolExecuted {
		return false, 0
	}

	var apiErr *llm.APIError
	if !errors.As(runErr, &apiErr) {
		// 非 *llm.APIError（如 ctx 取消 / 整体超时 / 内部错误）→ 不重试。
		return false, 0
	}
	if !apiErr.Retryable {
		// 不可重试错误（客户端 4xx、连接拒绝/DNS 不可达、非瞬时空闲超时等）→ 不重试。
		return false, 0
	}
	switch apiErr.Kind {
	case "tool_args_invalid", "provider_breaker_open":
		// 模型侧协议错误 / 熔断拦截：重放无益甚至有害。
		return false, 0
	}
	// 熔断正在拦截该 provider：重放必然立即再被熔断，纯属狂打，不重试。
	if llm.ProviderBreaker.IsOpen(providerName) {
		return false, 0
	}
	return true, apiErr.RetryAfter
}

// runTurnWithRetry 包裹单次 agent.Run，按 B6 规则对整轮失败做有限次自动重放。
// 重放复用同一 turnID 与同一初始上下文（messages），每次用原始 token 快照的副本，
// 因此不会产生孤儿消息或工具双写（见 classifyTurnRetry 的护栏）。
func (a *App) runTurnWithRetry(ctx context.Context, opts agent.RunOptions, providerName string, baseTokens map[string]int) (agent.AgentLoopResult, error) {
	var (
		result agent.AgentLoopResult
		runErr error
	)
	for attempt := 0; attempt <= maxTurnRetries; attempt++ {
		// 每次重放使用原始 token 快照的副本，避免原地累加触发误压缩。
		opts.RunningTokens = copyTokenMap(baseTokens)
		result, runErr = a.agent.Run(ctx, opts)
		if runErr == nil {
			return result, nil
		}
		retryable, retryAfter := classifyTurnRetry(runErr, result, providerName)
		if !retryable || attempt >= maxTurnRetries {
			return result, runErr
		}
		backoff := turnRetryBackoff(attempt+1, retryAfter)
		a.logger.Warn("turn 级自动重放（与 breaker/Retryable 协调）",
			"session_id", opts.SessionID,
			"turn_id", opts.TurnID,
			"attempt", attempt+1,
			"max_retries", maxTurnRetries,
			"backoff_ms", backoff.Milliseconds(),
			"provider", providerName,
			"err", agent.FriendlyError(runErr))
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
	return result, runErr
}
