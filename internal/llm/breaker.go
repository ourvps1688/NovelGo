package llm

import (
	"fmt"
	"sync"
	"time"
)

// ── 供应商熔断重设计（P0 · breaker.go）────────────────────────────────
//
// 背景 / 目标：原 provider_trip.go 把「近 10min LLM 流式空闲超时 ≥10 次」当作硬信号误熔断
// （thinking 模型长静默 / 代理缓冲被误杀）。本文件把「空闲超时」从熔断触发信号剥离，
// 熔断只认硬失败（连接拒绝 / DNS 不可达 / 首字节超时 / >=5xx），并改为按 provider 隔离的
// 三态（Closed/Open/Half-Open）自愈熔断器。冷却到期后自动半开探针，探针成功回到 Closed、
// 失败回到 Open。
//
// 设计取舍（详见 docs/future/llm-provider-breaker-idle-timeout-redesign.md）：
//   - 空闲超时（idle watchdog）只作观测（沿用 idleFires），绝不再驱动熔断；
//   - 三态自愈：Open 冷却到期 → 自动 Half-Open 允许一次探针；探针成败决定回 Closed 或回 Open；
//   - 按 provider 索引：单个供应商挂起不影响其它供应商；
//   - ProviderBreaker 全局单例，pipeline 与 chat 共用（P1 补齐 chat 接线，本文件只落地数据结构与能力）。
//
// 阈值初值（用户指定）：hardFailureWindow=2min、hardFailureTrip=5、breakerCooldown=30s。

const (
	// hardFailureWindow 是硬失败计数的滑动窗口：窗口内同 provider 硬失败次数达到
	// hardFailureTrip 即熔断（Open）。窗口外失败自然衰减，不累计。
	hardFailureWindow = 2 * time.Minute
	// hardFailureTrip 是熔断阈值：窗口内硬失败次数达到该值则 Open。
	hardFailureTrip = 5
	// breakerCooldown 是 Open 状态持续时长：到期后自动进入 Half-Open 允许一次探针。
	breakerCooldown = 30 * time.Second
	// breakerProbeSuccess 是 Half-Open 回到 Closed 所需的连续成功次数（探针通过标准）。
	breakerProbeSuccess = 1
)

// breakerState 表示熔断器三态。
type breakerState int

const (
	stateClosed   breakerState = iota // 正常放行
	stateOpen                         // 熔断中，拒绝派发
	stateHalfOpen                     // 冷却到期后的探针态，放行一次以验证恢复
)

// breaker 是单个 provider 的熔断器。
type breaker struct {
	mu             sync.Mutex
	state          breakerState
	hardFailures   []time.Time // 窗口内硬失败时刻（滑动）
	openAt         time.Time   // 进入 Open 的时刻（冷却起点）
	probeSuccesses int         // Half-Open 累计成功次数
}

// trim 裁剪窗口外的硬失败时刻（调用方持 b.mu）。
func (b *breaker) trim(now time.Time) {
	cutoff := now.Add(-hardFailureWindow)
	kept := b.hardFailures[:0]
	for _, t := range b.hardFailures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	b.hardFailures = kept
}

// providerBreakerManager 管理所有 provider 的熔断器，按 providerName 索引。
type providerBreakerManager struct {
	mu       sync.Mutex
	breakers map[string]*breaker
}

// ProviderBreaker 是全局单例，pipeline / chat 共用（chat 接线属 P1）。
// 生产代码经此单例驱动熔断；测试可直接构造零值 providerBreakerManager 隔离实例。
var ProviderBreaker = &providerBreakerManager{
	breakers: make(map[string]*breaker),
}

// get 返回（必要时创建）指定 provider 的熔断器（持 manager.mu 短暂）。
func (m *providerBreakerManager) get(providerName string) *breaker {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.breakers[providerName]
	if !ok {
		b = &breaker{}
		m.breakers[providerName] = b
	}
	return b
}

// RecordHardFailure 记录一次硬失败：滑动窗口计数；Closed 态下达阈值即 Open；
// Half-Open 态下探针失败立即回到 Open 并重置冷却；Open 态下保持并续接窗口。
//
// 硬失败口径（retry_matrix.go 置 Hard=true）：连接拒绝、DNS 不可达、首字节超时、>=5xx。
// 空闲超时（idle watchdog）绝不调用本方法——那是观测信号，不该熔断。
func (m *providerBreakerManager) RecordHardFailure(providerName string) {
	b := m.get(providerName)
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.trim(now)

	if b.state == stateHalfOpen {
		// 探针失败：立即回到 Open，重置冷却起点，清空探针计数。
		b.state = stateOpen
		b.openAt = now
		b.probeSuccesses = 0
		return
	}
	if b.state == stateOpen {
		// 已在熔断中，仅续接失败窗口（不重复置 openAt）。
		b.hardFailures = append(b.hardFailures, now)
		return
	}
	// Closed 态：累计硬失败，达阈值 → Open。
	b.hardFailures = append(b.hardFailures, now)
	if len(b.hardFailures) >= hardFailureTrip {
		b.state = stateOpen
		b.openAt = now
	}
}

