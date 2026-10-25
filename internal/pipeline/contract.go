package pipeline

import (
	"fmt"
	"reflect"
	"strings"
)

// ── issue JSON 契约（单源）────────────────────────────────────────────────
// LLM 输出的 issue JSON 契约在此唯一定义：字段名、枚举、必填说明、提示词内嵌
// schema 片段、陈旧 run 判定全部由本文件派生。新增/修改字段只改这里 + AuditIssue
// struct（store.go）+ skill md 示例，契约测试（contract_test.go）强制三处同步，
// 不再逐处手抄 schema 字符串（历史上加 3 个枚举字段改了 9 个文件）。
//
// 消费方：
//   - 提示词：signabilityPrompt / finalAuditPrompt / stockAuditPrompt（全量契约）、
//     blueprintAuditPrompt / driftPatrolPrompt（核心字段）、
//     fixPrompt / blueprintFixPrompt（修复回执）、verifyPrompt（增量复验回执）
//   - skill md：signability-audit.md 输出格式节（示例 JSON）
//   - 前端：types/pipeline.ts（AuditIssue / AuditData / FixIssuePayload / RewriteItem）
//   - 陈旧 run 守卫：executeFix 按 issueContractGroups 嗅探 + schema_version 版本门

// AuditSchemaVersion 是 issue JSON 契约的版本号（体检结果 schema_version 字段的当前值）。
// 每次给 issue 契约增删字段或变更语义时 +1：executeFix 的版本门会拒绝低于该版本的体检
// 结果（强制重跑体检），避免旧数据被新路由误读。未标版本（0，旧版数据）不触发版本门，
// 回退字段嗅探——两道闸任一命中即判陈旧。
const AuditSchemaVersion = 3

// issueField 是 issue JSON 契约的一个字段定义。
type issueField struct {
	Name   string   // JSON 字段名（与 AuditIssue struct 的 json tag 对齐，契约测试校验）
	Enum   []string // 允许枚举值；空 = 自由文本
	Sample string   // 提示词 schema 示例值（含引号的 JSON 片段）
	Doc    string   // 字段必填说明（进提示词）；空 = 不单独说明
	Full   bool     // true=仅全量契约（签约/终检/存量体检）要求；false=核心字段（蓝图/巡检同要求）
}

// issueContractFields 是 issue JSON 契约的唯一定义（§0.4 冻结）。
var issueContractFields = []issueField{
	{Name: "id", Sample: `"I1"`},
	{Name: "level", Enum: []string{"P0", "P1"}, Sample: `"P0|P1"`},
	{Name: "area", Sample: `"维度"`},
	{Name: "desc", Sample: `"..."`},
	{Name: "fix_type", Enum: []string{FixTypeBody, FixTypeLedger}, Sample: `"body|ledger"`, Full: true,
		Doc: "fix_type 必填：body=正文级需章节重写（S5 修不动，将回流 S3 重写回路）；ledger=台账/卷纲/单章 edit 可修。"},
	{Name: "target_scope", Enum: []string{TargetScopeActiveVolume, TargetScopeFrozenVolume, TargetScopeNone},
		Sample: `"active_volume|frozen_volume|none"`, Full: true,
		Doc: "target_scope 必填：active_volume=问题落点在当前活跃卷；frozen_volume=问题落点在已冻结定稿卷（写保护）；none=全书级/不涉具体卷。"},
	{Name: "dimension", Enum: []string{"character", "locus_arc", "timeline", "reader_pov", DimensionNone},
		Sample: `"character|locus_arc|timeline|reader_pov|none"`, Full: true,
		Doc: "dimension 必填：character=角色 | locus_arc=地点·弧线 | timeline=时间线 | reader_pov=读者视角（series bible 四维）；不属四维的纯质量问题（节奏/爽点/文笔等）填 none。"},
	{Name: "direction", Enum: []string{DirectionForwardFix, DirectionRetconCandidate},
		Sample: `"forward_fix|retcon_candidate"`, Full: true,
		Doc: "direction 必填：forward_fix=前向修复（改当前卷及后续适配已定稿前卷，默认）；retcon_candidate=硬矛盾且无法前向消解、需回改已冻结前卷（将进人工审批闸门，慎用）。"},
	{Name: "chapter_ids", Sample: `[1,2,3]`, Full: true,
		Doc: "chapter_ids 必填：问题涉及章节的全局阅读序号数组（与 get_chapter_list 一致，如第6-10章填 [6,7,8,9,10]；无具体章节的台账/全书级问题填 []）。供修复按作用域分组与章节定位，不要只写在 desc 文本里。"},
}

