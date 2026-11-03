// verdict → i18n key 映射（book/volume=pass|needs_fix|error；章节质量档案=checked|
// pending|skipped；卷/结构=pass|needs_fix）。未知值如实显示原值（宁缺勿错不造词）。
// 独立小模块共享常量（react-refresh/only-export-components 处方）：消费方为
// SignabilityAssessmentCard（徽章）与 SignabilityWorkflowView / DetectionWorkflowView
// （完成 toast）。单源，勿在组件内再复制。
export const VERDICT_KEYS: Record<string, string> = {
  pass: "journey.quality.detect.verdict.pass",
  needs_fix: "journey.quality.detect.verdict.needs_fix",
  checked: "journey.quality.detect.verdict.checked",
  pending: "journey.quality.detect.verdict.pending",
  skipped: "journey.quality.detect.verdict.skipped",
  error: "journey.quality.detect.verdict.error",
};
