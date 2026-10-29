package pipeline

import (
	"context"
	"log/slog"
	"strings"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/voice"
	"gorm.io/gorm"
)

// P1 复验 L0 确定性快路径（审计 P0+P2 复验异步化的速度增益项）：
// 纯机械类工单（字数/章尾钩子/AI味/称呼混用/死人出镜/战力数值序/文风漂移）经
// quality.RunL0Checks 确定性判定修复状态，免 LLM 调用——消除复验的主要耗时与 LLM 错误暴露面。
// 仅当工单确属机械类、且依赖台账配置齐备时才替代 LLM；否则保守退回 P0+P2 的原 LLM 复验路径。

// mechanicalCheckKeywords 把机械类问题关键词映射到 L0 检查项（quality 的 Check 常量，单源）。
// 战力数值序对应 power_upset/power_crush 两个检查项，共用同一组关键词。
var mechanicalCheckKeywords = map[string][]string{
	quality.CheckWordCount:      {"字数", "偏短", "篇幅", "太短", "字数不足", "字数不够", "字数不达标", "字数未达标"},
	quality.CheckTailHook:       {"章尾", "钩子", "悬念", "结尾", "收尾", "章末"},
	quality.CheckAIFlavor:       {"ai味", "ai 味", "套话", "油腻", "口水", "机翻"},
	quality.CheckNameDrift:      {"称呼", "叫法", "名字混乱", "同人异称", "别名", "称谓", "称呼混用"},
	quality.CheckDeadAppearance: {"死人", "已死", "去世", "亡故", "复活", "死亡"},
	quality.CheckPowerUpset:     {"战力", "境界", "越级", "等级", "修为", "实力", "碾压", "低阶", "越阶"},
	quality.CheckPowerCrush:     {"战力", "境界", "越级", "等级", "修为", "实力", "碾压", "低阶", "越阶"},
	quality.CheckStyleDrift:     {"文风", "风格", "语感", "漂移", "笔调", "文笔", "语调", "文风漂移"},
}

// semanticExclusionKeywords 是强语义矛盾信号：命中则视为非机械类，保守走 LLM，避免误判。
// 例如「情节矛盾」「人设崩塌」虽可能含机械词，但本质是语义问题，L0 无法判定。
var semanticExclusionKeywords = []string{
	"矛盾", "冲突", "逻辑", "人设", "情节", "崩", "连贯", "伏笔", "前后不一", "设定冲突",
}

// classifyMechanicalChecks 判断工单是否属机械类，返回候选 L0 检查项集合与是否语义歧义。
// 纯函数、可单测；命中语义排除词即判歧义（走 LLM）。
func classifyMechanicalChecks(area, desc, fixType string) (map[string]bool, bool) {
	if strings.TrimSpace(area) == "" && strings.TrimSpace(desc) == "" {
		return nil, false
	}
	text := strings.ToLower(area + " " + desc)
	checks := map[string]bool{}
	for check, kws := range mechanicalCheckKeywords {
		for _, kw := range kws {
			if strings.Contains(text, strings.ToLower(kw)) {
				checks[check] = true
				break
			}
		}
	}
	ambiguous := false
	for _, ex := range semanticExclusionKeywords {
		if strings.Contains(text, strings.ToLower(ex)) {
			ambiguous = true
			break
		}
	}
	return checks, ambiguous
}

// l0ChapterReader 读取一章正文（可注入便于测试，默认走 git 文件存储）。
type l0ChapterReader func(novelID, chapterID int64) (string, bool)

// defaultL0ChapterReader 从 git 管理的章节文件读取正文（与章节 CI 同一数据源）。
func defaultL0ChapterReader(novelID, chapterID int64) (string, bool) {
	content, err := git.ReadFile(novelID, git.ChapterPath(chapterID))
	if err != nil {
		return "", false
	}
	return content, true
}

// l0Verdict 由 L0 发现清单与有效候选检查项判定复验结论（纯函数、可单测）。
// decided=true 时：verified=false 表示候选机械问题仍有 Finding 命中（明确未修复，退回 pending，安全）；
// 仅当 allVerifiable=true（全部候选检查项都具备依赖可判定）且零命中时，verified=true 自动确认通过（免 LLM）。
// 若有候选检查项因依赖缺失被剔除（!allVerifiable），即使已验证项全消除也不自行确认，保守退回 LLM 复核未验证方面，避免漏判。
func l0Verdict(findings []quality.Finding, effective map[string]bool, allVerifiable bool) (decided, verified bool, hitChecks []string) {
	hits := map[string]bool{}
	for c := range effective {
		for _, f := range findings {
			if f.Check == c {
				hits[c] = true
				break
			}
		}
	}
	if len(hits) > 0 {
		names := make([]string, 0, len(hits))
		for c := range hits {
			names = append(names, c)
		}
		return true, false, names
	}
	if !allVerifiable {
		// 候选检查项存在依赖缺失无法判定者：保守退回 LLM，不臆测修复状态。
		return false, false, nil
	}
	return true, true, nil
}

