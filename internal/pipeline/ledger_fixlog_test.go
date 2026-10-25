package pipeline

// R2b-7 §8.3-3 台账变更进修复日志测试：
//   - ledgerChangeWatermark 水位语义（空表=0/取数失败=-1）；
//   - appendLedgerChangeLog 增量入账（"L:" 前缀、Note 格式、keep-first 幂等、
//     无效水位跳过、跨书隔离）；
//   - ledgerChangeNotes 前缀过滤；
//   - verifyPrompt 台账变更块：空=提示词与旧文逐字一致（降级零提示），
//     非空=追加「下游影响核对」块；
//   - executeFix 端到端：修复动台账 → 履历增量落进修复日志 → 复验提示词带下游核对块。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedLedgerChange 落一条台账变更履历，返回履历行。
func seedLedgerChange(t *testing.T, db *gorm.DB, novelID, entryID int64, field, oldV, newV string) storage.LedgerChangeHistory {
	t.Helper()
	row := storage.LedgerChangeHistory{
		NovelID: novelID, EntryTable: "characters", EntryID: entryID,
		Field: field, Old: oldV, New: newV, Source: storage.HistorySourceAuto,
		At: time.Now(),
	}
	require.NoError(t, db.Create(&row).Error)
	return row
}

// TestLedgerChangeWatermark 水位语义：空表=0（从头入账）；表缺失取数失败=-1（跳过）。
func TestLedgerChangeWatermark(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerChangeHistory{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())

	require.Equal(t, int64(0), svc.ledgerChangeWatermark(ctx, 927), "空表水位=0")
	r1 := seedLedgerChange(t, db, 927, 1, "name", "旧", "新")
	require.Equal(t, r1.ID, svc.ledgerChangeWatermark(ctx, 927), "水位=本书履历最大 id")

	db2 := newTestDB(t) // 未建 ledger_change_history 表
	svc2 := NewService(db2, nil, nil, nil, "", "", context.Background(), slog.Default())
	require.Equal(t, int64(-1), svc2.ledgerChangeWatermark(ctx, 927), "取数失败=-1（调用方跳过入账）")
}

