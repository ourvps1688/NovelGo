package pipeline

// rewrite_skills_test.go —— P2-1 低分维 → 生成技能定向路由契约测试：
// 映射单源（键域 ⊆ 八维契约 / issue dimension 枚举、技能名 ⊆ skill.BuiltinFS）、
// 低分判定/去重/体量封顶、缺省零注入（通用提示词逐字不变）。

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/review"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRewriteSkillRoutesContract 映射单源契约：键域合法 + 技能名可解析 +
// 任务口径三例（dim1→opening-chapters、dim3→character-design、dim4→pacing 双技能）。
func TestRewriteSkillRoutesContract(t *testing.T) {
	// 体检八维键域 ⊆ review.AuditDimOrder（八维契约单源）。
	validAudit := map[string]bool{}
	for _, k := range review.AuditDimOrder {
		validAudit[k] = true
	}
	for key, names := range auditDimSkillRoutes {
		assert.True(t, validAudit[key], "auditDimSkillRoutes 键 %q 不在 review.AuditDimOrder", key)
		assert.NotEmpty(t, names, "auditDimSkillRoutes[%s] 技能清单为空", key)
	}
	// issue dimension 键域 ⊆ contract.go dimension 枚举（四维台账归属）。
	validIssue := map[string]bool{"character": true, "locus_arc": true, "timeline": true, "reader_pov": true}
	for key, names := range issueDimSkillRoutes {
		assert.True(t, validIssue[key], "issueDimSkillRoutes 键 %q 不在 dimension 枚举", key)
		assert.NotEmpty(t, names, "issueDimSkillRoutes[%s] 技能清单为空", key)
	}
	// 技能名必须存在于 skill.BuiltinFS 且正文（去 frontmatter）非空。
	checkSkills := func(m map[string][]string) {
		for key, names := range m {
			for _, name := range names {
				raw, err := skill.BuiltinFS.ReadFile("builtin/" + name + ".md")
				require.NoError(t, err, "技能 %q（%s）不在 skill.BuiltinFS", name, key)
				require.NotEmpty(t, raw, "技能 %q（%s）文件为空", name, key)
				require.NotEmpty(t, loadRewriteMethodBody(name), "技能 %q（%s）正文为空", name, key)
			}
		}
	}
	checkSkills(auditDimSkillRoutes)
	checkSkills(issueDimSkillRoutes)

	// 任务口径三例断言。
	assert.Equal(t, []string{"opening-chapters"}, auditDimSkillRoutes[review.AuditDimOpening])
	assert.Equal(t, []string{"character-design", "dialogue-subtext"}, auditDimSkillRoutes[review.AuditDimCharacter])
	assert.Equal(t, []string{"pacing-control", "scene-beats"}, auditDimSkillRoutes[review.AuditDimPacing])
	// #111 P1 新增的三项：读者体感三件套（去 AI 味 / 通稿修订 / 代入感）。
	assert.Equal(t, []string{"reader-immersion", "deai-flavor", "revision-pass"}, auditDimSkillRoutes[review.AuditDimReader])
}

// TestLowAuditDims 低分判定：星级 ≤3 入选、0/4/5 剔除；顺序按 AuditDimOrder 确定性。
func TestLowAuditDims(t *testing.T) {
	dims := []AuditDimScore{
		{Key: review.AuditDimPacing, Score: 3},
		{Key: review.AuditDimOpening, Score: 2},
		{Key: review.AuditDimReader, Score: 4},
		{Key: review.AuditDimFit, Score: 0},
		{Key: review.AuditDimCharacter, Score: 5},
	}
	low := lowAuditDims(dims)
	require.Len(t, low, 2)
	assert.Equal(t, review.AuditDimOpening, low[0].Key) // AuditDimOrder 序（opening 先于 pacing）
	assert.Equal(t, review.AuditDimPacing, low[1].Key)
	assert.Empty(t, lowAuditDims(nil))
}

