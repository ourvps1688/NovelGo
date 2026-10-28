import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  FileText,
  BookOpen,
  Radar,
  Play,
  AlertTriangle,
  Loader2,
  ArrowRight,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ListDetectionTaskStatuses,
  RunDetectionTask,
} from "@/lib/wailsjs/go/app/App";
import { detectionKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { useChapters } from "@/components/chapter/useChapters";
import { useVolumes } from "@/components/volume/useVolumes";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { usePanelStore } from "@/stores/usePanelStore";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import type { PanelId } from "@/types/panel";
import type { chapter, volume } from "@/lib/wailsjs/go/models";

type DetectKind = "chapter" | "volume" | "structure";

// 检测结果跳转落点（与 DetectionTasks 一致）：章节→章节面板（质量档案），
// 卷/结构→检测核心面板（工单/张力报告）。
const RESULT_PANEL: Record<DetectKind, PanelId> = {
  chapter: "chapters",
  volume: "pipeline",
  structure: "pipeline",
};

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
// 独立启动界面——章节/卷需先选 scope，结构无需 scope。只启动检测、不跑流水线；
// 完成后跳到结果落点（章节→质量档案，卷/结构→检测核心）。修复由人工触发。
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
      setActivePanel(RESULT_PANEL[kind]);
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setLaunching(false);
    }
  }, [novelId, hasModel, kind, effScope, queryClient, setActivePanel, t]);

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

        {st?.last_run_at ? (
          <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground/70">
            <span>
              {t("journey.quality.detect.lastRun", {
                at: fmtTime(st.last_run_at),
                count: st.last_issue_count ?? 0,
              })}
            </span>
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
          {st?.last_run_at ? (
            <button
              type="button"
              onClick={() => setActivePanel(RESULT_PANEL[kind])}
              className="inline-flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:border-foreground/20 hover:bg-muted/50"
            >
              {t("journey.quality.detect.viewResult")}
              <ArrowRight className="w-3.5 h-3.5" />
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}
