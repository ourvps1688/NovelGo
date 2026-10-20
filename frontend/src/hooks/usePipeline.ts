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
  FixProgressState,
  FixActivityItem,
} from "@/types/pipeline";
import type { AgentEvent } from "@/components/chat/types";
import { AgentEventType } from "@/components/chat/types";

const FIX_ACTIVITY_MAX = 40;

// 订阅后端 pipeline 事件并暴露控制方法给 UI。
// C 期：状态推进由用户在前端显式点「继续/修改/终止」驱动，后端只负责
// 持久化与广播；A 期（PR5）将把顺序逻辑下沉到后端 Orchestrator。
export function usePipeline(novelId: number) {
  const [run, setRun] = useState<pipeline.PipelineRun | null>(null);
  const [stage, setStage] = useState<string>("");
  const [status, setStatus] = useState<string>("");
  const [gate, setGate] = useState<GatePayload | null>(null);
  // S5 修复进度：修复清单（fix_start 携带）+ 实时活动流（pipeline-agent:1 工具事件）。
  const [fixProgress, setFixProgress] = useState<FixProgressState>({
    running: false,
    ok: null,
    issues: [],
  });
  const [fixActivity, setFixActivity] = useState<FixActivityItem[]>([]);

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
      // 闸门携带最新阶段状态；重新拉取 run 以拿到最新 ContextJSON
      // （S2 蓝图完成后 Blueprint 字段落在此处，需刷新才能渲染卷结构）。
      GetPipelineState(novelId)
        .then((r: pipeline.PipelineRun) => {
          setRun(r);
          setStage(r.CurrentStage);
        })
        .catch(() => {});
    });
    const offAck = EventsOn("pipeline:control:ack", (p: ControlAckPayload) => {
      setStatus(p.status);
    });
    // 全量对齐体检完成后后端广播该事件，重新拉取 run 以拿到 stock_audit 结果。
    const offStock = EventsOn(
      "pipeline:stock_audit",
      (p: { novel_id: number }) => {
        if (p.novel_id !== novelId) return;
        GetPipelineState(novelId)
          .then((r: pipeline.PipelineRun) => {
            setRun(r);
            setStage(r.CurrentStage);
          })
          .catch(() => {});
      },
    );
    // S5 修复开始（每批一次）：携带本批 issue 清单 + 全量问题状态清单 + 分批进度，
    // 清空上一批活动流。
    const offFixStart = EventsOn(
      "pipeline:fix_start",
      (p: {
        novel_id: number;
        issues?: FixProgressState["issues"];
        all_issues?: FixProgressState["issues"];
        summary?: string;
        batch_index?: number;
        batch_total?: number;
        done_ids?: string[];
      }) => {
        if (p.novel_id !== novelId) return;
        setFixActivity([]);
        setFixProgress({
          running: true,
          ok: null,
          summary: p.summary,
          issues: p.issues ?? [],
          allIssues: p.all_issues ?? [],
          batchIndex: p.batch_index,
          batchTotal: p.batch_total,
          doneIds: p.done_ids ?? [],
        });
      },
    );
    // S5 修复结束：展示结果（修复完成 → 自动复评；失败 → 展示错误 + 恢复提示）。
    const offFixDone = EventsOn(
      "pipeline:fix_done",
      (p: { novel_id: number; ok?: boolean; error?: string }) => {
        if (p.novel_id !== novelId) return;
        setFixProgress((prev) => ({
          ...prev,
          running: false,
          ok: p.ok ?? false,
          error: p.error,
        }));
      },
    );
    // pipeline 会话的 agent 活动流（工具调用完成/失败），驱动 S5 修复实时进度。
    const offAgentActivity = EventsOn("pipeline-agent:1", (ev: AgentEvent) => {
      if (ev.type !== AgentEventType.ToolCall) return;
      // 只记录「执行完成 / 失败」两个终态，跳过 selected/executing 预告，避免刷屏。
      if (ev.phase !== "completed" && ev.phase !== "failed") return;
      if (!ev.display_text) return;
      setFixActivity((prev) => {
        const next = [
          ...prev,
          {
            seq: ev.seq ?? 0,
            text: ev.display_text ?? "",
            ok: ev.success ?? true,
            failed: ev.phase === "failed",
          },
        ];
        return next.slice(-FIX_ACTIVITY_MAX);
      });
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
      offStock();
      offFixStart();
      offFixDone();
      offAgentActivity();
    };
  }, [novelId]);

  const start = useCallback(
    (
      mode: string = "new",
      startChapter: number | null = null,
      autoRun = false,
    ) => {
      return StartPipeline(novelId, mode, startChapter, autoRun).then(
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
      return ControlPipeline(run.RunID, cmd).then(() => {
        // 控制指令（含 set_auto 切换自动运行）后重新拉取 run，确保 UI 状态与后端一致。
        return GetPipelineState(novelId)
          .then((r: pipeline.PipelineRun) => {
            setRun(r);
            setStatus(r.Status);
          })
          .catch(() => {});
      });
    },
    [run, novelId],
  );

  const autoRun = run?.AutoRun ?? false;

  return {
    run,
    stage,
    status,
    gate,
    autoRun,
    fixProgress,
    fixActivity,
    start,
    resume,
    control,
  };
}
