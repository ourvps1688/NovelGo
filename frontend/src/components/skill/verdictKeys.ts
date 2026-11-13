// verdict → i18n key 映射（检测任务=pass|needs_fix|error；章节质量档案=checked|
// pending|skipped；判官评分=pass|fail|revise）。未知值如实显示原值（宁缺勿错不造词）。
// 独立小模块共享常量（react-refresh/only-export-components 处方）：消费方为
// SignabilityAssessmentCard（徽章）与 SignabilityWorkflowView / DetectionWorkflowView
// （完成 toast）。单源，勿在组件内再复制。
export const VERDICT_KEYS: Record<string, string> = {
  pass: "journey.quality.detect.verdict.pass",
  // #115：判官结论不是程序错误，fail/revise 须有本地化文案，否则面板裸显英文
  // 让用户误以为「代码失败」。仅补映射，不改 verdict 取值与落库口径。
  fail: "journey.quality.detect.verdict.fail",
  revise: "journey.quality.detect.verdict.revise",
  needs_fix: "journey.quality.detect.verdict.needs_fix",
  checked: "journey.quality.detect.verdict.checked",
  pending: "journey.quality.detect.verdict.pending",
  skipped: "journey.quality.detect.verdict.skipped",
  error: "journey.quality.detect.verdict.error",
};
