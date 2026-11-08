package pipeline

// ── 安全审核定位：真实请求内容单元化（#104）────────────────────────────────
//
// #101 的定位探针只重放「巡检语料」（章节正文 + 台账行）。但签约体检走的是 24 轮
// agent loop：首轮提示词之后，工具返回的章节列表/台账快照/伏笔/弧线会以 tool message
// 追加进下一轮请求；实测一次被拒请求里仅「已知信息」台账块就有 17,866 字。这些内容
// 从未被探针重放，于是上游判 high risk 时报告只能写「逐段探测未发现」，作者仍无从下手。
//
// 本模块把「本次真正发出去的请求文本」切成与原文同源、连续、不重排的片段单元：
//   - 提示词（signabilityPrompt / synthesizePrompt 及其上下文块）；
//   - 本次执行产生的工具返回消息（按 sessionID + 起始消息 ID 精确圈定，不含历史会话）。
// 单元交给 localizeSafetyTriggers 做二分探测，Kind="request" 的片段按原文直接重放，
// 不额外加标题前缀，尽量还原上游看到的文本形态。
//
// 只读、只诊断：不改写任何正文/台账，不触发修复，DB 缺表/查询失败一律优雅降级。

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// requestUnitMaxRunes 是单个探针片段的字符上限。切得太小会让组合触发被拆散误判为
	// 全清，切得太大则探针本身可能因请求过长被拦、且命中摘录看不到片段尾部的触犯内容；
	// 600 rune ≈ 台账十几行 / 半章短段，兼顾定位精度与摘录可读性。
	requestUnitMaxRunes = 600
	// requestUnitMaxUnits 是请求侧单元总数硬帽，避免异常大的工具返回撑爆定位预算。
	requestUnitMaxUnits = 120
	// requestSourceCapRunes 是单个请求源纳入定位的最大字符数（仅保前段并如实标注）。
	requestSourceCapRunes = 60000
	// requestToolRowsCap 是单次定位最多读取的工具返回消息条数。
	requestToolRowsCap = 64
)

// splitRequestText 把一段请求文本按行聚合成不超过 requestUnitMaxRunes 的连续片段。
// 片段拼接后与原文逐字一致（仅去掉首尾空白），保证「报出来的片段确实发送过」。
func splitRequestText(text, labelPrefix string) []patrolUnit {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	runes := []rune(trimmed)
	truncated := false
	if len(runes) > requestSourceCapRunes {
		runes = runes[:requestSourceCapRunes]
		truncated = true
	}

	var chunks []string
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			chunks = append(chunks, sb.String())
			sb.Reset()
		}
	}
	for _, line := range strings.Split(string(runes), "\n") {
		lr := []rune(line)
		// 超长单行按上限硬切成多个片段（仍保持原文连续）。
		for len(lr) > requestUnitMaxRunes {
			flush()
			chunks = append(chunks, string(lr[:requestUnitMaxRunes]))
			lr = lr[requestUnitMaxRunes:]
		}
		if sb.Len() > 0 && utf8.RuneCountInString(sb.String())+len(lr)+1 > requestUnitMaxRunes {
			flush()
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(string(lr))
	}
	flush()

	units := make([]patrolUnit, 0, len(chunks))
	for i, c := range chunks {
		units = append(units, patrolUnit{
			Kind:    "request",
			Label:   fmt.Sprintf("%s %d/%d（%s）", labelPrefix, i+1, len(chunks), requestUnitHead(c)),
			Content: c,
		})
	}
	if truncated && len(units) > 0 {
		units[len(units)-1].Label += "（源文本超长，已按预算截前段）"
	}
	return units
}

// requestUnitHead 取片段首句作人读定位（折叠空白，短帽避免 Label 撑大探针文本）。
func requestUnitHead(content string) string {
	collapsed := strings.Join(strings.Fields(content), " ")
	runes := []rune(collapsed)
	if len(runes) > 28 {
		runes = runes[:28]
	}
	if len(runes) == 0 {
		return "空片段"
	}
	return string(runes) + "…"
}

// requestUnitsRunes 返回单元总字符数，供失败态写明本次请求的体量（可操作诊断）。
func requestUnitsRunes(units []patrolUnit) int {
	n := 0
	for _, u := range units {
		n += utf8.RuneCountInString(u.Content)
	}
	return n
}

// sessionMessageBaseline 记录本次执行前该 session 的最大消息 ID。pipeline 的 skillRunner
// 只用 system+prompt 起会话、不回放 DB 历史（skill agent 的 ToAPI=false），因此定位只需
// 圈定「本次执行期间追加的 tool 消息」，绝不把跨天历史会话误当成本次请求内容。
func (s *Service) sessionMessageBaseline(ctx context.Context, sessionID string) int64 {
	if s == nil || s.store == nil || s.store.db == nil || sessionID == "" {
		return 0
	}
	var baseline int64
	row := s.store.db.WithContext(ctx).Table("messages").
		Where("session_id = ?", sessionID).
		Select("COALESCE(MAX(id), 0)").Row()
	if err := row.Scan(&baseline); err != nil {
		return 0 // 表不存在 / 查询失败：降级为无工具单元，不阻断检测
	}
	return baseline
}

type sessionToolRow struct {
	ID      int64
	Content string
}

// collectSessionToolUnits 读取本次执行产生的工具返回消息并切成可定位单元。
// 测试桩（内存库无 messages 表）与降级部署均返回 nil，由提示词单元继续提供定位能力。
func (s *Service) collectSessionToolUnits(ctx context.Context, sessionID string, baseline int64) []patrolUnit {
	if s == nil || s.store == nil || s.store.db == nil || sessionID == "" {
		return nil
	}
	var rows []sessionToolRow
	if err := s.store.db.WithContext(ctx).Table("messages").
		Select("id, content").
		Where("session_id = ? AND role = ? AND id > ?", sessionID, "tool", baseline).
		Order("id ASC").Limit(requestToolRowsCap).
		Scan(&rows).Error; err != nil {
		return nil
	}
	var units []patrolUnit
	for _, r := range rows {
		if strings.TrimSpace(r.Content) == "" {
			continue
		}
		units = append(units, splitRequestText(r.Content, fmt.Sprintf("工具返回 #%d", r.ID))...)
		if len(units) >= requestUnitMaxUnits {
			return units[:requestUnitMaxUnits]
		}
	}
	return units
}

// collectDetectSafetyUnits 汇总一次体检请求的可定位单元：提示词在前、工具返回在后，
// 与上游看到的消息顺序一致，二分探测的「区间」才有意义。
func (s *Service) collectDetectSafetyUnits(ctx context.Context, req detectRequest, baseline int64) []patrolUnit {
	units := splitRequestText(req.prompt, "体检提示词")
	return append(units, s.collectSessionToolUnits(ctx, req.sessionID, baseline)...)
}
