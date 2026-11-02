package pipeline

import (
	"context"
	"fmt"
	"strings"
)

// ── 一致性巡检语料预取（根治「让 LLM 自己逐章读取」的卡死设计）──────────────
//
// 旧设计：consistency-patrol skill 白名单只有只读工具，逼 LLM 用 read 逐章读（≈章数+ 次）
// 再读全部台账，单轮 50+ 次工具往返，慢且每次往返都碰供应商、易触发空闲看门狗/重试，
// 60 分钟硬超时内常跑不完 → 整轮超时丢弃、报告出不来。
//
// 新设计：后端在派发前批量取全书正文 + 台账快照，拼成【本书语料】块注入提示词，
// LLM 只做一次性分析。read/get_* 工具保留为可选交叉核对，不再强制逐章读。
// 取数全程是后端对 SQLite/磁盘的本地读取（无 LLM 往返），秒级完成。

const (
	// 语料预算收敛（2026-11-03）：原 4000/200000/60000 把思考模型上下文顶到 ~112k 字符
	// （章正文 ~52k + 台账快照高达 60k），思考模型单调用需 >45min 仍吐不完 → 巡检必超时失败。
	// 实测该供应商对大上下文极慢，且 SkillInput/技能均未暴露模型字段（无法换更快模型），
	// 故唯一稳妥杠杆是收紧语料：章正文 2000 + 台账 20000 + 总帽 120000，把单调用上下文压到
	// ~44k 字符（≈原 40%），配合下方「精简 JSON 输出」约束，45min 预算内稳定收口。
	// 覆盖性：13 章全书 13×2000=26k 仍全量纳入（仅每章更紧凑）；长篇小说按总帽均匀截断（优先保前段）。
	patrolChapterCap = 2000   // 单章正文注入上限（字符）；超出截断并标注
	patrolCorpusCap  = 120000 // 全书语料（章正文 + 台账）总字符预算
	patrolLedgerCap  = 20000  // 台账快照字符预算（核心事实源：角色/关系/时间线/不变量/弧线/读者视角）
	patrolLedgerRows = 200    // 单张台账表行数上限
)

// patrolLedgerTables 是需要随语料注入的台账表（均含 novel_id 列，可直接按小说过滤）。
var patrolLedgerTables = []string{
	"characters",          // 角色
	"character_relations", // 角色关系
	"character_groups",    // 角色分组
	"character_group_members",
	"locations", // 地点
	"location_relations",
	"chronology_anchors", // 时间线锚点
	"time_entries",       // 伏笔/时间线事件
	"story_arcs",         // 故事弧线
	"arc_nodes",
	"reader_perspectives", // 读者视角/已知态
	"pov_roster",
	"power_levels",
	"setting_items",
	"preference_items",
	"pinned_invariants", // 书写不变量
	"chapter_task_cards",
}

