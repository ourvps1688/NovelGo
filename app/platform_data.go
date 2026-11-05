package app

// P1-2 真实数据对账绑定（薄委托）：平台数据粘贴预览/落库/列表/清空 + 「流失章 ×
// 低分维」交叉诊断。领域编排在 internal/pipeline，本层只做 DTO 暴露与空值守卫。

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// PreviewPlatformMetrics 解析粘贴的平台数据文本并解析章号归属（不落库）。
func (a *App) PreviewPlatformMetrics(novelID int64, text string) (*pipeline.PlatformMetricsPreview, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.PreviewPlatformMetrics(a.ctx, novelID, text)
}

// SavePlatformMetricsText 解析粘贴文本并落库（同章最新粘贴覆盖）。
func (a *App) SavePlatformMetricsText(novelID int64, text, batch string) (*pipeline.PlatformMetricsSaveResult, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.SavePlatformMetricsText(a.ctx, novelID, text, batch)
}

// ListPlatformMetricsView 列出某书全部平台数据（含章节标题）。
func (a *App) ListPlatformMetricsView(novelID int64) ([]pipeline.PlatformMetricView, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.ListPlatformMetricsView(a.ctx, novelID)
}

// DeletePlatformMetrics 清空某书全部平台数据。
func (a *App) DeletePlatformMetrics(novelID int64) error {
	if a.pipeline == nil {
		return fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.DeletePlatformMetrics(a.ctx, novelID)
}

// GetPlatformReconciliation 返回「流失章 × 低分维」交叉诊断数据包（P1-2）。
func (a *App) GetPlatformReconciliation(novelID int64) (*pipeline.PlatformReconciliation, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.GetPlatformReconciliation(a.ctx, novelID)
}
