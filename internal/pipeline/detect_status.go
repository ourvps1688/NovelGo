package pipeline

// detect_status.go — R5-3 T1.3「检测任务结果消费面」（WBS §1 T1.3）：
//
//   - 检测状态查询：DetectionTaskStatus / GetDetectionTaskStatus /
//     ListDetectionTaskStatuses——四类检测的「最近一次结果 + 进行态」单一口，
//     供旅程检测任务卡（T2.1）与既有面板消费，不新建重复视图；
//   - 张力报告消费面（开放设计点②）：GetPatrolReport 合并「活跃流水线 run」与
//     「structure 检测容器」两源的 PatrolReport，按落库时间戳最新者胜——
//     PatrolReportPanel 现经 GetPipelineState→GetByNovel 读活跃 run，finished
//     检测容器不可见；本口补上检测容器来源，面板数据源换此口后两源皆可达；
//   - 检测进行态登记表（detectRunning，Service 字段）：派发即登记、执行体收口
//     （含失败）注销，覆盖排队/执行/legacy 直跑全阶段；纯进程内单源（任务队列
//     启动清扫 AbandonActive 使「进行态」与执行体生命周期同进程，无需回查 task 表）。
//
// 检测结果容器（T1.2/T1.3）：chapter→质量档案（chapter_quality）；
// volume/book→检测容器 run.ContextJSON.Audit + 发现项工单（pipeline_issues）；
// structure→检测容器 run.ContextJSON.PatrolReport。四类结果各自可达现有面板
// （质量档案/IssueTicketsPanel/PatrolReportPanel），本层只做取数与状态聚合。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/quality"
)

// DetectionTaskStatus 是某类检测任务的状态视图（T1.3）：进行态 + 最近一次结果摘要。
// scopeID 语义随 kind（同 RunDetectionTask）：chapter=chapterID、volume=volumeID、
// book/structure=忽略；scopeID 为空=该 kind「最近一次检测」的概览口径
// （ListDetectionTaskStatuses 四 kind 全量即此口径）。
type DetectionTaskStatus struct {
	Kind    string `json:"kind"`
	NovelID int64  `json:"novel_id"`
	ScopeID *int64 `json:"scope_id,omitempty"`
	// Running 是检测进行态（含排队中）：派发即 true，执行体收口（含失败）回 false。
	Running bool `json:"running"`
	// LastRunID 是最近一次结果容器的 RunID（chapter 无容器，为空）。
	LastRunID string `json:"last_run_id,omitempty"`
	// LastVerdict 是最近一次结果结论：volume/book=体检 verdict（pass|needs_fix）、
	// structure=张力报告 verdict、chapter=质量档案 L1Status（checked|pending|skipped）。
	LastVerdict string `json:"last_verdict,omitempty"`
	// LastIssueCount 是最近一次结果的发现数（chapter=L0+L1 档案发现；其余=issue 数）。
	LastIssueCount int `json:"last_issue_count"`
	// LastRunAt 是最近一次结果落库时间；从未检测过为空。
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
}

