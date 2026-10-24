// 创作流水线前端类型。PipelineRun 直接使用 wails 生成的 pipeline.PipelineRun，
// 避免手写类型与生成类型不一致导致 TS2345。

export type PipelineStatus =
  "pending" | "running" | "paused" | "awaiting_gate" | "finished" | "aborted";

export type PipelineMode = "new" | "continue";

// patrol = 手动触发全书一致性巡检（WBS 3.3「张力报告」，只报告不改写），
// 与 align_full 同构：后台异步执行、不改变当前阶段。
export type ControlCmd =
  "pause" | "resume" | "stop" | "set_auto" | "align_full" | "patrol";
export type GateDecision =
  | "continue"
  | "modify"
  | "abort"
  | "fix"
  | "audit_now"
  | "finish_book"
  | "finish";

export interface StagePayload {
  run_id: string;
  novel_id: number;
  stage: string;
  status: string;
  note?: string;
}

export interface GatePayload {
  run_id: string;
  novel_id: number;
  stage: string;
  gate_id: string;
  summary: string;
  options: string[];
}

export interface ControlAckPayload {
  run_id: string;
  cmd: string;
  status: string;
}

// 体检结果的一条 issue（与后端 pipeline.AuditIssue 对应）。
// issue 契约单源见后端 internal/pipeline/contract.go（字段名/枚举/语义在此对齐，
// 契约回归测试保证 struct ↔ schema ↔ skill md 三方同步）。
export interface AuditIssue {
  id: string;
  level: string;
  area: string;
  desc: string;
  // issue 三枚举（卷级作用域，可缺省）：target_scope=落点卷作用域（active_volume|frozen_volume|none）、
  // dimension=四维机器归属（character|locus_arc|timeline|reader_pov|none）、
  // direction=修复方向（forward_fix|retcon_candidate）。旧版体检数据可能缺省。
  target_scope?: string;
  dimension?: string;
  direction?: string;
  // 结构化涉及章节（契约 chapter_ids，R1 起）：问题涉及章节的全局阅读序号数组，
  // 修复作用域分组与章节定位依据；台账/全书级问题为 []，旧版数据缺省。
  chapter_ids?: number[];
}

// 体检结果（与后端 pipeline.AuditData 对应）：verdict=pass|needs_fix；
// schema_version=产出时的 issue 契约版本（旧版数据缺省）。
export interface AuditData {
  verdict: string;
  score?: number;
  summary?: string;
  schema_version?: number;
  issues?: AuditIssue[];
}

// S5 修复开始事件（pipeline:fix_start）里的一条修复项，与后端 FixIssuePayload 对应。
// status 是该问题的修复状态：pending（待修复）/ fixing（正在修复）/ fixed（已修复）/
// skipped（agent 上报无法修复，已跳过并记录原因，不再重试）/
// rewrite（正文级，已路由到 S3 章节重写回路，S5 不再处理）。
export interface FixIssuePayload extends AuditIssue {
  // retcon=命中冻结卷写保护/retcon 候选，已拦截自动修复、转人工裁决。
  status?: "pending" | "fixing" | "fixed" | "skipped" | "rewrite" | "retcon";
}

// 回流到 S3 章节重写回路的正文级问题（pipeline:rewrite_backlog / run.ContextJSON.rewrite_backlog），
// 与后端 RewriteItem 对应。作用域三枚举与源 issue 一致，供重写派发的冻结卷写保护判路与前端标签展示。
export interface RewriteItem extends AuditIssue {
  chapters?: number[];
}

// S5 修复进度状态（由 pipeline:fix_start / pipeline:fix_done 维护）。
export interface FixProgressState {
  running: boolean;
  ok: boolean | null;
  error?: string;
  summary?: string;
  issues: FixIssuePayload[];
  // 全量问题清单（含逐项 status）：修复清单按它渲染，每个问题带状态徽章。
  allIssues?: FixIssuePayload[];
  // 分批修复：当前批次序号（1 起）/ 总批数；此前批次已修复的问题 ID 列表。
  batchIndex?: number;
  batchTotal?: number;
  doneIds?: string[];
  // 重写交回：本轮无台账级问题可修、但存在正文级重写清单时置 true，
  // 前端据此展示「已转交章节重写」而非「修复完成」，避免「完成但零修复」的误导。
  rewriteHandoff?: boolean;
  rewriteCount?: number;
  // retcon 交回：问题命中冻结卷写保护/retcon 候选、自动修复被拦截时置 true，
  // 前端据此展示「已转交人工裁决」（冻结卷不自动回改）。
  retconHandoff?: boolean;
  retconCount?: number;
  // 陈旧体检：本 run 的体检结果缺 fix_type 字段（旧版数据不兼容自动重写路由），
  // 后端拒绝派发并交回用户重跑体检。前端据此展示专门提示，避免与普通修复失败混淆。
  stale?: boolean;
}

// S5 修复实时活动流的一条记录（来自 pipeline-agent:1 的 AgentEvent 工具事件）。
export interface FixActivityItem {
  seq: number;
  text: string;
  ok: boolean;
  failed: boolean;
}

// 自动章节重写回路的逐章进度（由 pipeline:rewrite_progress 维护）。
// 重写是「交回 S3 重写」环节全自动派发章节重写 agent 的过程：先逐章重写，再回到复评；
// 该状态让前端展示「正在自动重写第 X/Y 章」与每章结果，避免用户误以为流水线卡死。
export interface RewriteProgressState {
  running: boolean;
  index: number;
  total: number;
  items: RewriteProgressChapter[];
}

// 单章重写结果。
export interface RewriteProgressChapter {
  chapterId: number;
  readingNumber: number;
  phase: "starting" | "done" | "failed";
  error?: string;
}

// 线性阶段顺序。S0 = 导入/续写对齐入口；S4 = 签约体检（signability-audit，只读体检 + issue 清单）；
// S5 = 修复闭环（按 issue 清单改稿 / 同步台账，修复后回到 S4 复评）；S6 = 完本终检；
// GEnd = 完本终检闸门（确认 finish / 修复 fix / 终止 abort）。
export const PIPELINE_STAGES = [
  "S0",
  "S1",
  "G1",
  "S2",
  "S3",
  "G3b",
  "S4",
  "S5",
  "S4b",
  "S6",
  "GEnd",
] as const;
