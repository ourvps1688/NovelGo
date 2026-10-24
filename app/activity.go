package app

import "github.com/novelgo/novelgo/internal/activity"

// RecordCreativeActivity 累计近似活跃秒数，单次最多计一分钟；统计失败不向前端返回错误。
func (a *App) RecordCreativeActivity(seconds int64) {
	if seconds <= 0 || a.activity == nil {
		return
	}
	seconds = min(seconds, 60)
	a.activity.AddActivity(a.ctx, activity.ActivityDelta{CreativeSeconds: seconds})
}

// RecordFeatureUsage 记录一次功能使用（只记 key+次数，纯本地不上传）。
// 零返回值，失败静默——统计不可用时业务零感知。
func (a *App) RecordFeatureUsage(feature string) {
	if a.activity == nil {
		return
	}
	a.activity.AddFeatureUsage(a.ctx, feature)
}

// GetFeatureUsage 返回近 days 天（含今天）按功能键聚合的使用次数，按次数降序；
// days 非正时按 30 天。失败返回错误，由前端静默降级为空表。
func (a *App) GetFeatureUsage(days int) ([]activity.FeatureUsageStat, error) {
	if a.activity == nil {
		return []activity.FeatureUsageStat{}, nil
	}
	return a.activity.ListFeatureUsage(a.ctx, days)
}
