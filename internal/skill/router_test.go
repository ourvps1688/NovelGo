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

// TestRouteUserMessage_InternalOnlyRedirect #111 P1：internalOnly 命中只登记指引，
// 不进 Auto/Manual（不注入正文），且不阻断同轮其他 chat 可见技能被正常匹配。
func TestRouteUserMessage_InternalOnlyRedirect(t *testing.T) {
	internal := mkMeta("signability-fix", ModeManual, "修复")
	internal.InternalOnly = true
	all := []SkillMeta{
		internal,
		mkMeta("dialogue-subtext", ModeAuto, "修复"),
	}
	res := RouteUserMessage(all, "帮我修复一下这段", 3, 1, nil)
	if len(res.Auto) != 1 || res.Auto[0].Name != "dialogue-subtext" {
		t.Fatalf("chat 可见技能应仍能命中, got %+v", res.Auto)
	}
	if len(res.Manual) != 0 {
		t.Fatalf("internalOnly 不得进入 Manual, got %+v", res.Manual)
	}
	if len(res.Redirects) != 1 || res.Redirects[0].Name != "signability-fix" {
		t.Fatalf("internalOnly 应登记一条 Redirect, got %+v", res.Redirects)
	}
}

// TestRouteUserMessageSmart_InternalOnlyNotFallback #111 P1：Redirect 命中即视为
// 确定性命中，不再走 LLM 兜底；且 internalOnly 不进兜底候选、LLM 返回其名也不注入。
func TestRouteUserMessageSmart_InternalOnlyNotFallback(t *testing.T) {
	internal := mkMeta("signability-fix", ModeManual, "修复")
	internal.InternalOnly = true
	all := []SkillMeta{internal, mkMeta("scene-beats", ModeAuto, "场面")}

	fc := &fakeClassifier{name: "signability-fix", conf: 0.95}
	res := RouteUserMessageSmart(context.Background(), all, "帮我修复一下", 3, 1, nil, fc.fn)
	if fc.called {
		t.Fatal("Redirect 命中时不应再调用 LLM 兜底")
	}
	if res.Source != SourceCue || len(res.Redirects) != 1 {
		t.Fatalf("expected source=cue + 1 redirect, got %s / %+v", res.Source, res.Redirects)
	}

	fc2 := &fakeClassifier{name: "signability-fix", conf: 0.95}
	res2 := RouteUserMessageSmart(context.Background(), all, "随便聊点别的事情吧", 3, 1, nil, fc2.fn)
	if !fc2.called {
		t.Fatal("无 cue 命中时应允许走 LLM 兜底")
	}
	if len(res2.Auto) != 0 || len(res2.Manual) != 0 || len(res2.Redirects) != 0 {
		t.Fatalf("LLM 返回 internalOnly 技能名时不得注入, got %+v", res2)
	}
}
