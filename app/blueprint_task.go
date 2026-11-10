package app

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/pipeline"
)

// RunBlueprintTask 是 S2 全书蓝图的前端显式入口（#111 P2，§4.3）。
// 只创建一次任务并派发生成→体检，落工单后由用户在工单面板显式修复；
// 不恢复 StartPipeline 自动裁决、不新增检测 kind、不自动复评或自动修复。
func (a *App) RunBlueprintTask(novelID int64) (*pipeline.BlueprintTaskResult, error) {
	if a.pipeline == nil {
		return nil, fmt.Errorf("创作流水线服务未初始化")
	}
	return a.pipeline.RunBlueprintTask(a.ctx, novelID)
}
