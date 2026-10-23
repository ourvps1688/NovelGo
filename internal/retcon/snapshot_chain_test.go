package retcon

// retcon × snapshot 回滚链路集成（P1 T4.2，WBS §0.5）。
//
// 三执行体接真实现（与 app/handler.go 接线同构）：回滚点固化=snapshot.Commit、
// 差异摘要=snapshot.DiffAgainstCurrent+FormatDelta、回滚=snapshot.Restore。
// 验证：留痕差异摘要中 voice 变更可读（retcon 留痕同款渲染）；Rollback 后
// voice 行随 Restore 回滚（为 P2「retcon 支持风格回改」铺路）。
// A3 状态机零改动：本文件不触碰迁移表，状态语义由 state_test 钉死、
// 终态断言与既有测试同口径。

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/snapshot"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/voice"
	"github.com/novelgo/novelgo/internal/volume"
)

// openChainDB 建集成库：retcon_cases + snapshot 全量 schema（volume/快照/正文 +
// registry 全部台账表）。Restore 逐表 DELETE，registry 表缺一不可——
// 此处模型清单与 snapshot.registry 对应，缺表会以「no such table」显式失败。
func openChainDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	models := []any{
		&RetconCase{},
		&volume.Volume{}, &volume.VolumeSnapshot{}, &chapter.Chapter{},
		&character.Character{}, &character.CharacterRelation{}, &character.CharacterFactKnowledge{},
		&location.Location{},
		&storyarc.StoryArc{}, &storyarc.ArcNode{},
		&timeline.TimelineEntry{}, &timeline.ChronologyAnchor{},
		&reader.ReaderPerspective{}, &reader.PovRoster{},
		&voice.Entry{},
	}
	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatalf("migrate %T: %v", m, err)
		}
	}
	return db
}

// TestRollbackChainRestoresVoiceDimensions 走 retcon 受控编辑全链：
// 开窗固化回滚点 → 编辑 voice 行（改+增）→ 留痕收口（真实 diff 摘要可读）→
// 回滚（真实 Restore 还原 voice 行）。
func TestRollbackChainRestoresVoiceDimensions(t *testing.T) {
	ctx := context.Background()
	db := openChainDB(t)

	vol := volume.Volume{NovelID: 1, Name: "第一卷", SortOrder: 1, Frozen: true}
	if err := db.Create(&vol).Error; err != nil {
		t.Fatalf("seed volume: %v", err)
	}
	seeds := []voice.Entry{
		{NovelID: 1, Dimension: string(voice.DimRhythm), Content: "先结论后铺垫", Source: string(voice.SourceManual)},
		{NovelID: 1, Dimension: string(voice.DimSentenceFingerprint), Content: "短句为主", Source: string(voice.SourceManual)},
	}
	for i := range seeds {
		if err := db.Create(&seeds[i]).Error; err != nil {
			t.Fatalf("seed voice: %v", err)
		}
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	snaps := snapshot.NewService(db, volume.NewStore(db, logger), chapter.NewStore(db, logger))
	svc := newTestService(db)
	svc.SetCommitSnapshot(func(ctx context.Context, novelID, volumeID int64) (int64, error) {
		snap, err := snaps.Commit(ctx, novelID, volumeID)
		if err != nil {
			return 0, err
		}
		return snap.ID, nil
	})
	svc.SetDiffSummary(func(ctx context.Context, snapshotID int64) (string, error) {
		delta, err := snaps.DiffAgainstCurrent(ctx, snapshotID)
		if err != nil {
			return "", err
		}
		return snapshot.FormatDelta(delta), nil
	})
	svc.SetRestorer(func(ctx context.Context, _, snapshotID int64) error {
		_, err := snaps.Restore(ctx, snapshotID)
		return err
	})

	c, err := svc.CreateCandidate(ctx, CreateInput{NovelID: 1, VolumeID: volID(vol.ID), Title: "改前卷文风"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Approve(ctx, c.ID, "author", "批准受控回改"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	editing, err := svc.BeginControlledEdit(ctx, c.ID, vol.ID, "author")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	// 受控编辑窗口内的风格改动：改 rhythm 内容 + 增 taboo_words 行。
	var rhythm voice.Entry
	if err := db.Where("novel_id = ? AND dimension = ?", 1, voice.DimRhythm).First(&rhythm).Error; err != nil {
		t.Fatalf("load rhythm: %v", err)
	}
	rhythm.Content = "先铺垫后结论"
	if err := db.Save(&rhythm).Error; err != nil {
		t.Fatalf("edit rhythm: %v", err)
	}
	extra := voice.Entry{NovelID: 1, Dimension: string(voice.DimTabooWords), Content: "禁用突然/忽然", Source: string(voice.SourceManual)}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatalf("edit add taboo: %v", err)
	}

	recorded, err := svc.RecordWithDiff(ctx, editing.ID, "author")
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	// 留痕差异摘要中 voice 变更可读（FormatDelta retcon 留痕同款）。
	if !strings.Contains(recorded.DiffSummary, "voice_dimensions: +1 -0 ~1") {
		t.Errorf("差异摘要缺 voice 表计数: %q", recorded.DiffSummary)
	}
	if !strings.Contains(recorded.DiffSummary, `content: "先结论后铺垫" → "先铺垫后结论"`) {
		t.Errorf("差异摘要 content 变更不可读: %q", recorded.DiffSummary)
	}

	rolled, err := svc.Rollback(ctx, recorded.ID, "author", "风格改道，撤销回改")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rolled.Status != StatusRolledBack {
		t.Errorf("status = %q, want rolled_back", rolled.Status)
	}

	// voice 行随 Restore 回滚：改动还原、新增丢弃。
	var after voice.Entry
	if err := db.Where("novel_id = ? AND dimension = ?", 1, voice.DimRhythm).First(&after).Error; err != nil {
		t.Fatalf("reload rhythm: %v", err)
	}
	if after.Content != "先结论后铺垫" {
		t.Errorf("rhythm content 未回滚: %q", after.Content)
	}
	var n int64
	if err := db.Model(&voice.Entry{}).Count(&n).Error; err != nil {
		t.Fatalf("count voice: %v", err)
	}
	if n != 2 {
		t.Errorf("voice_dimensions 行数 = %d, want 2（新增行已丢弃）", n)
	}
	var gone int64
	db.Model(&voice.Entry{}).Where("dimension = ?", voice.DimTabooWords).Count(&gone)
	if gone != 0 {
		t.Error("回滚后仍存在快照后新增的 voice 行")
	}
}
