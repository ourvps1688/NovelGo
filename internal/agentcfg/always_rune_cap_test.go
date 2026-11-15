package agentcfg

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/skill"
)

// TestBuildAlwaysSkillsContent_CapsRuneBudget —— #128 常驻块字符帽：
// MaxAlwaysSkillsResident=3 只挡「几个技能」，挡不住用户自定义/勾选的超长正文。
func TestBuildAlwaysSkillsContent_CapsRuneBudget(t *testing.T) {
	dir := t.TempDir()
	longBody := strings.Repeat("长", maxAlwaysSkillsRuneCap+300) + "_TAIL_SENTINEL"
	writeTestSkill(t, dir, "huge", "always", longBody)
	writeTestSkill(t, dir, "after", "always", "AFTER_BODY")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := skill.NewStore(logger, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	meta := []skill.SkillMeta{
		{Name: "huge", Mode: skill.ModeAlways, Source: "user"},
		{Name: "after", Mode: skill.ModeAlways, Source: "user"},
	}
	content := BuildAlwaysSkillsContent(meta, store, int64(9))
	if strings.Contains(content, "_TAIL_SENTINEL") {
		t.Error("超长常驻正文未被节选，尾部哨兵仍被注入")
	}
	if strings.Contains(content, "AFTER_BODY") {
		t.Error("字符额度耗尽后仍注入了后续常驻技能")
	}
	for _, want := range []string{
		"常驻技能正文已按字符上限节选",
		"常驻技能正文已达 12000 字符上限",
		"【常驻技能（兼容层）】",
		"常驻上限 3 个。",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("缺少提示/原有口径 %q\ngot: %s", want, content)
		}
	}
	if got := len([]rune(content)); got > maxAlwaysSkillsRuneCap+600 {
		t.Errorf("常驻块总 rune=%d 超过帽+600 噪声余量", got)
	}
}

// TestBuildAlwaysSkillsContent_ShortBodyUnaffected 锁短正文零行为变化：
// 全文保留、无节选提示（补帽不得改动现有 3 个内置/勾选场景的输出）。
func TestBuildAlwaysSkillsContent_ShortBodyUnaffected(t *testing.T) {
	dir := t.TempDir()
	writeTestSkill(t, dir, "short", "always", "SHORT_BODY")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := skill.NewStore(logger, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	content := BuildAlwaysSkillsContent(
		[]skill.SkillMeta{{Name: "short", Mode: skill.ModeAlways, Source: "user"}},
		store, int64(3))
	if !strings.Contains(content, "SHORT_BODY") {
		t.Errorf("短正文应全文注入\ngot: %s", content)
	}
	if strings.Contains(content, "节选") || strings.Contains(content, "字符上限") {
		t.Errorf("短正文不应触发字符帽提示\ngot: %s", content)
	}
}
