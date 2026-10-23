package retcon

import (
	"context"
	"errors"
	"strings"
	"time"
)

// titleFallbackRunes 是 Title 缺省时从 Description 截取的 rune 数。
const titleFallbackRunes = 80

// RestorerFunc 是回滚执行体：把台账整体还原到回滚点快照（3.2 接入 snapshot.Restore）。
// 未接入时 Rollback 拒绝执行（绝不「名义回滚」骗状态）。
type RestorerFunc func(ctx context.Context, novelID, snapshotID int64) error

// StyleRestorerFunc 是风格案（KindStyle）的回滚执行体：只还原 voice_dimensions
// 到回滚点快照（P2 T4.2 接入 snapshot.RestoreStyle），事实层零触碰。与 RestorerFunc
// 同签名但语义独立，由 Rollback 按 Kind 路由（存量 fact 案仍走全量 Restorer）。
type StyleRestorerFunc func(ctx context.Context, novelID, snapshotID int64) error

// CommitSnapshotFunc 把当前台账固化为版本化快照并返回其 id（回滚点/留痕固化用，
// 3.2 接入 snapshot.Commit）。
type CommitSnapshotFunc func(ctx context.Context, novelID, volumeID int64) (int64, error)

// DiffSummaryFunc 返回回滚点快照与当前台账的差异摘要文本（3.2 接入
// snapshot.DiffAgainstCurrent + FormatDelta）。
type DiffSummaryFunc func(ctx context.Context, snapshotID int64) (string, error)

// Service 是 retcon 审批闸门（WBS 3.1）：候选的沉淀入口 + 显式裁决 + 受控编辑
// 生命周期驱动。所有状态迁移都只能经本 Service 的显式方法发生（见 state.go）。
type Service struct {
	store          *Store
	restorer       RestorerFunc
	styleRestorer  StyleRestorerFunc
	commitSnapshot CommitSnapshotFunc
	diffSummary    DiffSummaryFunc
}

// NewService 构造 retcon 闸门服务。
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// SetRestorer 注入回滚执行体（3.2 接入快照 Restore）。
func (s *Service) SetRestorer(r RestorerFunc) { s.restorer = r }

// SetStyleRestorer 注入风格案回滚执行体（P2 T4.2 接入 snapshot.RestoreStyle）。
// 未接入时风格案 Rollback 拒绝执行（绝不「名义回滚」骗状态）。
func (s *Service) SetStyleRestorer(r StyleRestorerFunc) { s.styleRestorer = r }

// SetCommitSnapshot 注入回滚点/留痕快照固化执行体（3.2 接入 snapshot.Commit）。
func (s *Service) SetCommitSnapshot(fn CommitSnapshotFunc) { s.commitSnapshot = fn }

// SetDiffSummary 注入差异摘要计算体（3.2 接入 snapshot.DiffAgainstCurrent + FormatDelta）。
func (s *Service) SetDiffSummary(fn DiffSummaryFunc) { s.diffSummary = fn }

// GetByID 按 id 取案件。
func (s *Service) GetByID(ctx context.Context, id int64) (*RetconCase, error) {
	return s.store.GetByID(ctx, id)
}

// ListByNovel 返回该小说的全部 retcon 案件（id 倒序），供审批闸门界面与巡检回溯。
func (s *Service) ListByNovel(ctx context.Context, novelID int64) ([]RetconCase, error) {
	return s.store.ListByNovel(ctx, novelID)
}

