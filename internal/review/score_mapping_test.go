package review

import (
	"reflect"
	"testing"
)

// TestScoreDimToAuditDimsKeysMatchScoringDimensions 锁定映射表判官侧键集
// == scoringDimensions 六键（评分判官改维度时必须同步本映射表）。
func TestScoreDimToAuditDimsKeysMatchScoringDimensions(t *testing.T) {
	want := map[string]bool{}
	for _, d := range scoringDimensions {
		want[d.Key] = true
	}
	got := map[string]bool{}
	for k := range ScoreDimToAuditDims {
		got[k] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("映射表判官侧键集与 scoringDimensions 不一致: got %v want %v", got, want)
	}
}

// TestScoreDimToAuditDimsTargetsValid 锁定映射目标 ⊆ AuditDimOrder 八键，
// 且覆盖除 fit（判官侧不评题材契合）外的全部体检维度。
func TestScoreDimToAuditDimsTargetsValid(t *testing.T) {
	valid := map[string]bool{}
	for _, k := range AuditDimOrder {
		valid[k] = true
	}
	covered := map[string]bool{}
	for jd, ads := range ScoreDimToAuditDims {
		for _, ad := range ads {
			if !valid[ad] {
				t.Fatalf("判官维度 %s 映射到未知体检维度 %q", jd, ad)
			}
			covered[ad] = true
		}
	}
	for _, ad := range AuditDimOrder {
		if ad == AuditDimFit {
			if covered[ad] {
				t.Fatalf("fit（题材契合）不应被判官维度覆盖（判官不评题材契合）")
			}
			continue
		}
		if !covered[ad] {
			t.Fatalf("体检维度 %s 未被任何判官维度覆盖", ad)
		}
	}
}

// TestAuditDimOrderMatchesSignabilityContract 锁定八键集合与体检契约一致
// （opening/fit/character/pacing/world/longArc/compliance/reader）。
func TestAuditDimOrderMatchesSignabilityContract(t *testing.T) {
	want := []string{"opening", "fit", "character", "pacing", "world", "longArc", "compliance", "reader"}
	if !reflect.DeepEqual(AuditDimOrder, want) {
		t.Fatalf("AuditDimOrder = %v, want %v", AuditDimOrder, want)
	}
}

func TestStarsFromScore(t *testing.T) {
	cases := []struct {
		score int
		want  int
	}{
		{-5, 1}, {0, 1}, {39, 1}, {40, 2}, {54, 2}, {55, 3},
		{69, 3}, {70, 4}, {84, 4}, {85, 5}, {100, 5}, {120, 5},
	}
	for _, c := range cases {
		if got := StarsFromScore(c.score); got != c.want {
			t.Fatalf("StarsFromScore(%d) = %d, want %d", c.score, got, c.want)
		}
	}
}

func TestJudgeToAuditScores(t *testing.T) {
	// 空输入 → 空输出
	if got := JudgeToAuditScores(nil); got != nil {
		t.Fatalf("nil 输入应返回 nil, got %v", got)
	}

	// word_count(80)+plot_advance(60) → pacing 平均 70 → 4 星；
	// plot_advance → longArc 60 → 3 星；emotion_hook(90) → opening 5 星 + reader 5 星。
	got := JudgeToAuditScores(map[string]int{
		"word_count":   80,
		"plot_advance": 60,
		"emotion_hook": 90,
	})
	want := []JudgeAuditDimScore{
		{Key: "opening", Score: 5, Contributed: []string{"emotion_hook"}},
		{Key: "pacing", Score: 4, Contributed: []string{"word_count", "plot_advance"}},
		{Key: "longArc", Score: 3, Contributed: []string{"plot_advance"}},
		{Key: "reader", Score: 5, Contributed: []string{"emotion_hook"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JudgeToAuditScores = %+v, want %+v", got, want)
	}

	// 未覆盖维度（如 consistency 缺失）不出现；fit 永不出现。
	for _, s := range JudgeToAuditScores(map[string]int{"taboo": 100}) {
		if s.Key == "fit" || s.Key == "character" {
			t.Fatalf("不应出现维度 %s", s.Key)
		}
	}
}
