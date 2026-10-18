package pipeline

import "fmt"

// 阶段与闸门标识（PR1 C 期 MVP 用到的子集；后续 PR 扩展为完整 DAG）。
const (
	StageS1 = "S1"  // 孵化（book-pitch）
	GateG1  = "G1"  // 蓝图确认闸
	StageS2 = "S2"  // 蓝图（PR2 落库）
	StageS3 = "S3"  // 逐章写作（含评分门禁 + statesync）
	GateG3b = "G3b" // 里程碑闸（PR4 启用，每 5 章 / 卷边界）
	StageS6 = "S6"  // 完本终检
)

// NextStage 按当前阶段与用户决策，返回下一阶段与「进入下一阶段前需停留的闸门」。
//   - gate 非空：推进到 next 阶段但置 awaiting_gate，由 EmitGate 等待用户 ResumePipeline。
//   - next 空串：流程结束（finished）。
//
// C 期（PR1）是线性骨架：S1 → G1 → S3（逐章循环）。后续 PR 会把 S2/S4/S5/S6
// 与回边（S4→S5→复评）补进此表，逻辑集中、不散落前端。
func NextStage(current, decision string) (next string, gate string, err error) {
	switch current {
	case StageS1:
		// 孵化完成 -> 蓝图确认闸（C 期蓝图未自动落库，仅作为流程节点）
		return StageS2, GateG1, nil
	case GateG1, StageS2:
		if decision == "abort" {
			return "", "", nil
		}
		// 蓝图确认通过 -> 进入逐章写作
		return StageS3, "", nil
	case StageS3:
		if decision == "abort" {
			return "", "", nil
		}
		// 逐章循环：每章后由前端触发里程碑闸门（PR4 真正启用 G3b）
		return StageS3, GateG3b, nil
	default:
		return "", "", fmt.Errorf("未知阶段 %q", current)
	}
}
