package llm

import (
	"testing"
	"time"
)

// 这些测试直接构造 providerBreakerManager 隔离实例（不触碰全局 ProviderBreaker 单例），
// 验证 P0 熔断重设计的三态迁移 / 阈值 / 按 provider 隔离 / 自愈 / 空闲不触发，
// 以及 P1 新增的 IsOpen 定向拦截（与 AnyOpen 共享自愈状态机）。

// TestBreakerClosedByDefault 新熔断器默认 Closed，未达阈值不熔断。
func TestBreakerClosedByDefault(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("new breaker should be closed, got open=%v", open)
	}
	// 未达阈值（4 次 < 5）不熔断。
	for i := 0; i < hardFailureTrip-1; i++ {
		m.RecordHardFailure("deepseek")
	}
	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("below threshold should not trip, got open=%v", open)
	}
	// RecordSuccess 在 Closed 态无副作用。
	m.RecordSuccess("deepseek")
	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("success in closed state must not trip, got open=%v", open)
	}
}

// TestBreakerTripsAfterThreshold 窗口内硬失败达阈值即 Open。
func TestBreakerTripsAfterThreshold(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	for i := 0; i < hardFailureTrip; i++ {
		m.RecordHardFailure("deepseek")
	}
	open := m.AnyOpen()
	if len(open) != 1 || open[0] != "deepseek" {
		t.Fatalf("expected [deepseek] open, got %v", open)
	}
	// 窗口外失败自然衰减：把旧失败推到窗口外，仅留 1 次，应回到 Closed。
	b := m.breakers["deepseek"]
	b.mu.Lock()
	old := time.Now().Add(-hardFailureWindow - time.Second)
	b.hardFailures = []time.Time{old, time.Now()}
	b.state = stateClosed
	b.mu.Unlock()
	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("only 1 in-window failure should be closed, got %v", open)
	}
}

// TestBreakerPerProviderIsolation 单 provider 熔断不影响其它 provider。
func TestBreakerPerProviderIsolation(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	for i := 0; i < hardFailureTrip; i++ {
		m.RecordHardFailure("deepseek")
	}
	open := m.AnyOpen()
	if len(open) != 1 || open[0] != "deepseek" {
		t.Fatalf("only deepseek should be open, got %v", open)
	}
	// 另一个 provider 正常成功，仍 Closed。
	m.RecordSuccess("openai")
	if open := m.AnyOpen(); len(open) != 1 || open[0] != "deepseek" {
		t.Fatalf("isolation broken, got %v", open)
	}
}

// TestBreakerSelfHeal 冷却到期自动 Half-Open，探针成功回到 Closed（自愈）。
func TestBreakerSelfHeal(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	for i := 0; i < hardFailureTrip; i++ {
		m.RecordHardFailure("deepseek")
	}
	if open := m.AnyOpen(); len(open) != 1 {
		t.Fatalf("should be open right after trip, got %v", open)
	}

	// 模拟冷却到期：把 openAt 推到 30s+ 前。
	b := m.breakers["deepseek"]
	b.mu.Lock()
	b.openAt = time.Now().Add(-breakerCooldown - time.Second)
	b.mu.Unlock()

	// 冷却到期后 AnyOpen 应自动转到 Half-Open，且不再计入 Open 列表（放行探针）。
	open := m.AnyOpen()
	if len(open) != 0 {
		t.Fatalf("cooldown-expired breaker should be half-open (not in open list), got %v", open)
	}
	if got := m.Snapshot(); got[0].State != "half-open" {
		t.Fatalf("expected half-open, got %s", got[0].State)
	}

	// 探针成功 → Closed。
	m.RecordSuccess("deepseek")
	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("probe success should close breaker, got %v", open)
	}
	if got := m.Snapshot(); got[0].State != "closed" {
		t.Fatalf("expected closed after probe, got %s", got[0].State)
	}
}

// TestBreakerHalfOpenProbeFailureReopens 探针失败立即回到 Open 并重置冷却。
func TestBreakerHalfOpenProbeFailureReopens(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	for i := 0; i < hardFailureTrip; i++ {
		m.RecordHardFailure("deepseek")
	}
	b := m.breakers["deepseek"]
	b.mu.Lock()
	b.openAt = time.Now().Add(-breakerCooldown - time.Second)
	b.mu.Unlock()

	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("should be half-open after cooldown, got %v", open)
	}
	// 探针（放行的一次调用）硬失败 → 立即回 Open。
	m.RecordHardFailure("deepseek")
	if open := m.AnyOpen(); len(open) != 1 || open[0] != "deepseek" {
		t.Fatalf("probe failure should reopen, got %v", open)
	}
}