// TestRewriteMethodBlock 方法论块：低分维/issue 维度命中注入、跨源去重、
// 高分维不注入、无命中空串（通用口径逐字不变）、同输入同输出。
func TestRewriteMethodBlock(t *testing.T) {
	// 低分维命中：含定向头/技能名/低分证据/技能正文。
	block := rewriteMethodBlock(
		[]AuditDimScore{{Key: review.AuditDimPacing, Score: 2, Note: "第6-10章节奏拖沓"}}, "")
	require.NotEmpty(t, block)
	assert.Contains(t, block, "【方法论定向】")
	assert.Contains(t, block, "pacing-control")
	assert.Contains(t, block, "scene-beats")
	assert.Contains(t, block, "第6-10章节奏拖沓")
	assert.Contains(t, block, "【方法论】pacing（低分维：第6-10章节奏拖沓）")

	// issue 维度命中（无 dims）：reader_pov → reader-immersion。
	block2 := rewriteMethodBlock(nil, "reader_pov")
	require.NotEmpty(t, block2)
	assert.Contains(t, block2, "reader-immersion")

	// 跨源去重：character 低分维 + issue dimension=character → character-design 只注一次。
	block3 := rewriteMethodBlock(
		[]AuditDimScore{{Key: review.AuditDimCharacter, Score: 2}}, "character")
	require.NotEmpty(t, block3)
	assert.Equal(t, 1, strings.Count(block3, "《character-design》"))

	// 高分维（4/5 星）+ 无 issue 维度 → 空串。
	assert.Empty(t, rewriteMethodBlock(
		[]AuditDimScore{{Key: review.AuditDimPacing, Score: 4}}, ""))
	assert.Empty(t, rewriteMethodBlock(nil, ""))
	assert.Empty(t, rewriteMethodBlock(nil, "none"))

	// 确定性：同输入逐字一致。
	a := rewriteMethodBlock([]AuditDimScore{{Key: review.AuditDimOpening, Score: 1}}, "locus_arc")
	b := rewriteMethodBlock([]AuditDimScore{{Key: review.AuditDimOpening, Score: 1}}, "locus_arc")
	assert.Equal(t, a, b)
	assert.Contains(t, a, "worldbuilding-design") // locus_arc 路由
}

// TestStripFrontmatter frontmatter 剥离：带 YAML 头去头、无头原样。
func TestStripFrontmatter(t *testing.T) {
	assert.Equal(t, "正文", stripFrontmatter("---\nname: x\n---\n正文"))
	assert.Equal(t, "无头正文", stripFrontmatter("无头正文"))
}

// TestChapterRewritePromptMethodBlock 提示词消费：methodBlock 非空注入（任务文本之后、
// 要求段之后追加），空串=通用口径与既有文案逐字不变。
func TestChapterRewritePromptMethodBlock(t *testing.T) {
	item := RewriteItem{ID: "I2", Level: "P1", Area: "节奏", Desc: "拖沓"}
	ch := chapter.Chapter{ID: 9, ReadingNumber: 3, Title: "第三章"}
	generic := chapterRewritePrompt(1, item, ch, "", contextasm.ContextBlocks{})
	assert.NotContains(t, generic, "【方法论定向】")
	assert.Contains(t, generic, "要求：用 edit 工具传 full_replace=true")

	with := chapterRewritePrompt(1, item, ch, "【方法论定向】…块…", contextasm.ContextBlocks{})
	assert.Contains(t, with, "【方法论定向】…块…")
	// 块在任务行之后、要求段之后追加（顺序稳定）。
	assert.Less(t, strings.Index(with, "问题 I2[P1]"), strings.Index(with, "【方法论定向】"))
	assert.Contains(t, with, "要求：用 edit 工具传 full_replace=true")
}

// TestJoinRewriteBlocks 可选块拼接：双空=空串、空块跳过、非空间空行分隔。
func TestJoinRewriteBlocks(t *testing.T) {
	assert.Empty(t, joinRewriteBlocks("", ""))
	assert.Equal(t, "A", joinRewriteBlocks("A", ""))
	assert.Equal(t, "B", joinRewriteBlocks("", "B"))
	assert.Equal(t, "A\n\nB", joinRewriteBlocks("A", "B"))
	assert.Equal(t, "A\n\nB", joinRewriteBlocks(" A ", " B "), "TrimSpace 归一")
}

// TestPlatformRhythmBlock 平台节奏块（P2-2 重写侧消费）：qimao profile 指名模板 →
// 非空块（scene-beats 提取）；未配 profile → 空串（缺省不模板）。
func TestPlatformRhythmBlock(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&quality.CheckerConfig{}))
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()
	const novelID int64 = 990

	assert.Empty(t, svc.platformRhythmBlock(ctx, novelID), "未配 profile 应空串")

	qs := quality.NewStore(db, slog.Default())
	require.NoError(t, qs.SaveCheckerConfig(ctx, &quality.CheckerConfig{
		NovelID: novelID, PlatformProfile: "qimao",
	}))
	block := svc.platformRhythmBlock(ctx, novelID)
	assert.NotEmpty(t, block)
	assert.Contains(t, block, "【平台节奏模板】")
	assert.Contains(t, block, "打脸")
}
