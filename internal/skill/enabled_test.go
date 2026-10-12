package skill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/novelgo/novelgo/internal/config"
)

func TestEnabledRoundtrip(t *testing.T) {
	novelID := int64(99001)

	// 文件不存在 → 空切片
	names, err := LoadEnabled(novelID)
	if err != nil {
		t.Fatalf("LoadEnabled(不存在) err = %v, want nil", err)
	}
	if len(names) != 0 {
		t.Fatalf("LoadEnabled(不存在) = %v, want empty", names)
	}

	// 写入 → 读回一致
	want := []string{"cool-style", "悬疑节奏", "cool-style", "  "}
	if err := SaveEnabled(novelID, want); err != nil {
		t.Fatalf("SaveEnabled err = %v", err)
	}
	got, err := LoadEnabled(novelID)
	if err != nil {
		t.Fatalf("LoadEnabled err = %v", err)
	}
	if len(got) != 2 || got[0] != "cool-style" || got[1] != "悬疑节奏" {
		t.Fatalf("LoadEnabled = %v, want 去重去空白后的 2 项", got)
	}

	// 文件确实落在小说技能目录，且 scanDir 不会把 .json 当技能
	path := EnabledFilePath(novelID)
	if filepath.Base(path) != ".enabled.json" {
		t.Fatalf("path = %s, want .enabled.json", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("enabled file missing: %v", err)
	}
	skills, err := scanDir(nil, config.NovelSkillsDir(novelID))
	if err != nil {
		t.Fatalf("scanDir err = %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("scanDir 把启用清单误认为技能: %v", skills)
	}

	// 全量覆盖语义
	if err := SaveEnabled(novelID, []string{"only-one"}); err != nil {
		t.Fatalf("SaveEnabled(覆盖) err = %v", err)
	}
	got, _ = LoadEnabled(novelID)
	if len(got) != 1 || got[0] != "only-one" {
		t.Fatalf("覆盖后 = %v, want [only-one]", got)
	}
}
