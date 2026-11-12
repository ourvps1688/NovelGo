package migrate

import (
	"os"
	"strings"
	"testing"
)

// #114 P2 供应商指标表按项目惯例走 AutoMigrate models（不写版本化迁移）。
// 本静态断言防止有人把 LLMSignalDaily 从 models 列表移除导致指标表消失。
func TestLLMSignalDailyRegisteredForAutoMigrate(t *testing.T) {
	data, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("读取 migrate.go 失败: %v", err)
	}
	src := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.Contains(src, "&activity.LLMSignalDaily{}") {
		t.Fatal("migrate.go 的 AutoMigrate models 必须包含 &activity.LLMSignalDaily{}")
	}
}
