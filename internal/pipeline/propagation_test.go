package pipeline

// propagation_test.go —— R2b-8 修复传播检查测试（提案 §8.4 组件 C）：
//   - verifyPrompt 传播块契约：空=提示词逐字一致（降级零提示），非空=共引条目
//     归因、位置在上下文块前；
//   - propagationCheck：种子章节（fixed 作用域）/种子条目（FixLog "L:" 履历）取
//     依赖闭包并按阅读序渲染；
//   - 闭包超阈值（>8 章）：传播块不注入（不硬修）+ 闸门 summary 明示升级；
//   - L0 全书规则扫描兜底：逐章重扫落质量档案、summary 计数、读失败跳过该章、
//     无读章能力降级零提示。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/timeline"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedPropChapters 顺序建 n 个章节（阅读序=创建序 1..n），返回章节 ID 列表。
func seedPropChapters(t *testing.T, db *gorm.DB, novelID int64, n int) []int64 {
	t.Helper()
	st := chapter.NewStore(db, slog.Default())
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		ch, err := st.Create(context.Background(), nil, novelID, nil, fmt.Sprintf("第%d章", i+1))
		require.NoError(t, err)
		ids = append(ids, ch.ID)
	}
	return ids
}

// seedPropTimeline 落一条伏笔条目（埋设章/回收章可空），返回条目 ID。
func seedPropTimeline(t *testing.T, db *gorm.DB, novelID int64, source, resolved *int64, targetReading int) int64 {
	t.Helper()
	row := timeline.TimelineEntry{
		NovelID:             novelID,
		Category:            categoryForeshadowing,
		Status:              "pending",
		Title:               "伏笔",
		SourceChapterID:     source,
		ResolvedChapterID:   resolved,
		TargetReadingNumber: targetReading,
	}
	require.NoError(t, db.Create(&row).Error)
	return row.ID
}

// TestVerifyPromptPropagationBlock 传播块契约：空=提示词与旧文逐字一致（降级零提示）；
// 非空=逐章列共引条目并要求核对传播损伤，位置在上下文块前。
func TestVerifyPromptPropagationBlock(t *testing.T) {
	targets := []verifyTarget{{ID: "I1", Level: "P1", Area: "人设", Desc: "动机缺失", Chapters: []int64{3}}}
	blocks := contextasm.ContextBlocks{Facts: "FACTS-MARKER"}

	base := verifyPrompt(1, targets, nil, "", blocks)
	require.Equal(t, base, verifyPrompt(1, targets, nil, propagationVerifyBlock(nil), blocks),
		"空闭包块=空串，提示词与旧文逐字一致（降级零提示）")
	require.NotContains(t, base, "【修复传播检查", "无依赖闭包不渲染该块")

	got := verifyPrompt(1, targets, nil,
		propagationVerifyBlock([]propDep{{ReadingNumber: 7, Refs: []string{"time_entries:12", "characters:3"}}}), blocks)
	require.Contains(t, got, "【修复传播检查（依赖闭包定向复验）】")
	require.Contains(t, got, "第 7 章（共引 time_entries:12、characters:3）", "共引条目归因逐章列出")
	require.Contains(t, got, "传播损伤", "要求核对修复传播损伤")
	require.Less(t, strings.Index(got, "【修复传播检查"), strings.Index(got, "FACTS-MARKER"),
		"块在上下文块（Facts）之前")
}

