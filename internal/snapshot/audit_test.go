package snapshot

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/location"
	"github.com/novelgo/novelgo/internal/reader"
	"github.com/novelgo/novelgo/internal/storyarc"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/volume"
)

// chapterForOrder 造指定 sort_order 的章节（挂进该小说首卷）并返回其 id，
// 供 lifecycle 时序测试构造「第 2 章埋、第 1 章收」的违规形态。
func chapterForOrder(t *testing.T, db *gorm.DB, novelID int64, sortOrder int) int64 {
	t.Helper()
	var vol volume.Volume
	if err := db.Where("novel_id = ?", novelID).Order("id ASC").First(&vol).Error; err != nil {
		t.Fatalf("load volume: %v", err)
	}
	ch := chapter.Chapter{
		NovelID: novelID, VolumeID: int64Ptr(vol.ID),
		Title: "第二章", SortOrder: sortOrder, WordCount: 1500,
	}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatalf("seed chapter(sort_order=%d): %v", sortOrder, err)
	}
	return ch.ID
}

func newAuditService(db *gorm.DB) *Service {
	return NewService(db, volume.NewStore(db, testSnapLogger()), chapter.NewStore(db, testSnapLogger()))
}

// issueCodes 折叠问题清单为 "severity:code" 集合，便于断言。
func issueCodes(r *AuditReport) map[string]int {
	out := map[string]int{}
	for _, is := range r.Issues {
		out[string(is.Severity)+":"+is.Code]++
	}
	return out
}

func TestAuditLedger_CleanFixture(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	seedFixture(t, db)
	svc := newAuditService(db)

	report, err := svc.AuditLedger(ctx, 1)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if report.ErrorCount != 0 || report.WarningCount != 0 {
		t.Errorf("干净夹具应零发现，got %+v", report.Issues)
	}
	if report.NovelID != 1 {
		t.Errorf("novel_id: want 1, got %d", report.NovelID)
	}
}

func TestAuditLedger_DetectsStructuralGarbage(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	seedFixture(t, db)
	svc := newAuditService(db)

	// 悬挂引用：关系指向不存在的角色
	var char character.Character
	db.Where("novel_id = ? AND name = ?", 1, "主角").First(&char)
	db.Create(&character.CharacterRelation{
		NovelID: 1, SourceCharacterID: char.ID, TargetCharacterID: char.ID + 999,
		RelationDescribe: "断线关系", IsCurrent: true,
	})
	// 枚举越界：弧线类型非法
	var arc storyarc.StoryArc
	db.Where("novel_id = ?", 1).First(&arc)
	db.Model(&storyarc.StoryArc{}).Where("id = ?", arc.ID).Update("arc_type", "主线")
	// 地点包含环
	locA := location.Location{NovelID: 1, Name: "王宫", LocationType: "city"}
	locB := location.Location{NovelID: 1, Name: "大殿", LocationType: "room"}
	db.Create(&locA)
	db.Create(&locB)
	db.Model(&location.Location{}).Where("id = ?", locA.ID).Update("parent_location_id", locB.ID)
	db.Model(&location.Location{}).Where("id = ?", locB.ID).Update("parent_location_id", locA.ID)
	// lifecycle：known 带回收章（无埋设章走显式分支）
	db.Create(&reader.ReaderPerspective{
		NovelID: 1, Type: reader.TypeKnown, Content: "主角是穿越者",
		RevealedChapterID: int64Ptr(1),
	})
	// lifecycle：先埋后收（有埋设章走 lifecycle.go 单源校验）：第 2 章埋、第 1 章收
	ch2 := chapterForOrder(t, db, 1, 2)
	db.Create(&reader.ReaderPerspective{
		NovelID: 1, Type: reader.TypeSuspense, Content: "密信是谁写的",
		PlantedChapterID: int64Ptr(ch2), RevealedChapterID: int64Ptr(1),
	})
	// 存量无从定位：无埋设章的悬念（warning）
	db.Create(&reader.ReaderPerspective{
		NovelID: 1, Type: reader.TypeSuspense, Content: "老账房的旧事",
	})
	// 伏笔 resolved 无回收章（warning）
	db.Create(&timeline.TimelineEntry{
		NovelID: 1, Category: "foreshadowing", Status: "resolved", Title: "断刀",
	})
	// 同名角色（warning）
	db.Create(&character.Character{NovelID: 1, Name: "主角"})

	report, err := svc.AuditLedger(ctx, 1)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	codes := issueCodes(report)

	want := map[string]int{
		"error:ref.dangling":                        1,
		"error:enum.invalid":                        1,
		"error:locations.parent_cycle":              2, // 环上两个节点各报一次
		"error:lifecycle.known_with_revealed":       1,
		"error:lifecycle.invalid":                   1, // 先埋后收
		"warning:lifecycle.unlocated":               1,
		"warning:timeline.resolved_without_chapter": 1,
		"warning:characters.duplicate_name":         1,
	}
	for code, n := range want {
		if codes[code] != n {
			t.Errorf("问题码 %s: want %d, got %d（全部：%v）", code, n, codes[code], codes)
		}
	}
	if report.ErrorCount != 6 || report.WarningCount != 3 {
		t.Errorf("计数: want 6 error / 3 warning, got %d / %d", report.ErrorCount, report.WarningCount)
	}
}

