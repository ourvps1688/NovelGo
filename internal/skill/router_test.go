package skill

import (
	"context"
	"testing"
)

func mkMeta(name, mode string, cues ...string) SkillMeta {
	return SkillMeta{
		Name:    name,
		Mode:    mode,
		UseWhen: []UseWhenEntry{{Intent: "intent-" + name, Cues: cues}},
	}
}

func TestRouteUserMessage_AutoMatch(t *testing.T) {
	all := []SkillMeta{
		mkMeta("worldbuilding-design", ModeAuto, "力量体系", "金手指", "世界观"),
		mkMeta("scene-beats", ModeAuto, "场景节拍", "张力"),
		mkMeta("next", ModeManual, "写下一章", "继续", "动笔"),
	}
	res := RouteUserMessage(all, "帮我设计一下力量体系", 2, 1, nil)
	if len(res.Auto) != 1 || res.Auto[0].Name != "worldbuilding-design" {
		t.Fatalf("expected worldbuilding-design auto match, got %+v", res.Auto)
	}
	if len(res.Manual) != 0 {
		t.Fatalf("expected no manual match, got %+v", res.Manual)
	}
}

func TestRouteUserMessage_ManualOrchestrator(t *testing.T) {
	all := []SkillMeta{
		mkMeta("worldbuilding-design", ModeAuto, "力量体系"),
		mkMeta("next", ModeManual, "写下一章", "继续", "动笔"),
	}
	res := RouteUserMessage(all, "继续写下一章", 2, 1, nil)
	if len(res.Manual) != 1 || res.Manual[0].Name != "next" {
		t.Fatalf("expected next manual match, got %+v", res.Manual)
	}
	if len(res.Auto) != 0 {
		t.Fatalf("expected no auto match, got %+v", res.Auto)
	}
}

func TestRouteUserMessage_MaxCap(t *testing.T) {
	all := []SkillMeta{
		mkMeta("a", ModeAuto, "升级"),
		mkMeta("b", ModeAuto, "升级"),
		mkMeta("c", ModeAuto, "修炼"),
	}
	res := RouteUserMessage(all, "主角要升级修炼了", 1, 1, nil)
	if len(res.Auto) != 1 {
		t.Fatalf("expected capped to 1 auto, got %d", len(res.Auto))
	}
}

func TestRouteUserMessage_Exclude(t *testing.T) {
	all := []SkillMeta{
		mkMeta("worldbuilding-design", ModeAuto, "力量体系"),
	}
	res := RouteUserMessage(all, "用 worldbuilding-design 帮我看下", 2, 1, map[string]bool{"worldbuilding-design": true})
	if len(res.Auto) != 0 {
		t.Fatalf("expected excluded skill to be skipped, got %+v", res.Auto)
	}
}

func TestRouteUserMessage_NoMatch(t *testing.T) {
	all := []SkillMeta{
		mkMeta("worldbuilding-design", ModeAuto, "力量体系"),
	}
	res := RouteUserMessage(all, "今天天气真好", 2, 1, nil)
	if len(res.Auto) != 0 || len(res.Manual) != 0 {
		t.Fatalf("expected no match, got auto=%v manual=%v", res.Auto, res.Manual)
	}
}

func TestRouteUserMessage_EmptyMessage(t *testing.T) {
	all := []SkillMeta{mkMeta("x", ModeAuto, "y")}
	res := RouteUserMessage(all, "   ", 2, 1, nil)
	if len(res.Auto) != 0 {
		t.Fatalf("expected no match on empty message, got %+v", res.Auto)
	}
}

// fakeClassifier 记录是否被调用，并按预设返回 (name, conf)。
type fakeClassifier struct {
	called bool
	name   string
	conf   float64
}

func (f *fakeClassifier) fn(_ context.Context, _ string, _ []IntentCandidate) (string, float64) {
	f.called = true
	return f.name, f.conf
}

func TestRouteUserMessageSmart_CuePriority(t *testing.T) {
	all := []SkillMeta{
		mkMeta("worldbuilding-design", ModeAuto, "力量体系"),
		mkMeta("scene-beats", ModeAuto, "场面"),
	}
	fc := &fakeClassifier{name: "scene-beats", conf: 0.9}
	res := RouteUserMessageSmart(context.Background(), all, "帮我设计一下力量体系", 1, 1, nil, fc.fn)
	if res.Source != SourceCue {
		t.Fatalf("expected source=cue, got %s", res.Source)
	}
	if len(res.Auto) != 1 || res.Auto[0].Name != "worldbuilding-design" {
		t.Fatalf("expected cue-matched worldbuilding-design, got %+v", res.Auto)
	}
	if fc.called {
		t.Fatalf("classifier must NOT be called when cue already matched")
	}
}

func TestRouteUserMessageSmart_LLMFallback(t *testing.T) {
	all := []SkillMeta{
		mkMeta("worldbuilding-design", ModeAuto, "力量体系"),
		mkMeta("scene-beats", ModeAuto, "场面"),
		mkMeta("next", ModeManual, "写下一章"),
	}
	fc := &fakeClassifier{name: "scene-beats", conf: 0.92}
	res := RouteUserMessageSmart(context.Background(), all, "我想聊聊今晚吃什么", 1, 1, nil, fc.fn)
	if !fc.called {
		t.Fatalf("classifier should be called when no cue matches")
	}
	if res.Source != SourceLLM {
		t.Fatalf("expected source=llm, got %s", res.Source)
	}
	if len(res.Auto) != 1 || res.Auto[0].Name != "scene-beats" {
		t.Fatalf("expected llm-matched scene-beats, got %+v", res.Auto)
	}
}

func TestRouteUserMessageSmart_LLMBelowThreshold(t *testing.T) {
	all := []SkillMeta{mkMeta("scene-beats", ModeAuto, "场面")}
	fc := &fakeClassifier{name: "scene-beats", conf: 0.2}
	res := RouteUserMessageSmart(context.Background(), all, "今晚吃什么好呢", 1, 1, nil, fc.fn)
	if res.Source != SourceNone {
		t.Fatalf("expected source=none, got %s", res.Source)
	}
	if len(res.Auto) != 0 {
		t.Fatalf("expected no injection below threshold, got %+v", res.Auto)
	}
}

func TestRouteUserMessageSmart_NoClassifier(t *testing.T) {
	all := []SkillMeta{mkMeta("scene-beats", ModeAuto, "场面")}
	res := RouteUserMessageSmart(context.Background(), all, "今晚吃什么好呢", 1, 1, nil, nil)
	if res.Source != SourceNone {
		t.Fatalf("expected source=none, got %s", res.Source)
	}
	if len(res.Auto) != 0 || len(res.Manual) != 0 {
		t.Fatalf("expected empty result with nil classifier, got auto=%v manual=%v", res.Auto, res.Manual)
	}
}

func TestRouteUserMessageSmart_ShortMessageSkipsLLM(t *testing.T) {
	all := []SkillMeta{mkMeta("scene-beats", ModeAuto, "场面")}
	fc := &fakeClassifier{name: "scene-beats", conf: 0.9}
	res := RouteUserMessageSmart(context.Background(), all, "你好", 1, 1, nil, fc.fn)
	if fc.called {
		t.Fatalf("classifier must be skipped on short/greeting message")
	}
	if res.Source != SourceNone {
		t.Fatalf("expected source=none, got %s", res.Source)
	}
}
