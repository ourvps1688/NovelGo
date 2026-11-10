package skill

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 技能装配契约（#111 P0）的开发期静态断言：健康检查失败 = 构建失败。
// 这是代码/文档一致性闸，不是运行时质量闸门；不改变「四 kind 独立启动 → 工单 →
// 人工确认 → 修复 → 显式复验」的质量保障唯一规范路径。

// mountOwners 是 mounts 枚举 → 消费点源码的静态映射。新增枚举必须同时给出 owner，
// 保证「声明了挂载点」不等于「真的有人消费」。
var mountOwners = map[string]string{
	"chat_intent":       "app/slash_command.go",
	"writing_bundle":    "app/slash_command.go",
	"chapter_ci":        "internal/mcp_tools/chapter_ci.go",
	"state_sync":        "internal/mcp_tools/rw_tools.go",
	"pipeline_dispatch": "internal/pipeline/provider_trip.go",
	"rewrite_method":    "internal/pipeline/rewrite_skills.go",
	"pitch_flow":        "frontend/src/components/skill/BookPitchWorkflowView.tsx",
	"blueprint_audit":   "internal/pipeline/service.go",
	"feedback":          "internal/mcp_tools/feedback_tools.go",
	"slash_only":        "app/slash_command.go",
	"always":            "internal/agentcfg/skill_catalog.go",
}

// #111 P3：catalog_read 已从合法 mounts 枚举中退役——技能目录降级为说明性，
// 不再承担正文通道职责；常驻 always 仅保留为 .enabled.json 兼容层，同样不得
// 作为内置技能的可用性挂载点（见 TestWorkflowAutoArmContract 第 3 条）。

// #111 P2：P0 的 legacyPlaceholders 临时豁免已清空——memory / review 已补齐
// useWhen 与 ≥5 行正文，全量技能一律过同一套硬断言，不留永久后门。

// agentTypeCollisions 显式登记技能名与 agentcfg.AgentType 子 Agent 枚举的重名。
// 二者同名异物（ReviewAgent / MemoryAgent 不是这两个技能），必须显式解耦。
var agentTypeCollisions = map[string]bool{"memory": true, "review": true}

// knownAgentTypes 是 agentcfg 子 Agent 类型枚举的镜像。internal/skill 不能 import
// agentcfg（循环依赖），新增子 Agent 类型且与技能名重名时须同步登记。
var knownAgentTypes = map[string]bool{
	"main": true, "memory": true, "review": true, "chapterci": true, "statesync": true,
}

func loadBuiltinForHealth(t *testing.T) ([]Skill, map[string]Skill) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	skills, err := LoadBuiltinSkills(logger)
	if err != nil {
		t.Fatalf("加载内置技能失败: %v", err)
	}
	if len(skills) != 27 {
		t.Fatalf("内置技能数量应为 27，实际 %d（新增/删除技能须同步 manifest 与健康检查）", len(skills))
	}
	byName := make(map[string]Skill, len(skills))
	for _, sk := range skills {
		if _, dup := byName[sk.Name]; dup {
			t.Errorf("技能名重复: %s", sk.Name)
		}
		byName[sk.Name] = sk
	}
	return skills, byName
}

func nonEmptyBodyLines(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// readHealthSource 读取静态断言所需的源码，并把 CRLF 归一为 LF。
// GitHub Actions 的 windows-latest 默认 core.autocrlf=true，checkout 后的 .go 文件
// 是 CRLF；健康检查按 "\n}\n" / 行边界截取声明窗口时必须免疫行尾差异，否则同一份
// 代码会 ubuntu/macos 全绿、windows 独红（#111 P2 的 TestP2SkillWiringZeroExemption
// 曾在 #420/#421 连续 3 次重跑仍失败）。仓库内 .go 行尾并不统一，故不靠
// .gitattributes 全量强制，只在读取侧归一，避免制造大规模行尾变更。
func readHealthSource(t *testing.T, parts ...string) string {
	t.Helper()
	name := filepath.Join(parts...)
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	return normalizeHealthSource(string(b))
}

func normalizeHealthSource(src string) string {
	return strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "")
}

