package mcp_tools

// chapter_ci.go —— 章节 CI（R2 提案 §4.2）：写后质检钩子。
//
// 每章正文落盘后：
//   - L0 确定性检查**同步**跑（毫秒级零 LLM）→ 指标/发现落质量档案（UpsertL0 不动 L1 列）；
//   - L1 增量检查**异步** debounced 派发 chapter-ci 子 Agent（小上下文、只报告不改写），
//     回执按 quality/contract.go 契约解析后落档案（UpsertL1 不动 L0 列）。
//
// 降级逐字一致（宁缺勿错）：任何失败（存储错/派发失败/回执坏）静默降级，
// 零提示零报错、不影响写入主链路；日志仅 Warn 供排查。

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/voice"
)

var (
	chapterCIMu     sync.Mutex
	chapterCITimers = map[string]*time.Timer{}
)

// chapterCIDebounce 是 L1 派发去抖窗口。比 stateSyncDebounce（6s）长，
// 让 L1 检查在 statesync 台账回填之后跑（「涉及台账条目」已是本章同步后状态）。
const chapterCIDebounce = 10 * time.Second

// ScheduleChapterCI 章落盘钩子入口（R2）：L0 同步落档案 + L1 异步去抖派发。
// content 是刚落盘的正文（直接喂 L0，避免回读）；ch 为 nil 时整体跳过。
func ScheduleChapterCI(ctx context.Context, tc ToolContext, ch *chapter.Chapter, content string) {
	if ch == nil || tc.DB == nil {
		return
	}
	runChapterL0(ctx, tc, ch.ID, content)
	if tc.RunSubAgent == nil {
		return // 无子 Agent 能力：L1 降级不跑（零提示）
	}
	key := fmt.Sprintf("%d:%d", tc.NovelID, ch.ID)
	chapterCIMu.Lock()
	if t, ok := chapterCITimers[key]; ok {
		t.Stop()
	}
	chapterCITimers[key] = time.AfterFunc(chapterCIDebounce, func() {
		chapterCIMu.Lock()
		delete(chapterCITimers, key)
		chapterCIMu.Unlock()
		runChapterL1(context.WithoutCancel(ctx), tc, tc.NovelID, ch.ID)
	})
	chapterCIMu.Unlock()
}

// runChapterL0 跑 L0 确定性检查并同步落档案（UpsertL0 只写 L0 列）。
// 输入缺啥降级啥：无基线零漂移发现、阅读序号取不到发现不带作用域、题材取不到不豁免。
func runChapterL0(ctx context.Context, tc ToolContext, chapterID int64, content string) {
	readingNumber, _ := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).
		GetReadingNumberByID(ctx, nil, tc.NovelID, chapterID)
	metrics, findings := quality.RunL0Checks(quality.L0Input{
		Text:          content,
		ReadingNumber: readingNumber,
		Baseline:      chapterCIVoiceBaseline(ctx, tc),
		Genre:         novelGenre(ctx, tc),
	})
	err := quality.NewStore(tc.DB, tc.LoggerOrDefault()).UpsertL0(ctx, &quality.ChapterQuality{
		NovelID:       tc.NovelID,
		ChapterID:     chapterID,
		ReadingNumber: readingNumber,
		L0Metrics:     quality.MarshalL0Metrics(metrics),
		L0Findings:    quality.MarshalFindings(findings),
		CheckedAt:     time.Now(),
	})
	if err != nil {
		tc.LoggerOrDefault().Warn("章节 CI L0 落档案失败（非阻断）", "err", err, "novelID", tc.NovelID, "chapterID", chapterID)
	}
}

// chapterCIVoiceBaseline 装配 Voice 台账基线指标（voice.MergeMetrics，与 app 层
// P2 漂移检测 provider 同一口径）。失败/空 → nil（无基线零漂移发现，不造默认基线）。
func chapterCIVoiceBaseline(ctx context.Context, tc ToolContext) map[string]float64 {
	entries, err := voice.NewStore(tc.DB, tc.LoggerOrDefault()).List(ctx, tc.NovelID)
	if err != nil {
		return nil
	}
	return voice.MergeMetrics(entries)
}

// runChapterL1 去抖到点后派发 L1 增量检查（回执落档案在派发链内完成）。
func runChapterL1(ctx context.Context, tc ToolContext, novelID, chapterID int64) {
	if tc.RunSubAgent == nil {
		return
	}
	_ = RunChapterCIForChapter(ctx, tc.RunSubAgent, tc.SkillStore, tc.DB, tc.LoggerOrDefault(), tc.ToolID, novelID, chapterID)
}

