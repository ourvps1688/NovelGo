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
	// StatusWaiting 标记 book-run 正在等待其卷级 scoped sub-run（体检/修复）收口。
	// scoped run 完成后唤醒回 S3 running；不由恢复机制误判为僵尸（恢复只扫 running）。
	StatusWaiting  = "waiting"
	StatusFinished = "finished"
	StatusAborted  = "aborted"
)

// ModeDetect 是检测任务（R5-3 T1.2/T1.3）结果容器 run 的 mode 家族前缀。
// 容器出生即 StatusFinished，借既有 status 过滤天然隔离 GetByNovel /
// GetActiveVolumeRun / RecoverInterruptedRuns / abortRelatives 等活跃 run 链路
// （既有查询零改动、绝不污染 book-run）；进行态可见走进程内检测登记表（T1.3）。
//
// 实际 mode 值按 kind 分容器（T1.3 开放设计点①定案）：'detect:<kind>'
// （detectModeOf）。book 与 structure 若共享全书容器有两害——①按 kind 的
// 「最近一次检测」粒度分不开 updated_at；② detect/patrol 是不同队列 dedup key
// 可并发执行，各自 read-modify-write 同一容器 ContextJSON 会互相覆盖丢结果。
// 复用走 GetDetectRun（mode+scope 匹配，同 scope 复用同 RunID）。
const ModeDetect = "detect"

// detectModeOf 生成检测容器的 mode 值：'detect:<kind>'（kind ∈ DetectionKind*）。
func detectModeOf(kind string) string { return ModeDetect + ":" + kind }

