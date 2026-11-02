package agent

import (
	"time"
)

// P2 可恢复错误重试配置
const (
	maxRetries = 8 // 最大重试次数

	// maxRetryAfter 限制服务商通过 Retry-After header 指定的等待时间上限。
	// 避免服务商异常或恶意指定过长等待导致对话长时间挂起。
	maxRetryAfter = 120 * time.Second
)

// idleRetryCap 是单轮内「连续空闲超时（流式响应静默超过空闲预算，供应商半死连接）」
// 的最大可重试次数：超过即判不可重试，直接失败。背景：供应商「连得通但不吐数据」时会
// 先回几个字节再静默 9 分钟，被空闲看门狗砍掉；因曾收到字节，idleRetryable 仍判可重试，
// 若放任会 8×9 分钟空转（单轮就可能超过检测 60 分钟硬超时）。连续命中达到本上限即放弃，
// 使死连接快速失败（与下游巡检/复验的优雅失败兜底联动，前端不再永久「运行中」）。
// 取到 2：允许一次完整 9 分钟空闲（思考模型合法长静默）后给第二次机会，仅当第二次仍静默才放弃。
var idleRetryCap = 2

// backoffBase 退避基数（已含抖动，不再额外加 jitter）。
// 8 次重试对应 2.347s / 4.618s / 8.283s / 16.742s / 31.189s / 48.631s / 60.473s / 60.218s。
// 总等待时间约 232.5s。
// 实测效果好于随机延迟退避
var backoffBase = [...]time.Duration{
	2347 * time.Millisecond,
	4618 * time.Millisecond,
	8283 * time.Millisecond,
	16742 * time.Millisecond,
	31189 * time.Millisecond,
	48631 * time.Millisecond,
	60473 * time.Millisecond,
	60218 * time.Millisecond,
}

// computeBackoff 计算重试退避时间。
//
// attempt: 第几次重试（1-indexed，1..maxRetries）
// retryAfter: 服务商通过 Retry-After header 指定的重试等待时间，>0 时优先遵循（上限 maxRetryAfter）
//
// 返回退避时间。
func computeBackoff(attempt int, retryAfter time.Duration) time.Duration {
	// 服务商指定优先遵循
	if retryAfter > 0 {
		if retryAfter > maxRetryAfter {
			return maxRetryAfter
		}
		return retryAfter
	}

	// 越界保护
	if attempt < 1 || attempt > len(backoffBase) {
		return 1 * time.Second
	}

	return backoffBase[attempt-1]
}
