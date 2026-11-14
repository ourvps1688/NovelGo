package mcp_tools

// #121 契约：写后维护独立预算必须覆盖 idle watchdog；两处去抖必须串行化，
// 且纯本地、零自动修复、零新增执行体、零上传。

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostWriteBudgetCoversIdleWatchdog(t *testing.T) {
	// internal/llm.thinkingIdleTimeout = 9min；旧值 8min 比它短，
	// 预算先到期、watchdog 后触发，后台检查必然半途而废。
	assert.Equal(t, 9*time.Minute, postWriteIdleBudget)
	assert.Equal(t, 10*time.Minute, postWriteSubAgentTimeout)
	assert.Greater(t, postWriteSubAgentTimeout, postWriteIdleBudget)

	parent, cancel := context.WithCancel(context.Background())
	cancel()
	bg, cancelBG := detachedPostWriteContext(parent)
	defer cancelBG()
	require.NoError(t, bg.Err(), "主 ctx 取消后写后维护仍应能收口")
	deadline, ok := bg.Deadline()
	require.True(t, ok)
	assert.InDelta(t, postWriteSubAgentTimeout.Seconds(), time.Until(deadline).Seconds(), 2)

	src, err := os.ReadFile("post_write_policy.go")
	require.NoError(t, err)
	assert.Contains(t, string(src), "postWriteIdleBudget")
	assert.NotContains(t, string(src), "8 * time.Minute", "旧 8 分钟预算不得回潮")
}

func TestPostWriteDebouncerDropsStaleTimer(t *testing.T) {
	d := newPostWriteDebouncer(time.Hour)
	d.mu.Lock()
	d.latest["k"] = 2 // 已被后续编辑覆盖
	d.mu.Unlock()
	ran := false
	d.fire("k", 1, func() { ran = true })
	assert.False(t, ran, "旧 timer 已触发但被覆盖时，回调必须作废")
	assert.False(t, d.running["k"])
}

func TestPostWriteDebouncerSerializesAndCoalesces(t *testing.T) {
	d := newPostWriteDebouncer(time.Hour)
	d.mu.Lock()
	d.latest["27:2910"] = 1
	d.mu.Unlock()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondRan := make(chan struct{})
	var mu sync.Mutex
	var order []string

	first := func() {
		mu.Lock()
		order = append(order, "first-start")
		mu.Unlock()
		close(firstStarted)
		<-releaseFirst
		mu.Lock()
		order = append(order, "first-end")
		mu.Unlock()
	}
	second := func() {
		mu.Lock()
		order = append(order, "second-start")
		mu.Unlock()
		close(secondRan)
	}

	go d.fire("27:2910", 1, first)
	<-firstStarted

	// 第一次 L1 在飞时同章又改了一稿：只合并，绝不能并发派发第二个子 Agent。
	d.mu.Lock()
	d.latest["27:2910"] = 2
	d.mu.Unlock()
	d.fire("27:2910", 2, second)
	select {
	case <-secondRan:
		t.Fatal("同章第二个 job 不得与第一个并发运行")
	default:
	}

	close(releaseFirst)
	select {
	case <-secondRan:
	case <-time.After(2 * time.Second):
		t.Fatal("在飞结束后必须补跑最后一次编辑")
	}
	mu.Lock()
	assert.Equal(t, []string{"first-start", "first-end", "second-start"}, order)
	mu.Unlock()
	assert.False(t, d.running["27:2910"])
}

func TestPostWriteDebouncerScheduleKeepsLastEdit(t *testing.T) {
	d := newPostWriteDebouncer(20 * time.Millisecond)
	var mu sync.Mutex
	var got []int
	done := make(chan struct{})
	d.Schedule("k", func() {
		mu.Lock()
		got = append(got, 1)
		mu.Unlock()
	})
	d.Schedule("k", func() {
		mu.Lock()
		got = append(got, 2)
		mu.Unlock()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("最后一次编辑的 timer 必须触发")
	}
	mu.Lock()
	assert.Equal(t, []int{2}, got, "连续编辑只应保留最后一次")
	mu.Unlock()
}

func TestPostWriteDebouncerWiring(t *testing.T) {
	ci, err := os.ReadFile("chapter_ci.go")
	require.NoError(t, err)
	rw, err := os.ReadFile("rw_tools.go")
	require.NoError(t, err)
	assert.Contains(t, string(ci), "chapterCIDebouncer.Schedule(key,")
	assert.Contains(t, string(rw), "stateSyncDebouncer.Schedule(key,")
	assert.NotContains(t, string(ci), "chapterCITimers", "旧裸 timer map 不得回潮")
	assert.NotContains(t, string(rw), "stateSyncTimers")
	assert.NotContains(t, string(ci), "time.AfterFunc(")
	assert.NotContains(t, string(rw), "time.AfterFunc(")
	// 独立预算仍在两处 job 内派生，主 ctx 死后不会无限续命。
	assert.Contains(t, string(ci), "detachedPostWriteContext(ctx)")
	assert.Contains(t, string(rw), "detachedPostWriteContext(ctx)")

	policy, err := os.ReadFile("post_write_policy.go")
	require.NoError(t, err)
	for _, banned := range []string{
		"StartIssueFixTickets", "StartPipeline", "autoFix", "Upload", "http.Post",
	} {
		assert.NotContains(t, string(policy), banned, "去抖单源不得含自动修复/执行体/上传")
	}
}
