package pipeline

// r5_e2e_smoke_test.go — R5 终态闭环冒烟（检测核心化收官验收，提案 §4.0 终态图）。
//
// 按终态闭环真实走一遍（LLM 以 skillRunner/章节检测执行器 stub 代跑，其余全走真实服务面）：
//
//	写（S3 逐章推进 + 里程碑非阻塞 note）
//	  → 检测任务（chapter / volume / book / structure 四 kind 各自独立启动）
//	  → 质量档案 + 发现项工单沉淀
//	  → 人工确认 / 去修复（工单执行体：正文级章节重写 + 台账级修复）
//	  → 显式复验（signability-verify 回执逐条落表）
//	  → 回检测（同容器整批替换刷新，新一轮清单语义）
//
// 并核汇总视图数据源口径（消费面：PipelineGateCard / 工单面板）：
//   - verdict/summary 取检测容器 ContextJSON 的 audit 字段（各容器互不串门）；
//   - 未决工单计数 = 非终态行数（verified/dismissed 终态不计未决）。
//
// 与既有单元测试的分工：本文件只锁「全链路能走通 + 各环衔接口径」；单环深挖
// （状态机边角/降级/幂等）不在本文件，见 issue_tickets_test / detect_task_test 等。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/volume"
)

// smokePassFixture 是修复后回检测的体检产出（pass，零 issue）。
const smokePassFixture = `{"verdict":"pass","score":95,"summary":"无致命伤","issues":[]}`

// smokeVerifyFixture 是显式复验的回执产出：I1 通过、I2 未通过退回待修。
const smokeVerifyFixture = `{"verified":[{"id":"I1","ok":true,"note":"人设已立"},{"id":"I2","ok":false,"note":"只改了一半"}],"summary":"1 项确认"}`