// healthVarBlock 截取 var 声明到其闭合 "}" 之间的片段。结尾标记用 "\n}\n" 而非
// "\n}"：[]struct{...}{ 那行本身以 "\n}{" 开头，用 "\n}" 会把窗口截断在字段定义处，
// 提取不到技能名。调用方必须先经 normalizeHealthSource 归一。
func healthVarBlock(src, marker string) (string, bool) {
	start := strings.Index(src, marker)
	if start < 0 {
		return "", false
	}
	rest := src[start:]
	end := strings.Index(rest, "\n}\n")
	if end <= 0 {
		return "", false
	}
	return rest[:end], true
}

// TestHealthSourceLineEndingTolerance 锁住 Windows CI 的 CRLF 检出不会让静态断言
// 误报：#111 P2 的两个 "\n}\n" 窗口曾在 CRLF 源码下失配，且本地 LF 环境测不出来。
func TestHealthSourceLineEndingTolerance(t *testing.T) {
	const lfSample = "package sample\n\n" +
		"var stateSyncSkills = []struct{ name, body string }{\n" +
		"\t{\"collect\", \"body\"},\n" +
		"\t{\"memory\", \"body\"},\n" +
		"}\n"
	crlfSample := strings.ReplaceAll(lfSample, "\n", "\r\n")
	if got := normalizeHealthSource(crlfSample); got != lfSample {
		t.Fatalf("CRLF 归一结果不等于 LF 原文:\ngot  %q\nwant %q", got, lfSample)
	}
	if _, ok := healthVarBlock(crlfSample, "var stateSyncSkills"); ok {
		t.Fatal("未归一的 CRLF 源码不应侥幸通过窗口提取（回归样本失效）")
	}
	block, ok := healthVarBlock(normalizeHealthSource(crlfSample), "var stateSyncSkills")
	if !ok {
		t.Fatal("归一后的 CRLF 源码应能提取 stateSyncSkills 声明窗口")
	}
	names := regexp.MustCompile(`\{"([a-z0-9-]+)",`).FindAllStringSubmatch(block, -1)
	if len(names) != 2 || names[0][1] != "collect" || names[1][1] != "memory" {
		t.Errorf("CRLF 源码窗口内技能名提取错误: %v", names)
	}
}

// TestBuiltinSkillManifestHealth 锁住 manifest 的最小可用契约：
// 零挂载点即构建失败、枚举有主、预算为正、internalOnly 自洽、占位稿不放行。
func TestBuiltinSkillManifestHealth(t *testing.T) {
	skills, byName := loadBuiltinForHealth(t)
	for name := range agentTypeCollisions {
		if _, ok := byName[name]; !ok {
			t.Errorf("agentTypeCollisions 含不存在的技能: %s", name)
		}
	}

	for _, sk := range skills {
		t.Run(sk.Name, func(t *testing.T) {
			if !sk.Deprecated && len(sk.Mounts) == 0 {
				t.Errorf("技能 %s 未声明 mounts 且未标 deprecated —— 零挂载点不允许上线", sk.Name)
			}
			for _, mount := range sk.Mounts {
				owner, ok := mountOwners[mount]
				if !ok {
					t.Errorf("技能 %s 声明了未知 mount %q（须在 mountOwners 登记消费点）", sk.Name, mount)
					continue
				}
				if _, err := os.Stat(filepath.Join("..", "..", owner)); err != nil {
					t.Errorf("mount %q 的 owner 文件不存在: %s", mount, owner)
				}
			}
			if sk.Budget <= 0 {
				t.Errorf("技能 %s 的 budget 必须为正（声明正文注入 rune 上限）", sk.Name)
			}
			if sk.InternalOnly {
				for _, mount := range sk.Mounts {
					if mount == "chat_intent" || mount == "writing_bundle" {
						t.Errorf("internalOnly 技能 %s 不得声明 chat 侧挂载点 %q", sk.Name, mount)
					}
				}
			}
			hasUseWhen := len(sk.UseWhen) > 0
			bodyOK := nonEmptyBodyLines(sk.Content) >= 5
			if !hasUseWhen {
				t.Errorf("技能 %s 缺 useWhen（cue 路由不可达）", sk.Name)
			}
			if !bodyOK {
				t.Errorf("技能 %s 正文非空行少于 5（占位稿须列入 legacyPlaceholders）", sk.Name)
			}
			if knownAgentTypes[sk.Name] && !agentTypeCollisions[sk.Name] {
				t.Errorf("技能 %s 与子 Agent 类型枚举重名，须在 agentTypeCollisions 显式登记", sk.Name)
			}
		})
	}
}