// TestBreakerIdleDoesNotTrip 空闲超时等非硬失败信号绝不触发熔断：
// 即使反复 RecordSuccess（模拟空闲超时只观测、不喂硬失败），也不会 Open。
func TestBreakerIdleDoesNotTrip(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	// 模拟 100 次空闲超时场景：只调 RecordSuccess（或完全不喂硬失败），不得熔断。
	for i := 0; i < 100; i++ {
		m.RecordSuccess("deepseek")
	}
	if open := m.AnyOpen(); len(open) != 0 {
		t.Fatalf("idle-only signal must never trip, got %v", open)
	}
}

// --- P1：IsOpen 定向拦截（供 LLM 调用入口 ChatStream / GenerateText）---

// TestBreaker_IsOpen_UnknownProvider 未知 provider 不应被判为 Open。
func TestBreaker_IsOpen_UnknownProvider(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	if m.IsOpen("nope") {
		t.Errorf("unknown provider should not be open")
	}
}

// TestBreaker_IsOpen_ClosedBelowThreshold 窗口内硬失败未达阈值 → Closed → IsOpen=false。
func TestBreaker_IsOpen_ClosedBelowThreshold(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	m.RecordHardFailure("p")
	m.RecordHardFailure("p") // 2 < hardFailureTrip(5)，仍 Closed
	if m.IsOpen("p") {
		t.Errorf("expected IsOpen=false for closed breaker (below trip threshold)")
	}
}

// TestBreaker_IsOpen_TripViaFailures 窗口内硬失败达阈值 → Open → IsOpen=true。
func TestBreaker_IsOpen_TripViaFailures(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	for i := 0; i < hardFailureTrip; i++ {
		m.RecordHardFailure("p")
	}
	if !m.IsOpen("p") {
		t.Errorf("expected IsOpen=true after %d hard failures", hardFailureTrip)
	}
}

// TestBreaker_IsOpen_OpenWithinCooldown Open 态冷却期内 → IsOpen 仍返回 true（拦截）。
func TestBreaker_IsOpen_OpenWithinCooldown(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	b := &breaker{state: stateOpen, openAt: time.Now()}
	m.breakers["p"] = b
	if !m.IsOpen("p") {
		t.Errorf("expected IsOpen=true within cooldown")
	}
}

// TestBreaker_IsOpen_CooldownExpiryAutoHalfOpen Open 态冷却到期 → 自动转 Half-Open →
// IsOpen 返回 false（放行一次探针），且与 AnyOpen 共享同一自愈状态机推进逻辑。
func TestBreaker_IsOpen_CooldownExpiryAutoHalfOpen(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	b := &breaker{state: stateOpen, openAt: time.Now().Add(-time.Hour)} // 冷却早已到期
	m.breakers["p"] = b
	if m.IsOpen("p") {
		t.Errorf("expected IsOpen=false after cooldown (auto half-open)")
	}
	if b.state != stateHalfOpen {
		t.Errorf("expected state half-open after cooldown, got %v", b.state)
	}
}

// TestBreaker_IsOpen_ConsistentWithAnyOpen 同一 provider 经 IsOpen 与 AnyOpen 观测到的
// 熔断状态一致（P1 验收点：chat 与 pipeline 共用同一 ProviderBreaker）。
func TestBreaker_IsOpen_ConsistentWithAnyOpen(t *testing.T) {
	m := &providerBreakerManager{breakers: make(map[string]*breaker)}
	// 触发熔断
	for i := 0; i < hardFailureTrip; i++ {
		m.RecordHardFailure("shared")
	}
	// IsOpen 与 AnyOpen 对同一个 Open provider 应同时判定为熔断。
	if !m.IsOpen("shared") {
		t.Errorf("IsOpen should report shared as open")
	}
	open := m.AnyOpen()
	found := false
	for _, name := range open {
		if name == "shared" {
			found = true
		}
	}
	if !found {
		t.Errorf("AnyOpen should include shared, got %v", open)
	}
}
