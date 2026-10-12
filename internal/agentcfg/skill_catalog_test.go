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
