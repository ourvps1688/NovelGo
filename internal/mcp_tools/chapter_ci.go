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

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/voice"
)

var (
	chapterCIMu     sync.Mutex
	chapterCITimers = map[string]*time.Timer{}
)

// TensionTicketLanderFunc 把一章的曲线四查发现落成/刷新 pipeline 情绪账工单
// （E 系，tension 容器），返回落库条数。
// 由 app 启动时注入（pipeline.LandTensionTickets）——mcp_tools 不能反向 import pipeline
// （pipeline→agent→mcp_tools 会成环），与 FeedbackTicketLanderFunc 同一避免循环依赖的纪律。
type TensionTicketLanderFunc func(ctx context.Context, db *gorm.DB, novelID int64, readingNumber int, findings []quality.Finding) (int, error)

// tensionTicketLander 是情绪账落单回调（nil=未装配，降级不落工单零提示）。
var tensionTicketLander TensionTicketLanderFunc

// SetTensionTicketLander 装配情绪账落单回调（app 启动时注入 pipeline.LandTensionTickets）。
func SetTensionTicketLander(fn TensionTicketLanderFunc) { tensionTicketLander = fn }

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

// runChapterL0 跑 L0 确定性检查并同步落档案（UpsertL0 只写 L0 列），
// 并刷新 L0 基线张力曲线（P0-2 情绪账，写后链路自动落库）。
// 输入缺啥降级啥：无基线零漂移发现、阅读序号取不到发现不带作用域、题材取不到不豁免、
// 角色台账取不到=称呼/死人检查自然跳过（无台账零发现逐字一致，R2b-5）。
// 题材卡参数（R2b-3）：章尾钩子必需/禁忌清单取本书检查器配置，未配置/读取失败回落缺省
// （=现行行为，逐字一致）。
func runChapterL0(ctx context.Context, tc ToolContext, chapterID int64, content string) {
	readingNumber, _ := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).
		GetReadingNumberByID(ctx, nil, tc.NovelID, chapterID)
	cfg := chapterCICheckerConfig(ctx, tc)
	metrics, findings := quality.RunL0Checks(quality.L0Input{
		Text:             content,
		ReadingNumber:    readingNumber,
		Baseline:         chapterCIVoiceBaseline(ctx, tc),
		Genre:            novelGenre(ctx, tc),
		TailHookRequired: cfg.TailHookRequired,
		TabooList:        cfg.Taboos(),
		Characters:       chapterCICharacters(ctx, tc),
		PowerLevels:      chapterCIPowerLevels(ctx, tc),
		Opening:          cfg.Opening(),
		Profile:          cfg.Profile(),
		BeatDensityMin:   cfg.EffectiveBeatDensityMin(),
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
	// P0-2 情绪账：L0 基线张力曲线随写后链路自动落库（纯确定性毫秒级）。
	// L0 行整体重算、既有 L1 行一并作废（正文已变）；失败仅告警，不阻断写入主链路。
	if terr := quality.NewStore(tc.DB, tc.LoggerOrDefault()).
		RefreshTensionCurve(ctx, tc.NovelID, chapterID, readingNumber, content); terr != nil {
		tc.LoggerOrDefault().Warn("章节 CI 张力曲线落库失败（非阻断）", "err", terr, "novelID", tc.NovelID, "chapterID", chapterID)
	}
	// P0-2 情绪账：曲线四查发现落工单（E 系，tension 容器；四查过滤在落单侧）。
	// 按章整批替换：零发现=清该章旧工单；未装配落单钩子=降级不落（零提示零报错）。
	if tensionTicketLander != nil {
		if _, terr := tensionTicketLander(ctx, tc.DB, tc.NovelID, readingNumber, findings); terr != nil {
			tc.LoggerOrDefault().Warn("章节 CI 情绪账工单落库失败（非阻断）", "err", terr, "novelID", tc.NovelID, "chapterID", chapterID)
		}
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

// chapterCICheckerConfig 装配本书检查器配置（题材卡机制化，R2b-3）。
// 未配置返回缺省（GetCheckerConfig 内建）；读取失败同样回落缺省（降级零提示，
// 绝不因配置缺失改变检查口径）。
func chapterCICheckerConfig(ctx context.Context, tc ToolContext) *quality.CheckerConfig {
	cfg, err := quality.NewStore(tc.DB, tc.LoggerOrDefault()).GetCheckerConfig(ctx, tc.NovelID)
	if err != nil {
		tc.LoggerOrDefault().Debug("章节 CI 检查器配置读取失败（回落缺省）", "err", err, "novelID", tc.NovelID)
		return quality.DefaultCheckerConfig(tc.NovelID)
	}
	return cfg
}

// chapterCICharacters 投影角色台账到 L0 输入（称呼混用/死人出镜，R2b-5）。
// 取数失败=nil=角色类检查自然跳过（降级零提示，无台账=零发现逐字一致）。
func chapterCICharacters(ctx context.Context, tc ToolContext) []quality.L0Character {
	rows := loadCharLedger(ctx, tc)
	out := make([]quality.L0Character, 0, len(rows))
	for _, r := range rows {
		out = append(out, quality.L0Character{Name: r.Name, Aliases: r.Aliases, Alive: r.Alive})
	}
	return out
}

// chapterCIPowerLevels 投影境界等级表到 L0 输入（越级取胜/低阶碾压，R2b-6）。
// 取数失败=nil=战力数值序检查自然跳过（降级零提示，无等级表=零发现逐字一致）。
func chapterCIPowerLevels(ctx context.Context, tc ToolContext) []quality.PowerLevelRef {
	if tc.DB == nil {
		return nil
	}
	rows, err := structure.NewStore(tc.DB).ListPowerLevels(ctx, tc.NovelID)
	if err != nil {
		return nil
	}
	out := make([]quality.PowerLevelRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, quality.PowerLevelRef{Name: r.Name, Rank: r.Rank})
	}
	return out
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
	instruction := buildChapterCIInstruction(skillStore, novelID, chapterID,
		chapterCITensionSegHints(ctx, db, logger, novelID, chapterID))
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
	landL1TensionCurve(ctx, db, logger, novelID, chapterID, readingNumber, receipt)
	return nil
}

// landL1TensionCurve 把回执 tension_curve 清洗后落 L1 曲线行（P0-2 情绪账：
// source=l1 与 L0 基线并存，消费侧 EffectiveTensionCurve 取 l1 > l0）。
// 段区间从既有 L0 基线行回填（seg → rune 区间对齐）；空曲线不动既有 L1 行
// （正文变化时 RefreshTensionCurve 已作废旧标注）；失败仅告警（降级零提示）。
func landL1TensionCurve(ctx context.Context, db *gorm.DB, logger *slog.Logger,
	novelID, chapterID int64, readingNumber int, receipt *quality.L1Receipt) {
	points := receipt.ToTensionPoints()
	if len(points) == 0 || db == nil {
		return
	}
	qs := quality.NewStore(db, logger)
	if rows, err := qs.ListTensionCurve(ctx, novelID, chapterID); err == nil {
		ranges := map[int][2]int{}
		for _, r := range rows {
			if r.Source == quality.TensionSourceL0 {
				ranges[r.Seg] = [2]int{r.StartRune, r.EndRune}
			}
		}
		for i := range points {
			if rg, ok := ranges[points[i].Seg]; ok {
				points[i].StartRune, points[i].EndRune = rg[0], rg[1]
			}
		}
	}
	if err := qs.ReplaceTensionCurve(ctx, novelID, chapterID, readingNumber, quality.TensionSourceL1, points); err != nil && logger != nil {
		logger.Warn("chapter-ci 情绪曲线落库失败（非阻断）", "err", err, "novelID", novelID, "chapterID", chapterID)
	}
}

// chapterCITensionSegHints 构造情绪曲线「段号对照」提示（seg → rune 区间 + L0 基线
// 标注），供 L1 标注对齐分段；无 L0 行/取失败返回空（调用方回落通用分段说明）。
func chapterCITensionSegHints(ctx context.Context, db *gorm.DB, logger *slog.Logger, novelID, chapterID int64) string {
	if db == nil {
		return ""
	}
	rows, err := quality.NewStore(db, logger).ListTensionCurve(ctx, novelID, chapterID)
	if err != nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range rows {
		if r.Source != quality.TensionSourceL0 {
			continue
		}
		if n >= chapterCITensionHintMaxSegs {
			b.WriteString("…（其余段按每约 400 字顺延）")
			break
		}
		if n > 0 {
			b.WriteString("、")
		}
		fmt.Fprintf(&b, "seg %d（字 %d-%d，基线 %s/%.0f）", r.Seg, r.StartRune, r.EndRune, r.BeatType, r.Tension)
		n++
	}
	return b.String()
}

// chapterCITensionHintMaxSegs 是「段号对照」提示的段数上限（防超长章提示膨胀）。
const chapterCITensionHintMaxSegs = 60

// upsertL1 落 L1 档案（UpsertL1 只写 L1 列）；失败仅日志（降级零提示）。
func upsertL1(ctx context.Context, db *gorm.DB, logger *slog.Logger, record *quality.ChapterQuality, novelID, chapterID int64) {
	if db == nil {
		return
	}
	if werr := quality.NewStore(db, logger).UpsertL1(ctx, record); werr != nil && logger != nil {
		logger.Warn("chapter-ci L1 落档案失败（非阻断）", "err", werr, "novelID", novelID, "chapterID", chapterID)
	}
}

// buildChapterCIInstruction 构造 L1 增量检查指令：小上下文范围 + 检查项 + 情绪曲线
// 标注（P0-2，段号对照对齐分段）+ 回执契约，有 chapter-ci 技能时注入方法论正文（单源），
// 无技能时内联最小指令（降级）。segHints 为空回落通用分段说明。
func buildChapterCIInstruction(skillStore *skill.Store, novelID, chapterID int64, segHints string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "章节（chapter_id=%d）正文刚刚修改并落盘。请对该章执行 L1 增量质检。\n", chapterID)
	b.WriteString("检查范围（小上下文，严禁全书通读）：本章正文（read 路径 chapters/id_" +
		fmt.Sprint(chapterID) + ".md）、前一章结尾/次一章开头片段（get_chapter_list 定位）、" +
		"本章计划卡（get_chapter_task_card）、涉及台账条目（get_* 定点读取）。\n")
	b.WriteString("检查项：continuity 一致性/连贯、pacing 节奏、foreshadow 伏笔 lifecycle、" +
		"reader_state 读者认知态、task_card 任务卡兑现、immersion_anchor 代入感锚点。\n")
	b.WriteString("只报告不改写：不调用任何写工具、不评分、不给 verdict；零发现报空清单。\n")
	if segHints != "" {
		fmt.Fprintf(&b, "情绪曲线标注：本章按每约 400 字分段，段号对照（seg（字区间，L0 基线标注））：%s。\n", segHints)
	} else {
		b.WriteString("情绪曲线标注：本章按每约 400 字自上而下分段（seg 从 0 起）。\n")
	}
	b.WriteString("对有把握的段在回执 tension_curve 标注情绪/张力起伏与爽点位置：seg 填段号，" +
		"tension 张力 0-100、emotion 情绪强度 0-100，beat_type 取 " +
		"climax|conflict|hook|dialogue|flat|transition（climax=爽点/情绪峰）。可只标部分段，宁缺勿错。\n")
	if skillStore != nil {
		if sk, ok := skillStore.Get(0, "chapter-ci"); ok && strings.TrimSpace(sk.Content) != "" {
			b.WriteString("\n# 本次任务方法论\n")
			b.WriteString(sk.Content)
			b.WriteString("\n")
			// #111 P0 统一账本：chapter-ci 走 RunSubAgent，不进 exec_run_stats，
			// 在注入点直接记账（口径陷阱之一）。
			activity.RecordSkillInvocation(context.Background(), activity.SkillInvocation{
				Skill: "chapter-ci", Channel: "chapter_ci", Mount: "chapter_ci",
				NovelID: novelID, ChapterID: chapterID, Runes: len([]rune(sk.Content)),
			})
		}
	}
	b.WriteString("\n最终回复末尾必须附 ```json 代码块上报回执，格式：\n")
	b.WriteString(quality.L1ReceiptFormatLine())
	b.WriteString("\n")
	return b.String()
}