// tryL0Verify 尝试用确定性 L0 快路径复验单工单。
// 返回 (decided, verified, note)：
//   - decided=false → 非机械类 / 语义歧义 / 依赖配置不齐备 / 无可读正文 → 调用方应走原 LLM 复验路径；
//   - decided=true  → 已确定性判定：verified=true 自动通过（免 LLM），false 明确未修复退回 pending。
func (s *Service) tryL0Verify(ctx context.Context, novelID int64, runID string, row PipelineIssue, reader l0ChapterReader) (decided, verified bool, note string) {
	checks, ambiguous := classifyMechanicalChecks(row.Area, row.Desc, row.FixType)
	if ambiguous || len(checks) == 0 {
		return false, false, ""
	}
	db := s.store.db
	logger := s.logger

	// 装配 L0 依赖配置（缺啥降级啥，与 chapter_ci.go 同口径）。
	baseline := s.l0VoiceBaseline(ctx, db, logger, novelID)
	chars := s.l0Characters(ctx, novelID)
	powers := s.l0PowerLevels(ctx, db, novelID)
	cfg, err := quality.NewStore(db, logger).GetCheckerConfig(ctx, novelID)
	if err != nil || cfg == nil {
		cfg = quality.DefaultCheckerConfig(novelID)
	}
	genre := s.novelGenre(ctx, novelID)

	// 依赖齐备性过滤：候选 check 所需台账缺失 → 该 check 不能由 L0 确定性判定，移除；
	// 全部移除则退回 LLM（避免 L0 零发现被误判为已修复）。
	effective := map[string]bool{}
	for c := range checks {
		switch c {
		case quality.CheckStyleDrift:
			if len(baseline) > 0 {
				effective[c] = true
			}
		case quality.CheckNameDrift, quality.CheckDeadAppearance:
			if len(chars) > 0 {
				effective[c] = true
			}
		case quality.CheckPowerUpset, quality.CheckPowerCrush:
			if len(powers) > 0 {
				effective[c] = true
			}
		default: // word_count / tail_hook / ai_flavor 不依赖外部台账，恒可判定
			effective[c] = true
		}
	}
	// 全部候选检查项都有依赖可判定才算 allVerifiable；否则存在缺失项无法复核，保守退回 LLM。
	allVerifiable := len(effective) == len(checks)
	if len(effective) == 0 {
		return false, false, "" // 候选检查项依赖全缺失 → 走 LLM，避免零发现误判已修复
	}

	// 读涉及章节正文，逐章跑 RunL0Checks 汇总发现。
	chapterIDs := decodeIssueChapterIDs(row.ChapterIDsJSON)
	if len(chapterIDs) == 0 {
		return false, false, "" // 无具体章节（全书/台账级）→ 走 LLM
	}
	chStore := chapter.NewStore(db, logger)
	readCount := 0
	var allFindings []quality.Finding
	for _, n := range chapterIDs {
		chID, err := chStore.GetChapterIDByReadingNumber(ctx, nil, novelID, int(n))
		if err != nil {
			continue
		}
		content, ok := reader(novelID, chID)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		readCount++
		readingNumber, _ := chStore.GetReadingNumberByID(ctx, nil, novelID, chID)
		_, fs := quality.RunL0Checks(quality.L0Input{
			Text:             content,
			ReadingNumber:    readingNumber,
			Baseline:         baseline,
			Genre:            genre,
			TailHookRequired: cfg.TailHookRequired,
			TabooList:        cfg.Taboos(),
			Characters:       chars,
			PowerLevels:      powers,
		})
		allFindings = append(allFindings, fs...)
	}
	if readCount == 0 {
		return false, false, "" // 章节正文均不可读 → 走 LLM，不臆测修复状态
	}

	decided, verified, hit := l0Verdict(allFindings, effective, allVerifiable)
	if !decided {
		return false, false, ""
	}
	if verified {
		return true, true, "L0 快检：机械类问题已消除，自动确认通过（免 LLM 调用）"
	}
	return true, false, "L0 快检：仍命中机械类问题（" + strings.Join(hit, ",") + "），已退回待修"
}

// ── L0 依赖装配（封装底层 store 调用，与 mcp_tools 辅助函数同口径但用导出符号）─────────────

func (s *Service) l0VoiceBaseline(ctx context.Context, db *gorm.DB, logger *slog.Logger, novelID int64) map[string]float64 {
	entries, err := voice.NewStore(db, logger).List(ctx, novelID)
	if err != nil || len(entries) == 0 {
		return nil
	}
	return voice.MergeMetrics(entries)
}

func (s *Service) l0PowerLevels(ctx context.Context, db *gorm.DB, novelID int64) []quality.PowerLevelRef {
	rows, err := structure.NewStore(db).ListPowerLevels(ctx, novelID)
	if err != nil {
		return nil
	}
	out := make([]quality.PowerLevelRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, quality.PowerLevelRef{Name: r.Name, Rank: r.Rank})
	}
	return out
}