// CreateCandidate 发起一条 retcon 候选（状态机起点）。
// pipeline 来源且带 IssueID 时按（issueID, description）对未终态案件去重，重复沉淀幂等
// 返回既有案件（体检每轮重算 RetconBacklog，不得堆重复候选）。目标卷若指定，必须是
// 已冻结卷（retcon 只针对已定稿前卷）。
func (s *Service) CreateCandidate(ctx context.Context, in CreateInput) (*RetconCase, error) {
	if in.NovelID <= 0 {
		return nil, errors.New("retcon: novel_id 必填")
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginAuthor
	}
	// Kind 缺省 fact（存量案行为逐字不变）；只接受枚举单源两值，非法值拒绝（宁缺勿错）。
	kind := in.Kind
	if kind == "" {
		kind = KindFact
	}
	if kind != KindFact && kind != KindStyle {
		return nil, errors.New("retcon: kind 只支持 " + KindFact + "/" + KindStyle)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = truncateRunes(strings.TrimSpace(in.Description), titleFallbackRunes)
	}
	if title == "" {
		return nil, errors.New("retcon: title/description 至少填一项")
	}
	if in.VolumeID != nil {
		frozen, err := s.store.VolumeFrozen(ctx, in.NovelID, *in.VolumeID)
		if err != nil {
			return nil, err
		}
		if !frozen {
			return nil, ErrNotFrozen
		}
	}
	if origin == OriginPipeline && in.IssueID != "" {
		if exist, err := s.store.FindOpenMatch(ctx, in.NovelID, in.IssueID, in.Description); err != nil {
			return nil, err
		} else if exist != nil {
			return exist, nil
		}
	}
	c := &RetconCase{
		NovelID:     in.NovelID,
		VolumeID:    in.VolumeID,
		Origin:      origin,
		Kind:        kind,
		IssueID:     in.IssueID,
		TargetScope: in.TargetScope,
		Dimension:   in.Dimension,
		Direction:   in.Direction,
		Title:       title,
		Description: in.Description,
		Status:      StatusCandidate,
	}
	if err := s.store.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Approve 显式批准一条候选（candidate→approved）：retcon 必须作者拍板，绝不自动。
// note 必填（裁决理由留痕）；目标卷若已指定，必须仍是已冻结卷。
func (s *Service) Approve(ctx context.Context, id int64, actor, note string) (*RetconCase, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, ErrNoteRequired
	}
	cur, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.VolumeID != nil {
		frozen, ferr := s.store.VolumeFrozen(ctx, cur.NovelID, *cur.VolumeID)
		if ferr != nil {
			return nil, ferr
		}
		if !frozen {
			return nil, ErrNotFrozen
		}
	}
	now := time.Now()
	return s.store.Advance(ctx, id, StatusApproved, func(c *RetconCase) {
		c.DecisionNote = note
		c.DecidedBy = actor
		c.DecidedAt = &now
	})
}

// Reject 显式驳回一条候选（candidate→rejected，终态）：走前向适配，不回改前卷。
// note 必填（裁决理由留痕）。
func (s *Service) Reject(ctx context.Context, id int64, actor, note string) (*RetconCase, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, ErrNoteRequired
	}
	now := time.Now()
	return s.store.Advance(ctx, id, StatusRejected, func(c *RetconCase) {
		c.DecisionNote = note
		c.DecidedBy = actor
		c.DecidedAt = &now
	})
}

// BeginEdit 进入受控编辑（approved→editing）：必须先固化回滚点（rollbackSnapshotID，
// 卷冻结提交的 committed snapshot），此后才允许在受控窗口内回改冻结卷（3.2 留痕编辑）。
func (s *Service) BeginEdit(ctx context.Context, id int64, actor string, rollbackSnapshotID int64) (*RetconCase, error) {
	if rollbackSnapshotID <= 0 {
		return nil, ErrRollbackPointRequired
	}
	snapID := rollbackSnapshotID
	return s.store.Advance(ctx, id, StatusEditing, func(c *RetconCase) {
		c.RollbackSnapshotID = &snapID
		c.AppliedBy = actor
	})
}

// Record 留痕收口（editing→recorded）：受控编辑完成，差异摘要（diffSummary）必填固化，
// 与回滚点共同构成「每次 retcon 有 diff + 回滚点」（WBS 3.2 验收）。
func (s *Service) Record(ctx context.Context, id int64, actor, diffSummary string) (*RetconCase, error) {
	diffSummary = strings.TrimSpace(diffSummary)
	if diffSummary == "" {
		return nil, ErrDiffRequired
	}
	now := time.Now()
	return s.store.Advance(ctx, id, StatusRecorded, func(c *RetconCase) {
		c.DiffSummary = diffSummary
		c.AppliedBy = actor
		c.AppliedAt = &now
	})
}

