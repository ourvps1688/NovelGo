package pipeline

// task_dispatch.go 是 R3b「执行体迁持久任务队列」的派发口
// （pipeline-core-redesign-proposal.md §4.4）：流水线执行体（体检/修复/复验/终检/
// 蓝图/S0 对齐/全量对齐/巡检）从裸 goroutine 迁到 internal/task 队列——
// 幂等消费（同 kind+run 去重）、崩溃续跑（租约过期重领）、可观察（task 表）。
//
// 外部语义不变（R3b 边界；Interrupted/Dispatching 两个补丁布尔后于 R3c 退役，
// 执行体存活判据改为租约语义，见本文件 hasActiveTask）：
//   - 队列不可用（tasks 表缺失 / Runner 未启动）→ 降级走 legacy 裸 goroutine，
//     行为与迁移前逐字一致；
//   - 跨进程崩溃续跑仍走 RecoverInterruptedRuns 的人工恢复链路（启动清扫
//     AbandonActive 把残留活跃任务置 dead，恢复时重派新任务）；
//   - dedup=kind:run_id 活跃去重：同执行体同 run 在排队/执行中时重复派发被合并
//     （幂等消费；此前裸 goroutine 会双跑）。

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/novelgo/novelgo/internal/task"
)

// 执行体种类（task.kind，Handler 注册键）。
const (
	taskKindAudit      = "audit"       // 签约体检（S4/S4b/蓝图体检，含 enterAudit/enterBlueprintAudit）
	taskKindFix        = "fix"         // S5 检测结果路由+工单沉淀（R5-2 自动修复摘除后）
	taskKindIssueFix   = "issue_fix"   // 发现项工单「去修复」执行体（#240 R5-2 工单接执行）
	taskKindFinalAudit = "final_audit" // S6 完本终检
	taskKindBlueprint  = "blueprint"   // S2 蓝图生成
	taskKindS0Align    = "s0_align"    // S0 导入对齐（建卷+statesync）
	taskKindFullAlign  = "full_align"  // 全量对齐（建卷+statesync+全本体检）
	taskKindPatrol     = "patrol"      // 一致性巡检（张力报告，只读）
	taskKindDetect     = "detect"      // 检测任务（卷体检/书体检，R5-3 T1.2 检测-only 皮；巡检·结构复用 taskKindPatrol）
)

// taskPayload 是执行参数（task.payload JSON；RunID/NovelID 同步冗余到 task 行供查询）。
type taskPayload struct {
	RunID   string `json:"run_id"`
	NovelID int64  `json:"novel_id"`
}

// StartTaskQueue 清理残留活跃任务并启动队列消费。应用启动时调用（幂等，
// OnStartup / RetryStartup 多次调用安全）。清扫在消费前：残留任务属于已退出进程，
// 跨进程恢复仍走 RecoverInterruptedRuns 人工链路（外部恢复语义不变）。
func (s *Service) StartTaskQueue() {
	if s.taskStore == nil || s.taskRunner == nil {
		return
	}
	if n, err := s.taskStore.AbandonActive(s.appCtx,
		"interrupted: 进程退出中断，任务由恢复流程重派"); err != nil {
		s.logger.Warn("任务队列：清理残留活跃任务失败（非阻断）", "err", err)
	} else if n > 0 {
		s.logger.Info("任务队列：已放弃进程退出残留的活跃任务（等待恢复重派）", "count", n)
	}
	s.taskRunner.Start(s.appCtx)
}

// StopTaskQueue 停止消费并等待收口。测试用；应用关停不调用（执行体随进程退出
// 消亡即为迁移前语义，且执行体内部用 appCtx 不响应取消，等待会拖死关停）。
func (s *Service) StopTaskQueue() {
	if s.taskRunner != nil {
		s.taskRunner.Stop()
	}
}

// hasActiveTask 是执行体存活的租约语义判据（task 表活跃任务，R3c）：
// RecoverInterruptedRuns / redispatchIfNeeded 用它替代 GateContext.Dispatching
// 补丁布尔。队列不可用（表缺失/读失败）时按「无活跃」处理——两条链路的降级
// 行为与布尔时代一致（Recover 照常暂停僵尸；resume 照常重派，dispatchExecutor
// 的活跃去重兜底防双执行体）。
func (s *Service) hasActiveTask(ctx context.Context, runID string) bool {
	if s.taskStore == nil {
		return false
	}
	active, err := s.taskStore.HasActiveForRun(ctx, runID)
	if err != nil {
		s.logger.Debug("任务队列：活跃任务查询失败（按无活跃处理）", "run_id", runID, "err", err)
		return false
	}
	return active
}

