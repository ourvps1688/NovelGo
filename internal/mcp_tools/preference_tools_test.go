package mcp_tools

import (
	"context"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/preference"
)

// TestUpsertPreferenceFlatFormCreate_Success 验证 upsert_preference 与 upsert_setting
// 同构：LLM 漏 preferences 包裹、直接平铺 category/content 时不再报 unknown field，
// 而是归一化后成功创建。
func TestUpsertPreferenceFlatFormCreate_Success(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertPreferenceTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&preference.PreferenceItem{}); err != nil {
		t.Fatal(err)
	}

	res := reg.Execute(context.Background(), "upsert_preference",
		[]byte(`{"category":"文风","content":"简洁克制，少用长句"}`),
		ToolContext{DB: db, NovelID: 1}, nil)

	if !res.Success {
		t.Fatalf("flat-form create failed: %+v", res)
	}
	data := res.Data
	ids, ok := data["preference_ids"].([]int64)
	if !ok || len(ids) != 1 {
		t.Fatalf("preference_ids = %v, want exactly 1 id", data["preference_ids"])
	}
	if data["count"] != 1 {
		t.Errorf("count = %v, want 1", data["count"])
	}
}

// TestUpsertPreferenceFlatForm_RejectsUnknownImmutableField 验证平铺容错同样没有打开
// 安全缺口：id/novel_id 等不可变字段仍被 DisallowUnknownFields 拒绝。
func TestUpsertPreferenceFlatForm_RejectsUnknownImmutableField(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertPreferenceTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&preference.PreferenceItem{}); err != nil {
		t.Fatal(err)
	}

	res := reg.Execute(context.Background(), "upsert_preference",
		[]byte(`{"category":"文风","content":"简洁","id":99}`),
		ToolContext{DB: db, NovelID: 1}, nil)

	if res.Success {
		t.Fatal("expected rejection of unknown immutable field 'id'")
	}
	if res.ErrKind != ErrKindArgs {
		t.Errorf("ErrKind = %q, want %q", res.ErrKind, ErrKindArgs)
	}
	if !strings.Contains(res.Error, "unknown field") {
		t.Errorf("Error = %q, want unknown-field rejection", res.Error)
	}
}

// TestUpsertPreferenceFlatForm_MissingRequired 验证平铺单条缺 category/content 时返回
// 明确校验失败。
func TestUpsertPreferenceFlatForm_MissingRequired(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertPreferenceTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&preference.PreferenceItem{}); err != nil {
		t.Fatal(err)
	}

	res := reg.Execute(context.Background(), "upsert_preference",
		[]byte(`{"category":"文风"}`),
		ToolContext{DB: db, NovelID: 1}, nil)

	if res.Success {
		t.Fatal("expected failure for missing content")
	}
	if !strings.Contains(res.Error, "缺少 category 或 content") {
		t.Errorf("Error = %q, want missing-field message", res.Error)
	}
}
