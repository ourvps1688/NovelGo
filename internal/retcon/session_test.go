package retcon

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

// sessionHooks 记录三执行体调用（回滚点固化 / 差异摘要 / 回滚还原），可注入失败。
type sessionHooks struct {
	commits    [][2]int64 // (novelID, volumeID)
	commitSeq  int64      // 返回的快照 id 序列
	commitErr  error
	diffText   string
	diffErr    error
	diffCalls  []int64
	restores   []int64
	restoreErr error
}

func (h *sessionHooks) install(s *Service) {
	s.SetCommitSnapshot(func(_ context.Context, novelID, volumeID int64) (int64, error) {
		if h.commitErr != nil {
			return 0, h.commitErr
		}
		h.commits = append(h.commits, [2]int64{novelID, volumeID})
		h.commitSeq++
		return h.commitSeq, nil
	})
	s.SetDiffSummary(func(_ context.Context, snapshotID int64) (string, error) {
		h.diffCalls = append(h.diffCalls, snapshotID)
		return h.diffText, h.diffErr
	})
	s.SetRestorer(func(_ context.Context, _ int64, snapshotID int64) error {
		h.restores = append(h.restores, snapshotID)
		return h.restoreErr
	})
}

// approvedWithVolume 建一条已批准、已绑定冻结卷的案件。
func approvedWithVolume(t *testing.T, db *gorm.DB, svc *Service) *RetconCase {
	t.Helper()
	seedVol(t, db, 1, 1, true)
	c, err := svc.CreateCandidate(context.Background(), CreateInput{
		NovelID: 1, VolumeID: volID(1), Title: "吃书申请",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	c, err = svc.Approve(context.Background(), c.ID, "author", "批准受控回改")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	return c
}

// TestBeginControlledEditCommitsRollbackPoint 验证受控编辑入口（WBS 3.2）：
// 绑定冻结卷 → 固化回滚点快照（第一次 Commit）→ editing；回滚点=固化返回的快照 id。
func TestBeginControlledEditCommitsRollbackPoint(t *testing.T) {
	ctx := context.Background()
	db := openRetconDB(t)
	svc := newTestService(db)
	h := &sessionHooks{}
	h.install(svc)
	c := approvedWithVolume(t, db, svc)

	got, err := svc.BeginControlledEdit(ctx, c.ID, 1, "author")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if got.Status != StatusEditing {
		t.Errorf("status = %q, want editing", got.Status)
	}
	if got.RollbackSnapshotID == nil || *got.RollbackSnapshotID != 1 {
		t.Errorf("回滚点 = %v, want 快照 1", got.RollbackSnapshotID)
	}
	if len(h.commits) != 1 || h.commits[0] != [2]int64{1, 1} {
		t.Errorf("commits = %v, want 一次 (novel=1, volume=1)", h.commits)
	}

	// 目标卷不一致 / 未接固化执行体 / 目标未冻结 / 状态不可绑。
	if _, err := svc.BeginControlledEdit(ctx, c.ID, 2, "author"); !errors.Is(err, ErrVolumeMismatch) {
		t.Errorf("换卷开窗 = %v, want ErrVolumeMismatch", err)
	}
	seedVol(t, db, 2, 1, false)
	c2, _ := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, VolumeID: volID(2), Title: "x"})
	if c2 != nil {
		t.Error("未冻结卷创建应失败")
	}
	plain := newTestService(db) // 无固化执行体
	cc, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "y"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := plain.BeginControlledEdit(ctx, cc.ID, 1, "author"); !errors.Is(err, ErrNoCommitter) {
		t.Errorf("未接固化执行体开窗 = %v, want ErrNoCommitter", err)
	}
	if _, err := svc.BindTargetVolume(ctx, got.ID, 1); !errors.Is(err, ErrNotEditable) {
		t.Errorf("editing 后换绑 = %v, want ErrNotEditable", err)
	}
}