// dispatchExecutor 把执行体派发进持久任务队列（dedup=kind:run_id 活跃去重）。
// 队列不可用时降级调 legacy（迁移前的裸 goroutine 行为，逐字保留）。
func (s *Service) dispatchExecutor(kind, runID string, novelID int64, legacy func()) {
	if s.taskStore == nil || s.taskRunner == nil || !s.taskRunner.Started() {
		legacy()
		return
	}
	payload, merr := json.Marshal(taskPayload{RunID: runID, NovelID: novelID})
	if merr != nil {
		legacy()
		return
	}
	_, existing, err := s.taskStore.Enqueue(s.appCtx, task.Task{
		Kind:     kind,
		DedupKey: fmt.Sprintf("%s:%s", kind, runID),
		Payload:  string(payload),
		RunID:    runID,
		NovelID:  novelID,
	})
	if err != nil {
		s.logger.Warn("任务队列：入队失败，降级为直接派发", "kind", kind, "run_id", runID, "err", err)
		legacy()
		return
	}
	if existing {
		s.logger.Info("任务队列：同键执行体已在排队/执行，合并跳过", "kind", kind, "run_id", runID)
	}
}

// registerTaskHandlers 把执行体注册到队列消费侧。Handler 必须幂等重入
// （executeAudit/executeFix 等均已幂等重入设计）。执行体内部沿用 appCtx
// （与迁移前一致：执行体不随请求 ctx 消亡）。
func (s *Service) registerTaskHandlers() {
	if s.taskRunner == nil {
		return
	}
	// (runID, novelID) 型执行体。
	s.taskRunner.Register(taskKindAudit, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.executeAudit(runID, novelID)
	}))
	s.taskRunner.Register(taskKindFix, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.executeFix(runID, novelID)
	}))
	s.taskRunner.Register(taskKindIssueFix, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.runIssueFixTickets(runID, novelID)
	}))
	s.taskRunner.Register(taskKindFinalAudit, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.executeFinalAudit(runID, novelID)
	}))
	s.taskRunner.Register(taskKindBlueprint, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.executeBlueprint(runID, novelID)
	}))
	s.taskRunner.Register(taskKindPatrol, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.runConsistencyPatrol(s.appCtx, runID, novelID)
	}))
	s.taskRunner.Register(taskKindDetect, s.runIDNovelIDHandler(func(runID string, novelID int64) {
		s.executeDetectAudit(runID, novelID)
	}))
	// (*PipelineRun) 型执行体：从 DB 重载 run（持久化状态为准）。
	s.taskRunner.Register(taskKindS0Align, s.runObjectHandler(func(run *PipelineRun) {
		s.executeS0Align(s.appCtx, run)
	}))
	s.taskRunner.Register(taskKindFullAlign, s.runObjectHandler(func(run *PipelineRun) {
		s.runFullAlign(s.appCtx, run)
	}))
}

// runIDNovelIDHandler 把 (runID, novelID) 型执行体包成队列 Handler。
func (s *Service) runIDNovelIDHandler(fn func(runID string, novelID int64)) task.Handler {
	return func(_ context.Context, t *task.Task) error {
		p, err := decodeTaskPayload(t.Payload)
		if err != nil {
			return err
		}
		fn(p.RunID, p.NovelID)
		return nil
	}
}

// runObjectHandler 把 (*PipelineRun) 型执行体包成队列 Handler：run 从 DB 重载，
// 加载失败按基础设施失败返回（队列按 attempt 重试/放弃）。
func (s *Service) runObjectHandler(fn func(run *PipelineRun)) task.Handler {
	return func(_ context.Context, t *task.Task) error {
		p, err := decodeTaskPayload(t.Payload)
		if err != nil {
			return err
		}
		run, gerr := s.store.GetByRunID(s.appCtx, p.RunID)
		if gerr != nil {
			return fmt.Errorf("任务队列：加载流水线失败 run_id=%s: %w", p.RunID, gerr)
		}
		if run == nil {
			return fmt.Errorf("任务队列：流水线不存在 run_id=%s", p.RunID)
		}
		fn(run)
		return nil
	}
}

func decodeTaskPayload(payload string) (taskPayload, error) {
	var p taskPayload
	if payload == "" {
		return p, fmt.Errorf("任务队列：payload 为空")
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return p, fmt.Errorf("任务队列：payload 解析失败: %w", err)
	}
	if p.RunID == "" {
		return p, fmt.Errorf("任务队列：payload 缺 run_id")
	}
	return p, nil
}
