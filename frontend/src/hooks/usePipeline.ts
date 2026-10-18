import { useCallback, useEffect, useState } from "react";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import {
  StartPipeline,
  GetPipelineState,
  ResumePipeline,
  ControlPipeline,
} from "@/lib/wailsjs/go/app/App";
import { pipeline } from "@/lib/wailsjs/go/models";
import type {
  StagePayload,
  GatePayload,
  ControlAckPayload,
  ControlCmd,
  GateDecision,
} from "@/types/pipeline";

// 订阅后端 pipeline 事件并暴露控制方法给 UI。
// C 期：状态推进由用户在前端显式点「继续/修改/终止」驱动，后端只负责
// 持久化与广播；A 期（PR5）将把顺序逻辑下沉到后端 Orchestrator。
export function usePipeline(novelId: number) {
  const [run, setRun] = useState<pipeline.PipelineRun | null>(null);
  const [stage, setStage] = useState<string>("");
  const [status, setStatus] = useState<string>("");
  const [gate, setGate] = useState<GatePayload | null>(null);

  useEffect(() => {
    const offStage = EventsOn("pipeline:stage", (p: StagePayload) => {
      if (p.novel_id !== novelId) return;
      setStage(p.stage);
      setStatus(p.status);
    });
    const offGate = EventsOn("pipeline:gate", (p: GatePayload) => {
      if (p.novel_id !== novelId) return;
      setGate(p);
      setStatus("awaiting_gate");
    });
    const offAck = EventsOn("pipeline:control:ack", (p: ControlAckPayload) => {
      setStatus(p.status);
    });

    // 进入时拉取当前活跃流水线，恢复进度。
    GetPipelineState(novelId)
      .then((r: pipeline.PipelineRun) => {
        setRun(r);
        setStage(r.CurrentStage);
        setStatus(r.Status);
      })
      .catch(() => {
        // 无活跃流水线时不报错，等待用户点击「开始」。
      });

    return () => {
      offStage();
      offGate();
      offAck();
    };
  }, [novelId]);

  const start = useCallback(
    (mode: string = "new", startChapter: number | null = null) => {
      return StartPipeline(novelId, mode, startChapter).then(
        (r: pipeline.PipelineRun) => {
          setRun(r);
          setStage(r.CurrentStage);
          setStatus(r.Status);
          return r;
        },
      );
    },
    [novelId],
  );

  const resume = useCallback(
    (decision: GateDecision, feedback = "") => {
      if (!run) return Promise.reject(new Error("no run"));
      return ResumePipeline(run.RunID, decision, feedback).then(() => {
        setGate(null);
      });
    },
    [run],
  );

  const control = useCallback(
    (cmd: ControlCmd) => {
      if (!run) return Promise.reject(new Error("no run"));
      return ControlPipeline(run.RunID, cmd);
    },
    [run],
  );

  return { run, stage, status, gate, start, resume, control };
}
