import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight, CheckCircle, Play, ScanSearch } from "lucide-react";
import { usePipeline } from "@/hooks/usePipeline";
import PipelineGateCard from "./PipelineGateCard";
import BlueprintWorkflowView from "./BlueprintWorkflowView";
import SignabilityAuditView, { StockAuditPanel } from "./SignabilityAuditView";
import PipelineControlBar from "./PipelineControlBar";

interface Props {
  novelId: number;
}

// C 期向导：运行控制 + 阶段内容 + 闸门确认。
// 阶段进度条已上提到 WorkspaceView 顶部全局条，避免重复并方便跨面板查看。
// 实际创作（孵化 / 逐章写作）仍走现有 UI，向导只负责可见进度与「仅点继续」的闭环。
export default function PipelineWizard({ novelId }: Props) {
  const { t } = useTranslation();
  const { run, stage, status, gate, autoRun, start, resume, control } =
    usePipeline(novelId);

  // 解析「全量对齐」触发的全本现状体检结果（存于 run.ContextJSON.stock_audit）。
  const stockAudit = useMemo(() => {
    if (!run?.ContextJSON) return null;
    try {
      const c = JSON.parse(run.ContextJSON);
      const raw = c.stock_audit;
      if (!raw) return null;
      return typeof raw === "string" ? JSON.parse(raw) : raw;
    } catch {
      return null;
    }
  }, [run]);

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-end border-b px-4 py-2">
        <PipelineControlBar
          status={status}
          autoRun={autoRun}
          onControl={control}
        />
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
            ) : stage === "S4" ||
              stage === "S4b" ||
              stage === "S5" ||
              stage === "S6" ? (
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
                  <div className="flex flex-col gap-3">
                    <p className="text-sm text-muted-foreground">
                      {t("pipeline.hint.s3")}
                    </p>
                    <div className="flex flex-wrap gap-2">
                      <button
                        type="button"
                        className="inline-flex w-fit items-center gap-1 rounded bg-primary px-3 py-1.5 text-primary-foreground text-sm"
                        onClick={() => resume("continue")}
                      >
                        <ArrowRight className="h-4 w-4" />
                        {t("pipeline.checkProgress")}
                      </button>
                      <button
                        type="button"
                        className="inline-flex w-fit items-center gap-1 rounded border px-3 py-1.5 text-sm"
                        title={t("pipeline.gate.finishBookHint")}
                        onClick={() => resume("finish_book")}
                      >
                        <CheckCircle className="h-4 w-4" />
                        {t("pipeline.gate.finishBook")}
                      </button>
                      <button
                        type="button"
                        className="inline-flex w-fit items-center gap-1 rounded border px-3 py-1.5 text-sm"
                        title={t("pipeline.alignFullHint")}
                        onClick={() => control("align_full")}
                      >
                        <ScanSearch className="h-4 w-4" />
                        {t("pipeline.alignFull")}
                      </button>
                    </div>
                  </div>
                ) : null}
              </>
            )}

            {stockAudit ? <StockAuditPanel data={stockAudit} /> : null}
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
