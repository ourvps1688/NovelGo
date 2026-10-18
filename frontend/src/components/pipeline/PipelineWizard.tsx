import { useTranslation } from "react-i18next";
import { ArrowRight, Play } from "lucide-react";
import { usePipeline } from "@/hooks/usePipeline";
import { PIPELINE_STAGES } from "@/types/pipeline";
import PipelineGateCard from "./PipelineGateCard";
import BlueprintWorkflowView from "./BlueprintWorkflowView";
import SignabilityAuditView from "./SignabilityAuditView";
import PipelineControlBar from "./PipelineControlBar";

interface Props {
  novelId: number;
}

// C 期向导：线性阶段进度条 + 运行控制 + 闸门确认。
// 实际创作（孵化 / 逐章写作）仍走现有 UI，向导只负责可见进度与「仅点继续」的闭环。
export default function PipelineWizard({ novelId }: Props) {
  const { t } = useTranslation();
  const { run, stage, status, gate, start, resume, control } =
    usePipeline(novelId);

  const activeIndex = PIPELINE_STAGES.indexOf(
    stage as (typeof PIPELINE_STAGES)[number],
  );

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-1 border-b px-4 py-2">
        {PIPELINE_STAGES.map((s, i) => (
          <div
            key={s}
            className={`rounded px-2 py-1 text-xs ${
              i === activeIndex
                ? "bg-primary text-primary-foreground"
                : "bg-muted text-muted-foreground"
            }`}
          >
            {t(`pipeline.stage.${s}`)}
          </div>
        ))}
        <div className="ml-auto">
          <PipelineControlBar status={status} onControl={control} />
        </div>
      </div>

      <div className="flex-1 overflow-auto p-4">
        {!run ? (
          <div className="flex flex-col items-start gap-3">
            <p className="text-sm text-muted-foreground">
              {t("pipeline.empty")}
            </p>
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-primary-foreground text-sm"
              onClick={() => start("new", null)}
            >
              <Play className="w-4 h-4" />
              {t("pipeline.start")}
            </button>
          </div>
        ) : null}

        {run && status !== "finished" && status !== "aborted" ? (
          <div className="flex flex-col gap-3">
            <p className="text-sm text-muted-foreground">
              {t("pipeline.currentStage", { stage })}
            </p>

            {stage === "S2" ? (
              <BlueprintWorkflowView
                run={run}
                stage={stage}
                status={status}
                gate={gate}
                onResume={resume}
              />
            ) : stage === "S4" || stage === "S5" ? (
              <SignabilityAuditView
                run={run}
                stage={stage}
                status={status}
                gate={gate}
                onResume={resume}
              />
            ) : (
              <>
                {gate ? (
                  <PipelineGateCard gate={gate} onResume={resume} />
                ) : null}
                {!gate && stage === "S1" ? (
                  <div className="flex flex-col gap-3">
                    <p className="text-sm text-muted-foreground">
                      {t("pipeline.hint.s1")}
                    </p>
                    <button
                      type="button"
                      className="inline-flex w-fit items-center gap-1 rounded bg-primary px-3 py-1.5 text-primary-foreground text-sm"
                      onClick={() => resume("continue")}
                    >
                      <ArrowRight className="h-4 w-4" />
                      {t("pipeline.continue")}
                    </button>
                  </div>
                ) : null}
                {!gate && stage === "S3" ? (
                  <p className="text-sm text-muted-foreground">
                    {t("pipeline.hint.s3")}
                  </p>
                ) : null}
              </>
            )}
          </div>
        ) : null}

        {status === "finished" || status === "aborted" ? (
          <p className="text-sm text-muted-foreground">
            {t(`pipeline.ended.${status}`)}
          </p>
        ) : null}
      </div>
    </div>
  );
}
