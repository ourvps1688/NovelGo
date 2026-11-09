package migrate

import (
	"os"
	"strings"
	"testing"
)

// 新增表按项目惯例走 AutoMigrate models（不写版本化迁移；v173 删表是特例）。
// 本静态断言防止有人把 SkillInvocation 从 models 列表移除导致账本表消失。
func TestSkillInvocationRegisteredForAutoMigrate(t *testing.T) {
	data, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("读取 migrate.go 失败: %v", err)
	}
	if !strings.Contains(string(data), "&activity.SkillInvocation{}") {
		t.Fatal("migrate.go 的 AutoMigrate models 必须包含 &activity.SkillInvocation{}")
	}
}
