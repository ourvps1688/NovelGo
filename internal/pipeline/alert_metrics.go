package pipeline

import (
	"context"
	"sort"
)

// 告警→真问题命中率度量（R2b-9，提案 §9.4 数据回流与校准）：L1/L2 告警的
// 产生/确认/未确认流转进本地度量（P4 底座 feature_usage_daily，纯本地不上传，
// 只记 key+次数不记内容，隐私红线）。校准方向（误报率高的检查器降权、命中率高的
// 升 gate）由消费面按数据决策——本层只记数不改行为（只报告不拦截，宁缺勿错）。
//
// 口径（单源注释；前端 featureKeys.ts 同族 key 镜像，组件不得自造）：
//   - raised=告警产生（曝光口径：每轮体检/扫描落档即计数，跨轮重复报不折算，
//     趋势口径非精确去重）。族：l2=体检 issue（含 V 系漂移并单后全量）；
//     l1=L0/L1 档案发现（l0RuleSweep 全书扫描落档侧；ChapterCI 写后钩子侧落档
//     暂未接入计数）；struct=结构告警（伏笔/不变量/台账对账）。
//   - confirmed=告警→真问题（修复回执 fixed，逐条计数）。
//   - dismissed=告警→未确认（修复回执 skipped，含误报/修不了/转人工）。
//     body 级 issue 走 S3 重写回路不经修复回执，不计 confirmed/dismissed
//     （无确定性流转信号，宁缺勿错）。
const (
	featureAlertRaisedL2     = "action.alert_raised.l2"
	featureAlertRaisedL1     = "action.alert_raised.l1"
	featureAlertRaisedStruct = "action.alert_raised.struct"
	featureAlertConfirmedL2  = "action.alert_confirmed.l2"
	featureAlertDismissedL2  = "action.alert_dismissed.l2"
)

// recordFeatureUsageN 按条数记 n 次本地度量（n<=0 不记；观测失败零影响）。
func (s *Service) recordFeatureUsageN(ctx context.Context, featureKey string, n int) {
	for i := 0; i < n; i++ {
		s.recordFeatureUsage(ctx, featureKey)
	}
}

// IssueHitStat 是一个机器归属维度（dimension）的告警流转统计（§9.4 校准取数）。
type IssueHitStat struct {
	// Dimension 是 issue 的机器归属维度（series bible 四维；空归入 "none"）。
	Dimension string
	// Total 是该维度工单总数；Fixed/Skipped 是修复回执流转数（命中/未确认）。
	Total   int
	Fixed   int
	Skipped int
	// Pending 是未收口数（pending/fixing/rewrite/retcon 等其余状态合计）。
	Pending int
}

// IssueHitStats 按 dimension 聚合 issue 工单流转（命中率/误报率校准取数，
// §9.4「误报率高的检查器降权、命中率高的升 gate」的数据基础）。
// 只读聚合不改行为；按 dimension 字典序排序（确定性）；查询失败返回错误由调用方降级。
func (s *Service) IssueHitStats(ctx context.Context, novelID int64) ([]IssueHitStat, error) {
	if s.store == nil || s.store.db == nil {
		return nil, nil
	}
	var rows []PipelineIssue
	if err := s.store.db.WithContext(ctx).
		Where("novel_id = ?", novelID).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	byDim := map[string]*IssueHitStat{}
	for _, r := range rows {
		dim := r.Dimension
		if dim == "" {
			dim = "none"
		}
		st, ok := byDim[dim]
		if !ok {
			st = &IssueHitStat{Dimension: dim}
			byDim[dim] = st
		}
		st.Total++
		switch r.Status {
		case IssueStatusFixed, IssueStatusVerified:
			// verified=复验通过（R5-1 工单态）与自动修复回执 fixed 同为「命中」口径。
			st.Fixed++
		case IssueStatusSkipped, IssueStatusDismissed:
			// dismissed=工单驳回（R5-1）与修复回执 skipped 同为「未确认」口径。
			st.Skipped++
		default:
			st.Pending++
		}
	}
	out := make([]IssueHitStat, 0, len(byDim))
	for _, st := range byDim {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dimension < out[j].Dimension })
	return out, nil
}
