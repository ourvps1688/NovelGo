package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// StartPipeline 启动一次创作流水线（mode: new|continue；续写模式传 startChapter；autoRun 开启自动连跑）。
func (a *App) StartPipeline(novelID int64, mode string, startChapter *int, autoRun bool) (*pipeline.PipelineRun, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.StartPipeline(a.ctx, novelID, mode, startChapter, autoRun)
}

// GetPipelineState 返回指定小说当前活跃的流水线（无则返回错误）。
func (a *App) GetPipelineState(novelID int64) (*pipeline.PipelineRun, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetPipelineState(a.ctx, novelID)
}

// ResumePipeline 在阶段完成或闸门点续跑，按 decision 推进流程。
func (a *App) ResumePipeline(runID, decision, feedback string) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ResumePipeline(a.ctx, runID, decision, feedback)
}

// ControlPipeline 处理运行控制指令：pause / resume / stop。
func (a *App) ControlPipeline(runID, cmd string) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ControlPipeline(a.ctx, runID, cmd)
}
