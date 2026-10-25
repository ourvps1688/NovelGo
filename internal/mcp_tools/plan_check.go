package mcp_tools

// plan_check.go —— 章纲级写前检查 L1.5（R2b-4，提案 §9.1）接线：
// 立卡/更新章纲后跑确定性对账，把告警文本随工具回执返回（只报告不拦截）。
//
// 检查器单源 internal/structure.CheckChapterPlan；本文件只做输入投影装配——
// 坑表（time_entries 伏笔）/书写不变量（pinned_invariants）/角色台账/题材卡配置
// 四路取数各自降级（宁缺勿错）：任一路失败=该项检查自然跳过，整体零提示零报错。

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/structure"
	"github.com/novelgo/novelgo/internal/timeline"
)

// planCheckCategoryForeshadowing 是伏笔条目分类（与 pipeline 侧同值）。
const planCheckCategoryForeshadowing = "foreshadowing"

// planCheckWarnings 跑写前对账并渲染告警文本。空返回=零告警或降级（零提示）。
// readingNumber 是本卡/计划对应章号（<=0 时跳过收章区间判定）；
// hookText 是钩子任务全文（坑表对账作用域）；texts 是全部待禁忌/死亡对账文本。
func planCheckWarnings(ctx context.Context, tc ToolContext, readingNumber int, hookText string, texts []structure.PlanField) string {
	if tc.DB == nil {
		return ""
	}
	in := structure.PlanCheckInput{
		ReadingNumber: readingNumber,
		HookText:      hookText,
		Texts:         texts,
	}

	// 坑表（失败=跳过坑对账整项：清空 hookText，存在性/区间判定自然不跑——
	// 不能让取数失败把全部坑名误判为「不在坑表」）。
	if page, err := timeline.NewStore(tc.DB, tc.LoggerOrDefault()).ListByNovel(ctx, tc.NovelID, timeline.ListByNovelOptions{
		Category:   planCheckCategoryForeshadowing,
		PageParams: storage.PageParams{Size: -1},
	}); err == nil {
		for _, e := range page.Items {
			in.Foreshadows = append(in.Foreshadows, structure.ForeshadowEntry{
				ID:                  e.ID,
				Title:               e.Title,
				Status:              e.Status,
				TargetReadingNumber: e.TargetReadingNumber,
				SourceChapterID:     e.SourceChapterID,
				ResolvedChapterID:   e.ResolvedChapterID,
			})
		}
	} else {
		in.HookText = ""
	}

	// 书写不变量（失败=跳过不变量对账）。
	if pins, err := structure.NewStore(tc.DB).ListByNovel(ctx, tc.NovelID); err == nil {
		in.Invariants = pins
	}

	// 角色台账（失败=跳过别名展开，仅按不变量参数名匹配）。
	var rows []character.Character
	if err := tc.DB.WithContext(ctx).Where("novel_id = ?", tc.NovelID).Find(&rows).Error; err == nil {
		for _, r := range rows {
			var aliases []string
			if strings.TrimSpace(r.Aliases) != "" {
				if jerr := json.Unmarshal([]byte(r.Aliases), &aliases); jerr != nil {
					aliases = nil
				}
			}
			in.Characters = append(in.Characters, structure.CharacterState{
				ID:                r.ID,
				Name:              r.Name,
				Aliases:           aliases,
				Alive:             r.Alive,
				CapabilityCeiling: r.CapabilityCeiling,
			})
		}
	}

	// 题材卡配置（失败=回落缺省：零禁忌零宽限）。
	if cfg, err := quality.NewStore(tc.DB, tc.LoggerOrDefault()).GetCheckerConfig(ctx, tc.NovelID); err == nil {
		in.Taboos = cfg.Taboos()
		in.WindowGrace = cfg.OverdueGrace()
	}

	return structure.RenderPlanWarnings(structure.CheckChapterPlan(in))
}

// planCheckData 追加写前对账告警到工具回执（空告警不加键，回执保持精简）。
func planCheckData(data map[string]any, warnings string) map[string]any {
	if strings.TrimSpace(warnings) != "" {
		data["warnings"] = warnings
	}
	return data
}
