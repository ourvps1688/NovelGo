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

// 顶部全局阶段条：无论当前在哪个面板，都显示运行中流水线的阶段进度，
// 点击即可跳回流水线面板。
export default function PipelineStageBar({ novelId, onClick }: Props) {
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

  return (
    <div
      className="h-9 flex items-center gap-1 border-b bg-sidebar px-3 cursor-pointer hover:bg-accent/50 transition-colors shrink-0"
      onClick={onClick}
      title={t("pipeline.stageBarTitle")}
    >
      <Workflow className="w-4 h-4 text-primary mr-1" />
      {PIPELINE_STAGES.map((s, i) => (
        <div
          key={s}
          className={`rounded px-2 py-0.5 text-[11px] whitespace-nowrap ${
            i === activeIndex
              ? "bg-primary text-primary-foreground"
              : i < activeIndex && activeIndex !== -1
                ? "bg-muted text-muted-foreground"
                : "text-muted-foreground"
          }`}
        >
          {t(`pipeline.stage.${s}`)}
        </div>
      ))}
      <span className="ml-auto text-[11px] text-muted-foreground">
        {run.Status === "awaiting_gate"
          ? t("pipeline.awaitingGate")
          : t("pipeline.running")}
      </span>
    </div>
  );
}
