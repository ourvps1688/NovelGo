// 检测任务卡区（R5-3 T2.1，WBS §2）：章节检测/卷体检/书体检/巡检·结构四启动入口。
// 契约 0.1：检测任务随时单独启动、不依赖流水线阶段（无活跃流水线也能跑）——
// 点选 scope → RunDetectionTask；契约 0.4：只扩充手动入口，自动检测节奏勿动。
// 本区=「启动入口 + 状态行 + 结果跳转」（T2.2 闭环：启动→进行态→查看结果）。
// 绝不内嵌/重排既有工作面——结果展示复用既有面板（工单/张力报告/质量档案）。
import { useState } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { useChapters } from "@/components/chapter/useChapters";
import { useVolumes } from "@/components/volume/useVolumes";
import {
  CancelDetectionTask,
  ListDetectionTaskStatuses,
  RunDetectionTask,
} from "@/lib/wailsjs/go/app/App";
import { detectionKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { useDetectionFeedback } from "@/hooks/useDetectionFeedback";
import { usePanelStore } from "@/stores/usePanelStore";
import type { PanelId } from "@/types/panel";
import type { chapter, volume } from "@/lib/wailsjs/go/models";

type DetectKind = "chapter" | "volume" | "book" | "structure";

// 检测结果跳转落点（T2.2）：章节→章节面板（侧栏质量档案标记）；
// 卷/书→发现项工单、结构→张力报告（均在流水线面板内恒渲染，T1.3 既有视图）。
const RESULT_PANEL: Record<DetectKind, PanelId> = {
  chapter: "chapters",
  volume: "pipeline",
  book: "pipeline",
  structure: "pipeline",
};

const SELECT_CLS =
  "w-full rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

const BTN_CLS =
  "h-8 rounded-md border border-primary/30 bg-primary/10 px-3 text-xs text-foreground/90 transition-colors hover:bg-primary/20 disabled:cursor-not-allowed disabled:opacity-50";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// verdict → i18n key（卷/书/结构=pass|needs_fix；章节=质量档案 L1Status
// checked|pending|skipped）。未知值如实显示原值（宁缺勿错不造词）。
const VERDICT_KEYS: Record<string, string> = {
  pass: "journey.quality.detect.verdict.pass",
  needs_fix: "journey.quality.detect.verdict.needs_fix",
  checked: "journey.quality.detect.verdict.checked",
  pending: "journey.quality.detect.verdict.pending",
  skipped: "journey.quality.detect.verdict.skipped",
  error: "journey.quality.detect.verdict.error",
};

export default function DetectionTasks() {
  const { t } = useTranslation();
  const novelId = useNovelStore((s) => s.activeNovelId);
  const queryClient = useQueryClient();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  // T2.2 检测结果反馈：完成事件→新结果徽标+状态行刷新（事件协议零改动）。
  const { freshKinds, markSeen } = useDetectionFeedback(novelId);
  const [chapterId, setChapterId] = useState<number | null>(null);
  const [volumeId, setVolumeId] = useState<number | null>(null);

  const { data: chapters } = useChapters(novelId);
  const { data: volumes } = useVolumes(novelId);
  const { data: statuses } = useQuery({
    queryKey: detectionKeys.list(novelId),
    queryFn: async () => (await ListDetectionTaskStatuses(novelId)) ?? [],
    enabled: !!novelId,
    // 进行态轻量轮询（事件接线属 T2.2）：仅检测运行中时轮询，收口即停。
    refetchInterval: (q) =>
      (q.state.data ?? []).some((s) => s.running) ? 5000 : false,
  });

  const statusOf = (kind: DetectKind) =>
    (statuses ?? []).find((s) => s.kind === kind);

  const chapterOpts: chapter.Chapter[] = chapters ?? [];
  const volumeOpts: volume.Volume[] = volumes ?? [];
  // scope 缺省取列表首项（少一次必点）；显式选择后以选择为准。
  const effChapterId = chapterId ?? chapterOpts[0]?.id ?? null;
  const effVolumeId = volumeId ?? volumeOpts[0]?.id ?? null;

  const launch = (kind: DetectKind, scopeID: number | null) => {
    if (!novelId) return;
    // 检测任务启动埋点：单源 action.detect（只记 key+次数，失败静默）。
    trackFeature(ACTION_FEATURE_KEYS.detect);
    RunDetectionTask(novelId, kind, scopeID)
      .then(() =>
        queryClient.invalidateQueries({
          queryKey: detectionKeys.list(novelId),
        }),
      )
      // 降级零提示：启动失败/回执 dispatched=false 一律静默（契约 0.1）。
      .catch(() => {});
  };

  // B5 取消在飞检测任务：best-effort——取消成功或在队列未起执行体均静默（契约 0.1）。
  const cancel = (kind: DetectKind, scopeID: number | null) => {
    if (!novelId) return;
    CancelDetectionTask(novelId, kind, scopeID)
      .then(() =>
        queryClient.invalidateQueries({
          queryKey: detectionKeys.list(novelId),
        }),
      )
      .catch(() => {});
  };

  const renderStatus = (kind: DetectKind): ReactNode => {
    const st = statusOf(kind);
    if (st?.running) {
      return (
        <p className="text-[10px] text-primary/80">
          {t("journey.quality.detect.running")}
        </p>
      );
    }
    if (!st?.last_run_at) {
      return (
        <p className="text-[10px] text-muted-foreground/60">
          {t("journey.quality.detect.never")}
        </p>
      );
    }
    const verdictText = st.last_verdict
      ? VERDICT_KEYS[st.last_verdict]
        ? t(VERDICT_KEYS[st.last_verdict])
        : st.last_verdict
      : "";
    return (
      <div className="flex items-center gap-1.5 text-[10px] text-muted-foreground/60">
        {verdictText ? (
          <span className="rounded bg-muted px-1.5 py-0.5 text-muted-foreground">
            {verdictText}
          </span>
        ) : null}
        <span className="truncate">
          {t("journey.quality.detect.lastRun", {
            at: fmtTime(st.last_run_at),
            count: st.last_issue_count ?? 0,
          })}
        </span>
      </div>
    );
  };

  const renderEntry = (
    kind: DetectKind,
    scopeNode: ReactNode,
    scopeReady: boolean,
    scopeID: number | null,
  ) => {
    const st = statusOf(kind);
    const running = !!st?.running;
    return (
      <div
        key={kind}
        data-testid={`detection-task-${kind}`}
        className="flex flex-col gap-2 rounded-xl border bg-card p-4"
      >
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-semibold text-foreground/90">
            {t(`journey.quality.detect.${kind}`)}
          </h3>
          {freshKinds.has(kind) ? (
            <span
              data-testid={`detection-fresh-${kind}`}
              className="rounded bg-primary/15 px-1.5 py-0.5 text-[10px] text-primary"
            >
              {t("journey.quality.detect.fresh")}
            </span>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground leading-relaxed">
          {t(`journey.quality.detect.${kind}Desc`)}
        </p>
        {scopeNode}
        <div className="flex items-center gap-2">
          <button
            type="button"
            disabled={!scopeReady || running}
            onClick={() => launch(kind, scopeID)}
            className={BTN_CLS}
          >
            {running
              ? t("journey.quality.detect.running")
              : t("journey.quality.detect.start")}
          </button>
          {/* B5 取消在飞检测：仅在检测进行中呈现，best-effort 静默（契约 0.1）。 */}
          {running ? (
            <button
              type="button"
              data-testid={`detection-cancel-${kind}`}
              onClick={() => cancel(kind, scopeID)}
              className="h-8 rounded-md border border-destructive/30 bg-destructive/10 px-3 text-xs text-foreground/90 transition-colors hover:bg-destructive/20"
            >
              {t("journey.quality.detect.cancel")}
            </button>
          ) : null}
          {/* T2.2 结果可达闭环：有结果（上次检测已落库）才提供跳转，
              落点复用既有面板（工单/张力报告/质量档案），零重复视图。 */}
          {st?.last_run_at ? (
            <button
              type="button"
              data-testid={`detection-result-${kind}`}
              onClick={() => {
                markSeen(kind);
                setActivePanel(RESULT_PANEL[kind]);
              }}
              className="h-8 rounded-md border px-3 text-xs text-muted-foreground transition-colors hover:border-foreground/20 hover:bg-muted/50"
            >
              {t("journey.quality.detect.viewResult")}
            </button>
          ) : null}
        </div>
        {renderStatus(kind)}
      </div>
    );
  };

  return (
    <section
      data-testid="journey-detection-tasks"
      aria-label={t("journey.quality.detect.title")}
      className="flex flex-col gap-3 sm:col-span-2 xl:col-span-3"
    >
      <div>
        <h2 className="text-sm font-semibold text-foreground">
          {t("journey.quality.detect.title")}
        </h2>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {t("journey.quality.detect.hint")}
        </p>
      </div>
      {!novelId ? (
        <p className="text-xs text-muted-foreground">
          {t("settings.selectNovelFirstHint")}
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {renderEntry(
            "chapter",
            chapterOpts.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {t("journey.quality.detect.emptyChapters")}
              </p>
            ) : (
              <select
                aria-label={t("journey.quality.detect.selectChapter")}
                value={effChapterId ?? ""}
                onChange={(e) => setChapterId(Number(e.target.value))}
                className={SELECT_CLS}
              >
                {chapterOpts.map((c) => (
                  <option key={c.id} value={c.id}>
                    {`${t("sidebar.chapterN", { n: c.reading_number })} ${c.title}`}
                  </option>
                ))}
              </select>
            ),
            effChapterId != null,
            effChapterId,
          )}
          {renderEntry(
            "volume",
            volumeOpts.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {t("journey.quality.detect.emptyVolumes")}
              </p>
            ) : (
              <select
                aria-label={t("journey.quality.detect.selectVolume")}
                value={effVolumeId ?? ""}
                onChange={(e) => setVolumeId(Number(e.target.value))}
                className={SELECT_CLS}
              >
                {volumeOpts.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.name}
                  </option>
                ))}
              </select>
            ),
            effVolumeId != null,
            effVolumeId,
          )}
          {renderEntry("book", null, true, null)}
          {renderEntry("structure", null, true, null)}
        </div>
      )}
    </section>
  );
}
