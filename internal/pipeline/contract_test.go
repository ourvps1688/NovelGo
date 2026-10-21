package pipeline

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// ── 契约三方同步（contract.go ↔ AuditIssue struct ↔ skill md 示例）────────
// 契约单源化的核心保障：任何一处改动而未同步其余两处，下列测试立即红。
// 历史教训：加 3 个枚举字段改了 9 个文件，靠人肉记忆同步必然漂移。

// issueContractNames 返回契约表字段名（按定义顺序）。
func issueContractNames() []string {
	names := make([]string, 0, len(issueContractFields))
	for _, f := range issueContractFields {
		names = append(names, f.Name)
	}
	return names
}

// structJSONNames 反射返回 struct 的 json tag 字段名集合。
func structJSONNames(t *testing.T, typ reflect.Type) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		names[tag] = true
	}
	return names
}

// TestIssueContractMatchesAuditIssueStruct 验证契约表与 AuditIssue struct 双向一致：
// struct 的每个 json 字段都在契约表里、契约表的每个字段都在 struct 里（struct 是字段名事实源）。
func TestIssueContractMatchesAuditIssueStruct(t *testing.T) {
	structNames := structJSONNames(t, reflect.TypeOf(AuditIssue{}))
	contractNames := map[string]bool{}
	for _, name := range issueContractNames() {
		contractNames[name] = true
	}
	for name := range structNames {
		require.True(t, contractNames[name], "AuditIssue 字段 %q 未进契约表（contract.go），请补 issueContractFields", name)
	}
	for name := range contractNames {
		require.True(t, structNames[name], "契约表字段 %q 在 AuditIssue struct 中不存在，请补 struct 或移除契约项", name)
	}
}

// TestIssueContractEnumsAndDocs 验证契约表内部自洽：枚举字段的示例值覆盖全部枚举项、
// 全量契约字段必带必填说明（进提示词的 Doc），生成片段不含 %（会被 fmt.Sprintf 误当动词）。
func TestIssueContractEnumsAndDocs(t *testing.T) {
	for _, f := range issueContractFields {
		for _, v := range f.Enum {
			require.Contains(t, f.Sample, v, "字段 %s 的示例值应包含枚举项 %q", f.Name, v)
		}
		if f.Full {
			require.NotEmpty(t, f.Doc, "全量契约字段 %s 必须带必填说明（进提示词）", f.Name)
			require.Contains(t, f.Doc, f.Name, "字段 %s 的说明应以字段名开头，便于 LLM 对号入座", f.Name)
		}
	}
	// 生成片段嵌入 fmt.Sprintf 的格式串，出现 % 会被当成格式动词。
	for _, frag := range []string{
		issueSchemaLine(true, issueSampleDefault),
		auditFormatLine(true, issueSampleDefault),
		auditFormatLine(false, issueSampleDrift),
		issueFieldRules(),
		fixReceiptFormatLine(),
	} {
		require.NotContains(t, frag, "%", "契约生成片段不得含 %%（会破坏 fmt.Sprintf 提示词拼接）")
	}
}

// TestAuditFormatLineTiers 验证全量/核心分档：全量含 schema_version 与三枚举字段，
// 核心只含 id/level/area/desc（蓝图/巡检不涉修复路由字段）。
func TestAuditFormatLineTiers(t *testing.T) {
	full := auditFormatLine(true, issueSampleDefault)
	require.Contains(t, full, `"schema_version":2`, "全量契约应要求 schema_version")
	for _, name := range issueContractNames() {
		require.Contains(t, full, `"`+name+`":`, "全量契约应含字段 %s", name)
	}

	core := auditFormatLine(false, issueSampleDrift)
	require.Contains(t, core, `"id":"D1"`, "巡检示例值应生效")
	require.Contains(t, core, `"area":"章节#12"`, "巡检示例值应生效")
	for _, name := range []string{"id", "level", "area", "desc"} {
		require.Contains(t, core, `"`+name+`":`, "核心契约应含字段 %s", name)
	}
	for _, f := range issueContractFields {
		if f.Full {
			require.NotContains(t, core, `"`+f.Name+`":`, "核心契约不应含全量字段 %s", f.Name)
		}
	}
}

