package llm

import (
	"errors"
	"net"
	"syscall"
)

// 重试矩阵（单源 · P4 T1.1）：LLM 请求失败的错误分类决策只允许在本文件定义，
// 三条请求路径（ChatStream / GenerateText / callToolNonStream）全部经
// apiErrorFromNetErr / apiErrorFromHTTP 构造 *APIError，禁止各路径自造分类。
//
// 矩阵定案（docs/future/p4-reliability-metrics-implementation-wbs.md §0.1）：
//
//	| 错误类                                    | Retryable | Hard | Hard 理由                |
//	|-------------------------------------------|-----------|------|--------------------------|
//	| 首字节超时（>10m）                        | true      | true | 服务端/链路硬不可用信号    |
//	| DNS NXDOMAIN / ECONNREFUSED（永久网络）   | false     | true | 域名/端口根本不可达       |
//	| 其余网络错误（EOF/reset）                 | true      | false| 瞬时性，重连可恢复        |
//	| 流式/非流式空闲超时（idle watchdog）      | true      | false| 仅观测，绝不驱动熔断      |
//	| HTTP >=5xx                                | true      | true | 服务端硬错误              |
//	| HTTP 429 / 408                            | true      | false| 限流/超时，软拒绝          |
//	| HTTP 其余 4xx                             | false     | false| 客户端错误，非供应商挂起  |
//	| tool_args_invalid（agent 层）             | true      | false| 协议层，非供应商故障      |
//
// 怎么重试（次数/退避/事件）由 agent 层协议决定，本文件只回答
// 「错误归哪类、可不可重试、要不要带 Retry-After、够不够硬（是否驱动熔断）」。
//
// Hard 口径（P0 熔断重设计）：只有「真故障」才置 true——连接拒绝 / DNS 不可达 /
// 首字节超时 / >=5xx。空闲超时等软信号一律 false，避免 thinking 模型长静默被误杀。

// isTimeoutErr 判断 err 是否为网络超时错误（首字节超时 / 读超时）。
// 供 apiErrorFromNetErr 分类与调用方日志分支共用，避免分类判定散落。
func isTimeoutErr(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// apiErrorFromNetErr 按重试矩阵把网络传输错误分类为 *APIError。
//
// 分类次序：超时（首字节 10m / 读超时）→ 可重试且 Hard=true（硬不可用信号）；
// DNS NXDOMAIN / ECONNREFUSED（域名无法解析 / 连接拒绝）→ 永久不可重试且 Hard=true；
// 其余（EOF / reset 等瞬时性）→ 可重试、非硬失败。
func apiErrorFromNetErr(err error) *APIError {
	if isTimeoutErr(err) {
		return &APIError{
			StatusCode: 0,
			Message:    "服务器响应超时（首字节超过 10m），请稍后重试",
			Retryable:  true,
			Hard:       true, // 首字节超时 = 服务端/链路硬不可用信号
		}
	}

	// 非超时网络错误：区分永久性（DNS NXDOMAIN / 连接拒绝）与瞬时性（EOF / reset）
	var dnsErr *net.DNSError
	var opErr *net.OpError
	if (errors.As(err, &dnsErr) && dnsErr.IsNotFound) ||
		(errors.As(err, &opErr) && errors.Is(opErr.Err, syscall.ECONNREFUSED)) {
		return &APIError{
			StatusCode: 0,
			Message:    "网络连接被拒绝或域名无法解析，请检查网络配置",
			Retryable:  false,
			Hard:       true, // 域名/端口根本不可达 = 硬失败
		}
	}

	return &APIError{
		StatusCode: 0,
		Message:    "网络连接失败，请检查网络后重试",
		Retryable:  true,
		// 瞬时性 EOF/reset：非硬失败，重连可恢复，不驱动熔断。
	}
}

// apiErrorFromHTTP 按重试矩阵把 HTTP 4xx/5xx 响应分类为 *APIError：
// 消息经 Provider.ParseError（nil 则 OpenAI 标准格式）解析；
// Retryable 由 statusRetryable 判定（429/408/5xx 可重试）；
// Hard 仅在 >=5xx 时置 true（服务端硬错误驱动熔断；429/408 与 4xx 为软拒绝/客户端错误，非硬失败）；
// Retry-After 头无条件解析（非可重试错误携带时无消费方，保持解析单源）。
func apiErrorFromHTTP(statusCode int, body []byte, parseError func([]byte) error, retryAfterHeader string) *APIError {
	msg := parseDefaultError(body).Error()
	if parseError != nil {
		msg = parseError(body).Error()
	}
	return &APIError{
		StatusCode: statusCode,
		Message:    msg,
		Retryable:  statusRetryable(statusCode),
		RetryAfter: parseRetryAfter(retryAfterHeader),
		Hard:       statusCode >= 500 && statusCode < 600, // 5xx = 服务端硬错误
	}
}
