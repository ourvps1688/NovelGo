package pipeline

import (
	"fmt"
	"strings"
)

// ── 修复派发前的台账字段可达性（#102）──────────────────────────────────────
//
// 故障复盘：一致性巡检把 characters.alive 这类结构化列当事实源读，发现「正文明写已死、
// alive=1」后判 fix_type=ledger（台账可修）；但台账写工具当时只开放 4 个自由文本列，
// LLM 只能改 description/personality，复验按结构化列核对必然判「修复只改了一半」，
// 于是空转两轮修复 + 两轮复验才转人工。工具面已补 alive 受控写入口，本模块补派发面：
//
//  1. 提示词护栏：把「可写字段 / 不可写结构化列」明确注入派发层的修复 prompt 与聊天
//     窗口的去修复提示词（fixPrompt 全文被 golden 基线逐字锁定，故不写进该契约单源），
//     命中不可写列时直接申报 skipped，不要求做等效文本标注（防假闭环）；
//  2. 本地预检：派发前确定性识别 desc 里点名的不可写列，直接落 skipped 转人工，
//     一次 LLM 调用都不浪费。
//
// 注意：mcp_tools → pipeline 是 import cycle，此处无法直接反射 *Args 结构，清单必须与
// internal/mcp_tools/character_tools.go 的 Create/UpdateCharacter*Args 字段保持同步；
// writableCharacterColumns 亦用于测试锁住「alive 不得再被判为不可写」。

// writableCharacterColumns 是 characters 表当前对 LLM 开放的字段（与工具 Args 同步）。
var writableCharacterColumns = []string{
	"name", "description", "personality", "abilities", "alive",
}

// unwritableLedgerColumns 是仍无任何写工具开放的结构化/系统列。只列高辨识度的列名，
// 并配合下方「赋值/字段/列」语境匹配，避免把正文里偶现的普通词误判为字段引用。
var unwritableLedgerColumns = []string{
	"is_current", "sealed", "volume_id", "changed_in_volume_id", "changed_in_chapter_id",
	"changed_at", "created_at", "updated_at", "capability_ceiling", "voice_traits",
	"appearance_anchors", "aliases", "gender", "birth_year", "location_ref", "faction", "role",
}

// fixReachabilityGuard 注入修复提示词的字段边界说明（fixPrompt / 前端去修复提示词同口径）。
func fixReachabilityGuard() string {
	return "**台账字段可达性护栏（必须遵守）**：characters 表当前只开放 " +
		strings.Join(writableCharacterColumns, " / ") + " 这几个写入字段，其中 alive 仅允许「在世→已死」单向" +
		"（复活属 retcon，须人工审批）；其余结构化列（is_current / sealed / volume_id / changed_at / " +
		"capability_ceiling / voice_traits / appearance_anchors / aliases / created_at / updated_at 等）" +
		"以及 id / novel_id 不在任何写工具白名单内，框架层会直接拒绝 schema 外字段。" +
		"若某条 issue 的修复目标落在这些不可写列上，**直接把它放入 skipped 并注明「字段 X 不在台账写工具白名单，需平台侧/人工修复」**，" +
		"不要只在 description / personality 里写等效标注就报 fixed——复验按结构化列核对，那会被判「只改了一半」并再空转一轮。"
}

// unreachableLedgerColumn 在 desc 明确点名「不可写结构化列」时返回该列名，否则返回空串。
// 要求列名后跟赋值号或「字段/列/仍为/未改」等语境，避免误杀（例如普通叙述里出现 role 一词）。
func unreachableLedgerColumn(desc string) string {
	lower := strings.ToLower(desc)
	for _, col := range unwritableLedgerColumns {
		idx := strings.Index(lower, col)
		if idx < 0 {
			continue
		}
		// 列名须是独立 token：前后不得接字母/数字/下划线（防 volume_id 命中 changed_in_volume_id 片段等）。
		if idx > 0 && isColumnChar(lower[idx-1]) {
			continue
		}
		tail := lower[idx+len(col):]
		if tail == "" {
			continue
		}
		if isColumnChar(tail[0]) {
			continue
		}
		trimmed := strings.TrimLeft(tail, " \t")
		if strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, ":") ||
			strings.HasPrefix(trimmed, "字段") || strings.HasPrefix(trimmed, "列") ||
			strings.HasPrefix(trimmed, "仍为") || strings.HasPrefix(trimmed, "未改") ||
			strings.HasPrefix(trimmed, "仍") || strings.HasPrefix(trimmed, "值") {
			// 已开放字段不判不可达（防止清单与工具面不同步时误杀）。
			for _, w := range writableCharacterColumns {
				if col == w {
					return ""
				}
			}
			return col
		}
	}
	return ""
}

func isColumnChar(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// filterUnreachableFixIssues 把命中不可写结构化列的 issue 从待派发批次中剔除，
// 返回可直接落 skipped 的原因（key=issue id）。纯本地、零 LLM 调用。
func filterUnreachableFixIssues(issues []AuditIssue) ([]AuditIssue, map[string]string) {
	kept := make([]AuditIssue, 0, len(issues))
	reasons := make(map[string]string)
	for _, it := range issues {
		if col := unreachableLedgerColumn(it.Desc); col != "" {
			reasons[it.ID] = fmt.Sprintf(
				"台账字段 %s 不在写工具白名单内（结构化列，需平台侧/人工修复），已跳过 LLM 修复以免空转复验", col)
			continue
		}
		kept = append(kept, it)
	}
	return kept, reasons
}
