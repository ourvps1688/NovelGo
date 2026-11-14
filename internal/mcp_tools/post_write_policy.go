package mcp_tools

// post_write_policy.go —— #119 P0：写后维护子 Agent 的生命周期与可见性单源。
//
// 2026-11-14 02:01 实测：正文已在 01:59:54 落盘并通过评分，主 chat 在 02:01:41
// 撞上 30 分钟硬上限；但 statesync / chapterci 由 debounce 派发时用了
// context.WithoutCancel(ctx) 且没有自己的超时，于是在主 ctx 死后继续跑了 6 分钟以上，
// 不断往会话写「正在思考」与工具结果、继续消耗 LLM 额度，而结果已无人收口。
//
// 这里只做两件事（不改派发链、不新增执行体、不引入自动修复）：
//   - 给 detached 链一个独立且有界的预算，主 ctx 结束后仍可收口但不会无限续命；
//   - 给请求打 Detached 标记，由 agent 层换独立事件前缀 + 关闭前端可见性。
//
// 纪律：纯本地、零上传、零自动修复；前台 run_subagent 与手动重检不走这里。

import (
	"context"
	"sync"
	"time"
)

// postWriteIdleBudget 与 internal/llm.thinkingIdleTimeout（9 分钟）同源。
// #121：旧值 8 分钟比 idle watchdog 还短，供应商静默时预算先到期、看门狗随后才触发，
// 03:17–03:18 出现 8 条 idle watchdog 与预算到期日志交错，后台检查必然半途而废。
// 预算必须大于单次空闲预算，让「一次 idle → 停止重试 → 收口」跑在同一个生命周期里。
const postWriteIdleBudget = 9 * time.Minute

// postWriteSubAgentTimeout 是写后维护子 Agent 的独立预算：
// 一次完整空闲预算 + 1 分钟收口余量（agent.maxRetryWindow=8min 会在 idle 后停止重试），
// 同时保证主轮结束后后台不会无限挂住。
const postWriteSubAgentTimeout = postWriteIdleBudget + time.Minute

// detachedPostWriteContext 派生「脱离主 ctx 取消、但自带超时」的写后维护上下文。
// 调用方必须 defer cancel()。
func detachedPostWriteContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), postWriteSubAgentTimeout)
}

// detachedPostWriteSubAgent 把写后维护的派发请求标记为后台，零改被调函数签名。
// 其他调用点（app/handler.go 的手动重检、pipeline、测试）不经过这里，行为逐字不变。
func detachedPostWriteSubAgent(run RunSubAgentFunc) RunSubAgentFunc {
	if run == nil {
		return nil
	}
	return func(ctx context.Context, req SubAgentRequest) (string, error) {
		req.Detached = true
		return run(ctx, req)
	}
}

// postWriteDebouncer 是 chapterci / statesync 两处写后维护去抖的共用单源（#121）。
//
// 旧实现每处各有一个 map[string]*time.Timer：新编辑来时 Stop() 旧 timer，
// 但旧 timer 一旦已经触发（Stop 返回 false），回调已经在跑或已排队，新 timer
// 照样会再派发一次 ⇒ 同章两个子 Agent 并发。03:16 实测 L1 已 checked 后 25 秒
// 又有一次 chapterci 派发失败，就是这条竞态。
//
// 这里保证三件事，不新增任何执行体、不改变派发时机与模型：
//   - 连续编辑按 delay 去抖，被覆盖的旧 timer 回调直接作废（seq 判定）；
//   - 同一 key 任意时刻只有一个 job 在飞；
//   - 在飞期间的新编辑只留最后一次，等前一个收口后再跑一次（合并而非丢弃）。
type postWriteDebouncer struct {
	mu      sync.Mutex
	delay   time.Duration
	next    uint64
	latest  map[string]uint64
	timers  map[string]*time.Timer
	running map[string]bool
	pending map[string]func()
}

func newPostWriteDebouncer(delay time.Duration) *postWriteDebouncer {
	return &postWriteDebouncer{
		delay:   delay,
		latest:  map[string]uint64{},
		timers:  map[string]*time.Timer{},
		running: map[string]bool{},
		pending: map[string]func(){},
	}
}

// Schedule 登记一次编辑；同 key 的待触发 timer 被覆盖，只有最后一次编辑会真正派发。
func (d *postWriteDebouncer) Schedule(key string, job func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	seq := d.next
	d.latest[key] = seq
	if t, ok := d.timers[key]; ok {
		t.Stop()
	}
	d.timers[key] = time.AfterFunc(d.delay, func() { d.fire(key, seq, job) })
}

// fire 是到点回调：作废过期 timer、把在飞期间的后来者合并为一次重跑。
func (d *postWriteDebouncer) fire(key string, seq uint64, job func()) {
	d.mu.Lock()
	if d.latest[key] != seq {
		d.mu.Unlock()
		return // 已被后续编辑覆盖，由它的 timer 负责跑最新一次
	}
	delete(d.timers, key)
	if d.running[key] {
		d.pending[key] = job // 只保留最后一次：L1 读的是落盘后的当前档案
		d.mu.Unlock()
		return
	}
	d.running[key] = true
	d.mu.Unlock()
	d.runQueued(key, job)
}

func (d *postWriteDebouncer) runQueued(key string, first func()) {
	defer func() {
		d.mu.Lock()
		delete(d.running, key)
		delete(d.pending, key)
		d.mu.Unlock()
	}()
	job := first
	for {
		job()
		d.mu.Lock()
		next, ok := d.pending[key]
		delete(d.pending, key)
		d.mu.Unlock()
		if !ok {
			return
		}
		job = next
	}
}
