import { useCallback, useEffect, useMemo, useState } from "react";
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
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import SignabilityAssessmentCard from "./SignabilityAssessmentCard";
import PlatformDataPanel from "./PlatformDataPanel";
import BenchmarkBooksPanel from "./BenchmarkBooksPanel";
import { VERDICT_KEYS } from "./verdictKeys";
import DetectRunningHint from "@/components/journey/DetectRunningHint";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import type { pipeline } from "@/lib/wailsjs/go/models";

interface Props {
  novelId: number;
}

// 签约体检入口（R5 检测核心收口，#260）：不再走旧流水线状态机，直接以检测任务
// 独立启动「全书签约体检」（RunDetectionTask(novelId, "book", null)）。完成后**就地
// 内联展示签约潜力评估与发现概览**（不再强制跳到共享「检测核心」页），修复由人工
// 在工单面板触发（零自动派发）。检测任务零依赖流水线阶段、可随时单独发起（契约 0.1）。
export default function SignabilityWorkflowView({ novelId }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);
  const modelsQuery = useModels();
  const settingsQuery = useSettings();
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

  // P0-A/B 检测完成反馈闭环：book 体检完成事件到达即失效重拉内联工单——
  // 修复「旧缓存 × 新 last_run_id 过滤必空」缺陷（状态轮询会先翻新 last_run_id，
  // 与旧缓存求交集必为空，落库的发现项界面上一条不显）；并弹一次性完成 toast
  // （P0-B：verdict + 发现数），消除「跑完没有任何反馈」。事件载荷
  // {novel_id, kind, verdict, issue_count}（detect.go emitDetectDone），kind 不符静默忽略。
  useEffect(() => {
    if (!novelId) return;
    const off = EventsOn(
      "pipeline:detect_done",
      (p: {
        novel_id?: number;
        kind?: string;
        verdict?: string;
        issue_count?: number;
      }) => {
        if (p.novel_id !== novelId || p.kind !== "book") return;
        queryClient.invalidateQueries({
          queryKey: ["signability-inline-issues", novelId],
        });
        queryClient.invalidateQueries({
          queryKey: detectionKeys.list(novelId),
        });
        const verdict = p.verdict ?? "";
        const verdictLabel = VERDICT_KEYS[verdict]
          ? t(VERDICT_KEYS[verdict])
          : verdict;
        toastSuccess(
          t("journey.quality.detect.doneToast", {
            verdict: verdictLabel,
            count: p.issue_count ?? 0,
          }),
        );
      },
    );
    return () => {
      // 测试环境 EventsOn 可能无返回值（同 useDetectionFeedback 口径），可选调用。
      off?.();
    };
  }, [novelId, queryClient, t]);

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
          <SignabilityAssessmentCard
            status={bookSt}
            inlineIssues={inlineIssues}
          />
        ) : null}

        {/* P2-3 对标书库：赛道标杆书结构特征样本录入（钩子/爽点/节奏），样本摘要
            随签约体检提示词注入，dim2 题材契合评分有据可依（不依赖书体检 run）。 */}
        <BenchmarkBooksPanel novelId={novelId} />

        {/* P1-2 真实数据对账：平台数据录入 + 「流失章 × 低分维」诊断（书级质检语义，
            挂签约体检视图；不依赖书体检 run，可独立录入，诊断按数据完备度降级呈现）。 */}
        <PlatformDataPanel novelId={novelId} />
      </div>

      <div className="px-6 py-4 border-t shrink-0">
        {!hasModel && !noNovel && (
          <div className="flex items-center gap-2 mb-2 text-[12px] text-destructive">
            <AlertTriangle className="w-3.5 h-3.5" />
            {t("signability.configureModelFirst")}
          </div>
        )}
        {bookRunning ? (
          <p className="flex flex-wrap items-center gap-1.5 text-xs text-primary/80">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            <span>{t("signability.auditing")}</span>
            <DetectRunningHint status={bookSt} />
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