// RecordSuccess 记录一次成功（正常流结束或 Half-Open 探针通过）：
// Half-Open 态累计成功达 breakerProbeSuccess → 回到 Closed、清空失败与探针计数；
// Closed 态无动作（保留历史失败计数以便窗口自然衰减）；Open 态不处理（成功只来自探针路径）。
func (m *providerBreakerManager) RecordSuccess(providerName string) {
	b := m.get(providerName)
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case stateHalfOpen:
		b.probeSuccesses++
		if b.probeSuccesses >= breakerProbeSuccess {
			b.state = stateClosed
			b.hardFailures = nil
			b.probeSuccesses = 0
		}
	case stateClosed:
		// 正常成功：无需动作；保留窗口内失败计数，待其自然衰减。
	default:
		// stateOpen：成功只可能来自 Half-Open 探针（由 AnyOpen 转换后产生），
		// Open 态直接进入的成功不会发生，忽略。
	}
}

// AnyOpen 返回当前处于 Open 状态的 provider 列表；并在 Open 冷却到期时自动将其转为
// Half-Open（允许下一次调用作为探针）。调用方据此决定是否跳派发——只要列表非空即熔断。
//
// 自愈语义：Open 态冷却到期 → Half-Open（不计入返回列表，放行探针）；探针成功
// （RecordSuccess）→ Closed，失败（RecordHardFailure）→ 回 Open 重新计时。
func (m *providerBreakerManager) AnyOpen() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var open []string
	now := time.Now()
	for name, b := range m.breakers {
		b.mu.Lock()
		// 状态机推进：Open 冷却到期 → Half-Open。
		if b.state == stateOpen && now.Sub(b.openAt) >= breakerCooldown {
			b.state = stateHalfOpen
			b.probeSuccesses = 0
		}
		if b.state == stateOpen {
			open = append(open, name)
		}
		b.mu.Unlock()
	}
	return open
}

// IsOpen 返回指定 provider 当前是否处于 Open（拦截）状态：若该 provider 的熔断器处于 Open
// 且冷却到期，则自动转为 Half-Open（放行一次探针）并返回 false；仍处于 Open（冷却中）返回 true；
// Closed / Half-Open / 无熔断器记录均返回 false。与 AnyOpen 共享同一自愈状态机推进逻辑，
// 但只针对单 provider 定向判断，供 LLM 调用入口（ChatStream / GenerateText）做调用前拦截——
// 这样 pipeline 与 chat 经同一 ProviderBreaker 单例共用同一熔断状态（P1 验收点：状态一致）。
func (m *providerBreakerManager) IsOpen(providerName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.breakers[providerName]
	if !ok {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	// 状态机推进：Open 冷却到期 → Half-Open。
	if b.state == stateOpen && now.Sub(b.openAt) >= breakerCooldown {
		b.state = stateHalfOpen
		b.probeSuccesses = 0
	}
	return b.state == stateOpen
}

// breakerBlockedProvider 是「指定 provider 当前是否被熔断拦截」的取数接缝
// （生产=ProviderBreaker.IsOpen，测试可替换为确定性驱动拦截）。判定逻辑单源在 breaker.go。
var breakerBlockedProvider = ProviderBreaker.IsOpen

// breakerOpenError 构造「供应商熔断拦截」错误（Kind=provider_breaker_open，Retryable=false）：
// 经 LLM 调用入口（ChatStream / GenerateText）快速失败——agent 层只对 Retryable 错误重试，
// 非可重试错误立即快速失败，避免 pipeline 已熔断、chat 仍在狂打该供应商的假阳性狂打。
// 该错误不喂入熔断器（不重复计数），只是被拦截调用方的出口。
func breakerOpenError(providerName string) error {
	return &APIError{
		StatusCode: 0,
		Message:    fmt.Sprintf("供应商（%s）疑似挂起，已熔断跳过本次调用，请稍后重试", providerName),
		Retryable:  false,
		Kind:       "provider_breaker_open",
	}
}

// BreakerSnapshot 是单个 provider 熔断器的只读快照（供测试 / 调试 / 未来 UI）。
type BreakerSnapshot struct {
	Provider string
	State    string // closed / open / half-open
	Failures int    // 窗口内硬失败次数
	OpenAt   time.Time
}

// Snapshot 返回所有 provider 的熔断器快照（按 provider 升序，便于稳定输出）。
func (m *providerBreakerManager) Snapshot() []BreakerSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.breakers))
	for name := range m.breakers {
		names = append(names, name)
	}
	// 简单升序排序，避免依赖 sort 包仅为此小功能。
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
		}
	}
	out := make([]BreakerSnapshot, 0, len(names))
	for _, name := range names {
		b := m.breakers[name]
		b.mu.Lock()
		st := "closed"
		switch b.state {
		case stateOpen:
			st = "open"
		case stateHalfOpen:
			st = "half-open"
		}
		out = append(out, BreakerSnapshot{
			Provider: name,
			State:    st,
			Failures: len(b.hardFailures),
			OpenAt:   b.openAt,
		})
		b.mu.Unlock()
	}
	return out
}