// RunChapterCIForChapter 对指定章节执行 L1 增量检查（chapterci 子 Agent）并把回执
// 落质量档案。runSub 为实际派发子 Agent 的函数（调用方决定用 ToolContext.RunSubAgent
// 还是 agent.RunSubAgent）；skillStore 用于注入 chapter-ci 技能方法论（可空，降级为
// 内联最小指令）；db 是质量档案存储。返回派发/回执错误（非阻断调用方可忽略）。
//
// 落档案口径（降级逐字一致）：
//   - 回执解析成功 → UpsertL1（findings + l1_status=checked）；
//   - 派发失败 / 回执坏 → UpsertL1（l1_status=skipped），零提示零报错。
func RunChapterCIForChapter(ctx context.Context, runSub RunSubAgentFunc, skillStore *skill.Store,
	db *gorm.DB, logger *slog.Logger, toolID string, novelID, chapterID int64) error {
	if runSub == nil {
		return nil
	}
	instruction := buildChapterCIInstruction(skillStore, novelID, chapterID)
	report, err := runSub(ctx, SubAgentRequest{
		AgentType:   "chapterci",
		NovelID:     novelID,
		Instruction: instruction,
		ToolID:      toolID,
	})

	// 落档案公共路径：readingNumber 取不到按 0（发现不带作用域，降级不阻断）。
	readingNumber, _ := chapter.NewStore(db, logger).GetReadingNumberByID(ctx, nil, novelID, chapterID)
	record := &quality.ChapterQuality{
		NovelID:       novelID,
		ChapterID:     chapterID,
		ReadingNumber: readingNumber,
		CheckedAt:     time.Now(),
	}

	if err != nil {
		if logger != nil {
			logger.Warn("chapter-ci 子 Agent 派发失败（非阻断）", "err", err, "novelID", novelID, "chapterID", chapterID)
		}
		record.L1Status = quality.L1StatusSkipped
		upsertL1(ctx, db, logger, record, novelID, chapterID)
		return err
	}
	receipt, perr := quality.ParseL1Receipt(report)
	if perr != nil {
		if logger != nil {
			logger.Warn("chapter-ci 回执解析失败（非阻断，降级跳过）", "err", perr, "novelID", novelID, "chapterID", chapterID)
		}
		record.L1Status = quality.L1StatusSkipped
		upsertL1(ctx, db, logger, record, novelID, chapterID)
		return perr
	}
	record.L1Status = quality.L1StatusChecked
	record.L1Findings = quality.MarshalFindings(receipt.ToFindings(readingNumber))
	upsertL1(ctx, db, logger, record, novelID, chapterID)
	return nil
}

// upsertL1 落 L1 档案（UpsertL1 只写 L1 列）；失败仅日志（降级零提示）。
func upsertL1(ctx context.Context, db *gorm.DB, logger *slog.Logger, record *quality.ChapterQuality, novelID, chapterID int64) {
	if db == nil {
		return
	}
	if werr := quality.NewStore(db, logger).UpsertL1(ctx, record); werr != nil && logger != nil {
		logger.Warn("chapter-ci L1 落档案失败（非阻断）", "err", werr, "novelID", novelID, "chapterID", chapterID)
	}
}

// buildChapterCIInstruction 构造 L1 增量检查指令：小上下文范围 + 检查项 + 回执契约，
// 有 chapter-ci 技能时注入方法论正文（单源），无技能时内联最小指令（降级）。
func buildChapterCIInstruction(skillStore *skill.Store, novelID, chapterID int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "章节（chapter_id=%d）正文刚刚修改并落盘。请对该章执行 L1 增量质检。\n", chapterID)
	b.WriteString("检查范围（小上下文，严禁全书通读）：本章正文（read 路径 chapters/id_" +
		fmt.Sprint(chapterID) + ".md）、前一章结尾/次一章开头片段（get_chapter_list 定位）、" +
		"本章计划卡（get_chapter_task_card）、涉及台账条目（get_* 定点读取）。\n")
	b.WriteString("检查项：continuity 一致性/连贯、pacing 节奏、foreshadow 伏笔 lifecycle、" +
		"reader_state 读者认知态、task_card 任务卡兑现。\n")
	b.WriteString("只报告不改写：不调用任何写工具、不评分、不给 verdict；零发现报空清单。\n")
	if skillStore != nil {
		if sk, ok := skillStore.Get(0, "chapter-ci"); ok && strings.TrimSpace(sk.Content) != "" {
			b.WriteString("\n# 本次任务方法论\n")
			b.WriteString(sk.Content)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n最终回复末尾必须附 ```json 代码块上报回执，格式：\n")
	b.WriteString(quality.L1ReceiptFormatLine())
	b.WriteString("\n")
	return b.String()
}
