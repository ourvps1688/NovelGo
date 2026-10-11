package mcp_tools

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/reader"
)

func TestCreateReaderPerspectiveEntry_ValidatesChapterNovel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&chapter.Chapter{}, &reader.ReaderPerspective{}); err != nil {
		t.Fatal(err)
	}
	currentChapter := chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "第一章"}
	otherNovelChapter := chapter.Chapter{NovelID: 2, SortOrder: 1, Title: "另一部小说"}
	if err := db.Create(&currentChapter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherNovelChapter).Error; err != nil {
		t.Fatal(err)
	}

	tool := &CreateReaderPerspectiveEntryTool{}
	tc := ToolContext{DB: db, NovelID: 1}
	result, err := tool.Execute(context.Background(), &CreateReaderPerspectiveEntryArgs{Entries: []CreateReaderPerspectiveEntryItem{{
		Type:             reader.TypeKnown,
		Content:          "读者知道真相",
		PlantedChapterID: currentChapter.ID,
	}}}, tc)
	if err != nil || !result.Success {
		t.Fatalf("create result = %+v, err = %v", result, err)
	}

	var created reader.ReaderPerspective
	if err := db.First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.PlantedChapterID == nil || *created.PlantedChapterID != currentChapter.ID {
		t.Errorf("planted chapter ID = %v, want %d", created.PlantedChapterID, currentChapter.ID)
	}

	result, err = tool.Execute(context.Background(), &CreateReaderPerspectiveEntryArgs{Entries: []CreateReaderPerspectiveEntryItem{{
		Type:             reader.TypeKnown,
		Content:          "跨小说引用",
		PlantedChapterID: otherNovelChapter.ID,
	}}}, tc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || !strings.Contains(result.Error, "不属于当前小说") {
		t.Errorf("cross-novel result = %+v, want business rejection", result)
	}
}

func TestFormatReaderPerspective_MissingPlantedChapter(t *testing.T) {
	formatted := formatReaderPerspective(
		[]reader.ReaderPerspective{{ID: 1, Content: "历史条目"}},
		nil,
		nil,
		nil,
	)
	if !strings.Contains(formatted, "章节信息缺失") {
		t.Errorf("formatted = %q, want missing chapter label", formatted)
	}
}

// 回归测试：LLM 会在 create 参数里带上 revealed_chapter_id（update 工具的字段），
// 框架层 DisallowUnknownFields 曾直接拒绝（json: unknown field "revealed_chapter_id"）。
// 修复后 create schema 接受该可选字段，并正确落库。
func TestCreateReaderPerspectiveEntry_AcceptsRevealedChapterID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&chapter.Chapter{}, &reader.ReaderPerspective{}); err != nil {
		t.Fatal(err)
	}
	ch := chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "第一章"}
	if err := db.Create(&ch).Error; err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := NewRegistry(logger)
	RegisterReaderPerspectiveTools(reg)

	raw := json.RawMessage(`{"entries":[{"type":"misconception","content":"读者以为主角死了","planted_chapter_id":1,"related_truth":"主角假死","revealed_chapter_id":1}]}`)
	res := reg.Execute(context.Background(), "create_reader_perspective_entry", raw, ToolContext{DB: db, NovelID: 1}, nil)
	if res.Error != "" || !res.Success {
		t.Fatalf("create with revealed_chapter_id failed: %+v", res)
	}

	var created reader.ReaderPerspective
	if err := db.First(&created).Error; err != nil {
		t.Fatal(err)
	}
	if created.RevealedChapterID == nil || *created.RevealedChapterID != 1 {
		t.Errorf("revealed chapter ID = %v, want 1", created.RevealedChapterID)
	}
}