// TestManifestFieldsParsed 验证 yaml tag 真的被解析（字段名大小写/驼峰易写错）。
func TestManifestFieldsParsed(t *testing.T) {
	_, byName := loadBuiltinForHealth(t)
	cases := []struct {
		name         string
		mount        string
		internalOnly bool
		budget       int
	}{
		{"next", "chat_intent", false, 2000},
		{"collect", "state_sync", true, 1200},
		{"memory", "state_sync", true, 1200},
		{"signability-fix", "pipeline_dispatch", true, 4000},
		{"reader-immersion", "writing_bundle", false, 4000},
		{"chapter-ci", "chapter_ci", true, 4000},
	}
	for _, c := range cases {
		sk, ok := byName[c.name]
		if !ok {
			t.Fatalf("缺技能 %s", c.name)
		}
		if !containsString(sk.Mounts, c.mount) {
			t.Errorf("%s 应声明 mount %s，实际 %v", c.name, c.mount, sk.Mounts)
		}
		if sk.InternalOnly != c.internalOnly {
			t.Errorf("%s internalOnly=%v，期望 %v", c.name, sk.InternalOnly, c.internalOnly)
		}
		if sk.Budget != c.budget {
			t.Errorf("%s budget=%d，期望 %d", c.name, sk.Budget, c.budget)
		}
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestHardcodedSkillListsDeclareMatchingMount 校验 5 处写死名单里的技能名确实存在，
// 且声明了对应 mount —— 不合并名单（各有业务语义与契约测试），先做到单源可校验。
func TestHardcodedSkillListsDeclareMatchingMount(t *testing.T) {
	_, byName := loadBuiltinForHealth(t)

	slash := readHealthSource(t, "..", "..", "app", "slash_command.go")
	decl := regexp.MustCompile(`writingBundleSkills = \[\]string\{([^}]*)\}`).FindStringSubmatch(slash)
	if len(decl) < 2 {
		t.Fatal("未找到 writingBundleSkills 声明（写章技法包单源）")
	}
	for _, m := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(decl[1], -1) {
		sk, ok := byName[m[1]]
		if !ok {
			t.Errorf("writingBundleSkills 含不存在的技能 %s", m[1])
			continue
		}
		if !containsString(sk.Mounts, "writing_bundle") {
			t.Errorf("写章技能 %s 未声明 writing_bundle mount", m[1])
		}
		if sk.InternalOnly {
			t.Errorf("internalOnly 技能 %s 不得进入写章技法包", m[1])
		}
	}

	src := readHealthSource(t, "..", "pipeline", "rewrite_skills.go")
	start := strings.Index(src, "var auditDimSkillRoutes")
	end := strings.Index(src, "// lowAuditDims")
	if start < 0 || end <= start {
		t.Fatal("未找到两张重写方法论路由表（映射单源结构已变更？）")
	}
	// 只在路由表区间内提取技能名，避免把 import 路径等无关字符串算进来。
	routes := src[start:end]
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z][a-z0-9-]{2,})"`).FindAllStringSubmatch(routes, -1) {
		sk, ok := byName[m[1]]
		if !ok || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		if !containsString(sk.Mounts, "rewrite_method") {
			t.Errorf("重写方法论技能 %s 未声明 rewrite_method mount", m[1])
		}
	}
	if len(seen) == 0 {
		t.Fatal("未能从 rewrite_skills.go 提取到任何技能名（路由表结构已变更？）")
	}

	// #111 P1：写章条件化技法单源。只提取 []string{...} 内的技能名（忽略条件 id），
	// 逐个校验存在性、writing_bundle mount 与非 internalOnly，防止条件表变成摆设。
	condSrc := readHealthSource(t, "..", "..", "app", "writing_skill_conditions.go")
	condStart := strings.Index(condSrc, "var writingSkillConditions")
	condEnd := strings.Index(condSrc, "// writingChapterContext")
	if condStart < 0 || condEnd <= condStart {
		t.Fatal("未找到 writingSkillConditions 声明（条件化技法单源）")
	}
	condNames := 0
	for _, group := range regexp.MustCompile(`\[\]string\{([^}]*)\}`).FindAllStringSubmatch(condSrc[condStart:condEnd], -1) {
		for _, m := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(group[1], -1) {
			condNames++
			sk, ok := byName[m[1]]
			if !ok {
				t.Errorf("writingSkillConditions 含不存在的技能 %s", m[1])
				continue
			}
			if !containsString(sk.Mounts, "writing_bundle") {
				t.Errorf("条件技法 %s 未声明 writing_bundle mount", m[1])
			}
			if sk.InternalOnly {
				t.Errorf("internalOnly 技能 %s 不得进入写章条件表", m[1])
			}
		}
	}
	if condNames == 0 {
		t.Fatal("未能从 writingSkillConditions 提取到任何技能名（结构已变更？）")
	}

	// #111 P1：章 CI 写后自检单源，逐个校验 chapter_ci mount 与非 internalOnly。
	ci := readHealthSource(t, "..", "mcp_tools", "chapter_ci.go")
	ciDecl := regexp.MustCompile(`chapterCISelfCheckSkills = \[\]string\{([^}]*)\}`).FindStringSubmatch(ci)
	if len(ciDecl) < 2 {
		t.Fatal("未找到 chapterCISelfCheckSkills 声明（写后自检单源）")
	}
	ciNames := 0
	for _, m := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(ciDecl[1], -1) {
		ciNames++
		sk, ok := byName[m[1]]
		if !ok {
			t.Errorf("chapterCISelfCheckSkills 含不存在的技能 %s", m[1])
			continue
		}
		if !containsString(sk.Mounts, "chapter_ci") {
			t.Errorf("写后自检技能 %s 未声明 chapter_ci mount", m[1])
		}
		if sk.InternalOnly {
			t.Errorf("internalOnly 技能 %s 不得进入写后自检表", m[1])
		}
	}
	if ciNames == 0 {
		t.Fatal("未能从 chapterCISelfCheckSkills 提取到任何技能名（结构已变更？）")
	}
}

