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
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/voice"
)

var (
	chapterCIMu     sync.Mutex
	chapterCITimers = map[string]*time.Timer{}
	// chapterCIMaxTurns 是单章 L1 复核的工具循环上限（#115 P0）。真实语料实测收敛约
	// 8 轮，留 12 轮余量；旧的 100 轮通用上限让一次检测可跑 36 次 LLM 调用 / 15 分钟。
	// 以 var 暴露便于按平台/模型调参与测试收缩。
	chapterCIMaxTurns = 12
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
	findings, err := recalcChapterL0(ctx, tc, chapterID, content)
	if err != nil {
		tc.LoggerOrDefault().Warn("章节 CI L0 落档案失败（非阻断）", "err", err, "novelID", tc.NovelID, "chapterID", chapterID)
	}
	readingNumber, _ := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).
		GetReadingNumberByID(ctx, nil, tc.NovelID, chapterID)
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

// recalcChapterL0 是 L0 重算的单一实现：按正文跑确定性检查并覆盖该章 L0 档案
// （UpsertL0 只写 L0 列，L1 列不动）。写后链路与显式「重新检测本章」共用，
// 保证两处的字数口径、检查器配置、题材/台账投影完全一致。
func recalcChapterL0(ctx context.Context, tc ToolContext, chapterID int64, content string) ([]quality.Finding, error) {
	readingNumber, _ := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).
		GetReadingNumberByID(ctx, nil, tc.NovelID, chapterID)
	cfg := chapterCICheckerConfig(ctx, tc)
	// #113 单章字数口径单源：平台 profile / 偏好卡 → 一次解析，检测侧 L0 与
	// 写作侧 system 共用同一结果，杜绝「体检 1000–1500、修复对比 2000」。
	chapterWords := quality.NewStore(tc.DB, tc.LoggerOrDefault()).ChapterWords(ctx, tc.NovelID)
	metrics, findings := quality.RunL0Checks(quality.L0Input{
		Text:             content,
		ReadingNumber:    readingNumber,
		ChapterWords:     chapterWords,
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
	return findings, err
}

// RecalcChapterL0FromDisk 按「当前已落盘的章节正文」重算并覆盖该章 L0 档案。
// #113：RunDetectionTask("chapter") 历史上只跑 L1，L0 只能靠写后链路刷新——
// 显式「重新检测本章」若不重算 L0，改稿后的字数/开篇/钩子发现会停留在旧值，
// 闭环看到的仍是修改前的问题。故在章节检测执行器派发前调用本函数。
//
// 边界：只动 L0 列；**不**刷新张力曲线、**不**重复落情绪账工单
// （E 系工单可能已被人工收口，重跑检测不得把它们唤醒）；
// 读正文/落库失败仅返回 error 由调用方告警，绝不阻断后续 L1 检测。
func RecalcChapterL0FromDisk(ctx context.Context, db *gorm.DB, logger *slog.Logger, novelID, chapterID int64) error {
	if db == nil {
		return fmt.Errorf("章节检测 L0 重算：db 未注入")
	}
	content, err := git.ReadFile(novelID, git.ChapterPath(chapterID))
	if err != nil {
		return fmt.Errorf("章节检测 L0 重算读正文：%w", err)
	}
	tc := ToolContext{DB: db, NovelID: novelID, Logger: logger, ToolID: "pipeline-detect"}
	_, err = recalcChapterL0(ctx, tc, chapterID, content)
	return err
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

// chapterCIFindings 读取本章当前档案的未消除发现，供写后自检技法定向（#116）。
// 只读、零派发：读失败 / 新章尚无档案 → nil，调用方回落全量自检，绝不阻断 L1。
func chapterCIFindings(ctx context.Context, db *gorm.DB, logger *slog.Logger, novelID, chapterID int64) []quality.Finding {
	if db == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	row, err := quality.NewStore(db, logger).GetByChapter(ctx, novelID, chapterID)
	if err != nil || row == nil {
		return nil
	}
	return append(quality.ParseFindings(row.L0Findings), quality.ParseFindings(row.L1Findings)...)
}

// RunChapterCIForChapter 对指定章节执行 L1 增量检查（chapterci 子 Agent）并把回执
// 落质量档案。runSub 为实际派发子 Agent 的函数（调用方决定用 ToolContext.RunSubAgent
// 还是 agent.RunSubAgent）；skillStore 用于注入 chapter-ci 技能方法论（可空，降级为
// 内联最小指令）；db 是质量档案存储。返回派发/回执错误（非阻断调用方可忽略）。
//
// 落档案口径（#115 P0：失败绝不覆盖上一轮结果）：
//   - 回执解析成功 → UpsertL1（findings + l1_status=checked）；
//   - 派发失败 / 回执坏 → 不写 L1，保留上一轮 checked 发现项与状态，仅记日志并返回错误。
//     UpsertL1 的 DoUpdates 含 l1_findings，旧实现会把有效发现清空并把状态打成 skipped，
//     用户点「重新检测」反而丢数据；检测失败不等于上一轮结果作废。
func RunChapterCIForChapter(ctx context.Context, runSub RunSubAgentFunc, skillStore *skill.Store,
	db *gorm.DB, logger *slog.Logger, toolID string, novelID, chapterID int64) error {
	if runSub == nil {
		return nil
	}
	instruction := buildChapterCIInstruction(skillStore, novelID, chapterID,
		chapterCITensionSegHints(ctx, db, logger, novelID, chapterID),
		chapterCIFindings(ctx, db, logger, novelID, chapterID)...)
	report, err := runSub(ctx, SubAgentRequest{
		AgentType:   "chapterci",
		NovelID:     novelID,
		Instruction: instruction,
		ToolID:      toolID,
		MaxTurns:    chapterCIMaxTurns,
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
			logger.Warn("chapter-ci 子 Agent 派发失败（非阻断，保留上一轮 L1）", "err", err, "novelID", novelID, "chapterID", chapterID)
		}
		return err
	}
	receipt, perr := quality.ParseL1Receipt(report)
	if perr != nil {
		if logger != nil {
			logger.Warn("chapter-ci 回执解析失败（非阻断，保留上一轮 L1）", "err", perr, "novelID", novelID, "chapterID", chapterID)
		}
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

// chapterCISelfCheckSkills 是章写后自检清单的技法单源（#111 P1，§3.3）。
// 与起草侧不同：自检类技法挂在「只报告不改写」的章 CI 指令里更贴切，既不占
// 写章 bundle 的 rune 预算，也不会把自检条目误读成正文创作指令。
// #111 P2：review 补齐「章后评审清单」正文后并入本表——原本近乎零使用的编辑视角
// 技能由此获得 chapter_ci 确定性挂载；其正文首段自带「不派发子 Agent」护栏，
// 不会被章 CI 子 Agent 误读成调度指令。
var chapterCISelfCheckSkills = []string{"deai-flavor", "revision-pass", "dialogue-subtext", "review"}

const chapterCISelfCheckSkillCap = 1500 // 单技能自检正文上限（rune）

// chapterCIFindingSkillRoutes 是 #116 写后自检「按本章发现定向」的单源映射。
// 旧链路每章固定注入 4 个技法（实测 chapter_ci_selfcheck 42 万 rune、占技能
// 字符 66%），但产出的 L1 发现维度（continuity/task_card/foreshadow）与这
// 4 个技法并不对应，属于「写完之后反复自检」的固定开销。这里只改注入集合，
// 不改检查项、不改回执契约、不新增任何执行体。
var chapterCIFindingSkillRoutes = map[string][]string{
	quality.CheckAIFlavor:     {"deai-flavor"},
	quality.CheckOpeningSlope: {"revision-pass"},
	quality.CheckOpeningHook:  {"revision-pass"},
	quality.CheckPacingDrag:   {"revision-pass"},
	quality.CheckBeatGap:      {"revision-pass"},
	quality.CheckBeatDensity:  {"revision-pass"},
	quality.CheckTailHook:     {"revision-pass"},
	"continuity":              {"review"},
	"foreshadow":              {"review"},
	"reader_state":            {"review"},
	"task_card":               {"review"},
	"immersion_anchor":        {"dialogue-subtext", "review"},
	"pacing":                  {"dialogue-subtext", "review"},
}

// chapterCIFindingSkills 按本章确定性/主观发现选自检技法。无发现项（新章、
// 档案未落库、测试直接调用）时回落全量 4 个，保证首章首次检测仍有完整
// 自检清单；命中但不落映射时给最小集 review。输出保持单源顺序（确定性）。
func chapterCIFindingSkills(findings []quality.Finding) []string {
	if len(findings) == 0 {
		return chapterCISelfCheckSkills
	}
	seen := map[string]bool{}
	for _, f := range findings {
		for _, name := range chapterCIFindingSkillRoutes[f.Check] {
			seen[name] = true
		}
	}
	var out []string
	for _, name := range chapterCISelfCheckSkills {
		if seen[name] {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		out = []string{"review"}
	}
	return out
}

// chapterCISelfCheckBlock 渲染写后自检块：按单源顺序取技能正文、单帽截断、
// 如实标注节选；技能缺失/正文为空/ store 为 nil 一律返回空串（调用方逐字不变）。
// #116：findings 非空时按发现项定向注入，零发现时保持既有全量口径。
func chapterCISelfCheckBlock(skillStore *skill.Store, novelID, chapterID int64, findings ...quality.Finding) string {
	if skillStore == nil {
		return ""
	}
	var b strings.Builder
	for _, name := range chapterCIFindingSkills(findings) {
		sk, ok := skillStore.Get(0, name)
		if !ok || strings.TrimSpace(sk.Content) == "" {
			continue
		}
		runes := []rune(sk.Content)
		truncated := false
		if len(runes) > chapterCISelfCheckSkillCap {
			runes = runes[:chapterCISelfCheckSkillCap]
			truncated = true
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "【写后自检·%s】\n%s", name, string(runes))
		if truncated {
			b.WriteString("\n（自检技法节选，完整以技能原文为准）")
		}
		activity.RecordSkillInvocation(context.Background(), activity.SkillInvocation{
			Skill: name, Channel: "chapter_ci_selfcheck", Mount: "chapter_ci",
			NovelID: novelID, ChapterID: chapterID, Runes: len(runes),
		})
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n# 写后自检清单（只报告不改写）\n以下技法的自检条目并入本次 L1 增量检查；命中问题写进清单，不要调用任何写工具：\n\n" +
		b.String() + "\n"
}

// buildChapterCIInstruction 构造 L1 增量检查指令：小上下文范围 + 检查项 + 情绪曲线
// 标注（P0-2，段号对照对齐分段）+ 回执契约，有 chapter-ci 技能时注入方法论正文（单源），
// 无技能时内联最小指令（降级）。segHints 为空回落通用分段说明。
// #111 P1 追加写后自检块（#116 按本章发现定向）；空块时输出与 P0 逐字一致，
// L1ReceiptFormatLine 契约不动。
func buildChapterCIInstruction(skillStore *skill.Store, novelID, chapterID int64, segHints string, findings ...quality.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "章节（chapter_id=%d）正文刚刚修改并落盘。请对该章执行 L1 增量质检。\n", chapterID)
	b.WriteString("检查范围（小上下文，严禁全书通读）：本章正文（read 路径 chapters/id_" +
		fmt.Sprint(chapterID) + ".md）、前一章结尾/次一章开头片段（get_chapter_list 定位）、" +
		"本章计划卡（get_chapter_task_card）、涉及台账条目（get_* 定点读取）。\n")
	b.WriteString("检查项：continuity 一致性/连贯、pacing 节奏、foreshadow 伏笔 lifecycle、" +
		"reader_state 读者认知态、task_card 任务卡兑现、immersion_anchor 代入感锚点。\n")
	b.WriteString("只报告不改写：不调用任何写工具、不评分、不给 verdict；零发现报空清单。\n")
	fmt.Fprintf(&b, "收敛要求（#115 P0）：本次复核最多 %d 轮工具调用，请一次读齐所需材料，勿反复检索同一来源；到上限前必须直接输出最终回执。\n", chapterCIMaxTurns)
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
	// #111 P1 / #116：写后自检清单（非空才注入；skillStore 为 nil 时整块为空，逐字不变）。
	b.WriteString(chapterCISelfCheckBlock(skillStore, novelID, chapterID, findings...))
	b.WriteString("\n最终回复末尾必须附 ```json 代码块上报回执，格式：\n")
	b.WriteString(quality.L1ReceiptFormatLine())
	b.WriteString("\n")
	return b.String()
}
