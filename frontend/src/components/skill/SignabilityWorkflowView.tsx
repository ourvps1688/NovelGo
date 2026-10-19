import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Target, Play, ArrowRight, AlertTriangle, Loader2 } from "lucide-react";
import { StartPipeline, GetPipelineState } from "@/lib/wailsjs/go/app/App";
import { pipeline } from "@/lib/wailsjs/go/models";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { usePanelStore } from "@/stores/usePanelStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

interface Props {
  novelId: number;
}

export default function SignabilityWorkflowView({ novelId }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);
  const [activeRun, setActiveRun] = useState<pipeline.PipelineRun | null>(null);

  const modelsQuery = useModels();
  const settingsQuery = useSettings();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);

  // 模型是否就绪：selected_model_key 指向的模型仍存在于模型列表。
  const selectedKey = settingsQuery.data?.selected_model_key || "";
  const hasModel = !!modelsQuery.data?.find((m) => m.Key === selectedKey);

  // 检测当前小说是否已有运行中的 pipeline，以便显示「继续」而非「开始」。
  useEffect(() => {
    if (!novelId) {
      setActiveRun(null);
      return;
    }
    GetPipelineState(novelId)
      .then((r) => {
        if (r.Status === "finished" || r.Status === "aborted") {
          setActiveRun(null);
        } else {
          setActiveRun(r);
        }
      })
      .catch(() => setActiveRun(null));
  }, [novelId]);

  const handleStart = useCallback(async () => {
    if (!novelId) {
      toastError(t("signability.selectNovelFirst"));
      return;
    }
    if (!hasModel) {
      toastError(t("signability.configureModelFirst"));
      return;
    }
    setLaunching(true);
    try {
      // PR3 后签约潜力工作流由 pipeline 状态机驱动，不再走 chat skill。
      await StartPipeline(novelId, "new", null, false);
      setActivePanel("pipeline");
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, setActivePanel, t]);

  const handleContinue = useCallback(() => {
    setActivePanel("pipeline");
  }, [setActivePanel]);

  const noNovel = !novelId;
  const buttonDisabled = launching || noNovel || !hasModel;
  const buttonTitle = noNovel
    ? t("signability.selectNovelFirst")
    : !hasModel
      ? t("signability.configureModelFirst")
      : "";

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <div className="px-6 py-4 border-b shrink-0">
        <div className="flex items-center gap-2">
          <Target className="w-5 h-5 text-primary" />
          <h1 className="text-lg font-semibold">{t("signability.title")}</h1>
        </div>
        <p className="text-sm text-muted-foreground mt-1 max-w-3xl">
          {t("signability.subtitle")}
        </p>
      </div>

      <div className="flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        <div className="rounded-lg border bg-muted/30 p-3 text-[13px] text-muted-foreground max-w-3xl">
          {t("signability.genreConstraint")}
        </div>
      </div>

      <div className="px-6 py-4 border-t shrink-0">
        {!hasModel && !noNovel && (
          <div className="flex items-center gap-2 mb-2 text-[12px] text-destructive">
            <AlertTriangle className="w-3.5 h-3.5" />
            {t("signability.configureModelFirst")}
          </div>
        )}
        {activeRun ? (
          <div className="flex flex-col gap-1">
            <p className="text-xs text-muted-foreground">
              {t("signability.continueHint", {
                stage: t(`pipeline.stage.${activeRun.CurrentStage}`),
              })}
            </p>
            <button
              type="button"
              disabled={buttonDisabled}
              title={buttonTitle}
              onClick={handleContinue}
              className="inline-flex w-fit items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <ArrowRight className="w-4 h-4" />
              {t("signability.continueButton")}
            </button>
          </div>
        ) : (
          <button
            type="button"
            disabled={buttonDisabled}
            title={buttonTitle}
            onClick={handleStart}
            className="inline-flex items-center gap-2 rounded-lg bg-primary px-4 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {launching ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Play className="w-4 h-4" />
            )}
            {t("signability.startButton")}
          </button>
        )}
      </div>
    </div>
  );
}
