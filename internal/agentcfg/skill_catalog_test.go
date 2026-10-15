package agentcfg

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/skill"
)

func writeTestSkill(t *testing.T, dir, name, mode, body string) {
	t.Helper()
	fm := "---\nname: " + name + "\nmode: " + mode + "\ndescription: 测试技能 " + name + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(fm), 0o644); err != nil {
		t.Fatalf("write skill %s: %v", name, err)
	}
}

func TestBuildAlwaysSkillsContent_MergesEnabled(t *testing.T) {
	userDir := t.TempDir()
	writeTestSkill(t, userDir, "skill-auto", "auto", "AUTO_BODY")
	writeTestSkill(t, userDir, "skill-manual", "manual", "MANUAL_BODY")
	writeTestSkill(t, userDir, "skill-always", "always", "ALWAYS_BODY")
	writeTestSkill(t, userDir, "skill-enabled", "auto", "ENABLED_BODY")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := skill.NewStore(logger, userDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	novelID := int64(42)

	// 勾选启用 skill-enabled；ghost 已不存在，应被忽略
	if err := skill.SaveEnabled(novelID, []string{"skill-enabled", "ghost"}); err != nil {
		t.Fatalf("SaveEnabled: %v", err)
	}

	// 注意：不走 store.ListMeta——它会自动从 config.UserSkillsDir() 重载并冲掉
	// 测试注入的临时目录；Get 不重载，手工构造 meta 即可覆盖三个分支。
	meta := []skill.SkillMeta{
		{Name: "skill-auto", Mode: skill.ModeAuto, Source: "user"},
		{Name: "skill-manual", Mode: skill.ModeManual, Source: "user"},
		{Name: "skill-always", Mode: skill.ModeAlways, Source: "user"},
		{Name: "skill-enabled", Mode: skill.ModeAuto, Source: "user"},
	}
	content := BuildAlwaysSkillsContent(meta, store, novelID)

	for _, want := range []string{"ALWAYS_BODY", "ENABLED_BODY"} {
		if !strings.Contains(content, want) {
			t.Errorf("常驻注入缺少 %s\ngot: %s", want, content)
		}
	}
	for _, notWant := range []string{"AUTO_BODY", "MANUAL_BODY"} {
		if strings.Contains(content, notWant) {
			t.Errorf("常驻注入不应包含 %s\ngot: %s", notWant, content)
		}
	}

	// 已勾选启用的技能从 auto 目录中排除（与 BuildSystemMessages 中组合一致），避免重复
	enabled, _ := skill.LoadEnabled(novelID)
	catalog := BuildSkillCatalog(excludeNames(store.ListMetaForCatalog(meta), enabled))
	if strings.Contains(catalog, "skill-enabled") {
		t.Errorf("目录不应包含已常驻的 skill-enabled\ngot: %s", catalog)
	}
	if !strings.Contains(catalog, "skill-auto") {
		t.Errorf("目录应包含未启用的 skill-auto\ngot: %s", catalog)
	}

	// 未勾选任何技能时回到原有行为：仅 always 注入
	if err := skill.SaveEnabled(novelID, nil); err != nil {
		t.Fatalf("SaveEnabled(empty): %v", err)
	}
	content = BuildAlwaysSkillsContent(meta, store, novelID)
	if !strings.Contains(content, "ALWAYS_BODY") || strings.Contains(content, "ENABLED_BODY") {
		t.Errorf("清空勾选后应仅注入 always，got: %s", content)
	}
}

func TestBuildSkillCatalog_GroupsByCategory(t *testing.T) {
	meta := []skill.SkillMeta{
		{Name: "scene-beats", Description: "场景节拍", Category: "结构", Mode: skill.ModeAuto, Source: "builtin"},
		{Name: "character-design", Description: "角色设计", Category: "角色", Mode: skill.ModeAuto, Source: "builtin"},
		{Name: "pacing-control", Description: "节奏控制", Category: "结构", Mode: skill.ModeAuto, Source: "builtin"},
		{Name: "dialogue-subtext", Description: "对白潜台词", Category: "角色", Mode: skill.ModeAuto, Source: "builtin"},
		{Name: "orphan-skill", Description: "无分类", Category: "", Mode: skill.ModeAuto, Source: "builtin"},
	}
	catalog := BuildSkillCatalog(meta)

	// 1. 每个分类以 ### 子标题呈现
	for _, cat := range []string{"### 结构", "### 角色", "### 其他"} {
		if !strings.Contains(catalog, cat) {
			t.Errorf("目录缺少分类子标题 %q\ngot: %s", cat, catalog)
		}
	}

	// 2. 分类区块出现在来源标题「内置技能（只读）」之下
	srcIdx := strings.Index(catalog, "## 内置技能（只读）")
	catIdx := strings.Index(catalog, "### 结构")
	if srcIdx < 0 || catIdx < 0 || catIdx < srcIdx {
		t.Errorf("分类子标题应位于来源标题之下，srcIdx=%d catIdx=%d", srcIdx, catIdx)
	}

	// 3. 同分类技能在该子标题下相邻出现（顺序稳定：首次出现优先）
	structBlock := catalog[strings.Index(catalog, "### 结构"):strings.Index(catalog, "### 角色")]
	if strings.Index(structBlock, "scene-beats") > strings.Index(structBlock, "pacing-control") {
		t.Errorf("同分类内应按首次出现顺序：scene-beats 应在 pacing-control 之前\ngot: %s", structBlock)
	}

	// 4. 无分类技能归入「其他」且置于末尾（在「角色」之后）
	otherIdx := strings.Index(catalog, "### 其他")
	roleIdx := strings.Index(catalog, "### 角色")
	if otherIdx < roleIdx {
		t.Errorf("「其他」应置于有名分类之后，otherIdx=%d roleIdx=%d", otherIdx, roleIdx)
	}
	if !strings.Contains(catalog[otherIdx:], "orphan-skill") {
		t.Errorf("「其他」组应包含无分类技能 orphan-skill\ngot: %s", catalog[otherIdx:])
	}

	// 5. 路由消歧指引存在
	if !strings.Contains(catalog, "路由指引（消歧）") {
		t.Errorf("目录应包含路由消歧指引\ngot: %s", catalog)
	}
}
