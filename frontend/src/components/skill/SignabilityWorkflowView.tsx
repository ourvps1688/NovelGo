import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Target, Play, AlertTriangle, Loader2 } from "lucide-react";
import { StartPipeline } from "@/lib/wailsjs/go/app/App";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { usePanelStore } from "@/stores/usePanelStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

// 仅承载阶段 key；技能标识与 kickoff 提示均经 i18n 注入（避免硬编码中文）。
const STAGES = ["stage0", "stage1", "stage2", "stage3", "stage4", "stage5"];

interface Props {
  novelId: number;
}

export default function SignabilityWorkflowView({ novelId }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);

  const modelsQuery = useModels();
  const settingsQuery = useSettings();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);

  // 模型是否就绪：selected_model_key 指向的模型仍存在于模型列表。
  const selectedKey = settingsQuery.data?.selected_model_key || "";
  const hasModel = !!modelsQuery.data?.find((m) => m.Key === selectedKey);

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
      await StartPipeline(novelId, "new", null);
      setActivePanel("pipeline");
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, setActivePanel, t]);

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
        <div className="rounded-lg border bg-muted/30 p-3 mb-4 text-[13px] text-muted-foreground max-w-3xl">
          {t("signability.genreConstraint")}
        </div>

        <div className="space-y-3 max-w-3xl">
          {STAGES.map((key, idx) => (
            <div key={key} className="flex gap-3 rounded-lg border bg-card p-3">
              <div className="shrink-0 w-7 h-7 rounded-full bg-primary/10 text-primary flex items-center justify-center text-sm font-semibold">
                {idx}
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-2">
                  <h2 className="text-sm font-semibold">
                    {t(`signability.${key}Title`)}
                  </h2>
                  <code className="shrink-0 text-[11px] text-muted-foreground font-mono">
                    {t(`signability.${key}Skill`)}
                  </code>
                </div>
                <p className="mt-1 text-[12px] text-muted-foreground">
                  <span className="text-foreground/80">
                    {t("signability.goalLabel")}
                  </span>
                  {t(`signability.${key}Goal`)}
                </p>
                <p className="mt-0.5 text-[12px] text-muted-foreground">
                  <span className="text-foreground/80">
                    {t("signability.doneLabel")}
                  </span>
                  {t(`signability.${key}Done`)}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>

      <div className="px-6 py-4 border-t shrink-0">
        {!hasModel && !noNovel && (
          <div className="flex items-center gap-2 mb-2 text-[12px] text-destructive">
            <AlertTriangle className="w-3.5 h-3.5" />
            {t("signability.configureModelFirst")}
          </div>
        )}
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
      </div>
    </div>
  );
}
