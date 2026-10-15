package skill

import "testing"

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
