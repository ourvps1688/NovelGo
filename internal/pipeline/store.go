package pipeline

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// 流水线运行状态。
const (
	StatusPending      = "pending"
	StatusRunning      = "running"
	StatusPaused       = "paused"
	StatusAwaitingGate = "awaiting_gate"
	StatusFinished     = "finished"
	StatusAborted      = "aborted"
)

// PipelineRun 记录一次创作流水线的运行状态，支持重启续跑。
type PipelineRun struct {
	ID           int64     `gorm:"primaryKey;autoIncrement"`
	NovelID      int64     `gorm:"column:novel_id;index;not null"`
	RunID        string    `gorm:"column:run_id;uniqueIndex;not null"`
	Mode         string    `gorm:"column:mode;not null;default:new"` // new | continue
	StartChapter *int      `gorm:"column:start_chapter"`             // 续写模式起始章号
	CurrentStage string    `gorm:"column:current_stage;not null"`
	Status       string    `gorm:"column:status;not null"`
	AutoRun      bool      `gorm:"column:auto_run;not null;default:false"`
	ContextJSON  string    `gorm:"column:context_json;type:text"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

// TableName 指定物理表名。
func (PipelineRun) TableName() string { return "pipeline_runs" }

// GateDecision 记录一次闸门的用户决策（持久化于 ContextJSON）。
type GateDecision struct {
	GateID   string    `json:"gate_id"`
	Decision string    `json:"decision"`
	Feedback string    `json:"feedback"`
	At       time.Time `json:"at"`
}

// GateContext 是编排过程中的上下文载体，存于 ContextJSON，支持重启续跑。
type GateContext struct {
	Stages []string       `json:"stages,omitempty"`
	Gates  []GateDecision `json:"gates,omitempty"`
	Extra  string         `json:"extra,omitempty"`
	// Blueprint 是 S2 蓝图阶段产出的卷结构概览 JSON（由 create_volume 落库的卷记录汇总），
	// 供前端 BlueprintWorkflowView 渲染与 G1 闸门确认时回看。
	Blueprint string `json:"blueprint,omitempty"`
	// Audit 是 S4 签约体检产出的结构化结果 JSON（见 AuditData），供前端 SignabilityAuditView
	// 渲染 issue 清单，也供 S5 修复 agent 作为修复依据。
	Audit string `json:"audit,omitempty"`
	// S4Audited 标记本书是否已做过一次签约体检（无论通过与否）。一旦为 true，
	// S3 逐章循环不再自动触发 S4a（避免每章都弹体检），改由 PR4 的 G3b 里程碑智能触发。
	S4Audited bool `json:"s4_audited,omitempty"`
	// LastAuditChapter 记录上次签约体检（S4/S4b）时本书已落库章节数；供 PR4 里程碑闸计算间距
	// （每 5 章触发一次复检）。未体检过为 0，等同强制下次进入里程碑判定。
	LastAuditChapter int `json:"last_audit_chapter,omitempty"`
	// AuditReason 记录上次进入体检的触发原因：golden3（黄金三章首检）| milestone（章节里程碑）| volume（卷边界）| final（完本终检）。
	AuditReason string `json:"audit_reason,omitempty"`
	// AuditStage 记录上次体检所处的阶段（S4 首检 / S4b 卷边界复评 / S6 完本终检），供修复后复评回到同阶段。
	AuditStage string `json:"audit_stage,omitempty"`
	// FixRounds 记录 S5 自动修复已发生的轮数，供 auto_run 模式判断是否达到上限改交人工。
	FixRounds int `json:"fix_rounds,omitempty"`
	// EntryStage 记录本书启动流水线时由 DetermineEntryStage 选定的入口阶段（S0/S1/S3），用于追溯。
	EntryStage string `json:"entry_stage,omitempty"`
	// ImportAligned 标记 S0 导入对齐子流程是否已完成（汇入公共循环后置 true）。
	ImportAligned bool `json:"import_aligned,omitempty"`
}

// AuditIssue 是签约体检发现的一条问题。
type AuditIssue struct {
	ID    string `json:"id"`    // 问题编号，如 I1
	Level string `json:"level"` // P0（阻断签约）/ P1（提升竞争力）
	Area  string `json:"area"`  // 维度，如 开篇过签力 / 节奏爽点
	Desc  string `json:"desc"`  // 问题描述
}

// AuditData 是 S4 签约体检的结构化结果，序列化进 GateContext.Audit。
type AuditData struct {
	Verdict string       `json:"verdict"` // pass | needs_fix
	Score   int          `json:"score"`   // 总分（/35）
	Summary string       `json:"summary"` // 一句话结论
	Issues  []AuditIssue `json:"issues"`  // issue 清单
}

// Store 封装 pipeline_runs 表的读写。
type Store struct {
	db *gorm.DB
}

// NewStore 构造仓储。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Create 写入一条新流水线记录。
func (s *Store) Create(ctx context.Context, run *PipelineRun) error {
	return s.db.WithContext(ctx).Create(run).Error
}

// GetByNovel 取该小说当前活跃（未结束/未中止）的流水线，取最新一条。
func (s *Store) GetByNovel(ctx context.Context, novelID int64) (*PipelineRun, error) {
	var run PipelineRun
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND status NOT IN ?", novelID, []string{StatusFinished, StatusAborted}).
		Order("id DESC").First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// GetByRunID 按 RunID 精确查询。
func (s *Store) GetByRunID(ctx context.Context, runID string) (*PipelineRun, error) {
	var run PipelineRun
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// SetStatus 仅更新状态与上下文（不改变阶段）。
func (s *Store) SetStatus(ctx context.Context, runID, status, contextJSON string) error {
	return s.db.WithContext(ctx).Model(&PipelineRun{}).
		Where("run_id = ?", runID).
		Updates(map[string]any{
			"status":       status,
			"context_json": contextJSON,
			"updated_at":   time.Now(),
		}).Error
}

// SetStageStatus 同时更新当前阶段与状态（阶段推进时用）。
func (s *Store) SetStageStatus(ctx context.Context, runID, stage, status, contextJSON string) error {
	return s.db.WithContext(ctx).Model(&PipelineRun{}).
		Where("run_id = ?", runID).
		Updates(map[string]any{
			"current_stage": stage,
			"status":        status,
			"context_json":  contextJSON,
			"updated_at":    time.Now(),
		}).Error
}

// SetAutoRun 切换流水线的 auto_run 开关（运行中由 ControlPipeline("set_auto") 调用）。
func (s *Store) SetAutoRun(ctx context.Context, runID string, autoRun bool) error {
	return s.db.WithContext(ctx).Model(&PipelineRun{}).
		Where("run_id = ?", runID).
		Updates(map[string]any{
			"auto_run":   autoRun,
			"updated_at": time.Now(),
		}).Error
}

// AppendGateDecision 把一次闸门决策追加进 ContextJSON 的 gates 列表。
func (s *Store) AppendGateDecision(ctx context.Context, runID, gateID, decision, feedback string) error {
	run, err := s.GetByRunID(ctx, runID)
	if err != nil {
		return err
	}
	var ctxt GateContext
	if run.ContextJSON != "" {
		if err := json.Unmarshal([]byte(run.ContextJSON), &ctxt); err != nil {
			return err
		}
	}
	if ctxt.Gates == nil {
		ctxt.Gates = []GateDecision{}
	}
	ctxt.Gates = append(ctxt.Gates, GateDecision{
		GateID:   gateID,
		Decision: decision,
		Feedback: feedback,
		At:       time.Now(),
	})
	b, err := json.Marshal(ctxt)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&PipelineRun{}).
		Where("run_id = ?", runID).
		Updates(map[string]any{
			"context_json": string(b),
			"updated_at":   time.Now(),
		}).Error
}
