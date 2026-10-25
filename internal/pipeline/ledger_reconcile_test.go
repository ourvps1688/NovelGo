package pipeline

// R2b-7 台账对账批测试（提案 §8.3 组件 B）：
//   - reconcileFindings 接线：来源失效（ledger_source_missing）/来源失支撑
//     （ledger_unsupported）进结构告警块，编号与既有批连续；
//   - 冲突不预设谁对（方向判断进 L1 契约）；
//   - 宁缺勿错降级：无来源标注/短标签/白名单外/单章正文读失败 → 该条目不判；
//     provenance 取数失败/依赖未注入 → 整批跳过（ok=false）；
//   - 跨表同 id 标签不互相覆盖（"表名:id" 索引）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedReconcileProvenance 落一条 provenance 来源标注，返回标注行 id。
func seedReconcileProvenance(t *testing.T, db *gorm.DB, novelID, entryID, sourceChapterID int64, table string) {
	t.Helper()
	src := sourceChapterID
	require.NoError(t, db.Create(&storage.LedgerProvenance{
		NovelID: novelID, EntryTable: table, EntryID: entryID, SourceChapterID: &src,
	}).Error)
}

// TestReconcileFindingsSourceMissing 来源章已不存在 → ledger_source_missing（S1）。
func TestReconcileFindingsSourceMissing(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerProvenance{}, &character.Character{}, &chapter.Chapter{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(920)
	c := character.Character{NovelID: novelID, Name: "林平安"}
	require.NoError(t, db.Create(&c).Error)
	seedReconcileProvenance(t, db, novelID, c.ID, 4242, "characters") // 4242 不存在
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return "林平安望向远方", nil
	})

	findings, ok := svc.reconcileFindings(ctx, novelID)
	require.True(t, ok)
	require.Len(t, findings, 1)
	require.Equal(t, "S1", findings[0].ID)
	require.Equal(t, structure.CheckLedgerSourceMissing, findings[0].Check)
	require.Equal(t, fmt.Sprintf("characters:%d", c.ID), findings[0].Ref)
	require.Contains(t, findings[0].Desc, "不预设谁对", "冲突文案不预设谁对（方向判断进 L1 契约）")
}

// TestReconcileFindingsUnsupportedAndSkips 失支撑告警 + 宁缺勿错降级矩阵：
// 正文不支撑→告警；别名命中→支撑（静默）；<2 rune 短标签/无来源标注/白名单外→不判。
func TestReconcileFindingsUnsupportedAndSkips(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerProvenance{}, &character.Character{}, &chapter.Chapter{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(921)
	ch := chapter.Chapter{NovelID: novelID, Title: "章", SortOrder: 1}
	require.NoError(t, db.Create(&ch).Error)

	unsupported := character.Character{NovelID: novelID, Name: "林平安"}
	aliasHit := character.Character{NovelID: novelID, Name: "苏婉儿", Aliases: `["婉儿"]`}
	shortLabel := character.Character{NovelID: novelID, Name: "七"}
	for _, c := range []*character.Character{&unsupported, &aliasHit, &shortLabel} {
		require.NoError(t, db.Create(c).Error)
	}
	seedReconcileProvenance(t, db, novelID, unsupported.ID, ch.ID, "characters")
	seedReconcileProvenance(t, db, novelID, aliasHit.ID, ch.ID, "characters")
	seedReconcileProvenance(t, db, novelID, shortLabel.ID, ch.ID, "characters")
	// 无来源标注（不判）+ 白名单外表（不判）。
	require.NoError(t, db.Create(&storage.LedgerProvenance{NovelID: novelID,
		EntryTable: "characters", EntryID: 990}).Error)
	require.NoError(t, db.Create(&storage.LedgerProvenance{NovelID: novelID,
		EntryTable: "story_inconsistencies", EntryID: 991, SourceChapterID: &ch.ID}).Error)
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return "婉儿回头望了一眼", nil // 含别名「婉儿」，不含「林平安」
	})

	findings, ok := svc.reconcileFindings(ctx, novelID)
	require.True(t, ok)
	require.Len(t, findings, 1, "仅失支撑条目告警：别名命中/短标签/无标注/白名单外全静默")
	require.Equal(t, structure.CheckLedgerUnsupported, findings[0].Check)
	require.Equal(t, fmt.Sprintf("characters:%d", unsupported.ID), findings[0].Ref)
	require.Contains(t, findings[0].Desc, "林平安")
	require.Contains(t, findings[0].Desc, "不预设谁对")
	require.Contains(t, findings[0].Desc, "正文错→body、台账错→ledger", "方向判断进 L1 契约的指引进告警文案")
}

