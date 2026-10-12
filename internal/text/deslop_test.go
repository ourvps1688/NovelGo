package text

import (
	"strings"
	"testing"
)

func TestBlockingIssues_EmDash(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"double em-dash", "他停下来——看着远方。", true},
		{"triple em-dash", "他停下来———看着远方。", true},
		{"u2015 bar", "他停下来――看着远方。", true},
		{"double hyphen", "他停下来--看着远方。", true},
		{"single em-dash allowed", "编号 3—5 的区间。", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := len(BlockingIssues(c.text)) > 0
			if got != c.want {
				t.Fatalf("BlockingIssues(%q) hit=%v want=%v", c.text, got, c.want)
			}
		})
	}
}

func TestBlockingIssues_SpecificPatterns(t *testing.T) {
	blocking := []string{
		"仿佛能穿透一切一般，他笑了。",
		"她声音不大，却带着不容置疑的力量。",
		"命运终于露出獠牙。",
		"属于他的反击才刚刚开始。",
		"这一刻，他终于明白。",
		"他不知道的是，更大的风暴即将来临。",
		"取而代之的是冰冷的沉默。",
		"他眼中闪过一丝悲伤。",
		"她嘴角勾起一抹冷笑。",
		"他深吸一口气，迈步上前。",
		"这是不容置疑的事实。",
	}
	for _, s := range blocking {
		if len(BlockingIssues(s)) == 0 {
			t.Errorf("expected blocking hit for: %q", s)
		}
	}
}

func TestBlockingIssues_AllowsCleanProse(t *testing.T) {
	clean := []string{
		"他把信折好，塞进外套内袋。",
		"门没锁，风一推就开了。",
		"她笑了，把茶递过来。",
		"老周说，明天还来。",
		"雨从下午下到天黑。",
		"编号 3—5 的区间也符合条件。",
	}
	for _, s := range clean {
		if issues := BlockingIssues(s); len(issues) > 0 {
			t.Errorf("clean prose wrongly flagged %q: %+v", s, issues)
		}
	}
}

// 验证常见自然用法不被误伤（避免对真实网文过度拒稿）
func TestBlockingIssues_NaturalUsageNotFlagged(t *testing.T) {
	natural := []string{
		"他不由得笑了起来。",    // 自然高频词，仅 advisory
		"她不禁红了眼眶。",     // 同上
		"他眉头微皱，看了她一眼。", //  milder body，仅 advisory
		"目光锐利地扫过人群。",   // 同上
		"事情并不是你想的那样。",  // "不是A" 无 "而是"，非翻转结构
		"他不是医生。",       // 单纯否定，无翻转
	}
	for _, s := range natural {
		if issues := BlockingIssues(s); len(issues) > 0 {
			t.Errorf("natural usage wrongly flagged %q: %+v", s, issues)
		}
	}
}

func TestCountEmDashes(t *testing.T) {
	if n := CountEmDashes("a——b"); n != 1 {
		t.Errorf("double dash = %d, want 1", n)
	}
	if n := CountEmDashes("a———b"); n != 1 {
		t.Errorf("triple dash = %d, want 1", n)
	}
	if n := CountEmDashes("a―b"); n != 1 {
		t.Errorf("u2015 = %d, want 1", n)
	}
	if n := CountEmDashes("区间 3—5"); n != 0 {
		t.Errorf("single dash = %d, want 0", n)
	}
}

func TestAdvisoryIssues_FlagsDensity(t *testing.T) {
	// 微动作复现 >=3
	body := "他深吸一口气。她不禁后退。他不由得停住。指尖轻叩桌面。"
	if !hasRule(AdvisoryIssues(body), "stock-reaction-tic") {
		t.Errorf("expected stock-reaction-tic for %q", body)
	}

	// 套词密度 >=4
	cliche := "仿佛一切都晚了。一丝慌乱爬上心头。一抹笑意浮现。几分不安涌来。些许希望重燃。"
	if !hasRule(AdvisoryIssues(cliche), "cliche-density-tic") {
		t.Errorf("expected cliche-density-tic for %q", cliche)
	}

	// 否定翻转
	flip := "他不是冷漠，而是绝望。"
	if !hasRule(AdvisoryIssues(flip), "negation-flip") {
		t.Errorf("expected negation-flip for %q", flip)
	}

	// tell-not-show
	tell := "他知道这一切都来不及了。"
	if !hasRule(AdvisoryIssues(tell), "tell-not-show") {
		t.Errorf("expected tell-not-show for %q", tell)
	}
}

func TestAdvisoryIssues_CleanNotFlagged(t *testing.T) {
	clean := "他把信折好。门没锁。她笑了。雨从下午下到天黑。"
	if issues := AdvisoryIssues(clean); len(issues) > 0 {
		t.Errorf("clean prose flagged: %+v", issues)
	}
}

func hasRule(issues []DeslopIssue, rule string) bool {
	for _, it := range issues {
		if it.Rule == rule {
			return true
		}
	}
	return false
}

func TestBlockingErrorMessage_ContainsHint(t *testing.T) {
	issues := BlockingIssues("他停下来——看着远方。")
	msg := BlockingErrorMessage(issues)
	if !strings.Contains(msg, "破折号") {
		t.Errorf("error message missing dash hint: %q", msg)
	}
	if !strings.Contains(msg, "AI 味硬伤") {
		t.Errorf("error message missing header: %q", msg)
	}
}
