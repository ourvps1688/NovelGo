import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  FileText,
  BookOpen,
  Radar,
  Play,
  AlertTriangle,
  Loader2,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  GetPatrolReport,
  ListDetectionTaskStatuses,
  ListIssueTickets,
  RunDetectionTask,
} from "@/lib/wailsjs/go/app/App";
import { detectionKeys, patrolKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import { useChapters } from "@/components/chapter/useChapters";
import { useVolumes } from "@/components/volume/useVolumes";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { usePanelStore } from "@/stores/usePanelStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import type { pipeline, chapter, volume } from "@/lib/wailsjs/go/models";
import type { AuditData } from "@/types/pipeline";
import PatrolReportPanel from "@/components/pipeline/PatrolReportPanel";
import SignabilityAssessmentCard from "./SignabilityAssessmentCard";

type DetectKind = "chapter" | "volume" | "structure";

// verdict → i18n key（卷/结构=pass|needs_fix；章节=质量档案 L1Status
// checked|pending|skipped）。未知值如实显示原值（宁缺勿错不造词）。
const VERDICT_KEYS: Record<string, string> = {
  pass: "journey.quality.detect.verdict.pass",
  needs_fix: "journey.quality.detect.verdict.needs_fix",
  checked: "journey.quality.detect.verdict.checked",
  pending: "journey.quality.detect.verdict.pending",
  skipped: "journey.quality.detect.verdict.skipped",
  error: "journey.quality.detect.verdict.error",
};

// 工单状态徽章配色（与 IssueTicketsPanel 同口径）：内联结果只读展示用——
// 已迁入 SignabilityAssessmentCard 复用，本文件仅保留章节质量档案跳转所需的
// VERDICT_KEYS / fmtTime（卷体检走共用评估卡，不再在此渲染工单预览）。

const SELECT_CLS =
  "w-full rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

interface Props {
  novelId: number;
  kind: DetectKind;
}

// 检测工作流详情视图（R5 检测核心）：工作流侧栏选中某一检测种类后，主区渲染对应的
// 独立落地页——章节/卷需先选 scope，结构无需 scope。只启动检测、不跑流水线；
// 完成后**就地内联展示结果**（章节→质量档案入口，卷/结构→本页工单概览），不再
// 强制跳到共享「检测核心」页。修复由人工在工单面板触发。
export default function DetectionWorkflowView({ novelId, kind }: Props) {
  const { t } = useTranslation();
  const [launching, setLaunching] = useState(false);
  const [scopeId, setScopeId] = useState<number | null>(null);
  const modelsQuery = useModels();
  const settingsQuery = useSettings();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  const queryClient = useQueryClient();

  const selectedKey = settingsQuery.data?.selected_model_key || "";
  const hasModel = !!modelsQuery.data?.find((m) => m.Key === selectedKey);

  const { data: chapters } = useChapters(novelId);
  const { data: volumes } = useVolumes(novelId);
  const chapterOpts: chapter.Chapter[] = chapters ?? [];
  const volumeOpts: volume.Volume[] = volumes ?? [];
  const effChapterId = scopeId ?? chapterOpts[0]?.id ?? null;
  const effVolumeId = scopeId ?? volumeOpts[0]?.id ?? null;
  const volumeName =
    volumeOpts.find((v) => v.id === effVolumeId)?.name ?? undefined;

  const { data: statuses } = useQuery({
    queryKey: detectionKeys.list(novelId),
    queryFn: async () => (await ListDetectionTaskStatuses(novelId)) ?? [],
    enabled: !!novelId,
    refetchInterval: (q) =>
      (q.state.data ?? []).some((s) => s.kind === kind && s.running)
        ? 3000
        : false,
  });
  const st = (statuses ?? []).find((s) => s.kind === kind);
  const running = !!st?.running;

  // 就地内联结果数据源：卷/结构检测落发现项工单（按 run_id 取本种类最近一次结果）；
  // 章节检测落质量档案（非工单），内联区仅给入口跳转，不列工单。
  const { data: allIssues } = useQuery({
    queryKey: ["detect-inline-issues", novelId],
    queryFn: async () => (await ListIssueTickets(novelId)) ?? [],
    enabled: !!novelId && !!st?.last_run_at,
  });
  const inlineIssues: pipeline.PipelineIssue[] = useMemo(() => {
    if (kind === "chapter" || !st?.last_run_id) return [];
    const runId = st.last_run_id;
    return (allIssues ?? []).filter((r) => r.run_id === runId);
  }, [allIssues, kind, st?.last_run_id]);

  // 巡检·结构结果消费面：经 GetPatrolReport 双源合并取数（活跃 run + structure 检测容器，
  // 按落库时间戳最新者胜），解析张力报告 AuditData 喂 PatrolReportPanel——
  // 取代旧「通用工单列表」呈现，使结构巡检与书体检/章节严格区分（结构只报告张力、零工单）。
  const { data: patrolView } = useQuery({
    queryKey: patrolKeys.report(novelId),
    queryFn: async () => (await GetPatrolReport(novelId)) ?? null,
    enabled: !!novelId && kind === "structure",
  });
  const patrolData: AuditData | null = useMemo(() => {
    const raw = patrolView?.report;
    if (!raw) return null;
    try {
      return typeof raw === "string" ? (JSON.parse(raw) as AuditData) : raw;
    } catch {
      return null;
    }
  }, [patrolView?.report]);

  // 结构巡检完成事件到达即失效重拉；tick 驱动 PatrolReportPanel 收起「运行中」提示。
  const [patrolTick, setPatrolTick] = useState(0);
  useEffect(() => {
    if (!novelId || kind !== "structure") return;
    const off = EventsOn(
      "pipeline:consistency_patrol",
      (p: { novel_id?: number }) => {
        if (p.novel_id !== novelId) return;
        queryClient.invalidateQueries({
          queryKey: patrolKeys.report(novelId),
        });
        setPatrolTick((prev) => prev + 1);
      },
    );
    return () => {
      off?.();
    };
  }, [novelId, kind, queryClient]);

  const scopeReady =
    kind === "chapter"
      ? effChapterId != null
      : kind === "volume"
        ? effVolumeId != null
        : true;
  const effScope: number | null =
    kind === "chapter" ? effChapterId : kind === "volume" ? effVolumeId : null;

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
      trackFeature(ACTION_FEATURE_KEYS.detect);
      await RunDetectionTask(novelId, kind, effScope);
      queryClient.invalidateQueries({ queryKey: detectionKeys.list(novelId) });
      // 不再强制跳转到共享「检测核心」页：结果在本页就地内联展示（见下方结果区）。
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, kind, effScope, queryClient, t]);

  const noNovel = !novelId;
  const buttonDisabled =
    launching || noNovel || !hasModel || running || !scopeReady;
  const buttonTitle = noNovel
    ? t("signability.selectNovelFirst")
    : !hasModel
      ? t("signability.configureModelFirst")
      : kind === "chapter" && !scopeReady
        ? t("journey.quality.detect.emptyChapters")
        : kind === "volume" && !scopeReady
          ? t("journey.quality.detect.emptyVolumes")
          : "";

  const Icon =
    kind === "chapter" ? FileText : kind === "volume" ? BookOpen : Radar;

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <div className="px-6 py-4 border-b shrink-0">
        <div className="flex items-center gap-2">
          <Icon className="w-5 h-5 text-primary" />
          <h1 className="text-lg font-semibold">
            {t(`journey.quality.detect.${kind}`)}
          </h1>
        </div>
        <p className="text-sm text-muted-foreground mt-1 max-w-3xl">
          {t(`journey.quality.detect.${kind}Desc`)}
        </p>
      </div>

      <div className="flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        {kind === "chapter" ? (
          chapterOpts.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("journey.quality.detect.emptyChapters")}
            </p>
          ) : (
            <label className="block max-w-md">
              <span className="text-xs text-muted-foreground">
                {t("journey.quality.detect.selectChapter")}
              </span>
              <select
                aria-label={t("journey.quality.detect.selectChapter")}
                value={effChapterId ?? ""}
                onChange={(e) => setScopeId(Number(e.target.value))}
                className={`mt-1 ${SELECT_CLS}`}
              >
                {chapterOpts.map((c) => (
                  <option key={c.id} value={c.id}>
                    {`${t("sidebar.chapterN", { n: c.reading_number })} ${c.title}`}
                  </option>
                ))}
              </select>
            </label>
          )
        ) : kind === "volume" ? (
          volumeOpts.length === 0 ? (
            <p className="text-xs text-muted-foreground">
              {t("journey.quality.detect.emptyVolumes")}
            </p>
          ) : (
            <label className="block max-w-md">
              <span className="text-xs text-muted-foreground">
                {t("journey.quality.detect.selectVolume")}
              </span>
              <select
                aria-label={t("journey.quality.detect.selectVolume")}
                value={effVolumeId ?? ""}
                onChange={(e) => setScopeId(Number(e.target.value))}
                className={`mt-1 ${SELECT_CLS}`}
              >
                {volumeOpts.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.name}
                  </option>
                ))}
              </select>
            </label>
          )
        ) : null}

        {kind === "structure" ? (
          <PatrolReportPanel
            data={patrolData}
            tick={patrolTick}
            onRun={() => {
              trackFeature(ACTION_FEATURE_KEYS.patrol);
              handleStart();
            }}
          />
        ) : st?.last_run_at ? (
          kind === "volume" ? (
            // 卷体检与书体检跑同一 signability-audit 技能、产物形态一致：复用八维评估卡
            // （标注卷 scope 铭牌），修复此前卷体检不渲染评分/八维、只给通用工单预览的缺口。
            <SignabilityAssessmentCard
              status={st}
              inlineIssues={inlineIssues}
              scopeLabel={volumeName}
            />
          ) : (
            // 章节检测产物是质量档案（非评分报告/工单），形态本就不同：仅给跳转入口。
            <div className="mt-4 rounded-lg border bg-muted/30 p-3 max-w-3xl">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-foreground">
                  {t("journey.quality.detect.resultTitle")}
                </span>
                {st.last_verdict ? (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
                    {VERDICT_KEYS[st.last_verdict]
                      ? t(VERDICT_KEYS[st.last_verdict])
                      : st.last_verdict}
                  </span>
                ) : null}
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                {t("journey.quality.detect.lastRun", {
                  at: fmtTime(st.last_run_at),
                  count: st.last_issue_count ?? 0,
                })}
              </p>
              <div className="mt-3">
                <button
                  type="button"
                  onClick={() => setActivePanel("chapters")}
                  className="rounded-lg border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:border-foreground/20 hover:bg-muted/50"
                >
                  {t("journey.quality.detect.viewQuality")}
                </button>
              </div>
            </div>
          )
        ) : null}
      </div>

      <div className="px-6 py-4 border-t shrink-0">
        {!hasModel && !noNovel && (
          <div className="flex items-center gap-2 mb-2 text-[12px] text-destructive">
            <AlertTriangle className="w-3.5 h-3.5" />
            {t("signability.configureModelFirst")}
          </div>
        )}
        {running ? (
          <p className="flex items-center gap-1.5 text-xs text-primary/80 mb-2">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            {t("journey.quality.detect.running")}
          </p>
        ) : null}
        <div className="flex items-center gap-2">
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
            {running
              ? t("journey.quality.detect.running")
              : t("journey.quality.detect.start")}
          </button>
        </div>
      </div>
    </div>
  );
}
