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

// patrolUnit 是巡检语料的最小可定位单元（一章正文或一条台账行），与拼入提示词的
// 文本同源同截断——安全审核定位（localizeSafetyTriggers）只探测真正发给过上游的
// 内容，绝不误报「未发送的条目」。Label 为面向用户的人读定位，Content 为探测文本。
type patrolUnit struct {
	Kind    string // "chapter" | "ledger"
	Label   string // 「第 7 章《夜色》」「台账 characters 第 3 行」
	Content string // 与拼入语料一致的文本（受同一预算截断）
}

// buildPatrolCorpus 批量取全书正文 + 台账快照，拼成可注入提示词的语料块。
// 任何单步失败都只记日志并降级（跳过该块），绝不阻断巡检；chapterTextReader 为 nil 时
// 整段章正文降级为空（沿用既有降级契约，保证测试与降级路径零行为变化）。
func (s *Service) buildPatrolCorpus(ctx context.Context, novelID int64, runID string) string {
	corpus, _ := s.buildPatrolCorpusWithUnits(ctx, novelID, runID)
	return corpus
}

// buildPatrolCorpusWithUnits 与 buildPatrolCorpus 同源渲染语料，并同步回收可定位
// 语料单元（章/台账行）供安全审核定位用。渲染文本与单元 Content 出自同一截断结果。
func (s *Service) buildPatrolCorpusWithUnits(ctx context.Context, novelID int64, runID string) (string, []patrolUnit) {
	var sb strings.Builder
	var units []patrolUnit
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
				units = append(units, patrolUnit{
					Kind:    "chapter",
					Label:   fmt.Sprintf("第%d章《%s》", rn, title),
					Content: text,
				})
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
	if ledger, ledgerUnits := s.dumpLedgers(ctx, novelID); ledger != "" {
		sb.WriteString("\n### 台账快照（角色/关系/地点/时间线/弧线/读者视角/不变量/任务卡）\n")
		sb.WriteString(ledger)
		units = append(units, ledgerUnits...)
	}

	return sb.String(), units
}

// dumpLedgers 把各台账表按小说导出为可读文本（含 novel_id 的表），并同步回收逐行
// 可定位单元。单表行数、总字符均受预算约束；单表查询失败仅跳过该表（不阻断）；
// store.db 为 nil 时整段降级为空。
func (s *Service) dumpLedgers(ctx context.Context, novelID int64) (string, []patrolUnit) {
	var units []patrolUnit
	if s.store == nil || s.store.db == nil {
		return "", units
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
		section, sectionUnits := formatLedgerRows(tbl, rows)
		if sb.Len()+len(section) > patrolLedgerCap {
			// 预算截断时逐行重建本节：只写完整行，且回收的单元恰好就是写进去的那些行。
			// 不能用「按字节估算 kept + truncateToBudget 按 rune 截断」的组合——中文台账下
			// 两者单位不一致，会把实际已发送、可能正是触犯内容的行排除在定位范围之外（#101 漏报）。
			header := fmt.Sprintf("\n#### %s（%d 条）\n", tbl, len(rows))
			budget := patrolLedgerCap - sb.Len()
			remain := budget - len(header)
			var keptSection strings.Builder
			if remain > 0 {
				keptSection.WriteString(header)
			}
			kept := 0
			for _, u := range sectionUnits {
				line := u.Content + "\n"
				if remain-len(line) < 0 {
					break
				}
				keptSection.WriteString(line)
				remain -= len(line)
				kept++
			}
			// keptSection 的字节数已 ≤ budget，故 rune 数必然 ≤ budget，此处不会二次截断；
			// 保留调用以维持「超预算加截断标记」的既有语义。
			section = truncateToBudget(keptSection.String(), budget)
			sb.WriteString(section)
			sb.WriteString("\n（台账快照因预算已截断）\n")
			units = append(units, sectionUnits[:kept]...)
			break
		}
		sb.WriteString(section)
		units = append(units, sectionUnits...)
	}
	return sb.String(), units
}

// formatLedgerRows 把一张表的行渲染为紧凑可读文本：每行「字段=值」以分号分隔。
// 同步回收逐行单元（Label 含表名与行号，供安全审核定位报出「哪张台账哪一行」）。
func formatLedgerRows(table string, rows []map[string]any) (string, []patrolUnit) {
	var sb strings.Builder
	var units []patrolUnit
	fmt.Fprintf(&sb, "\n#### %s（%d 条）\n", table, len(rows))
	for i, row := range rows {
		line := formatLedgerRow(row)
		sb.WriteString(line + "\n")
		units = append(units, patrolUnit{
			Kind:    "ledger",
			Label:   fmt.Sprintf("台账 %s 第 %d 行", table, i+1),
			Content: line,
		})
	}
	return sb.String(), units
}

// formatLedgerRow 渲染单行台账为「- 字段=值; …」（跳过时间戳噪声列）。
func formatLedgerRow(row map[string]any) string {
	var parts []string
	for k, v := range row {
		if k == "created_at" || k == "updated_at" || k == "changed_at" || strings.HasSuffix(k, "_at") {
			continue // 跳过时间戳噪声
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, flattenValue(v)))
	}
	return "- " + strings.Join(parts, "; ")
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