// PatrolReportView 是张力报告的消费视图（开放设计点②）：合并两源 PatrolReport，
// 按落库时间戳最新者胜。
type PatrolReportView struct {
	// Report 是张力报告结构化结果 JSON（AuditData 形状，与 GateContext.PatrolReport
	// 同形存储）；前端沿既有 JSON.parse 消费模式喂 PatrolReportPanel。
	Report string `json:"report"`
	// RunID 是胜出报告所在 run（活跃流水线 run 或 structure 检测容器）。
	RunID string `json:"run_id"`
	// Source 标记胜出来源：pipeline（活跃流水线 run）| detect（structure 检测容器）。
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PatrolReportView.Source 枚举。
const (
	PatrolSourcePipeline = "pipeline"
	PatrolSourceDetect   = "detect"
)

// ── 检测进行态登记表 ──────────────────────────────────────────────────────

// detectKey 生成登记表键：detect:<kind>:<novelID>:<scope>（scope 缺省 0）。
func detectKey(kind string, novelID int64, scopeID *int64) string {
	scope := int64(0)
	if scopeID != nil {
		scope = *scopeID
	}
	return fmt.Sprintf("detect:%s:%d:%d", kind, novelID, scope)
}

// beginDetect 登记检测进行态（派发口调用，覆盖排队/执行/legacy 直跑全阶段）。
// runID 是结果容器 RunID（章节检测无容器传空串），供执行体收口按 runID 注销。
func (s *Service) beginDetect(kind string, novelID int64, scopeID *int64, runID string) {
	s.detectRunning.Store(detectKey(kind, novelID, scopeID), runID)
}

// endDetect 按 key 注销检测进行态（章节检测收口用：无 runID 可寻址）。
func (s *Service) endDetect(kind string, novelID int64, scopeID *int64) {
	s.detectRunning.Delete(detectKey(kind, novelID, scopeID))
}

// endDetectByRunID 按结果容器 RunID 注销检测进行态（卷/书/结构执行体收口用，
// 含加载失败早退路径）。空 runID 直接返回，防误删章节条目（其 value 为空串）。
func (s *Service) endDetectByRunID(runID string) {
	if runID == "" {
		return
	}
	s.detectRunning.Range(func(k, v any) bool {
		if rid, ok := v.(string); ok && rid == runID {
			s.detectRunning.Delete(k)
		}
		return true
	})
}

// isDetectRunning 报告某 kind 某作用域的检测是否进行中（含排队中）。
func (s *Service) isDetectRunning(kind string, novelID int64, scopeID *int64) bool {
	_, ok := s.detectRunning.Load(detectKey(kind, novelID, scopeID))
	return ok
}

// anyDetectRunning 报告该 kind 在该书是否有任意作用域的检测进行中（kind 级概览用）。
func (s *Service) anyDetectRunning(kind string, novelID int64) bool {
	prefix := fmt.Sprintf("detect:%s:%d:", kind, novelID)
	found := false
	s.detectRunning.Range(func(k, _ any) bool {
		if key, ok := k.(string); ok && strings.HasPrefix(key, prefix) {
			found = true
			return false
		}
		return true
	})
	return found
}

// ── 检测任务取消/超时（B5）────────────────────────────────────────────────

// beginDetectCtx 为一次检测执行体创建「带超时+取消」的派生 ctx：从 appCtx 派生，
// 挂 detectTaskTimeout 硬上限，并把 cancel 存入 detectCancelFns（key=detectKey），
// 供 CancelDetectionTask 按 key 取出触发。返回派生 ctx 供执行体使用。
func (s *Service) beginDetectCtx(kind string, novelID int64, scopeID *int64) context.Context {
	ctx, cancel := context.WithTimeout(s.appCtx, detectTaskTimeout)
	s.detectCancelFns.Store(detectKey(kind, novelID, scopeID), cancel)
	return ctx
}

// endDetectCtx 收口一次检测执行体：撤销派生 ctx（cancel）+ 注销 cancel 登记 + 注销
// 检测进行态（T1.3）。幂等：cancel 已被 CancelDetectionTask 取走时仅做注销。
// 在 beginDetectCtx 之后调用（执行体 defer），与 beginDetect/endDetect 同生命周期。
func (s *Service) endDetectCtx(kind string, novelID int64, scopeID *int64) {
	key := detectKey(kind, novelID, scopeID)
	if v, ok := s.detectCancelFns.LoadAndDelete(key); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
	s.endDetect(kind, novelID, scopeID)
}

// CancelDetectionTask 取消一个正在进行的检测任务（B5 交互式退出）：按 detectKey 取出
// 执行体的 cancel 并触发，使执行体 ctx 立即 Done（agent loop / dispatchSkill 感知后
// 安全退出，收口照常落库为 error 态而非假结果）。同时立即注销检测进行态，让前端状态
// 行（轮询/事件）瞬时从「检测中」切回，无需等待执行体 unwind。
//
// 返回 cancelled=true 表示确有在飞执行体被取消；false 表示该方法/作用域当前无在飞
// 执行体（已收口、或仍在队列尚未起执行体——队列排队态无法跨进程取消，属已知限制，
// 桌面单进程下排队窗口极小）。CancelDetectionTask 本身不写运行时状态，调用方据此反馈。
// 未知 kind 回错（API 误用，同 RunDetectionTask）。
func (s *Service) CancelDetectionTask(novelID int64, kind string, scopeID *int64) (bool, error) {
	switch kind {
	case DetectionKindChapter, DetectionKindVolume, DetectionKindBook, DetectionKindStructure:
	default:
		return false, fmt.Errorf("检测取消：未知 kind %q", kind)
	}
	key := detectKey(kind, novelID, scopeID)
	v, ok := s.detectCancelFns.LoadAndDelete(key)
	cancelled := false
	if ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
			cancelled = true
		}
	}
	// 立即注销进行态，前端瞬时可见「已取消」。
	s.endDetect(kind, novelID, scopeID)
	if cancelled {
		EmitDetectCancelled(s.appCtx, map[string]any{
			"novel_id": novelID,
			"kind":     kind,
			"scope_id": scopeID,
		})
	}
	return cancelled, nil
}

