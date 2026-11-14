package llm

// #122 契约：idle watchdog 必须在请求读完后退出，同时保留对真实流静默的检测能力。
// 旧实现只有 fired 一个退出条件，正常完成的请求会在 9 分钟后补一条假告警、
// 误记一次 recordIdleFire，并泄漏 goroutine（11-14 14 点实测 43 条）。

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturingHandler struct {
	mu      sync.Mutex
	records []string
}

func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Message)
	return nil
}
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }
func (h *capturingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

func TestIdleWatchdogStopAfterRequestDone(t *testing.T) {
	h := &capturingHandler{}
	var once sync.Once
	cancelled := make(chan struct{})
	cancel := func() { once.Do(func() { close(cancelled) }) }

	wd := newIdleWatchdogWithPoll(cancel, slog.New(h), 20*time.Millisecond, 5*time.Millisecond)
	wd.stop() // 模拟：parseSSE / io.ReadAll 正常读完后的 defer
	wd.stop() // 幂等，重复调用不得 panic

	time.Sleep(140 * time.Millisecond) // 远超 timeout，旧实现必然误报
	assert.Equal(t, 0, h.count(), "请求已正常收口不得补发空闲超时告警")
	assert.False(t, wd.fired.Load())
	assert.True(t, wd.stopped.Load())
	select {
	case <-cancelled:
		t.Fatal("请求已收口不得再 cancel")
	default:
	}
}

func TestIdleWatchdogStillFiresOnRealStall(t *testing.T) {
	h := &capturingHandler{}
	var once sync.Once
	cancelled := make(chan struct{})
	cancel := func() { once.Do(func() { close(cancelled) }) }

	// 请求未结束（不调用 stop）：真·流静默仍必须按预算触发并取消请求。
	wd := newIdleWatchdogWithPoll(cancel, slog.New(h), 20*time.Millisecond, 5*time.Millisecond)
	defer wd.stop()
	deadline := time.Now().Add(2 * time.Second)
	for !wd.fired.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	assert.True(t, wd.fired.Load(), "真实静默仍须触发 watchdog")
	assert.Equal(t, 1, h.count(), "真实静默须恰好告警一次")
	select {
	case <-cancelled:
	default:
		t.Fatal("真实静默须取消底层请求")
	}
}

func TestIdleWatchdogStopWiring(t *testing.T) {
	stream, err := os.ReadFile("stream.go")
	require.NoError(t, err)
	gen, err := os.ReadFile("generate.go")
	require.NoError(t, err)
	assert.Contains(t, string(stream), "defer wd.stop()", "ChatStream 必须在读完后收口")
	assert.Contains(t, string(gen), "defer wd.stop()", "GenerateText 必须同口径收口")
	// #114 P2：观测信号与原告警文案不得被改写。
	assert.Contains(t, string(stream), "recordIdleFire()")
	assert.Contains(t, string(stream), "llm stream idle watchdog fired, canceling request")
	assert.Contains(t, string(stream), "idleTimeoutFor(p, model)", "空闲预算分档不变")
}
