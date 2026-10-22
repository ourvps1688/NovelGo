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

// Service 是 retcon 审批闸门（WBS 3.1）：候选的沉淀入口 + 显式裁决 + 受控编辑
// 生命周期驱动。所有状态迁移都只能经本 Service 的显式方法发生（见 state.go）。
type Service struct {
	store    *Store
	restorer RestorerFunc
}

// NewService 构造 retcon 闸门服务。
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// SetRestorer 注入回滚执行体（3.2 接入快照 Restore）。
func (s *Service) SetRestorer(r RestorerFunc) { s.restorer = r }

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
func (s *Service) Rollback(ctx context.Context, id int64, actor, note string) (*RetconCase, error) {
	if s.restorer == nil {
		return nil, ErrNoRestorer
	}
	cur, err := s.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cur.RollbackSnapshotID == nil {
		return nil, ErrRollbackPointRequired
	}
	if err := s.restorer(ctx, cur.NovelID, *cur.RollbackSnapshotID); err != nil {
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

// truncateRunes 截取前 n 个 rune（超出加省略号）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
