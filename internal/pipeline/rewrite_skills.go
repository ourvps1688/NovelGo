package pipeline

// rewrite_skills.go —— P2-1 低分维 → 生成技能定向路由（映射单源）。
//
// 体检低分维（dims 星级 ≤ rewriteLowDimMaxStars）与 issue 台账维度自动命中
// 对应方法论技能，技能正文（去 YAML frontmatter、体量封顶）注入重写提示词的
// 「方法论定向」块——让重写从一刀切的通用指令升级为按短板定向施治。
//
// 缺省纪律：无低分维且 issue 维度无映射 → 块为空串，chapterRewritePrompt 与
// 通用口径逐字一致（不盲调、不模板）。
//
// 映射单源纪律（rewrite_skills_test.go 契约测试锁定）：
//   - auditDimSkillRoutes 键 ⊆ review.AuditDimOrder（签约体检八维）；
//   - issueDimSkillRoutes 键 ⊆ issue dimension 枚举（contract.go 四维台账归属）；
//   - 技能名必须存在于 skill.BuiltinFS（builtin/*.md）。

import (
	"context"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/review"
	"github.com/novelgo/novelgo/internal/skill"
)

// 低分判定与体量封顶（提示词体量可控）。
const (
	rewriteLowDimMaxStars = 3     // dims 星级 ≤ 该值判低分维（1-5 星制）
	rewriteMethodSkillCap = 4000  // 单技能注入上限（rune）
	rewriteMethodBlockCap = 12000 // 「方法论定向」块总上限（rune）
)

// auditDimSkillRoutes 体检八维 → 方法论技能（P2-1 映射单源）。
//   - compliance 无对应方法论技能（禁忌合规是机械删改），走通用提示词，不入表；
//   - fit→book-pitch（题材卖点提炼）、longArc→plot-hook（伏笔/钩子）为近似口径；
//   - pacing 双技能（节奏控制 + 场景节拍），与任务口径 dim4→pacing-control/scene-beats 一致。
var auditDimSkillRoutes = map[string][]string{
	review.AuditDimOpening:   {"opening-chapters"},
	review.AuditDimFit:       {"book-pitch"},
	review.AuditDimCharacter: {"character-design"},
	review.AuditDimPacing:    {"pacing-control", "scene-beats"},
	review.AuditDimWorld:     {"worldbuilding-design"},
	review.AuditDimLongArc:   {"plot-hook"},
	review.AuditDimReader:    {"reader-immersion"},
}

// issueDimSkillRoutes issue 台账维度（contract dimension 枚举）→ 方法论技能。
// timeline 无对应方法论（时序矛盾属台账机械修复），不入表。
var issueDimSkillRoutes = map[string][]string{
	"character":  {"character-design"},
	"locus_arc":  {"worldbuilding-design"},
	"reader_pov": {"reader-immersion"},
}

// lowAuditDims 返回低分维（星级 ≤ rewriteLowDimMaxStars），
// 顺序按 review.AuditDimOrder（同输入同输出，确定性）。
func lowAuditDims(dims []AuditDimScore) []AuditDimScore {
	byKey := map[string]AuditDimScore{}
	for _, d := range dims {
		if d.Score > 0 && d.Score <= rewriteLowDimMaxStars {
			byKey[d.Key] = d
		}
	}
	var out []AuditDimScore
	for _, key := range review.AuditDimOrder {
		if d, ok := byKey[key]; ok {
			out = append(out, d)
		}
	}
	return out
}

// rewriteMethodBlock 渲染「方法论定向」块：低分维（按 AuditDimOrder 序）+ issue
// 台账维度命中方法论技能，技能正文逐个注入。同一技能跨源命中去重（如 character
// 同时是低分维与 issue 维度只注一次，保留首次语境）；体量按单技能/整块双封顶，
// 截断如实标注。无命中返回空串（通用提示词逐字不变）。
func rewriteMethodBlock(dims []AuditDimScore, issueDim string) string {
	type entry struct {
		label string // 语境标签（八维键或台账维度键）
		note  string // 低分维一句话证据（issue 维度无 note）
		name  string // 技能名
	}
	var entries []entry
	for _, d := range lowAuditDims(dims) {
		for _, name := range auditDimSkillRoutes[d.Key] {
			entries = append(entries, entry{label: d.Key, note: d.Note, name: name})
		}
	}
	dim := strings.TrimSpace(issueDim)
	for _, name := range issueDimSkillRoutes[dim] {
		entries = append(entries, entry{label: dim, name: name})
	}

	seen := map[string]bool{}
	var b strings.Builder
	total, written := 0, 0
	for _, e := range entries {
		if seen[e.name] {
			continue
		}
		body := loadRewriteMethodBody(e.name)
		if body == "" {
			continue
		}
		seen[e.name] = true
		if total >= rewriteMethodBlockCap {
			break
		}
		seg := body
		truncated := false
		if r := []rune(seg); len(r) > rewriteMethodSkillCap {
			seg = string(r[:rewriteMethodSkillCap])
			truncated = true
		}
		label := e.label
		if e.note != "" {
			label += "（低分维：" + e.note + "）"
		}
		if written > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "【方法论】%s——落实《%s》要点：\n%s", label, e.name, seg)
		if truncated {
			b.WriteString("\n（方法论节选，完整以技能原文为准）")
		}
		total += len([]rune(seg))
		written++
		// #111 P0 统一账本：重写方法论正文注入逐技能记账（解决子 Agent 初始 prompt
		// 不落库导致 exec_run_stats 看不到 8 个重写技法的口径缺口）。
		activity.RecordSkillInvocation(context.Background(), activity.SkillInvocation{
			Skill: e.name, Channel: "rewrite_method", Mount: "rewrite_method",
			Runes: len([]rune(seg)),
		})
	}
	if written == 0 {
		return ""
	}
	return "【方法论定向】本次重写命中以下方法论技能（按列出顺序优先落实，可结合章节实情取舍）：\n\n" +
		b.String()
}

// loadRewriteMethodBody 读取内置技能正文（去 YAML frontmatter，TrimSpace）。
// 读取失败/空正文返回空串（降级不注入；技能存在性由契约测试锁定）。
func loadRewriteMethodBody(name string) string {
	raw, err := skill.BuiltinFS.ReadFile("builtin/" + name + ".md")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stripFrontmatter(string(raw)))
}

// platformRhythmBlock 取当前平台节奏模板块（P2-2，重写侧消费）：CheckerConfig
// 平台 profile 指名节奏模板时经 skill.RhythmBlock 确定性提取（scene-beats 可选模板）；
// 未配置/未指名/提取失败 → 空串（缺省不模板）。读失败降级空串不阻断。
func (s *Service) platformRhythmBlock(ctx context.Context, novelID int64) string {
	if s.qualityStore == nil {
		return ""
	}
	cfg, err := s.qualityStore.GetCheckerConfig(ctx, novelID)
	if err != nil || cfg == nil {
		return ""
	}
	p := cfg.Profile()
	if p == nil || p.RhythmTemplate == "" {
		return ""
	}
	return skill.RhythmBlock(p.RhythmTemplate)
}

// joinRewriteBlocks 顺序拼接两个可选提示词块（双空=空串；空块跳过；非空间空行分隔）。
func joinRewriteBlocks(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n\n" + b
	}
}

// stripFrontmatter 委托 skill.Body：统一支持 LF/CRLF，避免内置技能（CRLF）的
// YAML 头被当成方法论正文注入重写提示词。保留本函数名供既有单测与调用点使用。
func stripFrontmatter(s string) string {
	return skill.Body(s)
}