// TestP2SkillWiringZeroExemption 锁住 #111 P2 的两条收口：占位稿豁免已清空、
// 同名异物技能真正接上子 Agent 调度，且新增的 state_sync / slash_only 挂点有单源校验。
func TestP2SkillWiringZeroExemption(t *testing.T) {
	skills, byName := loadBuiltinForHealth(t)
	for _, sk := range skills {
		if len(sk.UseWhen) == 0 {
			t.Errorf("零豁免：技能 %s 仍缺 useWhen", sk.Name)
		}
		if nonEmptyBodyLines(sk.Content) < 5 {
			t.Errorf("零豁免：技能 %s 正文非空行少于 5（占位稿不得再放行）", sk.Name)
		}
	}

	// statesync 前置技能单源：每个技能必须存在且声明 state_sync mount。
	rw := readHealthSource(t, "..", "mcp_tools", "rw_tools.go")
	rwBlock, ok := healthVarBlock(rw, "var stateSyncSkills")
	if !ok {
		t.Fatal("未找到 stateSyncSkills 单源声明（结构已变更？）")
	}
	stateNames := 0
	for _, m := range regexp.MustCompile(`\{"([a-z0-9-]+)",`).FindAllStringSubmatch(rwBlock, -1) {
		stateNames++
		sk, ok := byName[m[1]]
		if !ok {
			t.Errorf("stateSyncSkills 含不存在的技能 %s", m[1])
			continue
		}
		if !containsString(sk.Mounts, "state_sync") {
			t.Errorf("statesync 技能 %s 未声明 state_sync mount", m[1])
		}
	}
	if stateNames == 0 {
		t.Fatal("未能从 stateSyncSkills 提取到任何技能名")
	}

	// slash 确定性调度单源：同名异物必须显式登记，且技能声明 slash_only mount。
	sl := readHealthSource(t, "..", "..", "app", "slash_command.go")
	slBlock, ok := healthVarBlock(sl, "var slashSubAgentDispatch")
	if !ok {
		t.Fatal("未找到 slashSubAgentDispatch 声明（/memory、/review 调度已失联？）")
	}
	dispatch := map[string]bool{}
	for _, m := range regexp.MustCompile(`"([a-z0-9-]+)":\s*"[a-z0-9-]+"`).FindAllStringSubmatch(slBlock, -1) {
		dispatch[m[1]] = true
		sk, ok := byName[m[1]]
		if !ok {
			t.Errorf("slashSubAgentDispatch 含不存在的技能 %s", m[1])
			continue
		}
		if !containsString(sk.Mounts, "slash_only") {
			t.Errorf("调度技能 %s 未声明 slash_only mount", m[1])
		}
		if !agentTypeCollisions[m[1]] {
			t.Errorf("调度技能 %s 与子 Agent 枚举同名，须在 agentTypeCollisions 显式登记", m[1])
		}
	}
	for name := range agentTypeCollisions {
		if !dispatch[name] {
			t.Errorf("重名技能 %s 未接入 slashSubAgentDispatch（同名异物未解耦）", name)
		}
	}
}

