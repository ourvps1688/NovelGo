package app

import (
	"errors"
	"strings"

	"github.com/novelgo/novelgo/internal/llm"
)

// isLLMInfraError 判定错误是否为「基础设施 / 供应商侧」错误，而非代码回归：
//   - 账户类：401 未鉴权 / 402 余额不足 / 403 无权限 / 407 代理鉴权
//   - 限流：429
//   - 服务端：>= 500
//   - 网络层：StatusCode==0（连接拒绝 / DNS 不可达 / 首字节超时 / EOF）
//
// 这类错误在 CI 上不应阻断流水线 —— 真实 LLM E2E 测试应 t.Skip；
// 而 API 可达时的断言校验（结构化输出、命中率门槛）仍照常运行，能抓住真正的代码回归。
// 优先按 *llm.APIError.StatusCode 精确判定；对少数包装路径再用关键字兜底。
func isLLMInfraError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 0: // 网络不可达 / 空闲超时 / EOF，无 HTTP 状态
			return true
		case apiErr.StatusCode == 401, apiErr.StatusCode == 402,
			apiErr.StatusCode == 403, apiErr.StatusCode == 407,
			apiErr.StatusCode == 429:
			return true
		case apiErr.StatusCode >= 500:
			return true
		}
	}
	// 兜底：部分路径可能包了非 *APIError 的包装错误，按关键字识别。
	msg := strings.ToLower(err.Error())
	for _, k := range []string{
		"insufficient", "quota", "balance", "billing",
		"401", "402", "403", "407", "429",
		"rate limit", "too many requests",
		"server error", "bad gateway", "service unavailable", "gateway timeout",
		"i/o timeout", "context deadline exceeded", "no such host",
		"connection refused", "connection reset", "unexpected eof",
	} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}