// TestR5E2ESmokeLoop 按 §10 终态图走通全闭环并核汇总视图数据源口径。
func TestR5E2ESmokeLoop(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &quality.ChapterQuality{}, &volume.Volume{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	const novelID = int64(2560)

	// ── LLM 代跑 stub：按技能名回合法契约夹具。卷体检按轮次切换产出
	// （首轮 needs_fix 两条；回检测 pass 零条），模拟修复后复检刷新。
	var auditRound atomic.Int32
	var mu sync.Mutex
	var rewriteChapters []int64 // chapter-rewrite 派发定位到的章节（防并行写切片）
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			if auditRound.Load() >= 2 {
				return SkillOutput{Text: "结论\n```json\n" + smokePassFixture + "\n```"}, nil
			}
			return SkillOutput{Text: "结论\n```json\n" + detectAuditFixture + "\n```"}, nil
		case "signability-fix":
			// 台账级 I2 的修复回执（契约 fixReceiptFormatLine 形状）。
			return SkillOutput{Text: "已修复。\n```json\n{\"fixed\":[\"I2\"],\"skipped\":[]}\n```"}, nil
		case "chapter-rewrite":
			mu.Lock()
			rewriteChapters = append(rewriteChapters, extractChapterIDFromPrompt(t, input.Prompt))
			mu.Unlock()
			return SkillOutput{Text: "已整章重写"}, nil
		case "signability-verify":
			return SkillOutput{Text: "```json\n" + smokeVerifyFixture + "\n```"}, nil
		case "consistency-patrol":
			return SkillOutput{Text: "```json\n" + detectAuditFixture + "\n```"}, nil
		default: // blueprint-drift-check 等：无 JSON 回执 → 按零发现收口。
			return SkillOutput{Text: "ok"}, nil
		}
	}

	// ── 事件捕获：全程零闸门（写不弹闸 R5-4 + 检测-only 零闸门 R5-3）。
	gates := withCapturedGates(t)
	var notesMu sync.Mutex
	var notes []string
	t.Cleanup(swapEmitStage(func(_ context.Context, p StagePayload) {
		if p.Note != "" {
			notesMu.Lock()
			notes = append(notes, p.Note)
			notesMu.Unlock()
		}
	}))
	detectCh := make(chan map[string]any, 8)
	t.Cleanup(swapEmitDetectDone(func(_ context.Context, p map[string]any) { detectCh <- p }))
	patrolCh := make(chan map[string]any, 4)
	t.Cleanup(swapEmitConsistencyPatrol(func(_ context.Context, p map[string]any) { patrolCh <- p }))
	var fixStarts, fixDones atomic.Int32
	var lastFixDone map[string]any
	var fixDoneMu sync.Mutex
	t.Cleanup(swapEmitFixStart(func(_ context.Context, _ map[string]any) { fixStarts.Add(1) }))
	t.Cleanup(swapEmitFixDone(func(_ context.Context, p map[string]any) {
		fixDones.Add(1)
		fixDoneMu.Lock()
		lastFixDone = p
		fixDoneMu.Unlock()
	}))

	// ── 阶段 1：写（S3 逐章推进 5 章）——第 5 章触发里程碑复检建议（非阻塞 note）。
	runID := startS3Run(t, svc, novelID, `{"s4_audited":true,"last_audit_chapter":0}`)
	seeded := make([]chapter.Chapter, 0, 5)
	for i := 1; i <= 5; i++ {
		ch := chapter.Chapter{NovelID: novelID, Title: fmt.Sprintf("第%d章", i), SortOrder: i}
		require.NoError(t, db.Create(&ch).Error, "写入第 %d 章", i)
		seeded = append(seeded, ch)
		require.NoError(t, svc.ResumePipeline(ctx, runID, "continue", ""), "第 %d 章完成推进", i)
	}
	notesMu.Lock()
	gotNotes := append([]string(nil), notes...)
	notesMu.Unlock()
	require.Len(t, gotNotes, 5, "每章一条进度 note")
	require.Contains(t, gotNotes[0], "已完成第 1 章")
	require.NotContains(t, strings.Join(gotNotes[:4], "\n"), "建议", "普通节奏只给进度 note")
	require.Contains(t, gotNotes[4], "建议进入体检复检", "第 5 章里程碑应给非阻塞复检建议 note")
	got, gerr := svc.store.GetByRunID(ctx, runID)
	require.NoError(t, gerr)
	require.Equal(t, StageS3, got.CurrentStage)
	require.Equal(t, StatusRunning, got.Status, "写循环持续 running，不被检测打断（R5-4 G3b 退役）")

	// ── 阶段 2：检测任务（四 kind 各自独立启动，不依赖流水线阶段）。
	// ① 章节检测 → 质量档案（chapterci L1 回执）。
	svc.SetChapterCIRunner(func(c context.Context, novelID, chapterID int64) error {
		return mcp_tools.RunChapterCIForChapter(c, func(_ context.Context, req mcp_tools.SubAgentRequest) (string, error) {
			require.Equal(t, "chapterci", req.AgentType)
			return detectTaskL1Fixture, nil
		}, nil, db, slog.Default(), "smoke", novelID, chapterID)
	})
	cid := seeded[0].ID
	chRes, err := svc.RunDetectionTask(ctx, novelID, DetectionKindChapter, &cid)
	require.NoError(t, err)
	require.True(t, chRes.Dispatched)
	require.Eventually(t, func() bool {
		got, gerr := quality.NewStore(db, slog.Default()).GetByChapter(ctx, novelID, cid)
		return gerr == nil && got != nil && got.L1Status == quality.L1StatusChecked
	}, 5*time.Second, 10*time.Millisecond, "章节检测应落质量档案")

	// ② 卷体检 → 发现项工单（检测容器）。建卷归属校验面。
	vol := &volume.Volume{NovelID: novelID, Name: "第一卷", SortOrder: 1}
	require.NoError(t, db.Create(vol).Error)
	volRes, err := svc.RunDetectionTask(ctx, novelID, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	require.True(t, volRes.Dispatched)
	volDone := waitForDetectDone(t, detectCh)
	require.Equal(t, DetectionKindVolume, volDone["kind"])
	require.Equal(t, "needs_fix", volDone["verdict"])
	require.Equal(t, 2, volDone["issue_count"])
	require.Equal(t, volRes.RunID, volDone["run_id"])

	// ③ 书体检 → 发现项工单（独立容器）。
	bookRes, err := svc.RunDetectionTask(ctx, novelID, DetectionKindBook, nil)
	require.NoError(t, err)
	require.NotEqual(t, volRes.RunID, bookRes.RunID, "卷/书检测各自独立容器")
	bookDone := waitForDetectDone(t, detectCh)
	require.Equal(t, DetectionKindBook, bookDone["kind"])

	// ④ 巡检·结构 → 张力报告（只报告，零发现项工单）。
	structRes, err := svc.RunDetectionTask(ctx, novelID, DetectionKindStructure, nil)
	require.NoError(t, err)
	select {
	case p := <-patrolCh:
		require.Equal(t, structRes.RunID, p["run_id"])
	case <-time.After(5 * time.Second):
		t.Fatal("巡检完成事件未到达")
	}

	// 工单沉淀口径：卷/书各 2 条 pending，巡检零工单。
	volRows, lerr := svc.store.ListIssues(ctx, novelID, volRes.RunID)
	require.NoError(t, lerr)
	require.Len(t, volRows, 2, "卷体检发现项应落检测容器工单")
	bookRows, lerr := svc.store.ListIssues(ctx, novelID, bookRes.RunID)
	require.NoError(t, lerr)
	require.Len(t, bookRows, 2)
	structRows, lerr := svc.store.ListIssues(ctx, novelID, structRes.RunID)
	require.NoError(t, lerr)
	require.Empty(t, structRows, "巡检只报告不改写：零发现项工单")

	// ── 阶段 3：人工确认 + 去修复（工单执行体，显式触发）。
	refs := []IssueTicketRef{{RunID: volRes.RunID, IssueKey: "I1"}, {RunID: volRes.RunID, IssueKey: "I2"}}
	require.NoError(t, svc.ConfirmIssueTickets(ctx, novelID, refs))
	require.NoError(t, svc.StartIssueFixTickets(ctx, novelID, refs))

	// 正文级 I1（chapter_ids [1,2,3]）逐章重写；台账级 I2 走 signability-fix。
	mu.Lock()
	gotRewrites := append([]int64(nil), rewriteChapters...)
	mu.Unlock()
	require.Len(t, gotRewrites, 3, "正文级工单应按作用域逐章派发重写")
	volRows, lerr = svc.store.ListIssues(ctx, novelID, volRes.RunID)
	require.NoError(t, lerr)
	for _, r := range volRows {
		require.Equal(t, IssueStatusFixed, r.Status, "工单 %s 修复后应落 fixed", r.IssueKey)
	}
	require.Equal(t, int32(1), fixStarts.Load())
	require.Equal(t, int32(1), fixDones.Load())
	fixDoneMu.Lock()
	gotFixDone := lastFixDone
	fixDoneMu.Unlock()
	require.Equal(t, true, gotFixDone["ok"])
	require.Equal(t, true, gotFixDone["tickets"], "fix_done 应带 tickets 标记（去工单面板复验）")
	// 修复收口不动检测容器身份（出生即终态）。
	volRun, gerr := svc.store.GetByRunID(ctx, volRes.RunID)
	require.NoError(t, gerr)
	require.Equal(t, detectContainerStage, volRun.CurrentStage)
	require.Equal(t, StatusFinished, volRun.Status, "工单修复收口不得改写检测容器状态")
	var fixedCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(volRun.ContextJSON), &fixedCtxt))
	require.Equal(t, 1, fixedCtxt.FixRounds, "工单修复收口令修复轮数 +1")
	require.Contains(t, fixedCtxt.DriftReport, "蓝图巡检", "修复收口后蓝图巡检结论落 DriftReport")

	// ── 阶段 4：显式复验（signability-verify 回执逐条落表）。
	sum, err := svc.VerifyIssueTickets(ctx, novelID, refs)
	require.NoError(t, err)
	require.Equal(t, 1, sum.Confirmed, "I1 复验通过")
	require.Equal(t, 1, sum.Rejected, "I2 未通过退回待修")
	require.Equal(t, 0, sum.Unverified)
	volRows, lerr = svc.store.ListIssues(ctx, novelID, volRes.RunID)
	require.NoError(t, lerr)
	byKey := map[string]PipelineIssue{}
	for _, r := range volRows {
		byKey[r.IssueKey] = r
	}
	require.Equal(t, IssueStatusVerified, byKey["I1"].Status)
	require.Equal(t, "人设已立", byKey["I1"].VerifyReceipt)
	require.NotNil(t, byKey["I1"].VerifyAt)
	require.Equal(t, IssueStatusPending, byKey["I2"].Status, "复验未通过退回待修")
	require.Equal(t, "只改了一半", byKey["I2"].VerifyReceipt)

	// 驳回留痕（终态 + 理由）：书容器 I2 误报驳回，供汇总计数口径覆盖终态行。
	require.NoError(t, svc.DismissIssueTickets(ctx, novelID,
		[]IssueTicketRef{{RunID: bookRes.RunID, IssueKey: "I2"}}, "误报，编辑视角主观差异"))

	// ── 阶段 5：回检测（同容器整批替换刷新，新一轮清单语义）。
	auditRound.Store(2)
	res2, err := svc.RunDetectionTask(ctx, novelID, DetectionKindVolume, &vol.ID)
	require.NoError(t, err)
	require.Equal(t, volRes.RunID, res2.RunID, "同 kind+scope 复用同一检测容器")
	volDone2 := waitForDetectDone(t, detectCh)
	require.Equal(t, DetectionKindVolume, volDone2["kind"])
	require.Equal(t, "pass", volDone2["verdict"], "回检测应反映新一轮结论")
	require.Equal(t, 0, volDone2["issue_count"])
	volRows, lerr = svc.store.ListIssues(ctx, novelID, volRes.RunID)
	require.NoError(t, lerr)
	require.Empty(t, volRows, "新体检=新问题清单：整批替换，旧清单连同复验痕迹整体退场")
	volRun, gerr = svc.store.GetByRunID(ctx, volRes.RunID)
	require.NoError(t, gerr)
	var freshCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(volRun.ContextJSON), &freshCtxt))
	require.Nil(t, freshCtxt.FixStates, "新一轮清单应清修复断点")
	require.Nil(t, freshCtxt.FixLog, "新一轮清单应清修复日志")
	require.Empty(t, freshCtxt.RewriteBacklog, "pass 零正文级问题，重写清单为空")

	// ── 汇总视图数据源口径（消费面 PipelineGateCard / 工单面板）。
	// verdict/summary 取各检测容器 ContextJSON.audit（容器互不串门）。
	var volAudit, bookAudit AuditData
	require.NoError(t, json.Unmarshal([]byte(freshCtxt.Audit), &volAudit))
	require.Equal(t, "pass", volAudit.Verdict)
	require.Equal(t, "无致命伤", volAudit.Summary)
	bookRun, gerr := svc.store.GetByRunID(ctx, bookRes.RunID)
	require.NoError(t, gerr)
	var bookCtxt GateContext
	require.NoError(t, json.Unmarshal([]byte(bookRun.ContextJSON), &bookCtxt))
	require.NoError(t, json.Unmarshal([]byte(bookCtxt.Audit), &bookAudit))
	require.Equal(t, "needs_fix", bookAudit.Verdict, "书容器结论不受卷容器刷新影响")
	require.Equal(t, "检测出2条问题", bookAudit.Summary)
	// 未决工单计数 = 非终态行数（verified/dismissed 不计未决）：
	// 卷容器整批替换后仅剩书容器 I1（pending）+ I2（dismissed 终态）。
	allRows, lerr := svc.ListIssueTickets(ctx, novelID)
	require.NoError(t, lerr)
	pending, terminal := 0, 0
	for _, r := range allRows {
		if r.Status == IssueStatusVerified || r.Status == IssueStatusDismissed {
			terminal++
		} else {
			pending++
		}
	}
	require.Equal(t, 1, pending, "未决工单计数口径：非终态行")
	require.Equal(t, 1, terminal, "终态行（dismissed）不计未决")

	// 全程零闸门：写循环不弹闸（R5-4）+ 检测/工单/复验零闸门广播（检测核心）。
	require.Empty(t, gates.all(), "终态闭环全程零闸门事件")
}
