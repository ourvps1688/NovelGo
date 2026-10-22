package retcon

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openRetconDB 建内存 DB + retcon_cases 表；volumes 用最小 schema（冻结守卫只读 frozen），
// 避免 retcon 包 import volume 包。
func openRetconDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&RetconCase{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE TABLE volumes (
		id integer PRIMARY KEY,
		novel_id integer NOT NULL,
		frozen integer NOT NULL DEFAULT 0
	)`).Error; err != nil {
		t.Fatalf("create volumes: %v", err)
	}
	return db
}

func newTestService(db *gorm.DB) *Service {
	return NewService(NewStore(db))
}

func seedVol(t *testing.T, db *gorm.DB, id, novelID int64, frozen bool) {
	t.Helper()
	f := 0
	if frozen {
		f = 1
	}
	if err := db.Exec("INSERT OR IGNORE INTO volumes (id, novel_id, frozen) VALUES (?, ?, ?)", id, novelID, f).Error; err != nil {
		t.Fatalf("seed volume: %v", err)
	}
}

func volID(id int64) *int64 { return &id }

// TestCreateCandidateDedupe 验证 pipeline 同一 issue 反复沉淀幂等（未终态不重复建），
// 终态后可再立新案；author 候选不参与去重。
func TestCreateCandidateDedupe(t *testing.T) {
	ctx := context.Background()
	db := openRetconDB(t)
	svc := newTestService(db)

	in := CreateInput{NovelID: 1, Origin: OriginPipeline, IssueID: "I1",
		Title: "硬矛盾", Description: "第12章修为设定与冻结前卷冲突", Direction: "retcon_candidate"}
	first, err := svc.CreateCandidate(ctx, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := svc.CreateCandidate(ctx, in)
	if err != nil {
		t.Fatalf("dedupe create: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("同一 issue 未终态重复沉淀应幂等：%d vs %d", first.ID, second.ID)
	}

	if _, err := svc.Reject(ctx, first.ID, "author", "前向适配已消解"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	third, err := svc.CreateCandidate(ctx, in)
	if err != nil {
		t.Fatalf("post-reject create: %v", err)
	}
	if third.ID == first.ID {
		t.Error("终态后同一 issue 应可再立新案")
	}
	if third.Status != StatusCandidate {
		t.Errorf("status = %q, want candidate", third.Status)
	}

	// author 候选不按 issueID 去重。
	a1, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "主动吃书"})
	if err != nil {
		t.Fatalf("author create: %v", err)
	}
	a2, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "主动吃书"})
	if err != nil {
		t.Fatalf("author create 2: %v", err)
	}
	if a1.ID == a2.ID {
		t.Error("author 候选不应参与去重")
	}
	if a1.Origin != OriginAuthor {
		t.Errorf("origin = %q, want author", a1.Origin)
	}
}

// TestCreateCandidateTitleFallback 验证 Title 缺省取 Description 前 80 rune。
func TestCreateCandidateTitleFallback(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(openRetconDB(t))
	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Description: "改前卷第3章疤痕设定才能消解"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.Title != "改前卷第3章疤痕设定才能消解" {
		t.Errorf("title = %q", c.Title)
	}
	if _, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1}); err == nil {
		t.Error("title/description 全空应拒绝")
	}
	if _, err := svc.CreateCandidate(ctx, CreateInput{Title: "x"}); err == nil {
		t.Error("缺 novel_id 应拒绝")
	}
}

// TestApproveRejectRequireNoteAndCandidate 验证：裁决必须留痕理由、只能从 candidate 迁移。
func TestApproveRejectRequireNoteAndCandidate(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(openRetconDB(t))
	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "吃书申请"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := svc.Approve(ctx, c.ID, "author", "  "); !errors.Is(err, ErrNoteRequired) {
		t.Errorf("空理由批准 = %v, want ErrNoteRequired", err)
	}
	if _, err := svc.Reject(ctx, c.ID, "author", ""); !errors.Is(err, ErrNoteRequired) {
		t.Errorf("空理由驳回 = %v, want ErrNoteRequired", err)
	}

	approved, err := svc.Approve(ctx, c.ID, "author", "确需让已死角色复活，批准回改")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != StatusApproved || approved.DecisionNote == "" || approved.DecidedAt == nil {
		t.Errorf("批准后状态/留痕缺失: %+v", approved)
	}
	// 已批准的案不得再批/再驳（非 candidate）。
	if _, err := svc.Approve(ctx, c.ID, "author", "再来一次"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("重复批准 = %v, want ErrInvalidTransition", err)
	}
	if _, err := svc.Reject(ctx, c.ID, "author", "反悔"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("批准后驳回 = %v, want ErrInvalidTransition", err)
	}
}

// TestApproveRequiresFrozenVolume 验证 retcon 只针对已冻结卷：指定目标卷时，
// 未冻结/不存在的卷拒绝批准与发起。
func TestApproveRequiresFrozenVolume(t *testing.T) {
	ctx := context.Background()
	db := openRetconDB(t)
	svc := newTestService(db)
	seedVol(t, db, 1, 1, true)  // 已冻结
	seedVol(t, db, 2, 1, false) // 未冻结

	if _, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, VolumeID: volID(2), Title: "改活跃卷"}); !errors.Is(err, ErrNotFrozen) {
		t.Errorf("对未冻结卷发起 = %v, want ErrNotFrozen", err)
	}
	if _, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, VolumeID: volID(9), Title: "改幽灵卷"}); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("对不存在卷发起 = %v, want ErrRecordNotFound", err)
	}

	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, VolumeID: volID(1), Title: "改冻结卷"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 提案后卷被解冻 → 批准必须拒绝（回改目标不再是既定事实）。
	if err := db.Exec("UPDATE volumes SET frozen = 0 WHERE id = 1").Error; err != nil {
		t.Fatalf("unfreeze: %v", err)
	}
	if _, err := svc.Approve(ctx, c.ID, "author", "批准"); !errors.Is(err, ErrNotFrozen) {
		t.Errorf("对已解冻卷批准 = %v, want ErrNotFrozen", err)
	}
	if err := db.Exec("UPDATE volumes SET frozen = 1 WHERE id = 1").Error; err != nil {
		t.Fatalf("refreeze: %v", err)
	}
	if _, err := svc.Approve(ctx, c.ID, "author", "批准"); err != nil {
		t.Errorf("冻结卷批准失败: %v", err)
	}
}

// TestControlledEditLifecycle 验证受控编辑→留痕→回滚全链：回滚点必填、差异摘要必填、
// 回滚必须先接执行体且先还原后落状态。
func TestControlledEditLifecycle(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(openRetconDB(t))
	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "吃书申请"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Approve(ctx, c.ID, "author", "批准受控回改"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// 回滚点缺失拒绝开改。
	if _, err := svc.BeginEdit(ctx, c.ID, "author", 0); !errors.Is(err, ErrRollbackPointRequired) {
		t.Errorf("无回滚点开改 = %v, want ErrRollbackPointRequired", err)
	}
	editing, err := svc.BeginEdit(ctx, c.ID, "author", 42)
	if err != nil {
		t.Fatalf("begin edit: %v", err)
	}
	if editing.Status != StatusEditing || editing.RollbackSnapshotID == nil || *editing.RollbackSnapshotID != 42 {
		t.Errorf("受控编辑态/回滚点错误: %+v", editing)
	}
	// 缺差异摘要不收口。
	if _, err := svc.Record(ctx, c.ID, "author", " "); !errors.Is(err, ErrDiffRequired) {
		t.Errorf("空差异收口 = %v, want ErrDiffRequired", err)
	}
	recorded, err := svc.Record(ctx, c.ID, "author", "characters#1.alive: true→false；第3章补叙")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if recorded.Status != StatusRecorded || recorded.DiffSummary == "" || recorded.AppliedAt == nil {
		t.Errorf("留痕收口字段缺失: %+v", recorded)
	}

	// 未接回滚执行体拒绝回滚（绝不名义回滚）。
	if _, err := svc.Rollback(ctx, c.ID, "author", "撤销"); !errors.Is(err, ErrNoRestorer) {
		t.Errorf("无执行体回滚 = %v, want ErrNoRestorer", err)
	}

	// 接入假执行体：先还原后落状态，参数为（novelID, 回滚点快照 id）。
	var gotNovel, gotSnap int64
	var called bool
	svc.SetRestorer(func(_ context.Context, novelID, snapshotID int64) error {
		called, gotNovel, gotSnap = true, novelID, snapshotID
		return nil
	})
	rolled, err := svc.Rollback(ctx, c.ID, "author", "情节线改道，撤销回改")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if !called || gotNovel != 1 || gotSnap != 42 {
		t.Errorf("回滚执行体未按回滚点还原: called=%v novel=%d snap=%d", called, gotNovel, gotSnap)
	}
	if rolled.Status != StatusRolledBack || rolled.RolledBackAt == nil {
		t.Errorf("回滚终态字段缺失: %+v", rolled)
	}
	// 终态不可再动。
	if _, err := svc.Record(ctx, c.ID, "author", "补记"); !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("回滚后再留痕 = %v, want ErrInvalidTransition", err)
	}
}

// TestRollbackRestorerErrorKeepsState 验证还原失败不落状态（不「名义回滚」骗状态）。
func TestRollbackRestorerErrorKeepsState(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(openRetconDB(t))
	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, Title: "吃书申请"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Approve(ctx, c.ID, "author", "批准"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := svc.BeginEdit(ctx, c.ID, "author", 7); err != nil {
		t.Fatalf("begin edit: %v", err)
	}
	if _, err := svc.Record(ctx, c.ID, "author", "diff"); err != nil {
		t.Fatalf("record: %v", err)
	}

	boom := errors.New("restore boom")
	svc.SetRestorer(func(context.Context, int64, int64) error { return boom })
	if _, err := svc.Rollback(ctx, c.ID, "author", "撤销"); !errors.Is(err, boom) {
		t.Errorf("回滚失败应透传执行体错误: %v", err)
	}
	got, err := svc.store.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusRecorded {
		t.Errorf("还原失败后状态 = %q, want recorded（不得落 rolled_back）", got.Status)
	}
	if got.RolledBackAt != nil {
		t.Error("还原失败不得写回滚时间戳")
	}
}