// TestAppendLedgerChangeLog 增量入账语义：水位后增量按 "L:<id>" 入账（Note 确定性格式）、
// keep-first 幂等（重复拉取不覆盖不重复）、水位无效跳过、跨书履历不入账。
func TestAppendLedgerChangeLog(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerChangeHistory{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(928)

	base := seedLedgerChange(t, db, novelID, 1, "name", "旧名", "新名")
	watermark := svc.ledgerChangeWatermark(ctx, novelID)
	require.Equal(t, base.ID, watermark)

	// 水位后：本书记账、他书记账（不入本修复日志）。
	r2 := seedLedgerChange(t, db, novelID, 2, "aliases", `["小七"]`, `["七爷"]`)
	seedLedgerChange(t, db, 999, 3, "name", "他书", "他书新")

	ctxt := &GateContext{}
	svc.appendLedgerChangeLog(ctx, novelID, ctxt, watermark)
	require.Len(t, ctxt.FixLog, 1, "只入水位后本书增量")
	want := fmt.Sprintf("台账变更 characters:%d aliases: %q→%q（source=%s）",
		r2.EntryID, r2.Old, r2.New, storage.HistorySourceAuto)
	require.Equal(t, []FixLogEntry{{ID: fmt.Sprintf("L:%d", r2.ID), Note: want}}, ctxt.FixLog)

	// keep-first：重复拉取（并行批次收口）不覆盖不重复。
	keepFirst := *ctxt
	svc.appendLedgerChangeLog(ctx, novelID, &keepFirst, watermark)
	require.Equal(t, ctxt.FixLog, keepFirst.FixLog, "keep-first 幂等")

	// 水位无效（-1）→ 跳过（降级零提示）。
	silent := &GateContext{}
	svc.appendLedgerChangeLog(ctx, novelID, silent, -1)
	require.Empty(t, silent.FixLog)
}

// TestLedgerChangeNotes 前缀过滤：只取 "L:" 台账条目，顺序保持，无台账条目返回空。
func TestLedgerChangeNotes(t *testing.T) {
	entries := []FixLogEntry{
		{ID: "I1", Note: "正文修复摘要"},
		{ID: "L:5", Note: "台账变更 characters:1 name: \"旧\"→\"新\"（source=auto）"},
		{ID: "V1", Note: "文风豁免项"},
		{ID: "L:6", Note: "台账变更 time_entries:2 title: \"旧\"→\"新\"（source=author）"},
	}
	got := ledgerChangeNotes(entries)
	require.Equal(t, []string{
		`台账变更 characters:1 name: "旧"→"新"（source=auto）`,
		`台账变更 time_entries:2 title: "旧"→"新"（source=author）`,
	}, got)
	require.Nil(t, ledgerChangeNotes(nil))
	require.Nil(t, ledgerChangeNotes([]FixLogEntry{{ID: "I1", Note: "仅 issue 条目"}}))
}

// TestVerifyPromptLedgerChangesBlock 台账变更块契约：空/nil 台账清单 → 提示词与
// 旧文逐字一致（降级零提示）；非空 → 逐条列变更并要求核对下游影响，位置在上下文块前。
func TestVerifyPromptLedgerChangesBlock(t *testing.T) {
	targets := []verifyTarget{{ID: "I1", Level: "P1", Area: "人设", Desc: "动机缺失", Chapters: []int64{3}}}
	blocks := contextasm.ContextBlocks{Facts: "FACTS-MARKER"}

	base := verifyPrompt(1, targets, nil, blocks)
	require.Equal(t, base, verifyPrompt(1, targets, []string{}, blocks),
		"空清单与 nil 逐字一致（降级零提示）")
	require.NotContains(t, base, "【本轮台账变更清单", "无台账变更不渲染该块")

	notes := []string{`台账变更 characters:1 name: "旧名"→"新名"（source=auto）`}
	got := verifyPrompt(1, targets, notes, blocks)
	require.Contains(t, got, "【本轮台账变更清单（修复动过台账）】")
	require.Contains(t, got, notes[0])
	require.Contains(t, got, "下游影响", "要求核对台账修改的下游影响")
	require.Less(t, len(base), len(got), "非空清单必追加块")
	// 块在上下文块（Facts）之前：前缀=旧文的契约主体，尾部仍是上下文块。
	require.Less(t, strings.Index(got, "【本轮台账变更清单"), strings.Index(got, "FACTS-MARKER"))
	require.Contains(t, got, "FACTS-MARKER", "上下文块尾部装配不受影响")
}

// TestServiceFixLedgerChangesEnterFixLogAndVerify 端到端：修复会话动了台账（工具写
// 履历）→ 水位增量入修复日志 → 复验提示词带「下游影响核对」块；修复不动台账时复验
// 提示词无该块（空=旧文）。
func TestServiceFixLedgerChangesEnterFixLogAndVerify(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&storage.LedgerChangeHistory{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(929)

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"人设","desc":"主角动机缺失","fix_type":"ledger","target_scope":"active_volume","dimension":"character","direction":"forward_fix","chapter_ids":[3]}]}`
	var (
		verifyMu        sync.Mutex
		verifyPromptGot string
	)
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-fix":
			// 修复会话用台账工具改了角色名（工具走 GORM 钩子落履历——此处直落同款履历行）。
			// goroutine 内不用 require（t.FailNow 非测试协程不安全），失败随 SkillOutput 报错。
			row := storage.LedgerChangeHistory{
				NovelID: novelID, EntryTable: "characters", EntryID: 7,
				Field: "name", Old: "旧名", New: "新名", Source: storage.HistorySourceAuto,
				At: time.Now(),
			}
			if cerr := db.Create(&row).Error; cerr != nil {
				return SkillOutput{}, cerr
			}
			return SkillOutput{Text: "已修复。\n```json\n{\"fixed\":[\"I1\"],\"skipped\":[]}\n```"}, nil
		case "signability-verify":
			verifyMu.Lock()
			verifyPromptGot = input.Prompt
			verifyMu.Unlock()
			return SkillOutput{Text: "复验：\n```json\n{\"verified\":[{\"id\":\"I1\",\"ok\":true,\"note\":\"已生效\"}],\"summary\":\"1 项修复已确认\"}\n```"}, nil
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n{\"verdict\":\"pass\",\"score\":30,\"summary\":\"修复后通过\",\"issues\":[]}\n```"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.ResumePipeline(ctx, run.RunID, "fix", ""))
	waitForStageStatus(t, svc, run.RunID, StatusAwaitingGate)

	verifyMu.Lock()
	got := verifyPromptGot
	verifyMu.Unlock()
	require.NotEmpty(t, got, "复验应已派发")
	require.Contains(t, got, "【本轮台账变更清单（修复动过台账）】",
		"修复动了台账 → 复验提示词应带下游影响核对块")
	require.Contains(t, got, `台账变更 characters:7 name: "旧名"→"新名"（source=auto）`)
	require.Contains(t, got, "下游影响")
}
