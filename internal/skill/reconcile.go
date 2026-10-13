package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

// ReconcileResult 汇总一次 ReconcileAggregates 的结果。
type ReconcileResult struct {
	Checked  int      // 扫描到的现有派生聚合档数量
	Rebuilt  []string // 因源变更而自动重建的派生档名
	Deleted  []string // 源已空（孤儿）而删除的派生档名
	UpToDate int      // 摘要未变、无需重建的数量
}

// ReconcileAggregates 维护跨书聚合派生档，使其与当前源 skill 保持一致（T-AGG-5 自动重建）：
//   - 现有派生档的源（同作者）已全部消失 → 删除孤儿档（避免悬挂脏数据）。
//   - 现有派生档的源摘要（ComputeSourceDigest）与存档不符 → 用最新源重新生成并覆盖落盘。
//   - 仅维护「已存在」的派生档；不会凭空为某作者新建聚合档（新建由手动合并/RebuildAggregated 负责）。
//
// 入参：sources 为当前全部 mental 源（含 novel/user 级，未做聚合过滤）；existing 为当前
// UserSkillsDir 中已落盘的 skill（含派生档）。outputDir 为派生档目录（通常 config.UserSkillsDir()）。
// 单条目失败仅跳过该档，不中断整体。
func ReconcileAggregates(ctx context.Context, sources []MergeSource, existing []Skill, outputDir string) *ReconcileResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if outputDir == "" {
		outputDir = "."
	}

	// 1. 按归一作者分组当前源（仅 mental、非聚合）。
	type grp struct {
		author  string
		sources []MergeSource
	}
	byAuthor := make(map[string]*grp)
	for _, s := range sources {
		if s.Skill == nil || s.Skill.Aggregated || s.Skill.Kind != "mental" {
			continue
		}
		author := normalizeAuthor(s.Skill.SourceAuthor, nil)
		if author == "" {
			continue
		}
		g, ok := byAuthor[author]
		if !ok {
			g = &grp{author: author}
			byAuthor[author] = g
		}
		g.sources = append(g.sources, s)
	}

	res := &ReconcileResult{}
	for i := range existing {
		d := existing[i]
		if !d.Aggregated {
			continue
		}
		res.Checked++
		author := normalizeAuthor(d.SourceAuthor, nil)
		g, ok := byAuthor[author]
		if !ok {
			// 孤儿：源已全部消失 → 删除派生档。
			path := filepath.Join(outputDir, SanitizeFileName(d.Name)+".md")
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				continue // 删除失败不致命，仅跳过该档
			}
			res.Deleted = append(res.Deleted, d.Name)
			continue
		}
		cur := ComputeSourceDigest(g.sources)
		if d.AggregatedSourceHash == cur {
			res.UpToDate++
			continue
		}
		// 摘要不符 → 用最新源重建并覆盖落盘。
		derived, err := MergeMentalSkills(ctx, g.sources, MergeOptions{
			CanonicalAuthor: author,
			OutputDir:       outputDir,
			AggregatedBy:    "aggregate",
		})
		if err != nil {
			continue
		}
		if _, err := WriteSkill(outputDir, derived); err != nil {
			continue
		}
		res.Rebuilt = append(res.Rebuilt, d.Name)
	}
	return res
}