// ── 检测状态查询 ──────────────────────────────────────────────────────────

// GetDetectionTaskStatus 返回某 kind 检测任务的状态（进行态 + 最近一次结果摘要）。
// scopeID 为空取「最近一次检测」概览口径；未知 kind 回错（API 误用，同 RunDetectionTask）。
// 取数失败/无历史一律降级为零结果（宁缺勿错，不改检测口径）。
func (s *Service) GetDetectionTaskStatus(ctx context.Context, novelID int64, kind string, scopeID *int64) (*DetectionTaskStatus, error) {
	switch kind {
	case DetectionKindChapter, DetectionKindVolume, DetectionKindBook, DetectionKindStructure:
	default:
		return nil, fmt.Errorf("检测状态：未知 kind %q", kind)
	}
	st := &DetectionTaskStatus{Kind: kind, NovelID: novelID, ScopeID: scopeID}
	switch kind {
	case DetectionKindChapter:
		s.chapterDetectStatus(ctx, novelID, scopeID, st)
	case DetectionKindStructure:
		s.structureDetectStatus(ctx, novelID, st)
	default: // volume / book
		s.scopedDetectStatus(ctx, novelID, kind, scopeID, st)
	}
	return st, nil
}

// ListDetectionTaskStatuses 返回四类检测任务的状态全量（概览口径，scopeID 均为空），
// 供旅程检测任务卡一次取齐。固定 chapter → volume → book → structure 顺序。
func (s *Service) ListDetectionTaskStatuses(ctx context.Context, novelID int64) ([]DetectionTaskStatus, error) {
	kinds := []string{DetectionKindChapter, DetectionKindVolume, DetectionKindBook, DetectionKindStructure}
	out := make([]DetectionTaskStatus, 0, len(kinds))
	for _, kind := range kinds {
		st, err := s.GetDetectionTaskStatus(ctx, novelID, kind, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, nil
}

// chapterDetectStatus 章节检测状态：结果=质量档案（scopeID→GetByChapter；
// 概览→CheckedAt 最新行）。LastVerdict=L1Status，发现数=L0+L1。
func (s *Service) chapterDetectStatus(ctx context.Context, novelID int64, scopeID *int64, st *DetectionTaskStatus) {
	if scopeID != nil {
		st.Running = s.isDetectRunning(DetectionKindChapter, novelID, scopeID)
	} else {
		st.Running = s.anyDetectRunning(DetectionKindChapter, novelID)
	}
	if s.qualityStore == nil {
		return
	}
	if scopeID != nil {
		row, err := s.qualityStore.GetByChapter(ctx, novelID, *scopeID)
		if err != nil || row == nil {
			return
		}
		fillChapterDetectStatus(st, row)
		return
	}
	rows, err := s.qualityStore.ListByNovel(ctx, novelID)
	if err != nil {
		return
	}
	var latest *quality.ChapterQuality
	for i := range rows {
		r := &rows[i]
		if latest == nil || r.CheckedAt.After(latest.CheckedAt) {
			latest = r
		}
	}
	if latest != nil {
		fillChapterDetectStatus(st, latest)
	}
}

// fillChapterDetectStatus 把质量档案行填进检测状态（章节检测结果口径）。
func fillChapterDetectStatus(st *DetectionTaskStatus, row *quality.ChapterQuality) {
	st.LastVerdict = row.L1Status
	st.LastIssueCount = len(quality.ParseFindings(row.L0Findings)) + len(quality.ParseFindings(row.L1Findings))
	at := row.CheckedAt
	st.LastRunAt = &at
}

// scopedDetectStatus 卷/书检测状态：结果=检测容器上下文 Audit（scopeID→同 scope
// 容器；概览→最近一次容器）。LastVerdict=体检 verdict，发现数=issue 数。
func (s *Service) scopedDetectStatus(ctx context.Context, novelID int64, kind string, scopeID *int64, st *DetectionTaskStatus) {
	if scopeID != nil {
		st.Running = s.isDetectRunning(kind, novelID, scopeID)
	} else {
		st.Running = s.anyDetectRunning(kind, novelID)
	}
	var (
		run *PipelineRun
		err error
	)
	if scopeID != nil {
		run, err = s.store.GetDetectRun(ctx, novelID, kind, scopeID)
	} else {
		run, err = s.store.GetLatestDetectRun(ctx, novelID, kind)
	}
	if err != nil || run == nil {
		return // 无检测历史：零结果
	}
	st.LastRunID = run.RunID
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	if audit := parseStoredAudit(ctxt.Audit); audit != nil {
		st.LastVerdict = audit.Verdict
		st.LastIssueCount = len(audit.Issues)
	}
	at := run.UpdatedAt
	st.LastRunAt = &at
}

// structureDetectStatus 巡检·结构检测状态：结果=检测容器上下文 PatrolReport。
// LastVerdict=张力报告 verdict，发现数=张力条数；落库时间优先 PatrolReportAt
// （旧数据无时间戳回退容器 UpdatedAt）。
func (s *Service) structureDetectStatus(ctx context.Context, novelID int64, st *DetectionTaskStatus) {
	st.Running = s.anyDetectRunning(DetectionKindStructure, novelID)
	run, err := s.store.GetDetectRun(ctx, novelID, DetectionKindStructure, nil)
	if err != nil || run == nil {
		return // 无检测历史：零结果
	}
	st.LastRunID = run.RunID
	var ctxt GateContext
	if run.ContextJSON != "" {
		_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
	}
	if report := parseStoredAudit(ctxt.PatrolReport); report != nil {
		st.LastVerdict = report.Verdict
		st.LastIssueCount = len(report.Issues)
	}
	at := run.UpdatedAt
	if ctxt.PatrolReportAt != nil {
		at = *ctxt.PatrolReportAt
	}
	st.LastRunAt = &at
}

// ── 张力报告消费面（开放设计点②）──────────────────────────────────────────

// GetPatrolReport 返回张力报告消费视图：合并「活跃流水线 run」（G_freeze 自动巡检 /
// 控制指令 "patrol" 的落点）与「structure 检测容器」（RunDetectionTask structure 的
// 落点）两源，按落库时间戳（PatrolReportAt；旧数据回退 run.UpdatedAt）最新者胜。
// 两源皆无报告返回 (nil, nil)；查询失败按无报告降级（宁缺勿错）。
func (s *Service) GetPatrolReport(ctx context.Context, novelID int64) (*PatrolReportView, error) {
	var best *PatrolReportView
	consider := func(report, runID, source string, at time.Time) {
		if strings.TrimSpace(report) == "" {
			return
		}
		if best == nil || at.After(best.UpdatedAt) {
			best = &PatrolReportView{Report: report, RunID: runID, Source: source, UpdatedAt: at}
		}
	}
	// 源①：活跃流水线 run。
	if run, err := s.store.GetByNovel(ctx, novelID); err == nil && run != nil {
		var ctxt GateContext
		if run.ContextJSON != "" {
			_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
		}
		at := run.UpdatedAt
		if ctxt.PatrolReportAt != nil {
			at = *ctxt.PatrolReportAt
		}
		consider(ctxt.PatrolReport, run.RunID, PatrolSourcePipeline, at)
	}
	// 源②：structure 检测容器（出生即 StatusFinished，不在活跃 run 查询内）。
	if run, err := s.store.GetDetectRun(ctx, novelID, DetectionKindStructure, nil); err == nil && run != nil {
		var ctxt GateContext
		if run.ContextJSON != "" {
			_ = json.Unmarshal([]byte(run.ContextJSON), &ctxt)
		}
		at := run.UpdatedAt
		if ctxt.PatrolReportAt != nil {
			at = *ctxt.PatrolReportAt
		}
		consider(ctxt.PatrolReport, run.RunID, PatrolSourceDetect, at)
	}
	return best, nil
}

// parseStoredAudit 解析落库的结构化体检/巡检结果 JSON（AuditData 形状）。
// 空串/坏数据返回 nil（宁缺勿错，不报错）。
func parseStoredAudit(raw string) *AuditData {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var a AuditData
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil
	}
	return &a
}
