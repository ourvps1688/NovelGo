package pipeline

// tension_run.go —— 情绪账曲线发现工单（P0-2，quality/tension_checks.go 契约消费侧）。
//
// 张力曲线四查（拖沓段/爽点断档/开篇坡度/爽点密度）的发现要走既有修复闭环
// （工单面板/去修复/复验），落 pipeline_issues 工单。复用检测容器机制（ModeDetect
// 家族、出生即 StatusFinished 的隔离纪律）：每书一个 tension 容器 ensure 复用
// （区别于 feedback 容器的每样本新建）——曲线发现按章整批替换（E 系按章前缀，
// ReplaceIssuesByPrefix），同章新结果覆盖旧工单、跨章累积。
//
// 命名空间裁决：I 归常规体检、D 归蓝图漂移、T 归巡检、V 归文风漂移、F 归编辑反馈、
// E 归情绪账（key=E{阅读序号}-{seq}）。
//
// 落工单触发点（零自动流转纪律不变——只落发现项，人工确认→修复→显式复验闭环）：
//   - mcp_tools 写后链路 runChapterL0（经 SetTensionTicketLander 注入本包函数，
//     SetFeedbackTicketLander 同款装配纪律——mcp_tools 不得反向 import pipeline）；
//   - 本包 l0RuleSweep 全书规则扫描逐章落单（回填语义：零发现=清该章旧工单）。

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/quality"
)

// TensionKind 是情绪账工单容器的 kind（mode='detect:tension'，detectModeOf 口径）。
// GetDetectRun 只按显式 kind 查询，检测面板/恢复链路不会误碰本容器。
const TensionKind = "tension"

// tensionChecks 是落工单的曲线检查项（四查全落）：拖沓段/爽点断档/开篇坡度 +
// 爽点密度——beat_gap/beat_density 是配置互斥对，同缺陷须对称覆盖（配对互斥由
// quality.RunL0Checks 内部裁决，同一运行至多命中其一）。其余 L0 检查项不出 E 系工单。
var tensionChecks = map[string]bool{
	quality.CheckPacingDrag:   true,
	quality.CheckBeatGap:      true,
	quality.CheckOpeningSlope: true,
	quality.CheckBeatDensity:  true,
}

// tensionAreaLabels 是四查的工单展示标签（Area）。
var tensionAreaLabels = map[string]string{
	quality.CheckPacingDrag:   "拖沓段",
	quality.CheckBeatGap:      "爽点断档",
	quality.CheckOpeningSlope: "开篇坡度",
	quality.CheckBeatDensity:  "爽点密度",
}

// tensionIssuePrefix 是某章 E 系工单的键前缀（ReplaceIssuesByPrefix 口径）。
// 形如 "E5-"：只匹配本章工单（E5-1/E5-2/…），不吃到 E51-1（前缀含尾随 '-'）。
func tensionIssuePrefix(readingNumber int) string {
	return fmt.Sprintf("E%d-", readingNumber)
}

// TensionIssuesToAudit 把一章的曲线发现投影为工单契约 issue（E 系，seq 1..n 顺编）。
// 只收 tensionChecks 四查发现（其余检查项不出 E 系工单）；每条带本章阅读序号作用域。
// E 系不属 series bible 四维（Dimension=none）、不涉卷作用域（TargetScope=none，
// writeProtected 不命中，可正常派发修复）、修复方向恒前向适配；
// FixType=body（节奏/爽点/开篇坡度需正文级调整，走章节重写修复路由）。
func TensionIssuesToAudit(readingNumber int, findings []quality.Finding) []AuditIssue {
	out := make([]AuditIssue, 0, len(findings))
	seq := 0
	for _, f := range findings {
		if !tensionChecks[f.Check] {
			continue
		}
		seq++
		out = append(out, AuditIssue{
			ID:          fmt.Sprintf("E%d-%d", readingNumber, seq),
			Level:       f.Level,
			Area:        tensionAreaLabels[f.Check],
			Desc:        f.Desc,
			FixType:     FixTypeBody,
			TargetScope: TargetScopeNone,
			Dimension:   DimensionNone,
			Direction:   DirectionForwardFix,
			ChapterIDs:  []int64{int64(readingNumber)},
		})
	}
	return out
}

// CreateTensionRun 新建情绪账工单容器 run（每书一个，出生即 StatusFinished，
// 后续按章落单复用同一 RunID 走 ReplaceIssuesByPrefix 整批替换）。
func (s *Store) CreateTensionRun(ctx context.Context, novelID int64) (*PipelineRun, error) {
	run := &PipelineRun{
		NovelID:      novelID,
		RunID:        "ts-" + uuid.NewString(),
		Mode:         detectModeOf(TensionKind),
		CurrentStage: detectContainerStage,
		Status:       StatusFinished,
	}
	if err := s.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("情绪账：创建工单容器失败: %w", err)
	}
	return run, nil
}

// ensureTensionRun 建/复用情绪账工单容器（每书一个 ensure 复用）。
// create=false（零发现且无既有容器）→ 返回 (nil, nil)：不建容器不落库。
func (s *Store) ensureTensionRun(ctx context.Context, novelID int64, create bool) (*PipelineRun, error) {
	existing, err := s.GetDetectRun(ctx, novelID, TensionKind, nil)
	if err == nil && existing != nil {
		return existing, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("情绪账：查询工单容器失败: %w", err)
	}
	if !create {
		return nil, nil
	}
	return s.CreateTensionRun(ctx, novelID)
}

// LandTensionTickets 把一章的曲线四查发现落成/刷新情绪账工单（E 系，tension 容器），
// 返回落库条数。按章整批替换（ReplaceIssuesByPrefix）：新结果覆盖本章旧工单、
// 零发现=清该章旧工单（回填语义）；无容器且零发现=不建容器（直接跳过）。
// novelID<=0 或阅读序号缺失（<=0）跳过（发现不带作用域时无法落章级工单，宁缺勿错）。
//
// 调用方是 mcp_tools 写后链路与本包 l0RuleSweep：mcp_tools 不能反向 import 本包
// （pipeline→agent→mcp_tools 会成环），由 app 启动时把本函数注入
// mcp_tools.SetTensionTicketLander（SetFeedbackTicketLander 同款装配纪律）。
func LandTensionTickets(ctx context.Context, db *gorm.DB, novelID int64, readingNumber int, findings []quality.Finding) (int, error) {
	if novelID <= 0 || readingNumber <= 0 {
		return 0, nil
	}
	s := NewStore(db)
	issues := TensionIssuesToAudit(readingNumber, findings)
	run, err := s.ensureTensionRun(ctx, novelID, len(issues) > 0)
	if err != nil {
		return 0, err
	}
	if run == nil {
		return 0, nil // 无容器且零发现：不建容器
	}
	if err := s.ReplaceIssuesByPrefix(ctx, novelID, run.RunID, tensionIssuePrefix(readingNumber), 0, issues); err != nil {
		return 0, err
	}
	return len(issues), nil
}
