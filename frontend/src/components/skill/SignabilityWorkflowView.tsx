import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Target, Play, AlertTriangle, Loader2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ListDetectionTaskStatuses,
  ListIssueTickets,
  RunDetectionTask,
} from "@/lib/wailsjs/go/app/App";
import { detectionKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { usePanelStore } from "@/stores/usePanelStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import type { pipeline } from "@/lib/wailsjs/go/models";

interface Props {
  novelId: number;
}

// verdict → i18n key（book=pass|needs_fix|error）。未知值如实显示原值。
const VERDICT_KEYS: Record<string, string> = {
  pass: "journey.quality.detect.verdict.pass",
  needs_fix: "journey.quality.detect.verdict.needs_fix",
  error: "journey.quality.detect.verdict.error",
};

// 工单状态徽章配色（与 IssueTicketsPanel 同口径）：内联评估只读展示用。
const STATUS_CLS: Record<string, string> = {
  pending: "bg-warning/10 text-warning-foreground",
  confirmed: "bg-primary/10 text-primary",
  fixing: "bg-primary/10 text-primary",
  fixed: "bg-primary/10 text-primary",
  verified: "bg-success/10 text-success-foreground",
  dismissed: "bg-muted text-muted-foreground",
  skipped: "bg-muted text-muted-foreground",
  rewrite: "bg-warning/10 text-warning-foreground",
  retcon: "bg-warning/10 text-warning-foreground",
};

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// 签约潜力评估八维口径（与 signability-audit 技能 v2 评分维度逐字一致）：
// 开篇过签力/题材契合/人设卖点/节奏爽点/设定自洽/长线牵引/平台合规/读者视角。
const SIGN_DIMENSIONS = [
  "opening",
  "fit",
  "character",
  "pacing",
  "world",
  "longArc",
  "compliance",
  "reader",
] as const;

// 签约体检入口（R5 检测核心收口，#260）：不再走旧流水线状态机，直接以检测任务
// 独立启动「全书签约体检」（RunDetectionTask(novelId, "book", null)）。完成后**就地
// 内联展示签约潜力评估与发现概览**（不再强制跳到共享「检测核心」页），修复由人工
// 在工单面板触发（零自动派发）。检测任务零依赖流水线阶段、可随时单独发起（契约 0.1）。
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
  const bookSt = statuses?.find((s) => s.kind === "book");
  const bookRunning = !!bookSt?.running;

  // 就地内联评估数据源：全书签约体检落发现项工单（按 run_id 取最近一次 book 结果）。
  const { data: allIssues } = useQuery({
    queryKey: ["signability-inline-issues", novelId],
    queryFn: async () => (await ListIssueTickets(novelId)) ?? [],
    enabled: !!novelId && !!bookSt?.last_run_at,
  });
  const inlineIssues: pipeline.PipelineIssue[] = useMemo(() => {
    const runId = bookSt?.last_run_id ?? "";
    if (!runId) return [];
    return (allIssues ?? []).filter((r) => r.run_id === runId);
  }, [allIssues, bookSt?.last_run_id]);

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
      // 不再强制跳转到共享「检测核心」页：签约潜力评估与发现概览在本页就地展示。
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, queryClient, t]);

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

        {bookSt?.last_run_at ? (
          <div className="mt-4 rounded-lg border p-3 max-w-3xl">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium text-foreground">
                {t("signability.evaluation")}
              </span>
              {bookSt.last_verdict ? (
                <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
                  {VERDICT_KEYS[bookSt.last_verdict]
                    ? t(VERDICT_KEYS[bookSt.last_verdict])
                    : bookSt.last_verdict}
                </span>
              ) : null}
            </div>

            {/* 签约潜力评估核心区：评分 + 结论 + 八维口径（与巡检张力报告明确区分） */}
            {typeof bookSt.last_score === "number" ? (
              <div className="mt-2 flex items-baseline gap-1.5">
                <span className="text-xs text-muted-foreground">
                  {t("signability.scoreLabel")}
                </span>
                <span className="text-lg font-semibold leading-none text-foreground">
                  {bookSt.last_score}
                  <span className="text-xs font-normal text-muted-foreground">
                    /40
                  </span>
                </span>
              </div>
            ) : null}
            {bookSt.last_summary ? (
              <p className="mt-1.5 text-sm leading-relaxed text-foreground/90">
                {bookSt.last_summary}
              </p>
            ) : null}
            <div className="mt-3">
              <p className="text-xs font-medium text-foreground">
                {t("signability.dimensionTitle")}
              </p>
              <ul className="mt-1 grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-2">
                {SIGN_DIMENSIONS.map((d) => (
                  <li
                    key={d}
                    className="flex items-center gap-1.5 text-xs text-muted-foreground"
                  >
                    <span className="h-1 w-1 shrink-0 rounded-full bg-primary/60" />
                    {t(`signability.dimensions.${d}`)}
                  </li>
                ))}
              </ul>
            </div>

            <p className="mt-3 text-xs text-muted-foreground">
              {t("signability.evaluationHint")}
            </p>
            <p className="mt-1 text-xs text-muted-foreground/70">
              {t("journey.quality.detect.lastRun", {
                at: fmtTime(bookSt.last_run_at),
                count: bookSt.last_issue_count ?? 0,
              })}
            </p>

            {inlineIssues.length > 0 ? (
              <ul className="mt-2 flex flex-col gap-1.5">
                {inlineIssues.slice(0, 12).map((r) => (
                  <li
                    key={r.id}
                    className="rounded border bg-card px-2 py-1.5 text-xs"
                  >
                    <div className="flex items-center gap-2">
                      <span
                        className={`rounded px-1.5 py-0.5 text-[11px] ${
                          STATUS_CLS[r.status] ??
                          "bg-muted text-muted-foreground"
                        }`}
                      >
                        {t(`pipeline.tickets.status.${r.status}`)}
                      </span>
                      <span className="shrink-0 font-medium">
                        {r.issue_key}
                      </span>
                      <span className="truncate font-medium">{r.area}</span>
                    </div>
                    <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">
                      {r.desc}
                    </p>
                  </li>
                ))}
              </ul>
            ) : null}

            <div className="mt-3">
              <button
                type="button"
                onClick={() => setActivePanel("pipeline")}
                className="rounded-lg border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:border-foreground/20 hover:bg-muted/50"
              >
                {t("journey.quality.detect.viewTickets")}
              </button>
            </div>
          </div>
        ) : null}
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
