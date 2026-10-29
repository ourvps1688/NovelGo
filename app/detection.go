package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// ── 统一检测任务入口 API（R5-3 T1.2，提案 §10.4）────────────────────────────
// 检测任务（章节检测/卷体检/书体检/巡检·结构）可脱离流水线任意时刻单独启动；
// 流水线降级为「写→检测」的编排预设后，本入口是检测环节的一等公民启动口。

// RunDetectionTask 按 kind 启动一次检测任务（scopeID 语义随 kind：chapter=chapterID、
// volume=volumeID、book/structure=忽略）。异步执行，回执只反映派发情况；
// 结果落对应容器（质量档案/发现项工单/张力报告）。
func (a *App) RunDetectionTask(novelID int64, kind string, scopeID *int64) (*pipeline.DetectionTaskResult, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.RunDetectionTask(a.ctx, novelID, kind, scopeID)
}

// CancelDetectionTask 取消一个正在进行的检测任务（B5 交互式退出）。按 kind 与作用域
// 取执行体 cancel 触发：在飞执行体立即感知 ctx.Done 安全退出；已收口/仍在队列则 best-effort
// （返回 cancelled=false，前端据此如实反馈）。作用域语义同 RunDetectionTask。
func (a *App) CancelDetectionTask(novelID int64, kind string, scopeID *int64) (bool, error) {
	if a.pipeline == nil {
		return false, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.CancelDetectionTask(novelID, kind, scopeID)
}

// GetDetectionTaskStatus 返回某类检测任务的状态（进行态 + 最近一次结果摘要，
// R5-3 T1.3）。scopeID 为空取该 kind「最近一次检测」概览口径。
func (a *App) GetDetectionTaskStatus(novelID int64, kind string, scopeID *int64) (*pipeline.DetectionTaskStatus, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetDetectionTaskStatus(a.ctx, novelID, kind, scopeID)
}

// ListDetectionTaskStatuses 返回四类检测任务的状态全量（概览口径），
// 供旅程检测任务卡一次取齐（R5-3 T1.3/T2.1）。
func (a *App) ListDetectionTaskStatuses(novelID int64) ([]pipeline.DetectionTaskStatus, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ListDetectionTaskStatuses(a.ctx, novelID)
}

// GetPatrolReport 返回张力报告消费视图（R5-3 T1.3）：合并活跃流水线 run 与
// structure 检测容器两源，按落库时间戳最新者胜；无报告返回 nil。
func (a *App) GetPatrolReport(novelID int64) (*pipeline.PatrolReportView, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetPatrolReport(a.ctx, novelID)
}
