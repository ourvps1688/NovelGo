import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Workflow } from "lucide-react";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import { GetPipelineState } from "@/lib/wailsjs/go/app/App";
import { pipeline } from "@/lib/wailsjs/go/models";
import { PIPELINE_STAGES } from "@/types/pipeline";
import type { StagePayload, GatePayload } from "@/types/pipeline";

interface Props {
  novelId: number;
  onClick: () => void;
}

// 右侧边栏内的迷你流水线进度：只显示当前阶段 + 一条分段进度条，
// 避免在窄边栏里堆叠 9 行文字破坏整体 UI。
export default function PipelineStageRail({ novelId, onClick }: Props) {
  const { t } = useTranslation();
  const [run, setRun] = useState<pipeline.PipelineRun | null>(null);

  useEffect(() => {
    if (!novelId) {
      setRun(null);
      return;
    }

    const offStage = EventsOn("pipeline:stage", (p: StagePayload) => {
      if (p.novel_id !== novelId) return;
      setRun((prev) =>
        prev
          ? pipeline.PipelineRun.createFrom({
              ...prev,
              CurrentStage: p.stage,
              Status: p.status,
            })
          : pipeline.PipelineRun.createFrom({
              CurrentStage: p.stage,
              Status: p.status,
            }),
      );
    });

    const offGate = EventsOn("pipeline:gate", (p: GatePayload) => {
      if (p.novel_id !== novelId) return;
      GetPipelineState(novelId)
        .then((r) => setRun(r))
        .catch(() => setRun(null));
    });

    GetPipelineState(novelId)
      .then((r) => {
        if (r.Status === "finished" || r.Status === "aborted") {
          setRun(null);
        } else {
          setRun(r);
        }
      })
      .catch(() => setRun(null));

    return () => {
      offStage();
      offGate();
    };
  }, [novelId]);

  if (!run) return null;

  const activeIndex = PIPELINE_STAGES.indexOf(
    run.CurrentStage as (typeof PIPELINE_STAGES)[number],
  );
  const currentStage =
    activeIndex >= 0 ? PIPELINE_STAGES[activeIndex] : run.CurrentStage;

  return (
    <div className="px-3 py-2.5 border-b bg-sidebar shrink-0">
      <button
        type="button"
        onClick={onClick}
        title={t("pipeline.stageBarTitle")}
        className="w-full text-left group cursor-pointer"
      >
        <div className="flex items-center justify-between gap-2 mb-1.5">
          <div className="flex items-center gap-1.5 min-w-0">
            <Workflow className="w-3.5 h-3.5 text-primary shrink-0" />
            <span className="text-xs font-medium text-foreground truncate">
              {t("pipeline.stageBarTitle")}
            </span>
          </div>
          <span
            className={`text-[10px] px-1.5 py-0.5 rounded-full shrink-0 ${
              run.Status === "awaiting_gate"
                ? "bg-warning/15 text-warning-foreground"
                : "bg-primary/10 text-primary"
            }`}
          >
            {run.Status === "awaiting_gate"
              ? t("pipeline.awaitingGate")
              : t("pipeline.running")}
          </span>
        </div>

        <p className="text-[11px] text-muted-foreground mb-1.5 truncate">
          {t("pipeline.currentStage", {
            stage: t(`pipeline.stage.${currentStage}`),
          })}
        </p>

        <div className="flex items-center gap-[3px]" role="progressbar">
          {PIPELINE_STAGES.map((s, i) => {
            const isActive = i === activeIndex;
            const isDone = activeIndex !== -1 && i < activeIndex;
            return (
              <div
                key={s}
                title={t(`pipeline.stage.${s}`)}
                className={`flex-1 h-1.5 rounded-full transition-colors ${
                  isActive
                    ? "bg-primary"
                    : isDone
                      ? "bg-muted-foreground"
                      : "bg-muted"
                }`}
              />
            );
          })}
        </div>
      </button>
    </div>
  );
}
