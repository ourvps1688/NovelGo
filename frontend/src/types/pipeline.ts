// 创作流水线前端类型。PipelineRun 直接使用 wails 生成的 pipeline.PipelineRun，
// 避免手写类型与生成类型不一致导致 TS2345。

export type PipelineStatus =
  "pending" | "running" | "paused" | "awaiting_gate" | "finished" | "aborted";

export type PipelineMode = "new" | "continue";

export type ControlCmd =
  "pause" | "resume" | "stop" | "set_auto" | "align_full";
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