// TestFixReceiptFormatLine 验证修复回执格式串可被 fixReceipt 结构解析。
func TestFixReceiptFormatLine(t *testing.T) {
	var r fixReceipt
	require.NoError(t, json.Unmarshal([]byte(fixReceiptFormatLine()), &r))
	require.NotEmpty(t, r.Fixed)
	require.NotEmpty(t, r.Skipped)
	require.NotEmpty(t, r.Skipped[0].ID)
	require.NotEmpty(t, r.Skipped[0].Reason)
}

// TestAuditStaleReason 验证双闸陈旧判定：字段嗅探（契约字段组缺失）+ 版本门
// （schema_version 显式低于当前契约），任一命中即陈旧；未标版本只走嗅探。
func TestAuditStaleReason(t *testing.T) {
	complete := AuditData{
		Verdict: "needs_fix", SchemaVersion: AuditSchemaVersion,
		Issues: []AuditIssue{{
			ID: "I1", Level: "P0", Area: "维度", Desc: "d",
			FixType: "ledger", TargetScope: "active_volume", Dimension: "none", Direction: "forward_fix",
		}},
	}

	require.Empty(t, auditStaleReason(complete), "契约齐全且版本当前应可用")

	unstamped := complete
	unstamped.SchemaVersion = 0
	require.Empty(t, auditStaleReason(unstamped), "未标版本不触发版本门，字段齐全应可用")

	old := complete
	old.SchemaVersion = AuditSchemaVersion - 1
	require.Contains(t, auditStaleReason(old), "schema_version", "显式旧版本应被版本门拦截")

	noFixType := complete
	noFixType.SchemaVersion = 0
	noFixType.Issues = []AuditIssue{{ID: "I1", Level: "P0", Area: "a", Desc: "d",
		TargetScope: "active_volume", Dimension: "none", Direction: "forward_fix"}}
	require.Contains(t, auditStaleReason(noFixType), "fix_type", "缺 fix_type 组应报字段名")

	noScope := complete
	noScope.SchemaVersion = 0
	noScope.Issues = []AuditIssue{{ID: "I1", Level: "P0", Area: "a", Desc: "d", FixType: "ledger"}}
	require.Contains(t, auditStaleReason(noScope), "target_scope", "缺作用域组应报字段名")

	// 「任一命中即覆盖」语义（宁松勿杀）：组内字段被任一 issue 携带即视为已覆盖。
	partial := complete
	partial.SchemaVersion = 0
	partial.Issues = []AuditIssue{
		{ID: "I1", Level: "P0", Area: "a", Desc: "d",
			TargetScope: "active_volume", Dimension: "none", Direction: "forward_fix"},
		{ID: "I2", Level: "P1", Area: "b", Desc: "e", FixType: "ledger"},
	}
	require.Empty(t, auditStaleReason(partial), "字段组由不同 issue 分担覆盖应判可用（与旧守卫语义一致）")
}

// TestParseFreshAuditStampsSchemaVersion 验证版本号落地：新鲜产出（当前提示词契约）未 echo
// schema_version 时补标当前版本；已落库历史 JSON 的再解析不补标（否则版本门失效）。
func TestParseFreshAuditStampsSchemaVersion(t *testing.T) {
	raw := `{"verdict":"needs_fix","score":20,"summary":"s","issues":[{"id":"I1","level":"P0","area":"a","desc":"d"}]}`

	fresh := parseFreshAudit(raw)
	require.Equal(t, AuditSchemaVersion, fresh.SchemaVersion, "新鲜产出应补标当前契约版本")

	stored := parseAudit(raw)
	require.Equal(t, 0, stored.SchemaVersion, "已落库旧数据不得补标（保留 0，仅走字段嗅探）")

	// 落库往返：带版本号的体检 marshal 后再解析应保留原版本。
	b, err := json.Marshal(fresh)
	require.NoError(t, err)
	require.Contains(t, string(b), `"schema_version"`, "非 0 版本应序列化进 ContextJSON")
	require.Equal(t, AuditSchemaVersion, parseAudit(string(b)).SchemaVersion, "往返解析应保留版本号")
}

// ── 提示词 ↔ 契约单源回归 ────────────────────────────────────────────────

