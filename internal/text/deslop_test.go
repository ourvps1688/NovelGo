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
		"她声音不大，却带着平静的力量。",
		"命运终于露出獠牙。",
		"属于他的反击才刚刚开始。",
		"反击才刚刚开始。",
		"他不知道的是，更大的风暴即将来临。",
		"取而代之的是冰冷的沉默。",
		"他眼中闪过一丝悲伤。",
		"她嘴角勾起一抹冷笑。",
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

// 验证常见自然用法不被误伤（避免对真实网文过度拒稿）。
// 这些词在真人语料中常见，命中一次就拒整章会让 LLM 重写整章反复命中、
// 连续失败 10 次中断对话（2026-10 实测），故全部降为 advisory。
func TestBlockingIssues_NaturalUsageNotFlagged(t *testing.T) {
	natural := []string{
		"他不由得笑了起来。",        // 自然高频词，仅 advisory
		"她不禁红了眼眶。",         // 同上
		"他眉头微皱，看了她一眼。",     // milder body，仅 advisory
		"目光锐利地扫过人群。",       // 同上
		"事情并不是你想的那样。",      // "不是A" 无 "而是"，非翻转结构
		"他不是医生。",           // 单纯否定，无翻转
		"多年以后，她才明白那句话的分量。", // 合法叙事手法，仅 advisory
		"从这一刻开始，他要为自己而活。",  // 立誓句式，仅 advisory
		"他深吸一口气，迈步上前。",     // 自然动作，仅 advisory 密度提示
		"这是毋庸置疑的事实。",       // 判断套词，仅 advisory
		"毫无疑问，她是最强的。",      // 台词内合法
		"他心中暗道不好。",         // 网文自然内心戏引导，仅 advisory 密度
		"他的人生才刚刚开始。",       // 非「反击/故事/一切」主语，不再硬拒
		"殊不知，这背后另有隐情。",     // 古风/历史真人常用连词，降 advisory，不再硬拒
		"乌云压顶，仿佛预示着一场风暴。",  // 同上，自然用法不误伤
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

	// 从 blocking 降级的合法常用语：advisory 提示但不硬拒
	if !hasRule(AdvisoryIssues("多年以后，她回到了故土。"), "future-flash") {
		t.Error("expected future-flash advisory for 多年以后")
	}
	if !hasRule(AdvisoryIssues("从这一刻开始，他为自己而活。"), "vow-opening") {
		t.Error("expected vow-opening advisory for 从这一刻开始")
	}
	if !hasRule(AdvisoryIssues("这是毋庸置疑的事实。"), "judgment-cliche") {
		t.Error("expected judgment-cliche advisory for 判断套词")
	}

	// 从 blocking 降级的软规则：advisory 提示但不硬拒
	if !hasRule(AdvisoryIssues("殊不知，这背后另有隐情。"), "god-view-soft") {
		t.Error("expected god-view-soft advisory for 殊不知")
	}
	if !hasRule(AdvisoryIssues("乌云压顶，仿佛预示着一场风暴。"), "god-view-soft") {
		t.Error("expected god-view-soft advisory for 仿佛预示着")
	}
	if !hasRule(AdvisoryIssues("这一刻，他终于明白。"), "epiphany-soft") {
		t.Error("expected epiphany-soft advisory for 这一刻明白")
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
	issues := BlockingIssues("他心中涌起一股寒意。他停下来——看着远方。")
	msg := BlockingErrorMessage(issues)
	if !strings.Contains(msg, "破折号") {
		t.Errorf("error message missing dash hint: %q", msg)
	}
	if !strings.Contains(msg, "AI 味硬伤") {
		t.Errorf("error message missing header: %q", msg)
	}
	// 定向修补引导：只改命中句，不要重写整章
	if !strings.Contains(msg, "不要重写整章") {
		t.Errorf("error message missing targeted-patch guidance: %q", msg)
	}
	// 命中项附带所在句上下文，便于 LLM 定位
	if !strings.Contains(msg, "所在句") {
		t.Errorf("error message missing context locator: %q", msg)
	}
	if !strings.Contains(msg, "他停下来——看着远方") {
		t.Errorf("error message missing sentence context: %q", msg)
	}
}

func TestBlockingIssues_ContextLocatesSentence(t *testing.T) {
	issues := BlockingIssues("夜色沉了下来。命运的齿轮开始转动。窗外有雨。")
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Context == "" || !strings.Contains(issues[0].Context, "命运的齿轮") {
		t.Errorf("context should contain the hit sentence: %q", issues[0].Context)
	}
}

// TestBlockingIssues_ForGenreExemptsWebNovel 验证题材感知豁免：
// 在网文题材（genre 非空）下，fate-cliche 与 指节泛白 这两个真人常用 idiom 不再硬拒；
// 但未选题材（genre 空）时仍保守硬拒；且其他规则（破折号/模板身体）不受影响始终拦截。
func TestBlockingIssues_ForGenreExemptsWebNovel(t *testing.T) {
	// 豁免：网文题材下命中即放行
	if got := len(BlockingIssuesForGenre("命运的齿轮开始转动。", "玄幻")); got != 0 {
		t.Errorf("fate-cliche should be exempt in web-novel genre, got %d issues", got)
	}
	if got := len(BlockingIssuesForGenre("他指节泛白，攥紧了衣角。", "言情")); got != 0 {
		t.Errorf("指节泛白 should be exempt in web-novel genre, got %d issues", got)
	}
	// 保底：未选题材仍硬拒
	if got := len(BlockingIssuesForGenre("命运的齿轮开始转动。", "")); got != 1 {
		t.Errorf("fate-cliche should block when genre empty, got %d issues", got)
	}
	if got := len(BlockingIssuesForGenre("他指节泛白。", "")); got != 1 {
		t.Errorf("指节泛白 should block when genre empty, got %d issues", got)
	}
	// 其他规则不受豁免影响，始终拦截
	if got := len(BlockingIssuesForGenre("他停下来——看着远方。", "玄幻")); got != 1 {
		t.Errorf("em-dash should always block, got %d issues", got)
	}
	if got := len(BlockingIssuesForGenre("他眼中闪过一丝悲伤。", "玄幻")); got != 1 {
		t.Errorf("stock-body(眼中闪过) should always block, got %d issues", got)
	}
}
