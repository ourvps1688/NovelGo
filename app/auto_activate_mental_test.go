package app

import (
	"testing"

	"github.com/novelgo/novelgo/internal/personasetting"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mentalSkillMarkdown = `---
name: test-mindset
kind: mental
mode: auto
source_author: "Test Author"
---
## 创作心智
- 偏好短句与留白
`
	structSkillMarkdown = `---
name: test-struct
kind: struct
mode: auto
---
## 套路模板
- 起承转合
`
)

// TestSaveMentalSkill_AutoActivates 验证：保存 kind=mental 的技能后，后端自动
// (A) 勾选常驻（写入本书 .enabled.json）(C) 设为本书写作人格（覆盖式）。
func TestSaveMentalSkill_AutoActivates(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	require.NoError(t, app.SaveContent(SaveContentInput{
		NovelID: novelID,
		Path:    "skills/test-mindset.md",
		Content: mentalSkillMarkdown,
	}))

	// (A) 已勾选常驻启用。
	enabled, err := skill.LoadEnabled(novelID)
	require.NoError(t, err)
	assert.Contains(t, enabled, "test-mindset")

	// (C) 已设为本书写作人格。
	p, err := personasetting.NewStore(app.db, app.logger).Get(app.ctx, novelID)
	require.NoError(t, err)
	assert.True(t, p.Enabled, "persona 应已启用")
	assert.Equal(t, "skill", p.Kind)
	assert.Equal(t, "test-mindset", p.Ref)
}

// TestSaveStructSkill_NotAutoActivated 验证：非 mental 技能（如套路）保存后不会误触发常驻/人格绑定。
func TestSaveStructSkill_NotAutoActivated(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	require.NoError(t, app.SaveContent(SaveContentInput{
		NovelID: novelID,
		Path:    "skills/test-struct.md",
		Content: structSkillMarkdown,
	}))

	enabled, err := skill.LoadEnabled(novelID)
	require.NoError(t, err)
	assert.NotContains(t, enabled, "test-struct")

	p, err := personasetting.NewStore(app.db, app.logger).Get(app.ctx, novelID)
	require.NoError(t, err)
	assert.False(t, p.Enabled, "非 mental 技能不应开启 persona")
}

// TestSaveMentalSkill_ClobbersExistingPersona 验证：再次保存另一心智时，本书活跃人格被覆盖为新心智。
func TestSaveMentalSkill_ClobbersExistingPersona(t *testing.T) {
	app := setupTestApp(t)
	novel := createTestNovel(t, app)
	novelID := novel.ID

	require.NoError(t, app.SaveContent(SaveContentInput{
		NovelID: novelID,
		Path:    "skills/first-mindset.md",
		Content: "---\nname: first-mindset\nkind: mental\nmode: auto\n---\nfirst\n",
	}))

	require.NoError(t, app.SaveContent(SaveContentInput{
		NovelID: novelID,
		Path:    "skills/second-mindset.md",
		Content: "---\nname: second-mindset\nkind: mental\nmode: auto\n---\nsecond\n",
	}))

	enabled, err := skill.LoadEnabled(novelID)
	require.NoError(t, err)
	assert.Contains(t, enabled, "first-mindset")
	assert.Contains(t, enabled, "second-mindset")

	p, err := personasetting.NewStore(app.db, app.logger).Get(app.ctx, novelID)
	require.NoError(t, err)
	assert.True(t, p.Enabled)
	assert.Equal(t, "second-mindset", p.Ref, "后保存的心智应覆盖为活跃人格")
}