// TestServiceVerifyPropagationBlockAndSummary 端到端（R5-2）：工单面板复验（VerifyIssueTickets）
// 复用增量复验契约——修复改动第 1 章 → 伏笔共引第 3 章 → 复验提示词带传播块（第 3 章+共引条目）。
func TestServiceVerifyPropagationBlockAndSummary(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &timeline.TimelineEntry{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(941)
	ids := seedPropChapters(t, db, novelID, 3)
	entryID := seedPropTimeline(t, db, novelID, &ids[0], &ids[2], 3)

	var verifyPromptGot string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-verify" {
			verifyPromptGot = input.Prompt
			return SkillOutput{Text: "```json\n" +
				`{"verified":[{"id":"I1","ok":true,"note":"已修复"}],"summary":"1 项修复已确认"}` + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第1章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[1]}]}`
	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{
		Audit: auditJSON, AuditStage: StageS4,
		FixLog: []FixLogEntry{{ID: "I1", Note: "重排了第1章节奏"}},
	}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.store.ReplaceIssues(ctx, novelID, run.RunID, 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏爽点", Desc: "第1章拖沓", FixType: FixTypeLedger, ChapterIDs: []int64{1}},
	}))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, novelID, run.RunID, "I1", IssueStatusFixing, ""))

	_, err = svc.VerifyIssueTickets(ctx, novelID, []IssueTicketRef{{RunID: run.RunID, IssueKey: "I1"}})
	require.NoError(t, err)

	require.Contains(t, verifyPromptGot, "【修复传播检查（依赖闭包定向复验）】", "闭包非空应注入传播块")
	require.Contains(t, verifyPromptGot, fmt.Sprintf("第 3 章（共引 time_entries:%d）", entryID))
	require.NotContains(t, verifyPromptGot, "第 1 章（共引", "种子章（改动章）不进依赖清单")
}

// TestServiceVerifyPropagationEscalates 超阈值（>8 章）= 结构性牵连：传播块不注入
// （不硬修），定点复验清单照常（已修复项仍要验）。
func TestServiceVerifyPropagationEscalates(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &timeline.TimelineEntry{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(942)
	ids := seedPropChapters(t, db, novelID, 10)
	// 第 1 章埋下 9 条伏笔、分别在第 2~10 章回收 → 依赖闭包 9 章（>8）。
	for i := 1; i < 10; i++ {
		seedPropTimeline(t, db, novelID, &ids[0], &ids[i], 0)
	}

	var verifyPromptGot string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		if name == "signability-verify" {
			verifyPromptGot = input.Prompt
			return SkillOutput{Text: "```json\n" +
				`{"verified":[{"id":"I1","ok":true,"note":"已修复"}],"summary":"1 项修复已确认"}` + "\n```"}, nil
		}
		return SkillOutput{Text: "ok"}, nil
	}

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"待修","issues":[` +
		`{"id":"I1","level":"P1","area":"节奏爽点","desc":"第1章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[1]}]}`
	run, err := svc.StartPipeline(ctx, novelID, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS4, StatusRunning, string(ctxtJSON)))

	require.NoError(t, svc.store.ReplaceIssues(ctx, novelID, run.RunID, 0, []AuditIssue{
		{ID: "I1", Level: "P1", Area: "节奏爽点", Desc: "第1章拖沓", FixType: FixTypeLedger, ChapterIDs: []int64{1}},
	}))
	require.NoError(t, svc.store.TransitionIssueTicket(ctx, novelID, run.RunID, "I1", IssueStatusFixing, ""))

	_, err = svc.VerifyIssueTickets(ctx, novelID, []IssueTicketRef{{RunID: run.RunID, IssueKey: "I1"}})
	require.NoError(t, err)

	require.NotContains(t, verifyPromptGot, "【修复传播检查",
		"闭包超阈值不注入传播块（结构性牵连不硬修）")
	require.Contains(t, verifyPromptGot, `"id":"I1"`, "定点复验清单照常（已修复项仍要验）")
}

// TestPropagationCheckSeedEntriesFromFixLog 条目种子：FixLog "L:" 台账变更履历 →
// 被改条目（表名:id）→ 绑定该条目的章节成为受影响依赖；跨书履历/坏 ID 不造种子。
func TestPropagationCheckSeedEntriesFromFixLog(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &storage.LedgerChangeHistory{}, &storage.LedgerProvenance{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(943)
	ids := seedPropChapters(t, db, novelID, 3)

	// 修复动了 characters:7（履历 L:1）；provenance 标注其来源章=第 2 章。
	hist := storage.LedgerChangeHistory{
		NovelID: novelID, EntryTable: "characters", EntryID: 7,
		Field: "name", Old: "旧名", New: "新名", Source: storage.HistorySourceAuto, At: time.Now(),
	}
	require.NoError(t, db.Create(&hist).Error)
	other := storage.LedgerChangeHistory{
		NovelID: 999, EntryTable: "characters", EntryID: 8,
		Field: "name", Old: "他书", New: "他书新", Source: storage.HistorySourceAuto, At: time.Now(),
	}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&storage.LedgerProvenance{
		NovelID: novelID, EntryTable: "characters", EntryID: 7, SourceChapterID: &ids[1],
	}).Error)

	fixLog := []FixLogEntry{
		{ID: fmt.Sprintf("L:%d", hist.ID), Note: "台账变更 characters:7 name: \"旧名\"→\"新名\"（source=auto）"},
		{ID: fmt.Sprintf("L:%d", other.ID), Note: "他书履历"}, // 跨书：不入种子
		{ID: "L:abc", Note: "坏履历 ID"},                     // 解析失败：不入种子
		{ID: "I1", Note: "正文修复摘要"},                        // 非台账条目：不入种子
	}

	// 无种子章节（targets 无作用域）时，仅凭条目种子也能拉出受影响章。
	got := svc.propagationCheck(ctx, novelID, nil, fixLog)
	require.Equal(t, []propDep{{ReadingNumber: 2, Refs: []string{"characters:7"}}}, got.deps,
		"被改条目的来源章=第 2 章（阅读序 2）成为受影响依赖")
	require.Equal(t, 1, got.count)
	require.False(t, got.escalate)
	require.Contains(t, got.block(), "第 2 章（共引 characters:7）")

	// 零种子=空结果（提示词逐字一致）。
	require.Empty(t, svc.propagationCheck(ctx, novelID, nil, nil).block())
}

