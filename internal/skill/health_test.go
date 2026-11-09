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
	"catalog_read":      "internal/agentcfg/skill_catalog.go",
}

// legacyPlaceholders 是 P0 的临时豁免：这两个技能仍是占位稿（memory 缺 useWhen、
// 两个正文均不足 5 行）。P2 补正文后必须清空本表，不留永久后门。
var legacyPlaceholders = map[string]bool{"memory": true, "review": true}

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

// TestBuiltinSkillManifestHealth 锁住 manifest 的最小可用契约：
// 零挂载点即构建失败、枚举有主、预算为正、internalOnly 自洽、占位稿不放行。
func TestBuiltinSkillManifestHealth(t *testing.T) {
	skills, byName := loadBuiltinForHealth(t)
	for name := range legacyPlaceholders {
		if _, ok := byName[name]; !ok {
			t.Errorf("legacyPlaceholders 含不存在的技能: %s", name)
		}
	}
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
			if legacyPlaceholders[sk.Name] {
				if hasUseWhen && bodyOK {
					t.Errorf("占位稿 %s 已补齐正文与 useWhen，请从 legacyPlaceholders 移除豁免", sk.Name)
				}
				return
			}
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

	slash, err := os.ReadFile(filepath.Join("..", "..", "app", "slash_command.go"))
	if err != nil {
		t.Fatalf("读取 slash_command.go 失败: %v", err)
	}
	decl := regexp.MustCompile(`writingBundleSkills = \[\]string\{([^}]*)\}`).FindStringSubmatch(string(slash))
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

	rewrite, err := os.ReadFile(filepath.Join("..", "pipeline", "rewrite_skills.go"))
	if err != nil {
		t.Fatalf("读取 rewrite_skills.go 失败: %v", err)
	}
	src := string(rewrite)
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
	cond, err := os.ReadFile(filepath.Join("..", "..", "app", "writing_skill_conditions.go"))
	if err != nil {
		t.Fatalf("读取 writing_skill_conditions.go 失败: %v", err)
	}
	condSrc := string(cond)
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
	ci, err := os.ReadFile(filepath.Join("..", "mcp_tools", "chapter_ci.go"))
	if err != nil {
		t.Fatalf("读取 chapter_ci.go 失败: %v", err)
	}
	ciDecl := regexp.MustCompile(`chapterCISelfCheckSkills = \[\]string\{([^}]*)\}`).FindStringSubmatch(string(ci))
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
