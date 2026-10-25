package storage

import (
	"context"
	"strings"
	"testing"
)

// ── 契约 ─────────────────────────────────────────────────────────────

// TestLedgerProvenanceValidate 定位/置信契约：entry_table 非空、entry_id>0、
// confidence 封闭枚举（空=未标注），非法拒收。
func TestLedgerProvenanceValidate(t *testing.T) {
	cases := []struct {
		name    string
		p       LedgerProvenance
		wantErr string
	}{
		{"合法-已标注", LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 3, Confidence: ConfidenceHigh}, ""},
		{"合法-置信留空", LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 3}, ""},
		{"缺表名", LedgerProvenance{NovelID: 1, EntryID: 3}, "entry_table"},
		{"非法条目ID", LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 0}, "entry_id"},
		{"非法置信", LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 3, Confidence: "sure"}, "confidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestLedgerProvenanceUpsertRoundTrip upsert 语义：(novel_id, entry_table, entry_id) 唯一，
// 同条目重复 upsert 整条覆盖（含 confirmed/pinned 显式落值）；Get 未标注返回 (nil, nil)；
// ListByNovel 按 id 升序。
func TestLedgerProvenanceUpsertRoundTrip(t *testing.T) {
	db := setupLedgerTemporalDB(t)
	s := NewProvenanceStore(db)
	ctx := context.Background()

	// 契约拒收不落库
	if err := s.Upsert(ctx, &LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 0}); err == nil {
		t.Fatal("非法 entry_id 应拒收")
	}

	// 未标注 → (nil, nil)
	got, err := s.Get(ctx, 1, "characters", 3)
	if err != nil || got != nil {
		t.Fatalf("未标注应返回 (nil,nil): %v, %v", got, err)
	}

	p := &LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 3,
		SourceChapterID: int64Ptr(7), Confidence: ConfidenceMedium}
	if err := s.Upsert(ctx, p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err = s.Get(ctx, 1, "characters", 3)
	if err != nil || got == nil {
		t.Fatalf("get after upsert: %v, %v", got, err)
	}
	if got.SourceChapterID == nil || *got.SourceChapterID != 7 || got.Confidence != ConfidenceMedium || got.Confirmed || got.Pinned {
		t.Fatalf("roundtrip 内容错误: %+v", got)
	}

	// 同条目重复 upsert = 整条覆盖（含布尔显式落值，无 GORM 零值跳写）
	p2 := &LedgerProvenance{NovelID: 1, EntryTable: "characters", EntryID: 3,
		Confidence: ConfidenceHigh, Confirmed: true, Pinned: true}
	if err := s.Upsert(ctx, p2); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}
	got, err = s.Get(ctx, 1, "characters", 3)
	if err != nil || got == nil {
		t.Fatalf("get after overwrite: %v, %v", got, err)
	}
	if got.SourceChapterID != nil || got.Confidence != ConfidenceHigh || !got.Confirmed || !got.Pinned {
		t.Fatalf("同名 upsert 应整条覆盖: %+v", got)
	}

	// ListByNovel id 升序
	if err := s.Upsert(ctx, &LedgerProvenance{NovelID: 1, EntryTable: "time_entries", EntryID: 1}); err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	rows, err := s.ListByNovel(ctx, 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].EntryTable != "characters" || rows[1].EntryTable != "time_entries" {
		t.Fatalf("list 顺序/数量错误: %+v", rows)
	}
}

// ── 钉住护栏 ─────────────────────────────────────────────────────────