// TestPropagationCheckDegrades 降级矩阵：章节表缺失/读序映射失败 → 空结果零提示；
// 非法阅读序号不造种子（宁缺勿错）。
func TestPropagationCheckDegrades(t *testing.T) {
	ctx := context.Background()
	// 章节表未建：GetReadingNumbersByNovel 取数失败 → 空结果（不 panic、零提示）。
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	got := svc.propagationCheck(ctx, 944, []verifyTarget{{ID: "I1", Chapters: []int64{1}}}, nil)
	require.Empty(t, got.deps)
	require.Empty(t, got.summary())

	// 阅读序号无法定位（章不存在）→ 不造种子。
	db2 := newTestDB(t)
	require.NoError(t, db2.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	svc2 := NewService(db2, nil, nil, nil, "", "", context.Background(), slog.Default())
	seedPropChapters(t, db2, 945, 2)
	got2 := svc2.propagationCheck(ctx, 945, []verifyTarget{{ID: "I1", Chapters: []int64{99}}}, nil)
	require.Empty(t, got2.deps, "无法定位的阅读序号跳过（宁缺勿错）")
}

// TestL0RuleSweep L0 全书规则扫描兜底：逐章重扫落质量档案、summary 计数、
// 单章读失败跳过该章、无读章能力/零章可扫降级零提示。
func TestL0RuleSweep(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &quality.ChapterQuality{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	novelID := int64(946)
	ids := seedPropChapters(t, db, novelID, 3)

	// 无读章能力：空串（summary 与无该段逐字一致）。
	require.Empty(t, svc.l0RuleSweep(ctx, novelID))

	texts := map[int64]string{
		ids[0]: "",               // 空章 → empty_chapter 发现
		ids[1]: "忽然，他停下了脚步。后续呢？", // 短章 → word_count 发现（有钩）
		ids[2]: "",               // 读取失败 → 跳过该章
	}
	svc.SetChapterTextReader(func(_ context.Context, _ int64, chapterID int64) (string, error) {
		if chapterID == ids[2] {
			return "", fmt.Errorf("read fail")
		}
		return texts[chapterID], nil
	})

	seg := svc.l0RuleSweep(ctx, novelID)
	require.Contains(t, seg, "L0 全书规则扫描：受检 2 章", "读取失败的章节跳过不计受检")
	require.Contains(t, seg, "规则告警")
	require.Contains(t, seg, "empty_chapter 1", "按检查项计数进 summary")

	// 档案落盘：空章档案带 empty_chapter 发现（C 系）；读失败章档案不被覆盖为「已查」。
	row, err := svc.qualityStore.GetByChapter(ctx, novelID, ids[0])
	require.NoError(t, err)
	require.Contains(t, row.L0Findings, "empty_chapter")
	row2, err := svc.qualityStore.GetByChapter(ctx, novelID, ids[2])
	require.NoError(t, err)
	require.Nil(t, row2, "读失败章不落档案（宁缺勿错，不谎报已查）")

	// 零告警口径：全部可读且规则通过 → 「零规则告警」。
	svc.SetChapterTextReader(func(_ context.Context, _ int64, chapterID int64) (string, error) {
		if chapterID == ids[0] || chapterID == ids[1] {
			return strings.Repeat("他望着远处的山，心中忽而一动。", 200) + "？", nil
		}
		return "", fmt.Errorf("read fail")
	})
	seg2 := svc.l0RuleSweep(ctx, novelID)
	require.Contains(t, seg2, "受检 2 章")
	require.Contains(t, seg2, "零规则告警")
}
