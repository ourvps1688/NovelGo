package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedFreezeFixtures 建「刚跨入新卷」形状：v1 两章（已写完）、v2 一章（当前卷）。
// 最新两章分属不同卷（defaultVolumeBoundary=true）；当前卷=v2，刚完结的前一卷=v1。
func seedFreezeFixtures(t *testing.T, db *gorm.DB, novelID int64) (int64, int64) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	v1 := &volume.Volume{NovelID: novelID, Name: "第一卷", SortOrder: 1}
	v2 := &volume.Volume{NovelID: novelID, Name: "第二卷", SortOrder: 2}
	require.NoError(t, db.Create(v1).Error)
	require.NoError(t, db.Create(v2).Error)
	for i := 1; i <= 2; i++ {
		require.NoError(t, db.Create(&chapter.Chapter{
			NovelID: novelID, Title: fmt.Sprintf("v1-%d", i), SortOrder: i, VolumeID: &v1.ID,
		}).Error)
	}
	require.NoError(t, db.Create(&chapter.Chapter{
		NovelID: novelID, Title: "v2-1", SortOrder: 1, VolumeID: &v2.ID,
	}).Error)
	return v1.ID, v2.ID
}

// freezeRecorder 记录 G_freeze 自动冻结调用；failWith 非空时返回该错误
// （模拟 2.4 冻结前台账审计拒绝）。
type freezeRecorder struct {
	calls    [][2]int64
	failWith error
}

func (r *freezeRecorder) freeze(_ context.Context, novelID, volumeID int64) error {
	r.calls = append(r.calls, [2]int64{novelID, volumeID})
	return r.failWith
}

// auditPassRunner 返回「体检通过」的 skillRunner stub，并抓取体检提示词。
func auditPassRunner(promptOut *string) SkillRunnerFunc {
	return func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-audit" {
			if promptOut != nil {
				*promptOut = input.Prompt
			}
			return SkillOutput{Text: "结论：\n```json\n" +
				`{"verdict":"pass","score":31,"summary":"能签","issues":[]}` + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}
}

// TestGFreezeOnVolumeBoundaryHandoff 验证 WBS 2.5 G_freeze 自动闸门主路径：
// 卷边界复评（audit_now 于跨卷时以 reason=volume 进入）定界在「刚完结的前一卷」（v1），
// 该 scoped run 过 S4+S5 收口（G4 continue）后自动冻结 v1 并触发快照提交；
// 当前卷 v2（仍在写）绝不冻结。
func TestGFreezeOnVolumeBoundaryHandoff(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(700)
	v1, v2 := seedFreezeFixtures(t, db, novelID)

	var prompt string
	svc.skillRunner = auditPassRunner(&prompt)
	rec := &freezeRecorder{}
	svc.SetVolumeFreezer(rec.freeze)

	runID := startS3Run(t, svc, novelID, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", "")) // 跨卷 → 非阻塞提示（R5-4 不弹闸）

	// 显式 audit_now：跨卷时 reason=volume（卷边界复评）。
	require.NoError(t, svc.ResumePipeline(ctx, runID, "audit_now", ""))
	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v1)
	require.NoError(t, err, "卷边界复评应定界在刚完结的前一卷 v1")
	require.Equal(t, runID, scoped.ParentRunID)
	require.Equal(t, StageS4b, scoped.CurrentStage)

	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)
	require.Contains(t, prompt, fmt.Sprintf("volume_id=%d", v1), "体检作用域应为前一卷 v1")

	// G4 选 continue：scoped 收口（该卷过 S4+S5）→ G_freeze 自动冻结 v1。
	require.NoError(t, svc.ResumePipeline(ctx, scoped.RunID, "continue", ""))
	require.Equal(t, [][2]int64{{novelID, v1}}, rec.calls, "应恰好冻结前一卷 v1 一次")
	for _, c := range rec.calls {
		require.NotEqual(t, v2, c[1], "当前卷 v2 仍在写，不得冻结")
	}

	scopedAfter, _ := svc.store.GetByRunID(ctx, scoped.RunID)
	require.Equal(t, StatusFinished, scopedAfter.Status)
	book, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StageS3, book.CurrentStage)
	require.Equal(t, StatusRunning, book.Status, "收口后 book-run 应唤醒回 S3")

	// 收口上下文记录的触发原因应为 volume（供 G_freeze 判据回看）。
	var sc GateContext
	require.NoError(t, json.Unmarshal([]byte(scopedAfter.ContextJSON), &sc))
	require.Equal(t, "volume", sc.AuditReason)
}

