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

// 右侧边栏内的垂直阶段轨：占用空间少，适合在 SidePanel 顶部常驻展示。
// 点击后跳回流水线面板。
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

  return (
    <div
      className="px-3 py-2.5 border-b bg-sidebar cursor-pointer hover:bg-accent/30 transition-colors shrink-0"
      onClick={onClick}
      title={t("pipeline.stageBarTitle")}
    >
      <div className="flex items-center gap-1.5 mb-2">
        <Workflow className="w-3.5 h-3.5 text-primary" />
        <span className="text-[11px] font-medium text-muted-foreground">
          {t("pipeline.stageBarTitle")}
        </span>
      </div>
      <div className="space-y-1">
        {PIPELINE_STAGES.map((s, i) => {
          const isActive = i === activeIndex;
          const isDone = activeIndex !== -1 && i < activeIndex;
          return (
            <div
              key={s}
              className={`flex items-center gap-2 text-[11px] leading-tight ${
                isActive
                  ? "text-primary font-medium"
                  : isDone
                    ? "text-muted-foreground"
                    : "text-muted-foreground/50"
              }`}
            >
              <span
                className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                  isActive
                    ? "bg-primary"
                    : isDone
                      ? "bg-muted-foreground"
                      : "bg-muted-foreground/30"
                }`}
              />
              <span className="truncate">{t(`pipeline.stage.${s}`)}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
