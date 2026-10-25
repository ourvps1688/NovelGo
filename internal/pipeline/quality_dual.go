package pipeline

// R2-4 里程碑档案汇总快审（双轨只读对照，提案 §4.2/§6 风险对策）：
//   - milestone 口径把「质量档案汇总」块追加进 L2 体检输入（无档案=空块零提示，
//     提示词与无该块逐字一致）；
//   - 档案存在时与旧全量通读**并行**跑一次「档案汇总快审」（只读档案汇总、禁取正文、
//     不出 issue 清单），记录其 verdict——裁决仍由旧全量通读把持（只读对照，
//     切换裁决待对照差异率达标后另行决策）；
//   - 快审 verdict 与全量裁决的异同记入本地度量（feature_usage_daily，纯本地，
//     只记 key+次数不记内容），供后续切换裁决取数；
//   - 降级零提示：无档案 / 派发失败 / 回执坏 / 快审超窗未归 → 静默不记，
//     主流程零感知（宁缺勿错）。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/novelgo/novelgo/internal/quality"
)

const (
	// 快审对照的本地度量键（action.* 命名空间，与前端 featureKeys.ts 同族三类之一；
	// 后端观测单源，前端组件不得自造）：agree=快审与全量裁决一致，diff=不一致。
	featureQuickAuditAgree = "action.audit_quick_agree"
	featureQuickAuditDiff  = "action.audit_quick_diff"

	// quickAuditJoinTimeout 是全量体检收口后等待快审归队的上限。快审与全量并行
	// 起跑，全量通读耗时远大于快审（快审 MaxTurns=4 且禁取正文），正常早已归队；
	// 超窗视为异常慢，不阻塞落闸、不记对照（降级零提示）。
	quickAuditJoinTimeout = 10 * time.Second

	// quickAuditMaxTurns 是快审轮数上限：只读汇总裁决，4 轮足够，也防止误取正文长跑。
	quickAuditMaxTurns = 4
)

// quickAuditResult 是快审轨的归队结果：ok=false 表示派发失败/回执坏/verdict 非法。
type quickAuditResult struct {
	verdict string
	ok      bool
}

// qualityLedgerBlock 读取本书质量档案并渲染「质量档案汇总」块。
// 无档案 / 读取失败 / 渲染为空 → 返回空串（无档案降级零提示）。
func (s *Service) qualityLedgerBlock(ctx context.Context, novelID int64) string {
	if s.qualityStore == nil {
		return ""
	}
	rows, err := s.qualityStore.ListByNovel(ctx, novelID)
	if err != nil {
		s.logger.Warn("质量档案汇总块：读取档案失败（降级零提示）", "novel_id", novelID, "err", err)
		return ""
	}
	return quality.RenderLedgerBlock(rows)
}

// ledgerQuickAuditPrompt 构造「档案汇总快审」提示词（快审轨的唯一输入=质量档案汇总块，
// R2b-1 起同路追加「结构告警」块，空块逐字一致）。
// 快审回执契约由 contract.go 单源生成（quickAuditFormatLine）。
func ledgerQuickAuditPrompt(novelID int64, ledgerBlock, structBlock string) string {
	input := ledgerBlock
	if structBlock != "" {
		input += "\n\n" + structBlock
	}
	return fmt.Sprintf("本书 novel_id=%d。这是一次**质量档案汇总快审**（双轨对照的快审轨，只读）："+
		"**禁止**调用 get_chapter_list / read 等取正文工具，不得通读或抽读章节正文——只依据下方「质量档案汇总」"+
		"（每章落盘后的 L0 确定性检查 + L1 增量检查记录）做汇总裁决，评估全书当前的整体质量风险"+
		"（发现密度、未修复发现、L1 覆盖缺口、字数与章尾钩子等结构指标异常）。"+
		"快审结论仅供双轨对照，不影响任何裁决与状态；档案全绿/零发现就如实报 pass，宁缺勿错。"+
		"最终回复末尾**必须**附一个 ```json 代码块，格式："+quickAuditFormatLine()+"。\n\n%s",
		novelID, input)
}

// runLedgerQuickAudit 跑一次档案汇总快审并归队结果（快审轨，只读）。
// 任何失败静默归队 ok=false（降级零提示零报错）。
func (s *Service) runLedgerQuickAudit(ctx context.Context, runID string, novelID int64, ledgerBlock, structBlock string) quickAuditResult {
	out, err := s.dispatchSkill(ctx, "signability-audit", SkillInput{
		NovelID:   novelID,
		Prompt:    ledgerQuickAuditPrompt(novelID, ledgerBlock, structBlock),
		MaxTurns:  quickAuditMaxTurns,
		SessionID: "pipeline:" + runID + ":audit-quick",
		RunID:     runID,
	})
	if err != nil {
		s.logger.Warn("档案汇总快审：派发失败（降级零提示）", "run_id", runID, "err", err)
		return quickAuditResult{}
	}
	return parseQuickAudit(out.Text)
}

// parseQuickAudit 解析快审回执（```json 块，契约 quickAuditFormatLine）：
// 无回执块 / 坏 JSON / verdict 非法 → ok=false（宁缺勿错，不猜结论）。
func parseQuickAudit(text string) quickAuditResult {
	m := auditJSONRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return quickAuditResult{}
	}
	var r struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(m[1]), &r); err != nil {
		return quickAuditResult{}
	}
	if r.Verdict != "pass" && r.Verdict != "needs_fix" {
		return quickAuditResult{}
	}
	return quickAuditResult{verdict: r.Verdict, ok: true}
}

// recordQuickAuditCompare 收取快审结果与全量裁决比对，一致/差异记入本地度量
// （纯本地，只记 key+次数不记内容）。快审失败/回执坏/超窗未归 → 静默不记
// （降级零提示）；观测自身绝不影响业务行为。
func (s *Service) recordQuickAuditCompare(ctx context.Context, ch <-chan quickAuditResult, fullVerdict string) {
	select {
	case qr := <-ch:
		if !qr.ok {
			return
		}
		key := featureQuickAuditDiff
		if qr.verdict == fullVerdict {
			key = featureQuickAuditAgree
		}
		s.recordFeatureUsage(ctx, key)
	case <-time.After(quickAuditJoinTimeout):
		s.logger.Debug("档案汇总快审：超窗未归（不记对照，零提示）", "full_verdict", fullVerdict)
	}
}
