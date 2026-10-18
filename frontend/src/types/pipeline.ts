// 创作流水线前端类型。字段名与后端 PipelineRun 的 JSON 序列化一致（PascalCase）。

export type PipelineStatus =
  "pending" | "running" | "paused" | "awaiting_gate" | "finished" | "aborted";

export type PipelineMode = "new" | "continue";

export interface PipelineRun {
  ID: number;
  NovelID: number;
  RunID: string;
  Mode: PipelineMode;
  StartChapter: number | null;
  CurrentStage: string;
  Status: PipelineStatus;
  AutoRun: boolean;
  ContextJSON: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export type ControlCmd = "pause" | "resume" | "stop";
export type GateDecision = "continue" | "modify" | "abort";

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

// 线性阶段顺序（C 期 MVP 实际用到 S1→G1→S3；其余为后续 PR 预留）。
export const PIPELINE_STAGES = [
  "S1",
  "G1",
  "S2",
  "S3",
  "G3b",
  "S4a",
  "S5",
  "S4b",
  "S6",
] as const;
