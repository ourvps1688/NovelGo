// 检测核心前端类型。AuditData/AuditIssue 对应后端 pipeline 的体检产出，
// IssueTicketRef/IssueVerifySummary 对应工单引用与批量复验小结。
// 旧「创作流水线」相关类型（PipelineStatus/PipelineMode/ControlCmd/GateDecision/
// StagePayload/GatePayload/ControlAckPayload/FixIssuePayload/RewriteItem/
// IssueTicketStatus/FixProgressState/FixActivityItem）已随 #259 重构删除，
// 避免死类型与生成类型漂移。

// 体检结果的一条 issue（与后端 pipeline.AuditIssue 对应）。
// issue 契约单源见后端 internal/skill/builtin/signability-audit.md（字段名/枚举/语义在此对齐，
// 契约回归测试保证 struct ↔ schema ↔ skill md 三方同步）。
export interface AuditIssue {
  id: string;
  level: string;
  area: string;
  desc: string;
  // 修复出口类型（契约 signability-audit.md 强制）：body=S3 章节重写 / ledger=自动修复。
  // 旧版体检数据可能缺省，解析层保留该字段以对齐契约单源。
  fix_type?: string;
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