func TestFreeze_BlockedByInconsistentLedger(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := newAuditService(db)

	// 注入 error 级垃圾：悬挂引用
	var char character.Character
	db.Where("novel_id = ? AND name = ?", 1, "主角").First(&char)
	bad := character.CharacterRelation{
		NovelID: 1, SourceCharacterID: char.ID, TargetCharacterID: char.ID + 999,
		RelationDescribe: "断线关系", IsCurrent: true,
	}
	db.Create(&bad)

	_, _, err := svc.Freeze(ctx, 1, volID)
	if err == nil {
		t.Fatal("error 级不自洽应阻断冻结")
	}
	var inconsistent *LedgerInconsistentError
	if !errors.As(err, &inconsistent) {
		t.Fatalf("应返回 LedgerInconsistentError, got %T: %v", err, err)
	}
	if inconsistent.Report.ErrorCount != 1 {
		t.Errorf("报告错误数: want 1, got %d", inconsistent.Report.ErrorCount)
	}

	// 冻结被拒：未置冻结、未提交快照
	var vol volume.Volume
	db.First(&vol, volID)
	if vol.Frozen || vol.SnapshotVersion != 0 {
		t.Errorf("被拒后不应置冻结/提交快照: %+v", vol)
	}
	if n := countRows(t, db, "volume_snapshots"); n != 0 {
		t.Errorf("被拒后快照行数: want 0, got %d", n)
	}

	// 修复垃圾后可冻结
	db.Delete(&character.CharacterRelation{}, bad.ID)
	v, snap, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("修复后冻结: %v", err)
	}
	if !v.Frozen || snap == nil {
		t.Errorf("修复后应冻结并提交快照: %+v %+v", v, snap)
	}
}

func TestFreeze_WarningsDoNotBlock(t *testing.T) {
	db := openSnapDB(t)
	ctx := context.Background()
	volID := seedFixture(t, db)
	svc := newAuditService(db)

	// 仅 warning 级：同名角色
	db.Create(&character.Character{NovelID: 1, Name: "主角"})

	report, err := svc.AuditLedger(ctx, 1)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if report.ErrorCount != 0 || report.WarningCount == 0 {
		t.Fatalf("预期仅 warning: %+v", report.Issues)
	}

	v, snap, err := svc.Freeze(ctx, 1, volID)
	if err != nil {
		t.Fatalf("warning 不应阻断冻结: %v", err)
	}
	if !v.Frozen || snap == nil {
		t.Errorf("应冻结并提交快照: %+v %+v", v, snap)
	}
}
