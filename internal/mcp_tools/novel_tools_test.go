//go:build cgo

package mcp_tools_test

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/mcp_tools"
)

func TestGetChapterListGroupsByVolume(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	firstVolumeID := seedVolume(t, db, tc.NovelID, "第一卷", 1)
	secondVolumeID := seedVolume(t, db, tc.NovelID, "第二卷", 2)

	firstVolume := firstVolumeID
	secondVolume := secondVolumeID
	chapters := []*chapter.Chapter{
		{NovelID: tc.NovelID, VolumeID: &firstVolume, SortOrder: 1, Title: "初入江湖", Summary: "不应出现的摘要", WordCount: 1100},
		{NovelID: tc.NovelID, VolumeID: &firstVolume, SortOrder: 2, Title: "夜宿客栈", WordCount: 1200},
		{NovelID: tc.NovelID, VolumeID: &secondVolume, SortOrder: 1, Title: "再入京城", WordCount: 1300},
		{NovelID: tc.NovelID, SortOrder: 1, Title: "未分卷草稿", WordCount: 1400},
	}
	for _, ch := range chapters {
		if err := db.Create(ch).Error; err != nil {
			t.Fatalf("seed chapter: %v", err)
		}
	}

	registry := mcp_tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	mcp_tools.RegisterNovelTools(registry)
	result := registry.Execute(ctx, "get_chapter_list", []byte(`{"page":1,"size":4}`), tc, nil)
	if !result.Success {
		t.Fatalf("get_chapter_list failed: %s", result.Error)
	}

	content, ok := result.Data["content"].(string)
	if !ok {
		t.Fatalf("content = %#v, want string", result.Data["content"])
	}
	// 升序：第一卷在前、第二卷次之、未分卷置于末尾；第1章对应第一卷首章。
	want := fmt.Sprintf(`## 第一卷 [volume_id:%d]
- 第1章《初入江湖》 [chapter_id:%d] · 1100 字
- 第2章《夜宿客栈》 [chapter_id:%d] · 1200 字

## 第二卷 [volume_id:%d]
- 第3章《再入京城》 [chapter_id:%d] · 1300 字

## 未分卷
- 第4章《未分卷草稿》 [chapter_id:%d] · 1400 字`,
		firstVolumeID, chapters[0].ID, chapters[1].ID, secondVolumeID, chapters[2].ID, chapters[3].ID)
	if content != want {
		t.Errorf("content =\n%s\nwant:\n%s", content, want)
	}
	if strings.Contains(content, chapters[0].Summary) {
		t.Error("content should not include chapter summary")
	}
	if _, ok := result.Data["items"]; ok {
		t.Error("data should expose grouped content instead of raw items")
	}
	if got := result.Data["total"]; got != int64(4) {
		t.Errorf("total = %v, want 4", got)
	}
}

func TestListVolumes(t *testing.T) {
	db, tc, ctx := setupRWEnv(t)
	firstID := seedVolume(t, db, tc.NovelID, "第一卷", 1)
	secondID := seedVolume(t, db, tc.NovelID, "第二卷", 2)

	registry := mcp_tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	mcp_tools.RegisterNovelTools(registry)
	result := registry.Execute(ctx, "list_volumes", []byte(`{}`), tc, nil)
	if !result.Success {
		t.Fatalf("list_volumes failed: %s", result.Error)
	}

	content, ok := result.Data["content"].(string)
	if !ok {
		t.Fatalf("content = %#v, want string", result.Data["content"])
	}

	if result.Data["count"] != 2 {
		t.Errorf("count = %v, want 2", result.Data["count"])
	}
	if !strings.Contains(content, fmt.Sprintf("[id:%d] 第一卷", firstID)) {
		t.Errorf("content missing first volume: %s", content)
	}
	if !strings.Contains(content, fmt.Sprintf("[id:%d] 第二卷", secondID)) {
		t.Errorf("content missing second volume: %s", content)
	}
	if !strings.Contains(content, fmt.Sprintf("volumes/id_%d.md", firstID)) {
		t.Errorf("content missing first volume path: %s", content)
	}
}