// PipelineRun 记录一次创作流水线的运行状态，支持重启续跑。
// 卷级作用域（G1 scoped sub-run）：book-run（VolumeID 为空）挂卷级 scoped sub-run
// （VolumeID 非空、ParentRunID 指向 book-run），体检/修复仅作用于该卷。
type PipelineRun struct {
	ID           int64  `gorm:"primaryKey;autoIncrement"`
	NovelID      int64  `gorm:"column:novel_id;index;not null"`
	RunID        string `gorm:"column:run_id;uniqueIndex;not null"`
	Mode         string `gorm:"column:mode;not null;default:new"` // new | continue | detect:<kind>（检测容器，detectModeOf）
	StartChapter *int   `gorm:"column:start_chapter"`             // 续写模式起始章号
	CurrentStage string `gorm:"column:current_stage;not null"`
	Status       string `gorm:"column:status;not null"`
	AutoRun      bool   `gorm:"column:auto_run;not null;default:false"`
	ContextJSON  string `gorm:"column:context_json;type:text"`
	// VolumeID 是本 run 的卷作用域：非空=卷级 scoped sub-run，S4/S5 仅作用于该卷；
	// 空=book-run（全书）。旧数据均为 NULL，即全书 book-run（向后兼容）。
	VolumeID *int64 `gorm:"column:volume_id;index"`
	// ParentRunID 挂接层级：卷级 scoped sub-run 记录其父 book-run 的 RunID；空=book-run。
	ParentRunID string    `gorm:"column:parent_run_id;index"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
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
	// AuditStage 记录上次体检所处的阶段（S4 首检 / S4b 卷边界复评 / S6 完本终检 / S2 蓝图体检），供修复后复评回到同阶段。
	AuditStage string `json:"audit_stage,omitempty"`
	// AuditKind 标记当前体检/修复的种类："blueprint" 走蓝图技能（蓝图体检/修复闭环），
	// ""（默认）走签约技能。S4/S4b/S6 与 S2 蓝图共享 S5 修复执行体，靠此字段分流技能与 issue 来源。
	AuditKind string `json:"audit_kind,omitempty"`
	// BlueprintAudit 是蓝图体检（blueprint-audit）产出的结构化结果 JSON，与签约 Audit 分字段存放，
	// 避免互相覆盖；供前端蓝图 G1b 闸门渲染 issue 清单，也供 S5 蓝图修复 agent 作为修复依据。
	BlueprintAudit string `json:"blueprint_audit,omitempty"`
	// DriftReport 是 G3 蓝图一致性巡检的人类可读结论（S5 修复完成后自动触发一次，只读非阻断）：
	// 空串=未巡检或无章节可巡；非空时由 emitGateDecision 追加到复评闸门 summary，
	// 提示用户哪些章节与当前卷纲冲突、是否需要重读。
	DriftReport string `json:"drift_report,omitempty"`
	// FixRounds 记录工单修复已发生的轮数（R5-2 起修复由发现项工单显式触发，每轮工单
	// 修复收口 +1），供 auto_run 上限判断与工单轮次统计（ReplaceIssues 的 round 参数）。
	FixRounds int `json:"fix_rounds,omitempty"`
	// EntryStage 记录本书启动流水线时由 DetermineEntryStage 选定的入口阶段（S0/S1/S3），用于追溯。
	EntryStage string `json:"entry_stage,omitempty"`
	// ImportAligned 标记 S0 导入对齐子流程是否已完成（汇入公共循环后置 true）。
	ImportAligned bool `json:"import_aligned,omitempty"`
	// StockAudit 是「全量对齐」按钮触发的全本现状体检结果（区别于 S4 流水线体检的 Audit 字段），
	// 只读展示，不影响流水线阶段流转。
	StockAudit string `json:"stock_audit,omitempty"`
	// PatrolReport 是全书一致性巡检（WBS 3.3「张力报告」）的结构化结果 JSON（AuditData 形状）：
	// 建议性报告，只报告不改写、绝不进自动修复路由（区别于 Audit/StockAudit），改不改由作者定。
	// 卷末（G_freeze 冻结成功）自动一次 + 控制指令 "patrol" 手动触发，供巡检报告视图只读展示。
	PatrolReport string `json:"patrol_report,omitempty"`
	// PatrolReportAt 是张力报告的落库时间戳（storePatrolReport 打戳；time.Time 是
	// struct 不能 omitempty，必须指针）。GetPatrolReport 双源合并（活跃 run + structure
	// 检测容器）按此时间戳最新者胜；旧数据无此键时回退 run.UpdatedAt（活跃 run 的
	// UpdatedAt 会被其他写入推高，只作兜底判据不作首选）。
	PatrolReportAt *time.Time `json:"patrol_report_at,omitempty"`
	// （R3c 退役）Interrupted/Dispatching 两个补丁布尔已删除：执行体存活与中断恢复
	// 改用任务队列的租约语义（task.Store.HasActiveForRun + kind:run_id 活跃去重，
	// 见 task_dispatch.go）。旧 ContextJSON 里残留的 interrupted/dispatching 键
	// 由 json.Unmarshal 忽略，无兼容问题。
	// FixStates 记录 S5 修复各问题的完成状态（issueID → "fixed"/"skipped"/"rewrite"）。
	// 分批修复每完成一批即落库，作为断点续修依据：修复失败暂停后恢复时跳过已完成问题；
	// rewrite 标记正文级问题已被路由到 S3 重写回路，S5 不再重试（消除对修不动问题的空转）。
	FixStates map[string]string `json:"fix_states,omitempty"`
	// RewriteBacklog 是回流到 S3 章节重写回路的正文级问题清单（fix_type=body 的 issue），
	// 每次体检（executeAudit，签约类）重新计算以反映最新体检结果。S5 不处理这些项，
	// 而是显式通知写作回路优先重写其涉及章节，闭合「审计→修台账→复评→正文重写→复评」链路。
	RewriteBacklog []RewriteItem `json:"rewrite_backlog,omitempty"`
	// RetconBacklog 是命中冻结卷写保护/retcon 候选的问题清单（target_scope=frozen_volume
	// 或 direction=retcon_candidate 的 issue，含 body 与 ledger 两类），每次体检重新计算。
	// S5/工单修复一律不碰这些项（冻结卷正文与既定设定不得被自动回改），交回人工裁决：
	// 前向适配调整本卷，或（阶段 3）经 retcon 审批闸门受控回改前卷。
	RetconBacklog []RewriteItem `json:"retcon_backlog,omitempty"`
	// FixLog 是本轮修复日志（R1b）：已修复条目的一行改动摘要（来自修复回执的可选
	// log 字段），按完成先后排列。同轮后续批次/并行组的修复提示词经 fixLogBlock 注入
	// 「本轮修复日志」，消除独立会话的重复勘察（F2）。executeAudit 落新体检结果时随
	// FixStates 一并重置（新一轮清单=新一轮日志）。
	FixLog []FixLogEntry `json:"fix_log,omitempty"`
	// ManualBacklog 是「转人工工单」清单（R1c）：修复 agent 上报 skipped（无法自动修复
	// 的工程级问题）即沉淀于此，由 G4/G终 闸门 summary 列出交作者人工处理，不再自动
	// 重试（F2 尾部空转根治）。executeAudit 落新体检结果时随 FixStates 一并重置
	// （新一轮体检=新一轮问题清单）；显式复验（VerifyIssueTickets）不触碰它。
	ManualBacklog []ManualItem `json:"manual_backlog,omitempty"`
}

// FixLogEntry 是修复日志里的一条：issueID → 一行改动摘要（agent 回执 log 字段上报）。
type FixLogEntry struct {
	ID   string `json:"id"`
	Note string `json:"note"`
}

// ManualItem 是「转人工工单」的一条（R1c）：agent 修复时上报 skipped 的问题沉淀于此，
// 带无法修复的原因，由闸门 summary 列出交作者人工处理，不再进自动修复/复验循环。
type ManualItem struct {
	ID       string  `json:"id"`
	Level    string  `json:"level"`
	Area     string  `json:"area"`
	Desc     string  `json:"desc"`
	Reason   string  `json:"reason"`             // agent 上报的无法修复原因（skipped reason）
	Chapters []int64 `json:"chapters,omitempty"` // 涉及章节（issueChapterIDs 有效值），供人工定位
}

// AuditIssue 是签约体检发现的一条问题。
// issue 三枚举（卷级作用域，§0.4 冻结）：target_scope / dimension / direction。
const (
	TargetScopeActiveVolume  = "active_volume"    // 当前活跃卷，可自动修（缺省安全值）
	TargetScopeFrozenVolume  = "frozen_volume"    // 已冻结定稿卷，禁止自动回改
	TargetScopeNone          = "none"             // 全书级 / 不涉具体卷
	DirectionForwardFix      = "forward_fix"      // 前向适配：改活跃卷适配已定稿前卷（默认）
	DirectionRetconCandidate = "retcon_candidate" // 硬矛盾且无法前向消解，需作者审批
	DimensionNone            = "none"             // 不属四维的纯质量问题（节奏/爽点等）
)

type AuditIssue struct {
	ID      string `json:"id"`                 // 问题编号，如 I1
	Level   string `json:"level"`              // P0（阻断签约）/ P1（提升竞争力）
	Area    string `json:"area"`               // 展示标签（D4 降级）：如 开篇过签力 / 节奏爽点
	Desc    string `json:"desc"`               // 问题描述
	FixType string `json:"fix_type,omitempty"` // 修复类型：body（正文级，需章节重写）/ ledger（台账/卷纲/可 edit 修补）；缺省按 ledger
	// TargetScope 问题指向的卷作用域：active_volume / frozen_volume / none（枚举见上方常量）。
	TargetScope string `json:"target_scope,omitempty"`
	// Dimension 机器归属维度（四维 series bible，路由用）：character / locus_arc / timeline /
	// reader_pov；不属四维的纯质量问题填 none。
	Dimension string `json:"dimension,omitempty"`
	// Direction 修复方向：forward_fix（默认）/ retcon_candidate。
	Direction string `json:"direction,omitempty"`
	// ChapterIDs 问题涉及章节的全局阅读序号（契约 chapter_ids，R1 结构化作用域）：
	// 修复作用域分组并行与章节定位的直接依据，替代从 area/desc 文本正则猜测（F5）。
	// 空 = 无具体章节（台账/全书级）或旧数据未输出，消费方 issueChapterIDs 回退正则兜底。
	ChapterIDs []int64 `json:"chapter_ids,omitempty"`
}

// issue 的 fix_type 枚举（契约单源见 contract.go）：
// FixTypeBody「正文级」——无法靠 S5 台账/单章 edit 修补解决，必须回流到 S3 章节重写回路；
// FixTypeLedger「台账级」——台账/卷纲/可 edit 修补（缺省按 ledger 处理）。
const (
	FixTypeBody   = "body"
	FixTypeLedger = "ledger"
)

// RewriteItem 是回流到 S3 章节重写回路的一条正文级问题（来自体检 issue 的 body 类）。
// 持久化于 GateContext.RewriteBacklog，供 G4 闸门展示与 S3 写作回路消费（优先重写被标记章节）。
// TargetScope/Direction 与源 issue 一致，供重写派发的冻结卷写保护（G_freeze）判路。
type RewriteItem struct {
	ID          string  `json:"id"`
	Level       string  `json:"level"`
	Area        string  `json:"area"`
	Desc        string  `json:"desc"`
	Chapters    []int64 `json:"chapters,omitempty"` // 从 area/desc 解析出的涉及章节号（用于写作回路定位）
	TargetScope string  `json:"target_scope,omitempty"`
	Dimension   string  `json:"dimension,omitempty"`
	Direction   string  `json:"direction,omitempty"`
}

// AuditData 是 S4 签约体检的结构化结果，序列化进 GateContext.Audit。
type AuditData struct {
	Verdict string `json:"verdict"` // pass | needs_fix
	Score   int    `json:"score"`   // 总分（/40，8 维 ×5）
	Summary string `json:"summary"` // 一句话结论
	// SchemaVersion 体检产出时的 issue JSON 契约版本（contract.go 的 AuditSchemaVersion）。
	// LLM 未标注时为 0（不触发版本门，仅靠字段嗅探兜底）；显式低于当前版本即判陈旧。
	SchemaVersion int          `json:"schema_version,omitempty"`
	Issues        []AuditIssue `json:"issues"` // issue 清单
}

// Store 封装 pipeline_runs 表的读写。
type Store struct {
	db *gorm.DB
}

// NewStore 构造仓储。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Create 写入一条新流水线记录。ContextJSON 的 9 大对象拆行入表（R3d），
// 调用方持有的 run 仍保持胖视图语义（不因落库被瘦身）。
func (s *Store) Create(ctx context.Context, run *PipelineRun) error {
	thin, objects := splitGateObjects(run.ContextJSON)
	original := run.ContextJSON
	defer func() { run.ContextJSON = original }()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run.ContextJSON = thin
		if err := tx.WithContext(ctx).Create(run).Error; err != nil {
			return err
		}
		return replaceGateObjects(ctx, tx, run.RunID, objects)
	})
	if err == nil {
		return nil
	}
	// 降级：大对象表不可用（如表缺失）→ 按 R3d 前行为整袋胖 blob 落库。
	run.ContextJSON = original
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
	s.hydrateGateObjects(ctx, []*PipelineRun{&run})
	return &run, nil
}

// GetByRunID 按 RunID 精确查询。
func (s *Store) GetByRunID(ctx context.Context, runID string) (*PipelineRun, error) {
	var run PipelineRun
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).First(&run).Error; err != nil {
		return nil, err
	}
	s.hydrateGateObjects(ctx, []*PipelineRun{&run})
	return &run, nil
}

// GetActiveVolumeRun 取该小说某卷当前活跃（未结束/未中止）的卷级 scoped sub-run。
// 供 enterAudit 复用既有 scoped run，避免同卷重复派生。无则返回 gorm.ErrNotFound。
func (s *Store) GetActiveVolumeRun(ctx context.Context, novelID, volumeID int64) (*PipelineRun, error) {
	var run PipelineRun
	err := s.db.WithContext(ctx).
		Where("novel_id = ? AND volume_id = ? AND status NOT IN ?", novelID, volumeID,
			[]string{StatusFinished, StatusAborted}).
		Order("id DESC").First(&run).Error
	if err != nil {
		return nil, err
	}
	s.hydrateGateObjects(ctx, []*PipelineRun{&run})
	return &run, nil
}

// GetDetectRun 取该小说某 kind 某作用域最新的检测任务容器 run（mode='detect:<kind>'，
// R5-3 T1.2/T1.3）。volumeID 为空=全书容器（书体检/巡检·结构），非空=卷级容器（卷体检）。
// 无则返回 gorm.ErrRecordNotFound，由 ensureDetectRun 新建。
func (s *Store) GetDetectRun(ctx context.Context, novelID int64, kind string, volumeID *int64) (*PipelineRun, error) {
	q := s.db.WithContext(ctx).Where("novel_id = ? AND mode = ?", novelID, detectModeOf(kind))
	if volumeID == nil {
		q = q.Where("volume_id IS NULL")
	} else {
		q = q.Where("volume_id = ?", *volumeID)
	}
	var run PipelineRun
	if err := q.Order("id DESC").First(&run).Error; err != nil {
		return nil, err
	}
	s.hydrateGateObjects(ctx, []*PipelineRun{&run})
	return &run, nil
}

// GetLatestDetectRun 取该小说某 kind 最近一次检测的容器 run（不限作用域）：
// 检测状态查询（T1.3）在无 scopeID 时取「最近一次检测」结果用。无则返回
// gorm.ErrRecordNotFound。
func (s *Store) GetLatestDetectRun(ctx context.Context, novelID int64, kind string) (*PipelineRun, error) {
	var run PipelineRun
	if err := s.db.WithContext(ctx).
		Where("novel_id = ? AND mode = ?", novelID, detectModeOf(kind)).
		Order("id DESC").First(&run).Error; err != nil {
		return nil, err
	}
	s.hydrateGateObjects(ctx, []*PipelineRun{&run})
	return &run, nil
}

// HasActiveScopedChild 判断指定 book-run 是否仍有活跃（未结束/未中止）的卷级 scoped sub-run。
// 供启动恢复自愈：waiting 的 book-run 若无活跃子 run，说明子 run 已收口而唤醒前崩溃，需唤醒。
func (s *Store) HasActiveScopedChild(ctx context.Context, parentRunID string) (bool, error) {
	var c int64
	err := s.db.WithContext(ctx).Model(&PipelineRun{}).
		Where("parent_run_id = ? AND status NOT IN ?", parentRunID,
			[]string{StatusFinished, StatusAborted}).
		Count(&c).Error
	if err != nil {
		return false, err
	}
	return c > 0, nil
}

// ListActiveScopedChildren 列出指定 book-run 下全部活跃（未结束/未中止）的卷级 scoped sub-run。
// 供 book-run 终止时联动清理孤儿子 run。
func (s *Store) ListActiveScopedChildren(ctx context.Context, parentRunID string) ([]*PipelineRun, error) {
	var runs []*PipelineRun
	err := s.db.WithContext(ctx).
		Where("parent_run_id = ? AND status NOT IN ?", parentRunID,
			[]string{StatusFinished, StatusAborted}).
		Order("id DESC").Find(&runs).Error
	if err != nil {
		return nil, err
	}
	s.hydrateGateObjects(ctx, runs)
	return runs, nil
}

// ListByStatus 列出处于指定状态的全部流水线运行（供启动恢复扫描僵尸运行）。
func (s *Store) ListByStatus(ctx context.Context, status string) ([]*PipelineRun, error) {
	var runs []*PipelineRun
	if err := s.db.WithContext(ctx).Where("status = ?", status).Find(&runs).Error; err != nil {
		return nil, err
	}
	s.hydrateGateObjects(ctx, runs)
	return runs, nil
}

// writeContext 更新 run 的 context_json 与其余列：9 大对象拆行入表+落瘦身 blob
// （同事务，整袋替换语义：传入上下文为全量，键缺即清）。大对象表不可用（如表缺失）
// 时退回「整袋落胖 blob」的 R3d 前行为，并尽力清旧行避免表行遮蔽新值（降级零提示）。
func (s *Store) writeContext(ctx context.Context, runID, contextJSON string, updates map[string]any) error {
	thin, objects := splitGateObjects(contextJSON)
	updates["context_json"] = thin
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := replaceGateObjects(ctx, tx, runID, objects); err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&PipelineRun{}).
			Where("run_id = ?", runID).
			Updates(updates).Error
	})
	if err == nil {
		return nil
	}
	// 降级：大对象表不可用（如表缺失）→ 按 R3d 前行为整袋落胖 blob。
	_ = s.db.WithContext(ctx).Where("run_id = ?", runID).Delete(&GateObject{}).Error
	updates["context_json"] = contextJSON
	return s.db.WithContext(ctx).Model(&PipelineRun{}).
		Where("run_id = ?", runID).
		Updates(updates).Error
}

// SetStatus 仅更新状态与上下文（不改变阶段）。
func (s *Store) SetStatus(ctx context.Context, runID, status, contextJSON string) error {
	return s.writeContext(ctx, runID, contextJSON, map[string]any{
		"status":     status,
		"updated_at": time.Now(),
	})
}

// SetStageStatus 同时更新当前阶段与状态（阶段推进时用）。
func (s *Store) SetStageStatus(ctx context.Context, runID, stage, status, contextJSON string) error {
	return s.writeContext(ctx, runID, contextJSON, map[string]any{
		"current_stage": stage,
		"status":        status,
		"updated_at":    time.Now(),
	})
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
// 读到的是胖视图（GetByRunID 已合并大对象），写回经 writeContext 拆合——
// 大对象不会因追加决策被就地覆盖丢失（R3d）。
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
	return s.writeContext(ctx, runID, string(b), map[string]any{
		"updated_at": time.Now(),
	})
}
