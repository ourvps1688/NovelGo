package setting

import (
	"strings"
	"testing"
	"time"
)

func mkItem(id int64, cat, content string, ageMinutes int) SettingItem {
	return SettingItem{
		ID:        id,
		Category:  cat,
		Content:   content,
		UpdatedAt: time.Now().Add(-time.Duration(ageMinutes) * time.Minute),
	}
}

func TestFormatSettings_Empty(t *testing.T) {
	out, trunc := FormatSettings(nil)
	if out != "" || trunc != 0 {
		t.Errorf("empty: got (%q, %d), want (\"\", 0)", out, trunc)
	}
}

func TestFormatSettings_NoTruncation(t *testing.T) {
	items := []SettingItem{
		mkItem(1, "世界观", "天地分九霄。", 10),
		mkItem(2, "角色", "主角名阿七。", 5),
	}
	out, trunc := FormatSettings(items)
	if trunc != 0 {
		t.Errorf("expected no truncation, got %d", trunc)
	}
	if !strings.Contains(out, "setting_id:1") || !strings.Contains(out, "setting_id:2") {
		t.Errorf("both items should appear: %q", out)
	}
}

// TestFormatSettings_BucketProtectsFoundational 是 P3 核心保证：
// 大量「角色」条目也不能把少量「世界观」根基设定挤出预算。
func TestFormatSettings_BucketProtectsFoundational(t *testing.T) {
	items := make([]SettingItem, 0, 103)
	// 3 条世界观（根基，权重 4），内容很小，必然保留
	items = append(items, mkItem(1, "世界观", "天地分九霄。", 100), mkItem(2, "世界观", "灵气源于地脉。", 90), mkItem(3, "世界观", "王朝名大周。", 80))
	// 100 条角色（权重 1），每条内容很长，必超出其桶预算被截断
	for i := 0; i < 100; i++ {
		items = append(items, mkItem(int64(100+i), "角色", strings.Repeat("某配角背景设定详尽描述占大量篇幅。", 20), i))
	}

	out, trunc := FormatSettings(items)
	if trunc == 0 {
		t.Errorf("expected 角色 bucket to be truncated, got trunc=0")
	}
	// 世界观三条必须全部保留（根基保护）
	for _, id := range []string{"setting_id:1", "setting_id:2", "setting_id:3"} {
		if !strings.Contains(out, id) {
			t.Errorf("foundational 世界观 item %s was dropped — bucket protection FAILED:\n%s", id, out)
		}
	}
	// 角色应被截断（并非全部 100 条都保留）；断言至少有一条高位 id 缺失
	if strings.Contains(out, "setting_id:199") {
		t.Errorf("expected high-id 角色 item to be truncated, but setting_id:199 present")
	}
	// 排序：世界观（权重4）应出现在角色（权重1）之前
	wIdx := strings.Index(out, "| 世界观")
	rIdx := strings.Index(out, "| 角色")
	if wIdx == -1 || rIdx == -1 || wIdx > rIdx {
		t.Errorf("expected 世界观 bucket before 角色 bucket, got wIdx=%d rIdx=%d", wIdx, rIdx)
	}
}

// TestFormatSettings_PerBucketCap 单分类超量时按本桶预算截断。
func TestFormatSettings_PerBucketCap(t *testing.T) {
	items := make([]SettingItem, 0, 50)
	for i := 0; i < 50; i++ {
		// 每条约 480 字，50 条远超单桶预算（12k token），必触发截断
		items = append(items, mkItem(int64(i+1), "角色", strings.Repeat("长设定内容。", 80), i))
	}
	out, trunc := FormatSettings(items)
	if trunc == 0 {
		t.Errorf("single-category overflow should truncate, got 0")
	}
	if !strings.Contains(out, "分类超出其预算上限") {
		t.Errorf("expected per-bucket truncation note, got:\n%s", out)
	}
}

// TestSettingCategoryWeight 校验分类权重与关键词兜底。
func TestSettingCategoryWeight(t *testing.T) {
	cases := map[string]int{
		"世界观":    4,
		"力量体系":   3,
		"历史":     3,
		"地理":     2,
		"角色":     1,
		"物品":     1,
		"世界观设定":  3, // 关键词兜底
		"修仙力量体系": 3, // 关键词兜底
		"某自定义分类": 1,
	}
	for cat, want := range cases {
		if got := settingCategoryWeight(cat); got != want {
			t.Errorf("settingCategoryWeight(%q) = %d, want %d", cat, got, want)
		}
	}
}

// TestFormatSettings_EmptyCategoryFallsBack 空分类归入「其他」并默认权重。
func TestFormatSettings_EmptyCategoryFallsBack(t *testing.T) {
	items := []SettingItem{
		mkItem(1, "", "无分类设定。", 10),
		mkItem(2, "", "另一条无分类。", 9),
	}
	out, trunc := FormatSettings(items)
	if trunc != 0 {
		t.Errorf("expected no truncation for tiny input, got %d", trunc)
	}
	if !strings.Contains(out, "其他") {
		t.Errorf("empty category should render as 其他: %q", out)
	}
}
