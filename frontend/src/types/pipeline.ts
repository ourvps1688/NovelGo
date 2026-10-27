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
// R5-1 起该清单是工单表的兼容投影（事实源=pipeline_issues，驳回/复验收口即时出清单）。
export interface RewriteItem extends AuditIssue {
  chapters?: number[];
}

// 发现项工单状态（R5-1，与后端 internal/pipeline/issue.go 状态机对齐）。
// 生命周期：发现(pending) → 确认(confirmed) →（人工）修复中(fixing) → 复验通过(verified)/
// 驳回(dismissed)；verified/dismissed 为终态无出边，→verified 只经复验回执落表。
// 流水线路由态（fixed/skipped/rewrite/retcon）兼容读，各留工单收口出口。
export type IssueTicketStatus =
  | "pending"
  | "confirmed"
  | "fixing"
  | "fixed"
  | "verified"
  | "dismissed"
  | "skipped"
  | "rewrite"
  | "retcon";

// 工单引用（run 内稳定键 issue_key + run_id），与后端 pipeline.IssueTicketRef 对应。
export interface IssueTicketRef {
  run_id: string;
  issue_key: string;
}

// 工单批量复验小结（与后端 pipeline.IssueVerifySummary 对应）：
// confirmed=复验通过（verified）、rejected=未通过退回待修（pending）、unverified=跳过（终态等）。
export interface IssueVerifySummary {
  confirmed: number;
  rejected: number;
  unverified: number;
  detail?: string;
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

// 侧栏/顶部阶段条仅追踪**写作阶段**进度（S0 对齐入口 → S3 逐章写作 → G3b 里程碑）。
// R5 检测核心已将「检测 / 修复闭环 / 完本终检」从流水线解耦为独立检测任务
// （四 kind 独立启动 + 工单人工修复），故阶段条不再含 S4/S5/S4b/S6/GEnd
// （旧「修复闭环」等阶段不再作为流水线阶段渲染，消除图3 不合理展示）。
export const PIPELINE_STAGES = ["S0", "S1", "G1", "S2", "S3", "G3b"] as const;
