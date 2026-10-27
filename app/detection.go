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
