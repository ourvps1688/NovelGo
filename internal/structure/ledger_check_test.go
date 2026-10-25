package structure

import (
	"fmt"
	"strings"
	"testing"
)

// TestCheckLedgerReconcileSourceMissing 来源失效：标注来源章已不存在 → 告警；
// 未标注/来源仍在不告警。
func TestCheckLedgerReconcileSourceMissing(t *testing.T) {
	findings := CheckLedgerReconcile([]ReconcileEntry{
		{Ref: "characters:1", Labels: []string{"林澈"}, Sourced: true, SourceGone: true},
		{Ref: "characters:2", Labels: []string{"苏晚"}, Sourced: true, SourceText: "苏晚转身离去。"},
		{Ref: "characters:3", Labels: []string{"无来源"}, Sourced: false},
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Check != CheckLedgerSourceMissing || f.Ref != "characters:1" || f.ID != "S1" {
		t.Fatalf("finding 错误: %+v", f)
	}
	if !strings.Contains(f.Desc, "不预设谁对") {
		t.Fatalf("来源失效告警应声明不预设谁对: %s", f.Desc)
	}
}

// TestCheckLedgerReconcileUnsupported 来源失支撑：来源章正文不含任何标签 → 告警；
// 任一标签（含别名）出现即算被支撑；短标签/空正文/无标签跳过（宁缺勿错）。
func TestCheckLedgerReconcileUnsupported(t *testing.T) {
	findings := CheckLedgerReconcile([]ReconcileEntry{
		// 正文完全不含条目标签 → 失支撑
		{Ref: "characters:1", Labels: []string{"林澈"}, Sourced: true, SourceText: "他推门而入，没有回头。"},
		// 别名在正文出现 → 被支撑，不告警
		{Ref: "characters:2", Labels: []string{"苏晚", "小七"}, Sourced: true, SourceText: "小七攥紧了拳头。"},
		// 短标签（<2 rune）不参与匹配 → 无有效标签跳过
		{Ref: "characters:3", Labels: []string{"七"}, Sourced: true, SourceText: "正文里什么都没有。"},
		// 正文不可得 → 跳过
		{Ref: "characters:4", Labels: []string{"陈守拙"}, Sourced: true, SourceText: ""},
		// 无标签 → 跳过
		{Ref: "characters:5", Sourced: true, SourceText: "正文。"},
	})
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Check != CheckLedgerUnsupported || f.Ref != "characters:1" {
		t.Fatalf("finding 错误: %+v", f)
	}
	if !strings.Contains(f.Desc, "不预设谁对") || !strings.Contains(f.Desc, "body") || !strings.Contains(f.Desc, "ledger") {
		t.Fatalf("失支撑告警应含方向判断提示: %s", f.Desc)
	}
	if !strings.Contains(f.Desc, "林澈") {
		t.Fatalf("失支撑告警应含标签预览: %s", f.Desc)
	}
}

// TestCheckLedgerReconcileDeterministic 同输入同输出：乱序输入 → 按 Ref 定序编号 S1..Sn。
func TestCheckLedgerReconcileDeterministic(t *testing.T) {
	in := []ReconcileEntry{
		{Ref: "time_entries:9", Labels: []string{"三年之约"}, Sourced: true, SourceGone: true},
		{Ref: "characters:2", Labels: []string{"林澈"}, Sourced: true, SourceGone: true},
		{Ref: "characters:10", Labels: []string{"苏晚"}, Sourced: true, SourceGone: true},
	}
	a := CheckLedgerReconcile(in)
	b := CheckLedgerReconcile([]ReconcileEntry{in[1], in[2], in[0]})
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("乱序输入产出不一致: %+v vs %+v", a[i], b[i])
		}
	}
	wantRefs := []string{"characters:10", "characters:2", "time_entries:9"} // 字符串升序
	for i, f := range a {
		if f.Ref != wantRefs[i] || f.ID != fmt.Sprintf("S%d", i+1) {
			t.Fatalf("定序错误 [%d]: %+v", i, f)
		}
	}
}

// TestCheckLedgerReconcileEmpty 零输入零告警（零提示契约）。
func TestCheckLedgerReconcileEmpty(t *testing.T) {
	if got := CheckLedgerReconcile(nil); len(got) != 0 {
		t.Fatalf("零输入应零告警: %+v", got)
	}
	if got := CheckLedgerReconcile([]ReconcileEntry{{Ref: "x:1"}}); len(got) != 0 {
		t.Fatalf("无来源标注应零告警: %+v", got)
	}
}
