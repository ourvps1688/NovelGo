import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Target, Play, AlertTriangle, Loader2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ListDetectionTaskStatuses,
  RunDetectionTask,
} from "@/lib/wailsjs/go/app/App";
import { detectionKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { usePanelStore } from "@/stores/usePanelStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

interface Props {
  novelId: number;
}

// 签约体检入口（R5 检测核心收口，#260）：不再走旧流水线状态机，直接以检测任务
// 独立启动「全书签约体检」（RunDetectionTask(novelId, "book", null)），完成后跳到
// 检测核心面板（pipeline）查看工单与汇总视图。检测任务零依赖流水线阶段、可随时
// 单独发起（契约 0.1）；修复由人工在 IssueTicketsPanel 触发（零自动派发）。
export default function SignabilityWorkflowView({ novelId }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);
  const modelsQuery = useModels();
  const settingsQuery = useSettings();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  const queryClient = useQueryClient();

  // 模型是否就绪：selected_model_key 指向的模型仍存在于模型列表。
  const selectedKey = settingsQuery.data?.selected_model_key || "";
  const hasModel = !!modelsQuery.data?.find((m) => m.Key === selectedKey);

  // 全书签约体检（book 检测）进行态：进行中禁用按钮 + 提示，收口即停。
  const { data: statuses } = useQuery({
    queryKey: detectionKeys.list(novelId),
    queryFn: async () => (await ListDetectionTaskStatuses(novelId)) ?? [],
    enabled: !!novelId,
    refetchInterval: (q) =>
      (q.state.data ?? []).some((s) => s.kind === "book" && s.running)
        ? 3000
        : false,
  });
  const bookRunning = !!statuses?.find((s) => s.kind === "book" && s.running);

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
      // 检测任务启动埋点（R5-3 T2.1，单源 action.detect）。
      trackFeature(ACTION_FEATURE_KEYS.detect);
      await RunDetectionTask(novelId, "book", null);
      queryClient.invalidateQueries({
        queryKey: detectionKeys.list(novelId),
      });
      // 跳到检测核心面板：结果以工单/汇总视图呈现，修复由人工触发。
      setActivePanel("pipeline");
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, setActivePanel, queryClient, t]);

  const noNovel = !novelId;
  const buttonDisabled = launching || noNovel || !hasModel || bookRunning;
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
        {bookRunning ? (
          <p className="flex items-center gap-1.5 text-xs text-primary/80">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            {t("signability.auditing")}
          </p>
        ) : null}
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
