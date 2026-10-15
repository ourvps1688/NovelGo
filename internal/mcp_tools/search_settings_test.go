//go:build cgo

package mcp_tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/novelgo/novelgo/internal/setting"
)

func TestSearchSettingsSchema(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal((&SearchSettingsTool{}).JSONSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("no properties")
	}
	if _, ok := props["query"]; !ok {
		t.Error("schema should expose query")
	}
	if _, ok := props["top_k"]; !ok {
		t.Error("schema should expose top_k")
	}
}

func TestSearchSettingsExecute_KeywordFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings_test.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&setting.SettingItem{}); err != nil {
		t.Fatal(err)
	}
	// 释放底层连接，避免测试结束时 TempDir 清理因文件被占用而失败。
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()
	items := []setting.SettingItem{
		{NovelID: 1, Category: "世界观", Content: "天地分九重，灵气稀薄"},
		{NovelID: 1, Category: "角色", Content: "林惊羽身负血脉诅咒"},
		{NovelID: 2, Category: "世界观", Content: "另一本书的设定"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatal(err)
	}

	reg := testMCPRegistry(t, &SearchSettingsTool{})

	// 语义命中：query "林惊羽" 命中 novel1 的角色条目
	raw, _ := json.Marshal(map[string]any{"query": "林惊羽", "top_k": 3})
	res := reg.Execute(context.Background(), "search_settings", raw, ToolContext{DB: db, NovelID: 1}, nil)
	if !res.Success {
		t.Fatalf("search failed: %v", res.Error)
	}
	data := res.Data
	if data["count"].(int) != 1 {
		t.Fatalf("expected 1 match for 林惊羽, got %v", data["count"])
	}
	first := data["settings"].([]map[string]any)[0]
	if first["setting_id"].(int64) != items[1].ID {
		t.Fatalf("expected novel1 item1, got %v", first["setting_id"])
	}

	// top_k 钳制：>10 变 5；空 query 返回 novel1 全部（limit 内）
	raw2, _ := json.Marshal(map[string]any{"query": "", "top_k": 99})
	res2 := reg.Execute(context.Background(), "search_settings", raw2, ToolContext{DB: db, NovelID: 1}, nil)
	if !res2.Success {
		t.Fatalf("search2 failed: %v", res2.Error)
	}
	if res2.Data["count"].(int) != 2 {
		t.Fatalf("expected 2 novel1 items, got %v", res2.Data["count"])
	}

	// novel 隔离：novel2 仅 1 条
	raw3, _ := json.Marshal(map[string]any{"query": ""})
	res3 := reg.Execute(context.Background(), "search_settings", raw3, ToolContext{DB: db, NovelID: 2}, nil)
	if !res3.Success {
		t.Fatalf("search3 failed: %v", res3.Error)
	}
	if res3.Data["count"].(int) != 1 {
		t.Fatalf("expected 1 novel2 item, got %v", res3.Data["count"])
	}
}