// issueFieldGroup 是陈旧 run 判定的字段覆盖组：组内任一字段被任一 issue 携带即视为该组
// 已覆盖。与被替代的 auditHasFixType / auditHasScopeFields 的「任一命中」语义一致——
// 严格到「每条 issue 都要带全」会把 LLM 偶发缺字段的新体检误判成陈旧，宁松勿杀。
type issueFieldGroup struct {
	Name   string   // 组名（进错误提示，测试按字段名断言）
	Fields []string // 组内 JSON 字段名
}

var issueContractGroups = []issueFieldGroup{
	{Name: "fix_type", Fields: []string{"fix_type"}},
	{Name: "target_scope/dimension/direction", Fields: []string{"target_scope", "dimension", "direction"}},
}

// issueSample 是提示词 schema 里的示例值定制（不同体检场景的 id/area 语义不同）。
type issueSample struct{ ID, Level, Area string }

var (
	issueSampleDefault = issueSample{ID: "I1", Level: "P0|P1", Area: "维度"}
	issueSampleDrift   = issueSample{ID: "D1", Level: "P1", Area: "章节#12"}
	issueSamplePatrol  = issueSample{ID: "T1", Level: "P0|P1", Area: "张力维度"}
	// issueSampleStyle 是 P2 文风漂移（V 系）示例。ID 命名空间裁决（WBS p2 §0.2）：
	// D 归 G3 蓝图漂移（issueSampleDrift）、T 归巡检、I 归常规体检——V 系勿混用。
	issueSampleStyle = issueSample{ID: "V1", Level: "P1", Area: "文风漂移·章节#12"}
)