// TestWorkflowAutoArmContract 锁住 #111 P3 的目录降级收口：工作流自动武装清单
// 必须由真实装配器消费、覆盖全部确定性挂载点，且目录自读/勾选常驻不再算挂载点。
// 这是开发期静态断言，不引入运行时闸门，也不改变质量保障唯一规范路径。
func TestWorkflowAutoArmContract(t *testing.T) {
	skills, _ := loadBuiltinForHealth(t)

	// 1. 每个自动武装点的 mount 有主、且至少有一个内置技能真的声明了它（不空转）。
	seenMount := map[string]bool{}
	for _, arm := range WorkflowAutoArms {
		if arm.Workflow == "" || arm.Mount == "" {
			t.Fatalf("自动武装点字段不得为空: %+v", arm)
		}
		if seenMount[arm.Mount] {
			t.Errorf("自动武装点重复声明 mount %q", arm.Mount)
		}
		seenMount[arm.Mount] = true
		owner, ok := mountOwners[arm.Mount]
		if !ok {
			t.Errorf("自动武装点 %s 的 mount %q 未在 mountOwners 登记消费点", arm.Workflow, arm.Mount)
			continue
		}
		if _, err := os.Stat(filepath.Join("..", "..", owner)); err != nil {
			t.Errorf("自动武装点 %s 的 owner 文件不存在: %s", arm.Workflow, owner)
		}
		declared := false
		for _, sk := range skills {
			if containsString(sk.Mounts, arm.Mount) {
				declared = true
				break
			}
		}
		if !declared {
			t.Errorf("自动武装点 %s 的 mount %q 无任何内置技能声明（空转）", arm.Workflow, arm.Mount)
		}
	}

	// 2. 所有确定性挂载点都必须被自动武装清单覆盖，否则目录说明会撒谎。
	// slash_only 是高级入口、always 是兼容层，均不作为可用性指标，故排除。
	declaredMounts := map[string]bool{}
	for _, sk := range skills {
		for _, m := range sk.Mounts {
			if m == "slash_only" || m == "always" || m == "catalog_read" {
				continue
			}
			declaredMounts[m] = true
		}
	}
	for m := range declaredMounts {
		if !seenMount[m] {
			t.Errorf("内置技能声明了挂载点 %q，但工作流自动武装清单未覆盖", m)
		}
	}

	// 3. 目录降级后，内置技能不得再靠「LLM 自读 / 用户勾选常驻」当挂载点。
	for _, sk := range skills {
		for _, m := range sk.Mounts {
			if m == "catalog_read" {
				t.Errorf("技能 %s 仍声明已退役的 catalog_read（目录已降级为说明性）", sk.Name)
			}
			if m == "always" {
				t.Errorf("技能 %s 仍声明 always（常驻已降级为 .enabled.json 兼容层）", sk.Name)
			}
		}
	}

	// 4. 常驻上限不得被抬高：兼容层不能重新膨胀成正文主通道。
	catalogSrc := readHealthSource(t, "..", "agentcfg", "skill_catalog.go")
	if !regexp.MustCompile(`MaxAlwaysSkillsResident\s*=\s*3`).MatchString(catalogSrc) {
		t.Error("MaxAlwaysSkillsResident 必须为 3（P3 收口硬约束，抬高须重新评审）")
	}
}