// Rollback 回滚（recorded→rolled_back，终态）：先经回滚执行体把台账还原到回滚点快照，
// 还原成功后才落状态（绝不「名义回滚」骗状态）。执行体未接入（3.2 前）拒绝执行。
// 执行体按 Kind 路由（P2 T4.2）：style 案走 StyleRestorer（只还原 voice 行），
// fact/存量空 kind 案走全量 Restorer（行为逐字不变）。
// 回滚成功后（落状态前）再固化一份快照，让「最新快照=既定事实注入源」跟上回滚后的 canon。
func (s *Service) Rollback(ctx context.Context, id int64, actor, note string) (*RetconCase, error) {
	cur, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// 两个钩子虽同签名但具名类型不同，经底层类型变量路由（fact/存量空 kind→全量）。
	var restore func(ctx context.Context, novelID, snapshotID int64) error
	if cur.Kind == KindStyle {
		restore = s.styleRestorer
	} else {
		restore = s.restorer
	}
	if restore == nil {
		return nil, ErrNoRestorer
	}
	if cur.RollbackSnapshotID == nil {
		return nil, ErrRollbackPointRequired
	}
	if err := restore(ctx, cur.NovelID, *cur.RollbackSnapshotID); err != nil {
		return nil, err
	}
	// 还原已生效（Restore 幂等，失败重试安全）；固化失败则不落状态，避免状态与事实脱节。
	if err := s.commitCanon(ctx, cur); err != nil {
		return nil, err
	}
	now := time.Now()
	return s.store.Advance(ctx, id, StatusRolledBack, func(c *RetconCase) {
		c.RolledBackBy = actor
		c.RolledBackAt = &now
		if strings.TrimSpace(note) != "" {
			c.DecisionNote = strings.TrimSpace(note)
		}
	})
}

// BindTargetVolume 把案件绑定到目标冻结卷（candidate/approved 态可绑可换；editing 之后
// 目标卷随回滚点锁定）。目标卷必须是已冻结卷（retcon 只针对已定稿前卷）。
func (s *Service) BindTargetVolume(ctx context.Context, id, volumeID int64) (*RetconCase, error) {
	cur, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.Status != StatusCandidate && cur.Status != StatusApproved {
		return nil, ErrNotEditable
	}
	frozen, err := s.store.VolumeFrozen(ctx, cur.NovelID, volumeID)
	if err != nil {
		return nil, err
	}
	if !frozen {
		return nil, ErrNotFrozen
	}
	if err := s.store.SetVolume(ctx, id, volumeID); err != nil {
		return nil, err
	}
	return s.store.GetByID(ctx, id)
}

// BeginControlledEdit 受控编辑入口（approved→editing，WBS 3.2）：绑定目标冻结卷后
// 固化回滚点快照（CommitSnapshot 钩子），再进 editing 窗口。回滚点固化执行体未接入
// 时拒绝开窗（受控编辑必须可回滚）。
func (s *Service) BeginControlledEdit(ctx context.Context, id, volumeID int64, actor string) (*RetconCase, error) {
	if s.commitSnapshot == nil {
		return nil, ErrNoCommitter
	}
	cur, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.VolumeID != nil && *cur.VolumeID != volumeID {
		return nil, ErrVolumeMismatch
	}
	if _, err := s.BindTargetVolume(ctx, id, volumeID); err != nil {
		return nil, err
	}
	snapID, err := s.commitSnapshot(ctx, cur.NovelID, volumeID)
	if err != nil {
		return nil, err
	}
	return s.BeginEdit(ctx, id, actor, snapID)
}

// RecordWithDiff 留痕收口（editing→recorded，WBS 3.2）：以回滚点快照与当前台账的
// 差异摘要自动留痕（DiffSummary 钩子），并再固化一份快照让「最新快照=既定事实注入源」
// 跟上 retcon 后的 canon，然后落状态。无台账变更时以「（无台账变更）」留痕收口。
func (s *Service) RecordWithDiff(ctx context.Context, id int64, actor string) (*RetconCase, error) {
	if s.diffSummary == nil {
		return nil, ErrNoDiffer
	}
	cur, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.RollbackSnapshotID == nil {
		return nil, ErrRollbackPointRequired
	}
	summary, err := s.diffSummary(ctx, *cur.RollbackSnapshotID)
	if err != nil {
		return nil, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = "（无台账变更）"
	}
	if err := s.commitCanon(ctx, cur); err != nil {
		return nil, err
	}
	return s.Record(ctx, id, actor, summary)
}

// commitCanon 固化「retcon 后的 canon」快照（Record/Rollback 终态前各一次），让
// LatestSnapshot（既定事实注入源，WBS 2.3）跟上 retcon/回滚后的台账现状。
// 钩子未接入或案件未绑定目标卷时跳过（3.1 手动留痕路径不受影响）。
func (s *Service) commitCanon(ctx context.Context, cur *RetconCase) error {
	if s.commitSnapshot == nil || cur.VolumeID == nil {
		return nil
	}
	_, err := s.commitSnapshot(ctx, cur.NovelID, *cur.VolumeID)
	return err
}

// truncateRunes 截取前 n 个 rune（超出加省略号）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