// issueSchemaLine 生成提示词内嵌的 issue 单条 JSON 格式串。
// full=true 输出全量契约字段（签约/终检/存量体检）；false 只输出核心字段（蓝图/巡检）。
func issueSchemaLine(full bool, s issueSample) string {
	parts := make([]string, 0, len(issueContractFields))
	for _, f := range issueContractFields {
		if !full && f.Full {
			continue
		}
		sample := f.Sample
		switch f.Name {
		case "id":
			sample = `"` + s.ID + `"`
		case "level":
			sample = `"` + s.Level + `"`
		case "area":
			sample = `"` + s.Area + `"`
		}
		parts = append(parts, fmt.Sprintf(`"%s":%s`, f.Name, sample))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// auditFormatLine 生成提示词内嵌的体检结果 JSON 顶层格式串。
// full=true 附 schema_version（版本门依据，见 AuditSchemaVersion）。
func auditFormatLine(full bool, s issueSample) string {
	head := `{"verdict":"pass|needs_fix","score":0,"summary":"..."`
	if full {
		head += fmt.Sprintf(`,"schema_version":%d`, AuditSchemaVersion)
	}
	return head + `,"issues":[` + issueSchemaLine(full, s) + `]}`
}

// issueFieldRules 生成提示词内嵌的契约字段必填说明（只含全量契约新增字段）。
func issueFieldRules() string {
	var b strings.Builder
	for _, f := range issueContractFields {
		if f.Full && f.Doc != "" {
			b.WriteString(f.Doc)
		}
	}
	return b.String()
}

// fixReceiptFormatLine 生成修复回执 JSON 格式串（fixPrompt / blueprintFixPrompt 共用）。
// log 是可选的修复日志字段（R1b）：逐条给出已修复项的一行改动摘要，同轮后续修复会话
// 会经「本轮修复日志」注入，消除重复勘察；省略不影响回执判定（兼容读旧回执）。
func fixReceiptFormatLine() string {
	return `{"fixed":["I.."],"skipped":[{"id":"I..","reason":"无法修复的原因"}],"log":[{"id":"I..","note":"一行改动摘要"}]}`
}

// verifyReceiptFormatLine 生成增量复验回执 JSON 格式串（verifyPrompt 专用，R1c）：
// verified 逐条给出已修复项的复验判定（ok=true 确认修复生效 / false 未解决或修歪），
// summary 是一句话复验小结。未列入回执的项视为未复验，留待后续复验轮次再次核对。
func verifyReceiptFormatLine() string {
	return `{"verified":[{"id":"I..","ok":true,"note":"一行复验结论"}],"summary":"一句话复验小结"}`
}

// quickAuditFormatLine 生成「档案汇总快审」回执 JSON 格式串（ledgerQuickAuditPrompt
// 专用，R2-4 双轨对照）：快审只裁决 verdict（pass/needs_fix）+ 一句话 summary，
// 不出 issue 清单（发现已沉淀在质量档案里）。verdict 与旧全量通读裁决做只读对照，
// 异同记本地度量；快审结论不进任何裁决与状态（双轨切换另行决策）。
func quickAuditFormatLine() string {
	return `{"verdict":"pass|needs_fix","summary":"一句话汇总裁决结论"}`
}

// auditMissingContractGroups 返回体检结果未覆盖的契约字段组（空 = 契约字段齐全）。
// 覆盖判定为「任一 issue 携带组内任一字段」，与旧 auditHasFixType/auditHasScopeFields
// 语义一致。
func auditMissingContractGroups(audit AuditData) []string {
	var missing []string
	for _, g := range issueContractGroups {
		covered := false
		for _, name := range g.Fields {
			if auditFieldCovered(audit, name) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, g.Name)
		}
	}
	return missing
}

// auditFieldCovered 判断任一 issue 是否携带指定 JSON 字段（按 struct json tag 反射匹配，
// struct 即字段名事实源，契约表与之由测试强制同步）。
// 注意：Value.String() 只对 string kind 返回零值判定——切片（如 chapter_ids）会返回
// 「<[]int64 Value>」这类恒非空占位串，必须按 kind 分派，否则空切片被误判为已覆盖。
func auditFieldCovered(audit AuditData, name string) bool {
	for _, it := range audit.Issues {
		v := reflect.ValueOf(it)
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			tag := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
			if tag != name {
				continue
			}
			f := v.Field(i)
			switch f.Kind() {
			case reflect.String:
				if f.String() != "" {
					return true
				}
			case reflect.Slice, reflect.Map, reflect.Array:
				if f.Len() > 0 {
					return true
				}
			default:
				if !f.IsZero() {
					return true
				}
			}
		}
	}
	return false
}

// auditStaleReason 判定体检结果是否为陈旧契约数据，返回非空理由（"" = 可用）。
// 两道闸任一命中即陈旧：
//  1. 字段嗅探：契约字段组缺失（旧版数据或 LLM 未按 schema 输出）；
//  2. 版本门：schema_version 显式低于 AuditSchemaVersion（语义已演进的旧数据，
//     即使字段碰巧齐全也不可信）。未标版本（0）不触发版本门，只走字段嗅探。
func auditStaleReason(audit AuditData) string {
	missing := auditMissingContractGroups(audit)
	oldVersion := audit.SchemaVersion > 0 && audit.SchemaVersion < AuditSchemaVersion
	switch {
	case len(missing) > 0 && oldVersion:
		return fmt.Sprintf("体检结果缺契约字段 %s（schema_version=%d < 当前 v%d）；请重新发起体检/流水线以启用完整路由。",
			strings.Join(missing, "、"), audit.SchemaVersion, AuditSchemaVersion)
	case len(missing) > 0:
		return fmt.Sprintf("体检结果缺契约字段 %s（旧版数据或未按契约输出）；请重新发起体检/流水线以启用完整路由（正文级回流/冻结卷写保护/前向默认）。",
			strings.Join(missing, "、"))
	case oldVersion:
		return fmt.Sprintf("体检结果 schema_version=%d 低于当前契约 v%d（字段语义已演进）；请重新发起体检/流水线。",
			audit.SchemaVersion, AuditSchemaVersion)
	}
	return ""
}
