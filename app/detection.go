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

// GetChapterQualityArchive 返回指定章节的质量档案详情（L0 指标 + L0/L1 发现清单），
// 供章节检测面板就地展开展示，取代原先跳转到章节列表的占位入口。
func (a *App) GetChapterQualityArchive(novelID int64, chapterID int64) (*pipeline.ChapterQualityArchive, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetChapterQualityArchive(a.ctx, novelID, chapterID)
}

// GetScoreTrend 返回全书评分趋势数据包（P0-3 双轨评分打通）：逐章最新/上一次判官分、
// 修复前后变化与发现数曲线，以及 6↔8 映射契约快照（单源 review，前端图例直接消费）。
func (a *App) GetScoreTrend(novelID int64) (*pipeline.ScoreTrend, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetScoreTrend(a.ctx, novelID)
}

// GetChapterScoreHistory 返回单章评分历史（修复前后对比，P0-3）：评分明细事件正序，
// 含 6 维判官分与换算后的 8 维体检同源分；旧数据回落快照回填（source=snapshot）。
func (a *App) GetChapterScoreHistory(novelID int64, chapterID int64) ([]pipeline.ScoreEventView, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetChapterScoreHistory(a.ctx, novelID, chapterID)
}

// RescoreChapter 显式重判分（#115 P0）：用户点「重新判分」时用当前落盘正文调用一次
// 判官，追加 source=rescore 的评分历史。人工在环、零自动派发，不改正文、不触发审批、
// 不放宽 edit 评分门禁；模型与 chat / 后台子 Agent 同源（每次现取设置，切换即生效）。
func (a *App) RescoreChapter(novelID int64, chapterID int64) (*pipeline.ScoreEventView, error) {
	if a.db == nil || a.llmClient == nil {
		return nil, fmt.Errorf("创作服务未初始化")
	}
	s := a.settingsSnapshot()
	provider, modelID := a.parseModelKey(s.SelectedModelKey)
	return pipeline.RescoreChapter(a.ctx, a.db, a.llmClient, a.logger, provider, modelID, novelID, chapterID)
}

// GetChapterTensionCurve 返回单章张力曲线视图（P0-2 情绪账）：生效曲线段
// （L1 标注优先于 L0 基线）+ 图表标记位（拖沓段/情绪峰/开篇窗口，与曲线三查同源），
// 供前端情绪曲线 SVG 可视化消费。
func (a *App) GetChapterTensionCurve(novelID int64, chapterID int64) (*pipeline.ChapterTensionView, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetChapterTensionCurve(a.ctx, novelID, chapterID)
}
