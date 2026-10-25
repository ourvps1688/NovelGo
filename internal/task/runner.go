package task

// Runner 是任务队列的消费侧（R3 §4.4）：按 kind 分发 Handler，领取即起 goroutine
// 执行（并行度与裸 goroutine 时代等价，队列提供持久化/续跑/观察而非并发限制）；
// 执行期间心跳续租，租约过期即视为执行体消亡、任务可被重领续跑。
// Handler 必须幂等重入（崩溃续跑=同一任务可能被执行多次）。

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Handler 执行一条任务。返回 error=本次尝试失败（按 MaxAttempts 重试/放弃）。
type Handler func(ctx context.Context, t *Task) error

// Runner 装配与生命周期。
type Runner struct {
	store    *Store
	logger   *slog.Logger
	handlers map[string]Handler

	leaseDur     time.Duration
	pollInterval time.Duration
	maxConcur    int

	mu      sync.Mutex
	active  map[int64]context.CancelFunc // 运行中任务的取消口（Stop 用）
	wg      sync.WaitGroup
	stopCh  chan struct{}
	stopped bool
}

// RunnerOption 定制 Runner（缺省见 Default* 常量）。
type RunnerOption func(*Runner)

// WithLeaseDuration 覆盖租约时长。
func WithLeaseDuration(d time.Duration) RunnerOption { return func(r *Runner) { r.leaseDur = d } }

// WithPollInterval 覆盖空转轮询间隔。
func WithPollInterval(d time.Duration) RunnerOption {
	return func(r *Runner) { r.pollInterval = d }
}

// WithMaxConcurrent 覆盖并发上限（<=0=不限，与裸 goroutine 时代等价）。
func WithMaxConcurrent(n int) RunnerOption { return func(r *Runner) { r.maxConcur = n } }

// NewRunner 装配 Runner。
func NewRunner(store *Store, logger *slog.Logger, opts ...RunnerOption) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Runner{
		store:        store,
		logger:       logger,
		handlers:     map[string]Handler{},
		leaseDur:     DefaultLeaseDuration,
		pollInterval: DefaultPollInterval,
		active:       map[int64]context.CancelFunc{},
		stopCh:       make(chan struct{}),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Register 注册 kind 的执行处理器（同 kind 重复注册覆盖，最后者生效）。
func (r *Runner) Register(kind string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[kind] = h
}

// Start 启动消费循环（非阻塞；ctx 取消或 Stop 后退出）。
func (r *Runner) Start(ctx context.Context) {
	r.wg.Add(1)
	go r.loop(ctx)
}

// Stop 停止消费并等待运行中任务收口（运行中任务收到取消信号后自行退出，
// 其租约会随心跳停止而过期，重启后被重领续跑）。
func (r *Runner) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	close(r.stopCh)
	for _, cancel := range r.active {
		cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Runner) loop(ctx context.Context) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		default:
		}
		if !r.hasCapacity() {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-time.After(r.pollInterval):
			}
			continue
		}
		t, err := r.store.Lease(ctx, r.leaseDur)
		if err != nil {
			r.logger.Warn("任务队列：领取任务失败", "err", err)
		}
		if t == nil {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-time.After(r.pollInterval):
			}
			continue
		}
		r.spawn(ctx, t)
	}
}

func (r *Runner) hasCapacity() bool {
	if r.maxConcur <= 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active) < r.maxConcur
}

// spawn 领取即起 goroutine 执行（并发与裸 goroutine 等价）。
func (r *Runner) spawn(ctx context.Context, t *Task) {
	handler := r.handlerOf(t.Kind)
	if handler == nil {
		r.logger.Error("任务队列：未注册的执行体种类（立即置 dead，不重试）", "kind", t.Kind, "task_id", t.ID)
		if _, err := r.store.Kill(context.WithoutCancel(ctx), t.ID, t.Attempt, "unregistered kind: "+t.Kind); err != nil {
			r.logger.Warn("任务队列：置 dead 失败", "task_id", t.ID, "err", err)
		}
		return
	}
	tctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		cancel()
		return
	}
	r.active[t.ID] = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	go r.execute(tctx, t, handler, cancel)
}

func (r *Runner) execute(ctx context.Context, t *Task, handler Handler, cancel context.CancelFunc) {
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.active, t.ID)
		r.mu.Unlock()
		r.wg.Done()
	}()
	// 心跳续租：执行体存活的唯一证明；续租失败（任务被回收）时取消执行，
	// 避免同一任务双执行体并行写（幂等兜底之外的并发防线）。
	hbStop := make(chan struct{})
	defer close(hbStop)
	go r.heartbeatLoop(ctx, t, hbStop)

	err := handler(ctx, t)
	// 终态落库用 WithoutCancel：handler 被取消（Stop/心跳回收）后状态仍要落盘，
	// 否则任务滞留 running 只能等租约过期。
	wctx := context.WithoutCancel(ctx)
	if err != nil {
		r.logger.Warn("任务队列：执行失败", "task_id", t.ID, "kind", t.Kind, "attempt", t.Attempt, "err", err)
		if ok, ferr := r.store.Fail(wctx, t.ID, t.Attempt, err.Error()); ferr != nil {
			r.logger.Warn("任务队列：失败状态落库失败", "task_id", t.ID, "err", ferr)
		} else if !ok {
			r.logger.Warn("任务队列：任务已被重领，放弃失败落库（防双跑）", "task_id", t.ID, "attempt", t.Attempt)
		}
		return
	}
	if ok, cerr := r.store.Complete(wctx, t.ID, t.Attempt); cerr != nil {
		r.logger.Warn("任务队列：完成状态落库失败", "task_id", t.ID, "err", cerr)
	} else if !ok {
		r.logger.Warn("任务队列：任务已被重领，放弃完成落库（防覆写）", "task_id", t.ID, "attempt", t.Attempt)
	}
}

// heartbeatLoop 周期续租；续租失败（任务已被重领/回收）→ 取消执行上下文。
func (r *Runner) heartbeatLoop(ctx context.Context, t *Task, stop <-chan struct{}) {
	interval := r.leaseDur / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		ok, err := r.store.Heartbeat(ctx, t.ID, t.Attempt, r.leaseDur)
		if err != nil {
			r.logger.Debug("任务队列：心跳续租失败（下个周期重试）", "task_id", t.ID, "err", err)
			continue
		}
		if !ok {
			r.logger.Warn("任务队列：租约被回收（取消本次执行，防止双执行体）", "task_id", t.ID)
			r.mu.Lock()
			if cancel, exists := r.active[t.ID]; exists {
				cancel()
			}
			r.mu.Unlock()
			return
		}
	}
}

func (r *Runner) handlerOf(kind string) Handler {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handlers[kind]
}
