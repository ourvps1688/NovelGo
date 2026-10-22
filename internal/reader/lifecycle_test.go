package reader

import (
	"strings"
	"testing"
)

func testOrderOf(chapterID int64) int {
	// 章 1/2/3 阅读序 1/2/3；其余返回 0（无法定位）。
	switch chapterID {
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	}
	return 0
}

func TestLifecycleEventMapping(t *testing.T) {
	// 起点事件（写 planted）。
	for _, tc := range []struct {
		typ  string
		want LifecycleEvent
	}{
		{TypeKnown, EventReveal},
		{TypeSuspense, EventRaise},
		{TypeMisconception, EventForm},
	} {
		got, ok := StartEvent(tc.typ)
		if !ok || got != tc.want {
			t.Errorf("StartEvent(%q) = %q,%v want %q", tc.typ, got, ok, tc.want)
		}
	}
	// 终点事件（写 revealed）：known 无终点。
	if _, ok := EndEvent(TypeKnown); ok {
		t.Error("EndEvent(known) 应为无终点事件（单点生命周期）")
	}
	for _, tc := range []struct {
		typ  string
		want LifecycleEvent
	}{
		{TypeSuspense, EventPayoff},
		{TypeMisconception, EventCorrect},
	} {
		got, ok := EndEvent(tc.typ)
		if !ok || got != tc.want {
			t.Errorf("EndEvent(%q) = %q,%v want %q", tc.typ, got, ok, tc.want)
		}
	}
	if _, ok := StartEvent("bogus"); ok {
		t.Error("StartEvent(非法类型) 应返回 ok=false")
	}
}

func TestValidateLifecycle_Valid(t *testing.T) {
	planted, revealed := int64(1), int64(2)
	same := int64(1)
	cases := []ReaderPerspective{
		{Type: TypeKnown, Content: "读者知道主角身世", PlantedChapterID: &planted},
		{Type: TypeSuspense, Content: "谁是幕后黑手", PlantedChapterID: &planted},
		{Type: TypeSuspense, Content: "已回收悬念", PlantedChapterID: &planted, RevealedChapterID: &revealed},
		// quick reveal 语义：回收章=埋设章（同章埋同章收）。
		{Type: TypeSuspense, Content: "同章回收", PlantedChapterID: &same, RevealedChapterID: &same},
		{Type: TypeMisconception, Content: "读者以为导师是好人", PlantedChapterID: &planted, RelatedTruth: "导师是内奸"},
		{Type: TypeMisconception, Content: "已纠正误知", PlantedChapterID: &planted, RevealedChapterID: &revealed},
	}
	for _, e := range cases {
		if err := ValidateLifecycle(e, testOrderOf); err != nil {
			t.Errorf("ValidateLifecycle(%+v) = %v, want nil", e, err)
		}
	}
}

func TestValidateLifecycle_Rejects(t *testing.T) {
	planted, revealed := int64(2), int64(1) // revealed 阅读序早于 planted
	valid := int64(1)

	cases := []struct {
		name    string
		entry   ReaderPerspective
		wantSub string
	}{
		{
			name:    "类型非法",
			entry:   ReaderPerspective{Type: "bogus", Content: "x", PlantedChapterID: &valid},
			wantSub: "类型必须是",
		},
		{
			name:    "内容为空",
			entry:   ReaderPerspective{Type: TypeSuspense, PlantedChapterID: &valid},
			wantSub: "内容不能为空",
		},
		{
			name:    "种下章缺失",
			entry:   ReaderPerspective{Type: TypeSuspense, Content: "x"},
			wantSub: "种下章节不能为空",
		},
		{
			name:    "known 单点不得设回收章",
			entry:   ReaderPerspective{Type: TypeKnown, Content: "x", PlantedChapterID: &valid, RevealedChapterID: &valid},
			wantSub: "单点生命周期",
		},
		{
			name:    "回收早于埋设",
			entry:   ReaderPerspective{Type: TypeSuspense, Content: "x", PlantedChapterID: &planted, RevealedChapterID: &revealed},
			wantSub: "回收章不能早于埋设章",
		},
		{
			name:    "纠正早于形成",
			entry:   ReaderPerspective{Type: TypeMisconception, Content: "x", PlantedChapterID: &planted, RevealedChapterID: &revealed},
			wantSub: "纠正章不能早于形成章",
		},
	}
	for _, tc := range cases {
		err := ValidateLifecycle(tc.entry, testOrderOf)
		if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
			t.Errorf("%s: ValidateLifecycle = %v, want error containing %q", tc.name, err, tc.wantSub)
		}
	}
}

func TestValidateLifecycle_OrderOfEdge(t *testing.T) {
	planted, revealed := int64(2), int64(1)
	e := ReaderPerspective{Type: TypeSuspense, Content: "x", PlantedChapterID: &planted, RevealedChapterID: &revealed}

	// orderOf 为 nil：跳过时序校验。
	if err := ValidateLifecycle(e, nil); err != nil {
		t.Errorf("orderOf=nil 应跳过时序校验, got %v", err)
	}
	// 章节无法定位（orderOf 返回 0）：跳过比较。
	if err := ValidateLifecycle(e, func(int64) int { return 0 }); err != nil {
		t.Errorf("章节无法定位应跳过时序比较, got %v", err)
	}
}

func TestValidateLifecycleUpdate_UnchangedLifecycleSkips(t *testing.T) {
	// 存量脏数据：known 带回收章（旧版未校验时写入）。
	planted, revealed := int64(1), int64(2)
	legacy := ReaderPerspective{Type: TypeKnown, Content: "旧文案", PlantedChapterID: &planted, RevealedChapterID: &revealed}

	// 只改文案（lifecycle 三字段不变）：放行，不被历史数据卡住。
	edited := legacy
	edited.Content = "新文案"
	if err := ValidateLifecycleUpdate(legacy, edited, testOrderOf); err != nil {
		t.Errorf("只改文案不应触发 lifecycle 校验, got %v", err)
	}

	// 动了 revealed（nil→set）：全量校验，known+revealed 拒绝。
	set := ReaderPerspective{Type: TypeKnown, Content: "x", PlantedChapterID: &planted, RevealedChapterID: &revealed}
	unset := ReaderPerspective{Type: TypeKnown, Content: "x", PlantedChapterID: &planted}
	if err := ValidateLifecycleUpdate(unset, set, testOrderOf); err == nil {
		t.Error("known 新增回收章应被拒绝")
	}

	// 合法变更（suspense 回收）：放行。
	oldSusp := ReaderPerspective{Type: TypeSuspense, Content: "x", PlantedChapterID: &planted}
	newSusp := ReaderPerspective{Type: TypeSuspense, Content: "x", PlantedChapterID: &planted, RevealedChapterID: &revealed}
	if err := ValidateLifecycleUpdate(oldSusp, newSusp, testOrderOf); err != nil {
		t.Errorf("suspense 回收写入应放行, got %v", err)
	}
}