// TestLedgerPinnedGuard 钉住护栏矩阵：已钉住条目的语义修改/删除仅放行 author 与
// 显式 retcon 标注；auto 拒收（行不落盘、不记履历）；未钉住/未标注/纯格式调整不受影响。
func TestLedgerPinnedGuard(t *testing.T) {
	db := setupLedgerTemporalDB(t)
	ctx := context.Background()
	autoCtx := WithLedgerSource(ctx, HistorySourceAuto)
	retconCtx := WithLedgerSource(ctx, HistorySourceRetcon)

	mk := func(name string) *LedgerTestEntry {
		e := LedgerTestEntry{NovelID: 1, Name: name, Description: "原始描述"}
		if err := db.Create(&e).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
		return &e
	}
	pin := func(entryID int64, pinned bool) {
		if err := NewProvenanceStore(db).Upsert(ctx, &LedgerProvenance{
			NovelID: 1, EntryTable: "ledger_test_entries", EntryID: entryID, Pinned: pinned,
		}); err != nil {
			t.Fatalf("upsert provenance: %v", err)
		}
	}
	descOf := func(entryID int64) string {
		var e LedgerTestEntry
		if err := db.First(&e, entryID).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		return e.Description
	}

	// ① auto 语义修改已钉住条目 → 拒收，行不变、不记履历
	pinned := mk("钉住条目")
	pin(pinned.ID, true)
	var e LedgerTestEntry
	if err := db.First(&e, pinned.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	e.Description = "AI 偷偷改的"
	err := db.WithContext(autoCtx).Save(&e).Error
	if err == nil {
		t.Fatal("auto 修改已钉住条目应拒收")
	}
	if !strings.Contains(err.Error(), "已钉住核心事实") {
		t.Fatalf("拒收错误应含钉住说明: %v", err)
	}
	if got := descOf(pinned.ID); got != "原始描述" {
		t.Fatalf("拒收后行不应变化: %q", got)
	}
	var cnt int64
	db.Model(&LedgerChangeHistory{}).Where("entry_id = ? AND field = ?", pinned.ID, "description").Count(&cnt)
	if cnt != 0 {
		t.Fatalf("拒收写不应记履历: %d", cnt)
	}

	// ② author 修改已钉住条目 → 放行（作者手动）
	var e2 LedgerTestEntry
	if err := db.First(&e2, pinned.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	e2.Description = "作者手动改的"
	if err := db.Save(&e2).Error; err != nil {
		t.Fatalf("author 修改应放行: %v", err)
	}
	if got := descOf(pinned.ID); got != "作者手动改的" {
		t.Fatalf("author 写应落盘: %q", got)
	}

	// ③ 显式 retcon 标注（已过审批的回改通道）→ 放行
	var e3 LedgerTestEntry
	if err := db.First(&e3, pinned.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	e3.Description = "审批后回改"
	if err := db.WithContext(retconCtx).Save(&e3).Error; err != nil {
		t.Fatalf("retcon 标注应放行: %v", err)
	}

	// ④ auto 删除已钉住条目 → 拒收；author 删除 → 放行
	var e4 LedgerTestEntry
	if err := db.First(&e4, pinned.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := db.WithContext(autoCtx).Delete(&e4).Error; err == nil {
		t.Fatal("auto 删除已钉住条目应拒收")
	}
	if got := descOf(pinned.ID); got != "审批后回改" {
		t.Fatalf("拒收删除后行应仍在: %q", got)
	}

	// ⑤ auto 纯格式调整（无语义变更）→ 放行（护栏只管语义修改）
	var e5 LedgerTestEntry
	if err := db.First(&e5, pinned.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	e5.Description = "审批后回改  "
	if err := db.WithContext(autoCtx).Save(&e5).Error; err != nil {
		t.Fatalf("纯格式调整应放行: %v", err)
	}

	// ⑥ 未钉住（pinned=false）auto 修改 → 放行
	plain := mk("未钉住条目")
	pin(plain.ID, false)
	var e6 LedgerTestEntry
	if err := db.First(&e6, plain.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	e6.Description = "auto 改未钉住"
	if err := db.WithContext(autoCtx).Save(&e6).Error; err != nil {
		t.Fatalf("未钉住 auto 修改应放行: %v", err)
	}

	// ⑦ 未标注（无 provenance 行）auto 修改 → 放行（缺省=现行行为逐字一致）
	unmarked := mk("未标注条目")
	var e7 LedgerTestEntry
	if err := db.First(&e7, unmarked.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	e7.Description = "auto 改未标注"
	if err := db.WithContext(autoCtx).Save(&e7).Error; err != nil {
		t.Fatalf("未标注 auto 修改应放行: %v", err)
	}

	// ⑧ author 删除已钉住条目 → 放行，删后 (delete) 履历照记
	var e8 LedgerTestEntry
	if err := db.First(&e8, pinned.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := db.Delete(&e8).Error; err != nil {
		t.Fatalf("author 删除应放行: %v", err)
	}
	var del LedgerChangeHistory
	if err := db.Where("entry_id = ? AND field = ?", pinned.ID, HistoryFieldDelete).First(&del).Error; err != nil {
		t.Fatalf("author 删除应记 (delete) 履历: %v", err)
	}
}

// int64Ptr 返回 int64 指针（夹具用）。
func int64Ptr(v int64) *int64 { return &v }