// TestPromptSchemaSingleSource 验证 5 处体检提示词与 2 处修复回执全部引用契约生成器：
// 全量提示词含当前 schema 片段与必填说明，核心提示词不含全量字段（防止回退到手抄 schema）。
func TestPromptSchemaSingleSource(t *testing.T) {
	rules := issueFieldRules()
	for name, p := range map[string]string{
		"signabilityPrompt": signabilityPrompt(1, ""),
		"finalAuditPrompt":  finalAuditPrompt(1),
		"stockAuditPrompt":  stockAuditPrompt(1),
	} {
		require.Contains(t, p, auditFormatLine(true, issueSampleDefault), "%s 应引用全量契约 schema 片段", name)
		require.Contains(t, p, rules, "%s 应引用契约必填说明", name)
		require.Contains(t, p, `"schema_version"`, "%s 应要求 schema_version", name)
	}
	for name, p := range map[string]string{
		"blueprintAuditPrompt": blueprintAuditPrompt(1),
		"driftPatrolPrompt":    driftPatrolPrompt(1),
	} {
		require.NotContains(t, p, `"fix_type"`, "%s（核心契约）不应含全量字段", name)
		require.NotContains(t, p, `"target_scope"`, "%s（核心契约）不应含全量字段", name)
	}
	require.Contains(t, driftPatrolPrompt(1), `"id":"D1"`, "巡检提示词应使用巡检示例值")
	for name, p := range map[string]string{
		"fixPrompt":          fixPrompt(1, nil, ""),
		"blueprintFixPrompt": blueprintFixPrompt(1, nil, ""),
	} {
		require.Contains(t, p, fixReceiptFormatLine(), "%s 应引用契约修复回执格式串", name)
	}
}

// ── skill md 示例 ↔ 契约同步 ────────────────────────────────────────────

// mdJSONKeyRe 从 skill md 的 JSON 示例块提取 `"key":` 形式的键名（示例值不含此类模式）。
var mdJSONKeyRe = regexp.MustCompile(`"([a-z_][a-z0-9_]*)"\s*:`)

// extractMDFirstJSONBlock 取 md 中第一个行首 ```json 围栏块（行首锚定，避免命中正文里的
// 行内 ` ```json ` 字面量）。
func extractMDFirstJSONBlock(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "读取 skill md 失败")
	re := regexp.MustCompile("(?ms)^```json\\s*\n(.*?)^```")
	m := re.FindStringSubmatch(string(raw))
	require.NotEmpty(t, m, "%s 应含行首 ```json 示例块", path)
	return m[1]
}

// TestSkillMDExamplesMatchContract 验证 skill md 输出格式示例与契约双向同步：
// signability-audit.md（全量契约）示例键 = AuditData ∪ 全部 AuditIssue 字段；
// blueprint-audit.md（核心契约）示例键 = AuditData（除 schema_version）∪ 核心 issue 字段。
// 示例里的字段名以 struct json tag 为事实源派生，md 改字段而不同步 struct/契约即红。
func TestSkillMDExamplesMatchContract(t *testing.T) {
	dataNames := structJSONNames(t, reflect.TypeOf(AuditData{}))
	coreNames := map[string]bool{}
	fullNames := map[string]bool{}
	for _, f := range issueContractFields {
		fullNames[f.Name] = true
		if !f.Full {
			coreNames[f.Name] = true
		}
	}

	// 全量：signability-audit.md 示例键 = 顶层数据字段 + 全部 issue 字段。
	wantFull := map[string]bool{}
	for n := range dataNames {
		wantFull[n] = true
	}
	for n := range fullNames {
		wantFull[n] = true
	}
	gotFull := map[string]bool{}
	for _, k := range mdJSONKeyRe.FindAllStringSubmatch(extractMDFirstJSONBlock(t, "../skill/builtin/signability-audit.md"), -1) {
		gotFull[k[1]] = true
	}
	require.Equal(t, wantFull, gotFull, "signability-audit.md 示例键与契约/struct 不一致（双向同步）")

	// 核心：blueprint-audit.md 示例键 = 顶层数据字段（除 schema_version）+ 核心 issue 字段。
	wantCore := map[string]bool{}
	for n := range dataNames {
		if n != "schema_version" {
			wantCore[n] = true
		}
	}
	for n := range coreNames {
		wantCore[n] = true
	}
	gotCore := map[string]bool{}
	for _, k := range mdJSONKeyRe.FindAllStringSubmatch(extractMDFirstJSONBlock(t, "../skill/builtin/blueprint-audit.md"), -1) {
		gotCore[k[1]] = true
	}
	require.Equal(t, wantCore, gotCore, "blueprint-audit.md 示例键与核心契约/struct 不一致（双向同步）")
}
