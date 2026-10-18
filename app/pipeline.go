package app

import (
	"context"
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// StartPipeline 启动一次创作流水线（mode: new|continue；续写模式传 startChapter）。
func (a *App) StartPipeline(ctx context.Context, novelID int64, mode string, startChapter *int) (*pipeline.PipelineRun, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.StartPipeline(ctx, novelID, mode, startChapter)
}

// GetPipelineState 返回指定小说当前活跃的流水线（无则返回错误）。
func (a *App) GetPipelineState(ctx context.Context, novelID int64) (*pipeline.PipelineRun, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetPipelineState(ctx, novelID)
}

// ResumePipeline 在阶段完成或闸门点续跑，按 decision 推进流程。
func (a *App) ResumePipeline(ctx context.Context, runID, decision, feedback string) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ResumePipeline(ctx, runID, decision, feedback)
}

// ControlPipeline 处理运行控制指令：pause / resume / stop。
func (a *App) ControlPipeline(ctx context.Context, runID, cmd string) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ControlPipeline(ctx, runID, cmd)
}
