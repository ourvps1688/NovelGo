import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Target, Play, Check, AlertTriangle, Loader2 } from "lucide-react";
import {
  useEnabledSkills,
  useSetEnabledSkills,
} from "@/components/chat/useNovelSkills";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

// 被编排的 manual 技能名（经 SetEnabledSkills 常驻注入 system 上下文）。
const SKILL_NAME = "signability-workflow";

// 仅承载阶段 key；技能标识与 kickoff 提示均经 i18n 注入（避免硬编码中文）。
const STAGES = ["stage0", "stage1", "stage2", "stage3", "stage4", "stage5"];

interface Props {
  novelId: number;
}

export default function SignabilityWorkflowView({ novelId }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);

  const enabledQuery = useEnabledSkills(novelId);
  const setEnabled = useSetEnabledSkills(novelId);
  const modelsQuery = useModels();
  const settingsQuery = useSettings();

  const enabledSet = useMemo(
    () => new Set(enabledQuery.data ?? []),
    [enabledQuery.data],
  );
  const isEnabled = enabledSet.has(SKILL_NAME);

  // 模型是否就绪：selected_model_key 指向的模型仍存在于模型列表。
  const selectedKey = settingsQuery.data?.selected_model_key || "";
  const hasModel = useMemo(
    () => !!modelsQuery.data?.find((m) => m.Key === selectedKey),
    [modelsQuery.data, selectedKey],
  );

  const kickoff = t("signability.kickoff");

  const handleStart = useCallback(async () => {
    if (!novelId) {
      toastError(t("signability.selectNovelFirst"));
      return;
    }
    if (!hasModel) {
      toastError(t("signability.configureModelFirst"));
      return;
    }
    const send = useChatLaunchStore.getState().send;
    if (!send) {
      toastError(t("signability.launchFailed"));
      return;
    }
    setLaunching(true);
    try {
      // 合并式启用：保留本书既有启用清单，仅追加本工作流技能。
      const base = new Set(enabledQuery.data ?? []);
      base.add(SKILL_NAME);
      await setEnabled.mutateAsync([...base]);
      // 技能已常驻注入，发 kickoff 消息驱动 LLM 进入工作流。
      send(kickoff);
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.enableFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, enabledQuery.data, setEnabled, kickoff, t]);

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
          {isEnabled && (
            <span className="inline-flex items-center gap-1 rounded-full bg-primary/10 text-primary border border-primary/30 px-2 py-0.5 text-[11px]">
              <Check className="w-3 h-3" />
              {t("signability.enabledBadge")}
            </span>
          )}
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