// TestNoGFreezeOnMidVolumeAudit 验证：中途体检（golden3 首检 / milestone 复检）收口
// 不触发 G_freeze——卷还在写，不能进只读冻结态。
func TestNoGFreezeOnMidVolumeAudit(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(701)
	_, v2 := seedFreezeFixtures(t, db, novelID)

	svc.skillRunner = auditPassRunner(nil)
	rec := &freezeRecorder{}
	svc.SetVolumeFreezer(rec.freeze)

	runID := startS3Run(t, svc, novelID, `{}`)
	book, err := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, err)

	for _, reason := range []string{"golden3", "milestone"} {
		svc.enterAudit(ctx, book, reason)
		scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v2)
		require.NoError(t, err, reason+" 应定界在当前卷 v2")
		waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)
		require.NoError(t, svc.ResumePipeline(ctx, scoped.RunID, "continue", ""))
		scopedAfter, _ := svc.store.GetByRunID(ctx, scoped.RunID)
		require.Equal(t, StatusFinished, scopedAfter.Status)
	}
	require.Empty(t, rec.calls, "中途体检收口不得触发 G_freeze")

	// book-run 应已唤醒回 S3。
	book, _ = svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusRunning, book.Status)
}

// TestGFreezeFailureDoesNotBlockHandoff 验证 G_freeze 失败（如 2.4 台账审计不过）不阻断
// scoped 收口：book-run 照常唤醒、scoped 照常终结，冻结被拒以 note 广播，卷保持可写。
func TestGFreezeFailureDoesNotBlockHandoff(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(702)
	v1, _ := seedFreezeFixtures(t, db, novelID)

	svc.skillRunner = auditPassRunner(nil)
	rec := &freezeRecorder{failWith: errors.New("台账审计未通过：error 级不自洽 2 条")}
	svc.SetVolumeFreezer(rec.freeze)

	var notes []string
	t.Cleanup(swapEmitStage(func(_ context.Context, p StagePayload) {
		if p.Note != "" {
			notes = append(notes, p.Note)
		}
	}))

	runID := startS3Run(t, svc, novelID, `{"last_audit_chapter":3,"s4_audited":true}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""))
	require.NoError(t, svc.ResumePipeline(ctx, runID, "audit_now", ""))
	scoped, err := svc.store.GetActiveVolumeRun(ctx, novelID, v1)
	require.NoError(t, err)
	waitForStageStatus(t, svc, scoped.RunID, StatusAwaitingGate)
	require.NoError(t, svc.ResumePipeline(ctx, scoped.RunID, "continue", ""))

	require.Len(t, rec.calls, 1, "应尝试冻结一次")
	scopedAfter, _ := svc.store.GetByRunID(ctx, scoped.RunID)
	require.Equal(t, StatusFinished, scopedAfter.Status, "冻结失败不得阻断收口")
	book, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusRunning, book.Status, "book-run 应照常唤醒")

	var joined string
	for _, n := range notes {
		joined += n
	}
	require.Contains(t, joined, "自动冻结被拒", "冻结被拒应广播 note 提示")
}

// TestGFreezeOnFinalFinish 验证完本收口（G终 finish）把本书尚未冻结的卷全部冻结并提交快照，
// 已冻结的卷跳过——补齐「每卷有 committed snapshot」（末卷/单卷书没有跨卷时刻）。
func TestGFreezeOnFinalFinish(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(703)
	v1, v2 := seedFreezeFixtures(t, db, novelID)

	// v1 已在卷边界复评时冻结过：完本收口应跳过。
	_, err := volume.NewStore(db, slog.Default()).SetFrozen(ctx, nil, novelID, v1, true)
	require.NoError(t, err)

	rec := &freezeRecorder{}
	svc.SetVolumeFreezer(rec.freeze)

	runID := startS3Run(t, svc, novelID, `{}`)
	setRunStage(t, svc, runID, StageS6, StatusRunning, `{"audit":"{\"verdict\":\"pass\"}","audit_stage":"S6"}`)
	require.NoError(t, svc.ResumePipeline(ctx, runID, "finish", ""))

	got, _ := svc.store.GetByRunID(ctx, runID)
	require.Equal(t, StatusFinished, got.Status)
	require.Equal(t, [][2]int64{{novelID, v2}}, rec.calls, "完本收口只冻结未冻结的 v2")
}
