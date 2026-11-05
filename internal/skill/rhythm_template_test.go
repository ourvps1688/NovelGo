package skill

// rhythm_template_test.go —— P2-2 平台节奏模板提取契约：小节提取/缺省空串/
// profile↔模板 ID 两侧一致（quality 注册 profile 的 RhythmTemplate 必须能取到非空块）。

import (
	"testing"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRhythmBlock 模板块提取：qimao_face_slap 非空且含打脸要点；未知/空 ID 空串；同输入同输出。
func TestRhythmBlock(t *testing.T) {
	block := RhythmBlock(RhythmTemplateQimaoFaceSlap)
	require.NotEmpty(t, block)
	assert.Contains(t, block, "【平台节奏模板】")
	assert.Contains(t, block, "打脸")
	assert.Contains(t, block, "300")
	assert.Equal(t, block, RhythmBlock(RhythmTemplateQimaoFaceSlap), "同输入同输出")

	assert.Empty(t, RhythmBlock(""))
	assert.Empty(t, RhythmBlock("no_such_template"))
	assert.NotEmpty(t, RhythmBlock("  qimao_face_slap  "), "ID 经 TrimSpace 归一后应等同原 ID")
}

// TestExtractH3Section 小节提取：按 `### id` 边界切分（止于下一个标题），缺失空串。
func TestExtractH3Section(t *testing.T) {
	md := "# 头\n\n### alpha\nA 行\nA2 行\n\n### beta\nB 行\n\n## 附\nC 行\n"
	assert.Equal(t, "A 行\nA2 行", extractH3Section(md, "alpha"))
	assert.Equal(t, "B 行", extractH3Section(md, "beta"))
	assert.Empty(t, extractH3Section(md, "gamma"))
	assert.Empty(t, extractH3Section("### alpha\n", "alpha"), "空正文空串")
}

// TestRhythmTemplateProfileContract profile ↔ 模板 ID 契约：quality 注册表中
// 指名 RhythmTemplate 的 profile 必须能经 RhythmBlock 取到非空模板块（防两侧漂移）。
func TestRhythmTemplateProfileContract(t *testing.T) {
	// 注册名与别名逐一走 PlatformProfileByName（与运行时取用同口径）。
	for _, name := range []string{"qimao", "七猫"} {
		p := quality.PlatformProfileByName(name)
		require.NotNil(t, p, "profile %q 应已注册", name)
		require.Equal(t, RhythmTemplateQimaoFaceSlap, p.RhythmTemplate,
			"profile %q 的 RhythmTemplate 应指名 skill.RhythmTemplateQimaoFaceSlap", name)
		require.NotEmpty(t, RhythmBlock(p.RhythmTemplate),
			"profile %q 指名的模板 %q 应能提取出非空模板块", name, p.RhythmTemplate)
	}
}