// TestReconcileFindingsCrossTableSameID 跨表同 id 标签不互相覆盖（"表名:id" 索引）：
// characters:1 与 time_entries:1 同 id 但标签不同，正文只支撑其一。
func TestReconcileFindingsCrossTableSameID(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerProvenance{}, &character.Character{},
		&chapter.Chapter{}, &timeline.TimelineEntry{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	novelID := int64(922)
	ch := chapter.Chapter{NovelID: novelID, Title: "章", SortOrder: 1}
	require.NoError(t, db.Create(&ch).Error)
	c := character.Character{NovelID: novelID, Name: "林平安"}
	require.NoError(t, db.Create(&c).Error)
	te := timeline.TimelineEntry{NovelID: novelID, Category: "foreshadowing",
		Status: "pending", Title: "灯会夜"}
	require.NoError(t, db.Create(&te).Error)
	require.Equal(t, c.ID, te.ID, "夹具前提：两表首个条目同 id")
	seedReconcileProvenance(t, db, novelID, c.ID, ch.ID, "characters")
	seedReconcileProvenance(t, db, novelID, te.ID, ch.ID, "time_entries")
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return "林平安望向远方", nil // 支撑 characters，不支撑 time_entries
	})

	findings, ok := svc.reconcileFindings(ctx, novelID)
	require.True(t, ok)
	require.Len(t, findings, 1, "同 id 标签不得互相覆盖：仅 time_entries:1 失支撑")
	require.Equal(t, structure.CheckLedgerUnsupported, findings[0].Check)
	require.Equal(t, fmt.Sprintf("time_entries:%d", te.ID), findings[0].Ref)
	require.Contains(t, findings[0].Desc, "灯会夜", "用 time_entries 自己的标签，不是 characters 的")
}

// TestReconcileFindingsDegrades 降级矩阵：依赖未注入/provenance 取数失败 → 整批跳过
// （ok=false）；单章正文读失败 → 只跳该章条目（其余照判）。
func TestReconcileFindingsDegrades(t *testing.T) {
	ctx := context.Background()
	// ① chapterTextReader 未注入 → 整批跳过。
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerProvenance{}, &character.Character{}, &chapter.Chapter{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	findings, ok := svc.reconcileFindings(ctx, 923)
	require.False(t, ok)
	require.Nil(t, findings)

	// ② provenance 表缺失 → 整批跳过（ok=false，宁缺勿错）。
	db2 := newTestDB(t)
	require.NoError(t, db2.AutoMigrate(&character.Character{}, &chapter.Chapter{}))
	svc2 := NewService(db2, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc2.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) { return "", nil })
	findings, ok = svc2.reconcileFindings(ctx, 924)
	require.False(t, ok)
	require.Nil(t, findings)

	// ③ 单章正文读失败 → 只跳该章条目，其余照判。
	db3 := newTestDB(t)
	require.NoError(t, db3.AutoMigrate(&storage.LedgerProvenance{}, &character.Character{}, &chapter.Chapter{}))
	svc3 := NewService(db3, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(925)
	ch1 := chapter.Chapter{NovelID: novelID, Title: "一", SortOrder: 1}
	ch2 := chapter.Chapter{NovelID: novelID, Title: "二", SortOrder: 2}
	require.NoError(t, db3.Create(&ch1).Error)
	require.NoError(t, db3.Create(&ch2).Error)
	c1 := character.Character{NovelID: novelID, Name: "林平安"}
	c2 := character.Character{NovelID: novelID, Name: "苏婉儿"}
	require.NoError(t, db3.Create(&c1).Error)
	require.NoError(t, db3.Create(&c2).Error)
	seedReconcileProvenance(t, db3, novelID, c1.ID, ch1.ID, "characters") // 读失败章
	seedReconcileProvenance(t, db3, novelID, c2.ID, ch2.ID, "characters") // 正常章（不支撑）
	svc3.SetChapterTextReader(func(_ context.Context, _, chapterID int64) (string, error) {
		if chapterID == ch1.ID {
			return "", errors.New("章节正文读取失败")
		}
		return "正文里谁也没提", nil
	})
	findings, ok = svc3.reconcileFindings(ctx, novelID)
	require.True(t, ok)
	require.Len(t, findings, 1, "读失败章条目不判（宁缺勿错），其余照判")
	require.Equal(t, fmt.Sprintf("characters:%d", c2.ID), findings[0].Ref)
}

// TestStructureBlockMergesReconcileFindings 对账批进结构告警块，S 编号与既有批连续
// （伏笔批 3 条 S1–S3 → 对账批 S4），全文含「不预设谁对」方向指引。
func TestStructureBlockMergesReconcileFindings(t *testing.T) {
	rec := &dualRecorder{quickText: dualQuickPass}
	svc, runID := newDualTest(t, 826, "volume", false, rec)
	seedForeshadowData(t, svc.store.db, 826)
	require.NoError(t, svc.store.db.AutoMigrate(&storage.LedgerProvenance{}, &character.Character{}))
	c := character.Character{NovelID: 826, Name: "林平安"}
	require.NoError(t, svc.store.db.Create(&c).Error)
	seedReconcileProvenance(t, svc.store.db, 826, c.ID, 999, "characters") // 来源章不存在
	svc.SetChapterTextReader(func(_ context.Context, _, _ int64) (string, error) {
		return "", nil
	})
	svc.executeAudit(runID, 826)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	require.Contains(t, rec.fullPrompt, "S4[ledger_source_missing]", "对账批编号接在伏笔批后")
	require.Contains(t, rec.fullPrompt, "不预设谁对")
	require.NotContains(t, rec.fullPrompt, "S5[", "编号恰好到 S4")
}