// buildPatrolCorpus 批量取全书正文 + 台账快照，拼成可注入提示词的语料块。
// 任何单步失败都只记日志并降级（跳过该块），绝不阻断巡检；chapterTextReader 为 nil 时
// 整段章正文降级为空（沿用既有降级契约，保证测试与降级路径零行为变化）。
func (s *Service) buildPatrolCorpus(ctx context.Context, novelID int64, runID string) string {
	var sb strings.Builder
	sb.WriteString("\n\n【本书语料（后端已批量取，直接通读分析即可，无需再用 read 逐章读取）】\n")

	// ── 章正文（按阅读顺序）──
	if s.chapterStore == nil {
		sb.WriteString("（章节存储不可用，已跳过章正文注入；可用 read 工具按需补读）\n")
	} else {
		chapters, err := s.chapterStore.ListAllByNovel(ctx, nil, novelID)
		if err != nil {
			s.logger.Warn("巡检语料：列章失败，跳过章正文", "novel_id", novelID, "err", err)
		} else {
			total := len(chapters)
			emitPatrolProgress(ctx, map[string]any{
				"novel_id": novelID, "run_id": runID, "phase": "reading", "total": total, "done": 0,
			})
			truncated := false
			for i, ch := range chapters {
				text := ""
				if s.chapterTextReader != nil {
					if t, rerr := s.chapterTextReader(ctx, novelID, ch.ID); rerr == nil {
						text = t
					}
				}
				if len(text) > patrolChapterCap {
					text = text[:patrolChapterCap] + "…（本章已按预算截断）"
				}
				rn := ch.ReadingNumber
				if rn == 0 {
					rn = i + 1
				}
				title := ch.Title
				if title == "" {
					title = fmt.Sprintf("第%d章", rn)
				}
				fmt.Fprintf(&sb, "\n### 第%d章《%s》\n%s\n", rn, title, text)
				emitPatrolProgress(ctx, map[string]any{
					"novel_id": novelID, "run_id": runID, "phase": "reading", "total": total, "done": i + 1,
				})
				if sb.Len() > patrolCorpusCap {
					truncated = true
					break
				}
			}
			if truncated && total > 0 {
				fmt.Fprintf(&sb, "\n（因上下文预算，第 %d 章之后的正文已省略；如需核查可用 read 工具补读具体章节）\n", min(total, len(chapters))+1)
			} else if total == 0 {
				sb.WriteString("（本书暂无章节正文；可用 read 工具按需补读）\n")
			}
		}
	}

	// ── 台账快照 ──
	if ledger := s.dumpLedgers(ctx, novelID); ledger != "" {
		sb.WriteString("\n### 台账快照（角色/关系/地点/时间线/弧线/读者视角/不变量/任务卡）\n")
		sb.WriteString(ledger)
	}

	return sb.String()
}

// dumpLedgers 把各台账表按小说导出为可读文本（含 novel_id 的表）。单表行数、总字符均受预算约束；
// 单表查询失败仅跳过该表（不阻断）；store.db 为 nil 时整段降级为空。
func (s *Service) dumpLedgers(ctx context.Context, novelID int64) string {
	if s.store == nil || s.store.db == nil {
		return ""
	}
	db := s.store.db
	var sb strings.Builder
	for _, tbl := range patrolLedgerTables {
		if sb.Len() >= patrolLedgerCap {
			sb.WriteString("\n（台账快照因预算已截断）\n")
			break
		}
		rows := []map[string]any{}
		if err := db.Table(tbl).Where("novel_id = ?", novelID).Limit(patrolLedgerRows).Find(&rows).Error; err != nil {
			continue
		}
		if len(rows) == 0 {
			continue
		}
		section := formatLedgerRows(tbl, rows)
		if sb.Len()+len(section) > patrolLedgerCap {
			section = truncateToBudget(section, patrolLedgerCap-sb.Len())
			sb.WriteString(section)
			sb.WriteString("\n（台账快照因预算已截断）\n")
			break
		}
		sb.WriteString(section)
	}
	return sb.String()
}

// formatLedgerRows 把一张表的行渲染为紧凑可读文本：每行「字段=值」以分号分隔。
func formatLedgerRows(table string, rows []map[string]any) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n#### %s（%d 条）\n", table, len(rows))
	for _, row := range rows {
		var parts []string
		for k, v := range row {
			if k == "created_at" || k == "updated_at" || k == "changed_at" || strings.HasSuffix(k, "_at") {
				continue // 跳过时间戳噪声
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, flattenValue(v)))
		}
		sb.WriteString("- " + strings.Join(parts, "; ") + "\n")
	}
	return sb.String()
}

// flattenValue 把 DB 值规整为可读字符串（[]byte 视为文本，nil 显式标注）。
func flattenValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "∅"
	case []byte:
		return string(val)
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

// truncateToBudget 在符文边界截断到 max 字符以内，避免截断半个多字节字符。
func truncateToBudget(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…（已截断）"
}