// TestRecordWithDiffAndCanonCommit 验证留痕收口（WBS 3.2「每次 retcon 有 diff + 回滚点」）：
// 差异摘要按回滚点固化、终态前再 Commit 刷新 canon（既定事实注入源跟上 retcon 后状态）；
// 空差异以「（无台账变更）」留痕。
func TestRecordWithDiffAndCanonCommit(t *testing.T) {
	ctx := context.Background()
	db := openRetconDB(t)
	svc := newTestService(db)
	h := &sessionHooks{diffText: "characters: +0 -0 ~1\n  ~ #1 alive: true → false"}
	h.install(svc)
	c := approvedWithVolume(t, db, svc)
	editing, err := svc.BeginControlledEdit(ctx, c.ID, 1, "author")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	recorded, err := svc.RecordWithDiff(ctx, editing.ID, "author")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if recorded.Status != StatusRecorded {
		t.Errorf("status = %q, want recorded", recorded.Status)
	}
	if recorded.DiffSummary != h.diffText {
		t.Errorf("diff 摘要 = %q", recorded.DiffSummary)
	}
	if len(h.diffCalls) != 1 || h.diffCalls[0] != *editing.RollbackSnapshotID {
		t.Errorf("diffCalls = %v, want 回滚点 %d", h.diffCalls, *editing.RollbackSnapshotID)
	}
	if len(h.commits) != 2 {
		t.Errorf("commits = %v, want 开窗+收口各一次", h.commits)
	}

	// 空差异 →「（无台账变更）」留痕。
	c2 := approvedWithVolume(t, db, svc)
	ed2, err := svc.BeginControlledEdit(ctx, c2.ID, 1, "author")
	if err != nil {
		t.Fatalf("begin 2: %v", err)
	}
	h.diffText = "  "
	rec2, err := svc.RecordWithDiff(ctx, ed2.ID, "author")
	if err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if rec2.DiffSummary != "（无台账变更）" {
		t.Errorf("空差异摘要 = %q", rec2.DiffSummary)
	}

	// 钩子缺失/未固化回滚点时拒绝自动收口。
	plain := newTestService(db)
	pc, _ := plain.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "z"})
	if _, err := plain.RecordWithDiff(ctx, pc.ID, "author"); !errors.Is(err, ErrNoDiffer) {
		t.Errorf("未接差异体收口 = %v, want ErrNoDiffer", err)
	}
	h2 := &sessionHooks{}
	h2.install(plain)
	if _, err := plain.RecordWithDiff(ctx, pc.ID, "author"); !errors.Is(err, ErrRollbackPointRequired) {
		t.Errorf("未开窗收口 = %v, want ErrRollbackPointRequired", err)
	}
}

// TestRollbackCommitsCanonAfterRestore 验证回滚次序：先还原（Restore）→ 再固化回滚后
// canon → 才落终态；canon 固化失败不落状态（重试安全，Restore 幂等）。
func TestRollbackCommitsCanonAfterRestore(t *testing.T) {
	ctx := context.Background()
	db := openRetconDB(t)
	svc := newTestService(db)
	h := &sessionHooks{}
	h.install(svc)
	c := approvedWithVolume(t, db, svc)
	ed, err := svc.BeginControlledEdit(ctx, c.ID, 1, "author")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	rec, err := svc.RecordWithDiff(ctx, ed.ID, "author")
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	h.commits = nil
	rolled, err := svc.Rollback(ctx, rec.ID, "author", "情节改道，撤销回改")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Status != StatusRolledBack {
		t.Errorf("status = %q, want rolled_back", rolled.Status)
	}
	if len(h.restores) != 1 || h.restores[0] != *ed.RollbackSnapshotID {
		t.Errorf("restores = %v, want 回滚点 %d", h.restores, *ed.RollbackSnapshotID)
	}
	if len(h.commits) != 1 {
		t.Errorf("回滚后应再固化一份 canon 快照: %v", h.commits)
	}

	// canon 固化失败 → 不落终态。
	c2 := approvedWithVolume(t, db, svc)
	ed2, _ := svc.BeginControlledEdit(ctx, c2.ID, 1, "author")
	rec2, _ := svc.RecordWithDiff(ctx, ed2.ID, "author")
	h.commitErr = errors.New("commit boom")
	if _, err := svc.Rollback(ctx, rec2.ID, "author", "撤销"); err == nil {
		t.Error("canon 固化失败应回滚失败")
	}
	after, _ := svc.store.GetByID(ctx, rec2.ID)
	if after.Status != StatusRecorded {
		t.Errorf("canon 失败后状态 = %q, want recorded", after.Status)
	}
}
