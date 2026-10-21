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
  RewriteProgressState,
} from "@/types/pipeline";
import type { AgentEvent } from "@/components/chat/types";
import { AgentEventType } from "@/components/chat/types";
import { toastError } from "@/utils/toast";
import { useTranslation } from "react-i18next";

const FIX_ACTIVITY_MAX = 40;

// 订阅后端 pipeline 事件并暴露控制方法给 UI。
// C 期：状态推进由用户在前端显式点「继续/修改/终止」驱动，后端只负责
// 持久化与广播；A 期（PR5）将把顺序逻辑下沉到后端 Orchestrator。
export function usePipeline(novelId: number) {
  const { t } = useTranslation();
  const [run, setRun] = useState<pipeline.PipelineRun | null>(null);
  const [stage, setStage] = useState<string>("");
  const [status, setStatus] = useState<string>("");
  // 后端 stage 事件附带的说明（如「已暂停，点击继续可恢复」「当前阶段由交互推进」），
  // 在向导「当前阶段」下方展示，避免恢复/暂停后界面语义不明。
  const [stageNote, setStageNote] = useState<string>("");
  const [gate, setGate] = useState<GatePayload | null>(null);
  // S5 修复进度：修复清单（fix_start 携带）+ 实时活动流（pipeline-agent:1 工具事件）。
  const [fixProgress, setFixProgress] = useState<FixProgressState>({
    running: false,
    ok: null,
    issues: [],
  });
  const [fixActivity, setFixActivity] = useState<FixActivityItem[]>([]);
  // 自动章节重写回路进度：重写清单指向可定位章节时，后端全自动派发重写 agent，
  // 该状态展示「正在自动重写第 X/Y 章」与每章结果。
  const [rewriteProgress, setRewriteProgress] = useState<RewriteProgressState>({
    running: false,
    index: 0,
    total: 0,
    items: [],
  });

  useEffect(() => {
    const offStage = EventsOn("pipeline:stage", (p: StagePayload) => {
      if (p.novel_id !== novelId) return;
      setStage(p.stage);
      setStatus(p.status);
      setStageNote(p.note ?? "");
    });
    const offGate = EventsOn("pipeline:gate", (p: GatePayload) => {
      if (p.novel_id !== novelId) return;
      setGate(p);
      setStatus("awaiting_gate");
      setStageNote("");
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
    // 正文级问题回流 S3 重写回路：后端沉淀重写清单后广播该事件，重新拉取 run 以拿到 rewrite_backlog。
    const offRewrite = EventsOn(
      "pipeline:rewrite_backlog",
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
        // 新一轮修复批次开始：清除上一轮自动重写的残留进度。
        setRewriteProgress({ running: false, index: 0, total: 0, items: [] });
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
      (p: {
        novel_id: number;
        ok?: boolean;
        error?: string;
        rewrite_handoff?: boolean;
        rewrite_count?: number;
        stale_audit?: boolean;
      }) => {
        if (p.novel_id !== novelId) return;
        setFixProgress((prev) => {
          // 批次失败/交回时，残留的「正在修复」徽标必须复位为「待修复」，
          // 否则暂停态下会出现「修复失败」与「正在修复」并存的矛盾展示。
          const resetFixing = (
            arr?: FixProgressState["issues"],
          ): FixProgressState["issues"] =>
            (arr ?? []).map((it) =>
              it.status === "fixing" ? { ...it, status: "pending" } : it,
            );
          return {
            ...prev,
            running: false,
            ok: p.ok ?? false,
            error: p.error,
            rewriteHandoff: p.rewrite_handoff ?? false,
            rewriteCount: p.rewrite_count ?? 0,
            stale: p.stale_audit ?? false,
            issues: resetFixing(prev.issues),
            allIssues: resetFixing(prev.allIssues),
          };
        });
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
    // 自动章节重写回路的逐章进度（starting/done/failed），展示「正在自动重写第 X/Y 章」。
    const offRewriteProgress = EventsOn(
      "pipeline:rewrite_progress",
      (p: {
        novel_id: number;
        phase: "starting" | "done" | "failed";
        index: number;
        total: number;
        chapter_id: number;
        reading_number: number;
        error?: string;
      }) => {
        if (p.novel_id !== novelId) return;
        setRewriteProgress((prev) => {
          const items = [...prev.items];
          const idx = items.findIndex((it) => it.chapterId === p.chapter_id);
          if (p.phase === "starting") {
            if (idx < 0) {
              items.push({
                chapterId: p.chapter_id,
                readingNumber: p.reading_number,
                phase: "starting",
              });
            } else {
              items[idx] = { ...items[idx], phase: "starting" };
            }
            return {
              running: true,
              index: p.index,
              total: p.total,
              items,
            };
          }
          // done / failed：更新对应章节终态；全部终态后停止 running。
          if (idx >= 0) {
            items[idx] = {
              ...items[idx],
              phase: p.phase,
              error: p.error,
            };
          } else {
            items.push({
              chapterId: p.chapter_id,
              readingNumber: p.reading_number,
              phase: p.phase,
              error: p.error,
            });
          }
          const allSettled = items.every(
            (it) => it.phase === "done" || it.phase === "failed",
          );
          return {
            running: !allSettled,
            index: p.index,
            total: p.total,
            items,
          };
        });
      },
    );

    // 进入时拉取当前活跃流水线，恢复进度。
    GetPipelineState(novelId)
      .then((r: pipeline.PipelineRun) => {
        setRun(r);
        setStage(r.CurrentStage);
        setStatus(r.Status);
        setStageNote("");
      })
      .catch(() => {
        // 无活跃流水线时不报错，等待用户点击「开始」。
      });

    return () => {
      offStage();
      offGate();
      offAck();
      offStock();
      offRewrite();
      offFixStart();
      offFixDone();
      offAgentActivity();
      offRewriteProgress();
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
      return ResumePipeline(run.RunID, decision, feedback)
        .then(() => {
          setGate(null);
        })
        .catch((e: unknown) => {
          // 后端拒绝推进（如已终止/状态不允许）时必须可见，否则按钮「点了没反应」。
          toastError(String(e));
        });
    },
    [run],
  );

  const control = useCallback(
    (cmd: ControlCmd) => {
      if (!run) {
        toastError(t("pipeline.control.noRun"));
        return Promise.resolve();
      }
      return ControlPipeline(run.RunID, cmd)
        .then(() => {
          // 控制指令（含 set_auto 切换自动运行）后重新拉取 run，确保 UI 状态与后端一致。
          return GetPipelineState(novelId)
            .then((r: pipeline.PipelineRun) => {
              setRun(r);
              setStatus(r.Status);
            })
            .catch(() => {});
        })
        .catch((e: unknown) => {
          // 后端拒绝控制指令（如「当前状态 running 无法恢复」）时必须可见。
          toastError(String(e));
        });
    },
    [run, novelId, t],
  );

  const autoRun = run?.AutoRun ?? false;

  return {
    run,
    stage,
    status,
    stageNote,
    gate,
    autoRun,
    fixProgress,
    fixActivity,
    rewriteProgress,
    start,
    resume,
    control,
  };
}
