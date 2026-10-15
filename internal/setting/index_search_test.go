package setting

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSettingChunkIDRoundTrip(t *testing.T) {
	id := int64(12345)
	chunkID := settingChunkID(id)
	if chunkID != "setting:12345" {
		t.Fatalf("unexpected chunk id: %q", chunkID)
	}
	if got := parseSettingID(chunkID); got != id {
		t.Fatalf("parse mismatch: got %d want %d", got, id)
	}
}

func TestParseSettingIDInvalid(t *testing.T) {
	cases := map[string]int64{
		"chapter:1":   0,
		"setting:":    0,
		"setting:abc": 0,
		"":            0,
	}
	for in, want := range cases {
		if got := parseSettingID(in); got != want {
			t.Fatalf("parseSettingID(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSplitCatContent(t *testing.T) {
	cat, content := splitCatContent("世界观：天地分九重")
	if cat != "世界观" || content != "天地分九重" {
		t.Fatalf("got %q / %q", cat, content)
	}
	// 正文本身含分隔符：仅首个分隔符生效
	cat, content = splitCatContent("世界观：修炼：灵气")
	if cat != "世界观" || content != "修炼：灵气" {
		t.Fatalf("got %q / %q", cat, content)
	}
	// 空分类
	cat, content = splitCatContent("只有内容")
	if cat != "" || content != "只有内容" {
		t.Fatalf("got %q / %q", cat, content)
	}
}

func TestBuildSettingChunkText(t *testing.T) {
	if got := buildSettingChunkText("力量体系", "练气九层"); got != "力量体系：练气九层" {
		t.Fatalf("got %q", got)
	}
	if got := buildSettingChunkText("", "无分类"); got != "无分类" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSettingChunk(t *testing.T) {
	item := SettingItem{ID: 7, Category: "角色", Content: "主角林惊羽"}
	c := BuildSettingChunk(item)
	if c.ID != "setting:7" || c.ChunkType != SettingChunkType || c.ChapterID != 0 {
		t.Fatalf("unexpected chunk: %+v", c)
	}
	if c.Content != "角色：主角林惊羽" {
		t.Fatalf("unexpected content %q", c.Content)
	}
}

func newSettingTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&SettingItem{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestKeywordSearchSettings(t *testing.T) {
	db := newSettingTestDB(t)
	items := []SettingItem{
		{NovelID: 1, Category: "世界观", Content: "天地分九重，灵气稀薄"},
		{NovelID: 1, Category: "力量体系", Content: "练气九层可御剑飞行"},
		{NovelID: 1, Category: "角色", Content: "林惊羽身负血脉诅咒"},
		{NovelID: 2, Category: "世界观", Content: "另一本书的设定"},
	}
	if err := db.Create(&items).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 关键词 "灵气" 只命中 novel1 的第 1 条（content 匹配）
	res, err := keywordSearchSettings(context.Background(), db, 1, "灵气", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 || res[0].SettingID != items[0].ID {
		t.Fatalf("expected only item0, got %+v", res)
	}

	// 空查询返回 novel1 全部（limit 内）
	res, err = keywordSearchSettings(context.Background(), db, 1, "", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("expected 3 novel1 items, got %d", len(res))
	}

	// novel 隔离
	res, err = keywordSearchSettings(context.Background(), db, 2, "", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 novel2 item, got %d", len(res))
	}

	// topK 截断
	res, err = keywordSearchSettings(context.Background(), db, 1, "", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected topK=2, got %d", len(res))
	}
}
