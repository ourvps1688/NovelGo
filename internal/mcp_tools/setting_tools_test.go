package mcp_tools

import (
	"context"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/setting"
)

// TestUpsertSettingFlatFormCreate_Success 验证 LLM 漏 settings 包裹、直接平铺单条
// category/content 时，工具不再报 "参数格式不正确: json: unknown field"（截图实测的
// 真实 bug），而是归一化后成功创建并返回 setting_id。
func TestUpsertSettingFlatFormCreate_Success(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertSettingTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&setting.SettingItem{}); err != nil {
		t.Fatal(err)
	}

	res := reg.Execute(context.Background(), "upsert_setting",
		[]byte(`{"category":"世界观","content":"灵气复苏，万物可修"}`),
		ToolContext{DB: db, NovelID: 1}, nil)

	if !res.Success {
		t.Fatalf("flat-form create failed: %+v", res)
	}
	data := res.Data
	ids, ok := data["setting_ids"].([]int64)
	if !ok || len(ids) != 1 {
		t.Fatalf("setting_ids = %v, want exactly 1 id", data["setting_ids"])
	}
	if data["count"] != 1 {
		t.Errorf("count = %v, want 1", data["count"])
	}
}

// TestUpsertSettingFlatForm_RejectsUnknownImmutableField 验证平铺容错没有打开安全缺口：
// id/novel_id 等不可变字段仍是 schema 外字段，框架层 DisallowUnknownFields 必须拒绝，
// 防止 LLM 经顶层字段覆盖实体归属。
func TestUpsertSettingFlatForm_RejectsUnknownImmutableField(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertSettingTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&setting.SettingItem{}); err != nil {
		t.Fatal(err)
	}

	res := reg.Execute(context.Background(), "upsert_setting",
		[]byte(`{"category":"世界观","content":"灵气","id":99}`),
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

// TestUpsertSettingFlatForm_MissingRequired 验证平铺单条缺 category/content 时返回
// 明确的业务校验失败（而非 unknown-field 解码错误），便于 LLM 自行纠正。
func TestUpsertSettingFlatForm_MissingRequired(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertSettingTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&setting.SettingItem{}); err != nil {
		t.Fatal(err)
	}

	res := reg.Execute(context.Background(), "upsert_setting",
		[]byte(`{"category":"世界观"}`),
		ToolContext{DB: db, NovelID: 1}, nil)

	if res.Success {
		t.Fatal("expected failure for missing content")
	}
	if !strings.Contains(res.Error, "缺少 category 或 content") {
		t.Errorf("Error = %q, want missing-field message", res.Error)
	}
}

// TestUpsertSettingFlatFormWithSettingID_Updates 验证平铺单条带 setting_id 时走更新
// （PATCH）分支而非新建，与数组形态的语义一致。
func TestUpsertSettingFlatFormWithSettingID_Updates(t *testing.T) {
	reg := testMCPRegistry(t, &UpsertSettingTool{})
	db := testMCPDB(t)
	if err := db.AutoMigrate(&setting.SettingItem{}); err != nil {
		t.Fatal(err)
	}

	created := reg.Execute(context.Background(), "upsert_setting",
		[]byte(`{"category":"世界观","content":"初版内容"}`),
		ToolContext{DB: db, NovelID: 1}, nil)
	if !created.Success {
		t.Fatalf("create failed: %+v", created)
	}
	createdID := created.Data["setting_ids"].([]int64)[0]

	updated := reg.Execute(context.Background(), "upsert_setting",
		[]byte(`{"setting_id":`+itoa(createdID)+`,"category":"世界观","content":"修订后的内容"}`),
		ToolContext{DB: db, NovelID: 1}, nil)
	if !updated.Success {
		t.Fatalf("flat-form update failed: %+v", updated)
	}
	updatedID := updated.Data["setting_ids"].([]int64)[0]
	if updatedID != createdID {
		t.Errorf("updated id = %d, want %d (should be in-place update, not new row)", updatedID, createdID)
	}

	// 确认 DB 中该条已被覆盖（仍只有一行，内容更新）。
	var rows []setting.SettingItem
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("row count = %d, want 1 (update must not create new row)", len(rows))
	}
	if rows[0].Content != "修订后的内容" {
		t.Errorf("content = %q, want updated content", rows[0].Content)
	}
}

// itoa 是小工具，避免引入 strconv 仅用于测试 ID 拼接。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
