import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  FileText,
  BookOpen,
  Radar,
  Play,
  AlertTriangle,
  Loader2,
  Send,
  RotateCw,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  GetChapterQualityArchive,
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
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { useModels } from "@/components/settings/useModels";
import { useSettings } from "@/components/settings/useSettings";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import type {
  pipeline,
  chapter,
  volume,
  quality,
} from "@/lib/wailsjs/go/models";
import type { AuditData } from "@/types/pipeline";
import PatrolReportPanel from "@/components/pipeline/PatrolReportPanel";
import DetectRunningHint from "@/components/journey/DetectRunningHint";
import SignabilityAssessmentCard from "./SignabilityAssessmentCard";
import ScoreTrendPanel from "./ScoreTrendPanel";
import TensionCurveCard from "./TensionCurveCard";
import { VERDICT_KEYS } from "./verdictKeys";

type DetectKind = "chapter" | "volume" | "structure";

// verdict → i18n key 映射收口在 ./verdictKeys（单源），本文件 import 复用
// （卷体检走共用评估卡 + 章节质量档案跳转均需同一份映射）。

const SELECT_CLS =
  "w-full rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// #114 P3：章节检测「启动 / 收口 / 重新检测 / 轮询边沿」共用同一份失效清单。
// 旧链路底部按钮只失效状态列表，档案要等 3s 轮询观测到 running true→false
// 才刷新，点完零反馈 → 用户连点三次。零自动派发，只做缓存失效。
function invalidateChapterViews(
  queryClient: ReturnType<typeof useQueryClient>,
  novelId: number,
) {
  void queryClient.invalidateQueries({ queryKey: ["chapter-quality-archive"] });
  void queryClient.invalidateQueries({ queryKey: ["chapter-tension-curve"] });
  void queryClient.invalidateQueries({ queryKey: ["score-trend"] });
  void queryClient.invalidateQueries({ queryKey: ["score-history"] });
  void queryClient.invalidateQueries({ queryKey: ["feature-usage"] });
  void queryClient.invalidateQueries({ queryKey: detectionKeys.list(novelId) });
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

  // 章节质量档案：就地展开（按选中章取，修掉「显示最近章而非选中章」的小坑）。
  const [archiveOpen, setArchiveOpen] = useState(true);
  const { data: archive, isLoading: archiveLoading } = useQuery({
    queryKey: ["chapter-quality-archive", novelId, effChapterId],
    queryFn: async () => {
      if (!effChapterId) return null;
      try {
        return await GetChapterQualityArchive(novelId, effChapterId);
      } catch {
        return null;
      }
    },
    // #113：质量档案由写后链路 L0 直接产出，不该等到「手动检测过一次」才可见；
    // 只要有选中章就展示，发现项才不会在首章写完后长期藏在检测按钮后面。
    enabled: kind === "chapter" && !!effChapterId && archiveOpen,
  });

  // 巡检·结构结果消费面：经 GetPatrolReport 双源合并取数（活跃 run + structure 检测容器，
  // 按落库时间戳最新者胜），解析张力报告 AuditData 喂 PatrolReportPanel——
  // 取代旧「通用工单列表」呈现，使结构巡检与书体检/章节严格区分（结构只报告张力、
  // 不触发自动改稿；张力项沉淀为发现项工单后由卡片提供人工「去修复/复验」入口）。
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
  // 同步失效内联工单缓存（张力项落工单后徽章立即可见）并弹中性完成提示——
  // 该事件成功/失败/拒绝三路都会发（patrol.go 三处 emitConsistencyPatrol），
  // 语义是「已结束、报告已落库」，故文案不称成功、用 toastInfo。
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
        queryClient.invalidateQueries({
          queryKey: ["detect-inline-issues", novelId],
        });
        queryClient.invalidateQueries({
          queryKey: detectionKeys.list(novelId),
        });
        setPatrolTick((prev) => prev + 1);
        toastInfo(t("journey.quality.detect.patrolDoneToast"));
      },
    );
    return () => {
      off?.();
    };
  }, [novelId, kind, queryClient, t]);

  // P0-A/B 检测完成反馈闭环（volume）：完成事件到达即失效重拉内联工单——
  // 修复「旧缓存 × 新 last_run_id 过滤必空」缺陷（状态轮询先翻新 last_run_id，
  // 与旧缓存求交集必为空，落库的发现项界面上一条不显）；并弹一次性完成 toast
  // （P0-B：verdict + 发现数）。chapter 走独立的 pipeline:chapter_detect_done
  // （见下方 #114 P3）、structure 走上方巡检事件；kind 不符静默忽略。
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
        if (p.novel_id !== novelId || p.kind !== kind) return;
        queryClient.invalidateQueries({
          queryKey: ["detect-inline-issues", novelId],
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
      off?.();
    };
  }, [novelId, kind, queryClient, t]);

  // #100 状态陈旧修复：工单修复/复验会改工单状态（fixing→fixed→verified、
  // 未确认回退快照等），这些事件到达即失效重拉内联工单——此前内联查询只在
  // 巡检/detect_done 事件失效，修复/复验后检测卡状态徽章长期停留在旧值
  // （staleTime 30s + refetchOnWindowFocus:false）。fix/verify 事件载荷均带
  // novel_id，不符静默忽略。
  useEffect(() => {
    if (!novelId) return;
    const invalidate = (p: { novel_id?: number }) => {
      if (p.novel_id !== novelId) return;
      queryClient.invalidateQueries({
        queryKey: ["detect-inline-issues", novelId],
      });
    };
    const offs = [
      EventsOn("pipeline:fix_start", invalidate),
      EventsOn("pipeline:fix_done", invalidate),
      EventsOn("pipeline:verify_done", invalidate),
      EventsOn("pipeline:verify_cancelled", invalidate),
    ];
    return () => {
      offs.forEach((off) => off?.());
    };
  }, [novelId, queryClient]);

  // #114 P3：章节检测收口发 pipeline:chapter_detect_done（带 ok），前端即时
  // 失效档案/曲线/评分/账本并给一次完成反馈，不必等 3s 轮询边沿。
  useEffect(() => {
    if (!novelId || kind !== "chapter") return;
    const off = EventsOn(
      "pipeline:chapter_detect_done",
      (p: { novel_id?: number; chapter_id?: number; ok?: boolean }) => {
        if (p.novel_id !== novelId) return;
        invalidateChapterViews(queryClient, novelId);
        toastSuccess(
          t(
            p.ok === false
              ? "journey.quality.detect.chapterDoneFailed"
              : "journey.quality.detect.chapterDoneToast",
          ),
        );
      },
    );
    return () => {
      off?.();
    };
  }, [novelId, kind, queryClient, t]);

  // 进行态 true→false 边沿仅作兜底（事件丢失/旧进程）：刷新同一份视图清单，
  // 不弹 toast——边沿区分不了「完成/取消/失败」，误报不如不报。
  const prevRunningRef = useRef(running);
  useEffect(() => {
    if (kind !== "chapter") return;
    const wasRunning = prevRunningRef.current;
    prevRunningRef.current = running;
    if (wasRunning && !running) {
      invalidateChapterViews(queryClient, novelId);
    }
  }, [kind, running, novelId, queryClient]);

  // #113：章节检测产物是质量档案（写后就有），即使从未手动启动过检测也可直接查看/
  // 处理发现项；卷/结构仍按「有检测结果」才展示结果区。
  const showChapterArchive = kind === "chapter" && effChapterId != null;
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
      const res = await RunDetectionTask(novelId, kind, effScope);
      if (kind === "chapter") {
        // #114 P3：与卡片内「重新检测本章」同款即时反馈（档案失效 + toast），
        // 消掉「点完没反应→连点三次」；后端同章在飞时合并并回执 already_running。
        invalidateChapterViews(queryClient, novelId);
        toastSuccess(
          t(
            res?.already_running
              ? "journey.quality.detect.alreadyRunning"
              : "journey.quality.detect.rerunSent",
          ),
        );
      } else {
        queryClient.invalidateQueries({
          queryKey: detectionKeys.list(novelId),
        });
      }
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
            novelId={novelId}
            runId={patrolView?.run_id ?? ""}
            onRun={() => {
              trackFeature(ACTION_FEATURE_KEYS.patrol);
              handleStart();
            }}
          />
        ) : st?.last_run_at || showChapterArchive ? (
          kind === "volume" ? (
            // 卷体检与书体检跑同一 signability-audit 技能、产物形态一致：复用八维评估卡
            // （标注卷 scope 铭牌），修复此前卷体检不渲染评分/八维、只给通用工单预览的缺口。
            st ? (
              <SignabilityAssessmentCard
                status={st}
                inlineIssues={inlineIssues}
                scopeLabel={volumeName}
              />
            ) : null
          ) : (
            // 章节检测产物是质量档案（非评分报告/工单），形态本就不同：仅给跳转入口。
            <div className="mt-4 rounded-lg border bg-muted/30 p-3 max-w-3xl">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-foreground">
                  {t("journey.quality.detect.resultTitle")}
                </span>
                {st?.last_verdict ? (
                  <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
                    {VERDICT_KEYS[st.last_verdict]
                      ? t(VERDICT_KEYS[st.last_verdict])
                      : st.last_verdict}
                  </span>
                ) : null}
              </div>
              {st?.last_run_at ? (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t("journey.quality.detect.lastRun", {
                    at: fmtTime(st.last_run_at),
                    count: st.last_issue_count ?? 0,
                  })}
                </p>
              ) : null}
              <div className="mt-3 flex flex-col gap-3">
                <button
                  type="button"
                  onClick={() => setArchiveOpen((v) => !v)}
                  className="self-start rounded-lg border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:border-foreground/20 hover:bg-muted/50"
                >
                  {archiveOpen
                    ? t("journey.quality.detect.collapseQuality")
                    : t("journey.quality.detect.viewQuality")}
                </button>
                {archiveOpen ? (
                  <ChapterQualityArchiveCard
                    archive={archive ?? null}
                    loading={archiveLoading}
                    novelId={novelId}
                    chapterId={effChapterId}
                    onArchiveInvalidate={() => {
                      // #114 P3：与底部「启动检测」、收口事件共用同一份失效清单。
                      invalidateChapterViews(queryClient, novelId);
                    }}
                  />
                ) : null}
              </div>
            </div>
          )
        ) : null}

        {/* P0-2 情绪账：选中章张力曲线（段落粒度 + 发现标记），数据经写后链路/
            L1 回执落库，不依赖检测 run，故与趋势面板同区恒挂。 */}
        {kind === "chapter" && effChapterId != null ? (
          <TensionCurveCard novelId={novelId} chapterId={effChapterId} />
        ) : null}

        {/* P0-3 评分打通：章节区恒挂趋势面板（评分来自判官评分明细，不依赖检测 run），
            曲线点选章联动上方 scope 选择器，单章修复前后历史回答「提分了没有」。 */}
        {kind === "chapter" && chapterOpts.length > 0 ? (
          <ScoreTrendPanel
            novelId={novelId}
            chapterId={effChapterId}
            onSelectChapter={setScopeId}
          />
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
          <p className="flex flex-wrap items-center gap-1.5 text-xs text-primary/80 mb-2">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            <span>{t("journey.quality.detect.running")}</span>
            <DetectRunningHint status={st} />
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

// 严重级徽章配色：P0=阻断（红，语义 destructive），P1=警告（琥珀，语义 tag-amber），其余=中性。
function levelBadgeClass(level: string): string {
  if (level === "P0") return "bg-destructive/10 text-destructive";
  if (level === "P1") return "bg-tag-amber text-tag-amber-foreground";
  return "bg-muted text-muted-foreground";
}

function FindingRow({
  f,
  selected,
  onToggle,
}: {
  f: quality.Finding;
  selected: boolean;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  const statusText = f.status
    ? t(`journey.quality.detect.findingStatus.${f.status}`, f.status)
    : "";
  return (
    <li className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 text-xs">
      <input
        type="checkbox"
        checked={selected}
        onChange={onToggle}
        aria-label={t("journey.quality.detect.selectFinding", {
          check: f.check,
        })}
        className="h-3.5 w-3.5 shrink-0 accent-primary"
      />
      <span
        className={`rounded px-1.5 py-0.5 font-medium ${levelBadgeClass(f.level)}`}
      >
        {f.level || "—"}
      </span>
      <span className="font-mono text-[11px] text-muted-foreground">
        {f.check}
      </span>
      <span className="flex-1 text-foreground">{f.desc}</span>
      {statusText ? (
        <span className="text-muted-foreground">{statusText}</span>
      ) : null}
      {f.ledger_refs && f.ledger_refs.length > 0 ? (
        <span className="text-[11px] text-muted-foreground">
          {f.ledger_refs.join(" · ")}
        </span>
      ) : null}
    </li>
  );
}

// 章节质量档案就地卡片：L0 指标 + L0/L1 发现清单（L1 状态徽章复用 VERDICT_KEYS）。
// #113 闭环：章节检测只写质量档案、不落工单，历史上发现项永远「待处理」且无任何
// 操作入口。这里提供「勾选发现项 → 发到中央 chat 人工协作改稿 → 重新检测本章」，
// 复用工单面板唯一通道 useChatLaunchStore.send，不新增工单、不改表结构、不派发
// 任何后台自动修复执行体（守「所有修复都走 chat」纪律）。
function ChapterQualityArchiveCard({
  archive,
  loading,
  novelId,
  chapterId,
  onArchiveInvalidate,
}: {
  archive: pipeline.ChapterQualityArchive | null;
  loading: boolean;
  novelId: number;
  chapterId: number | null;
  onArchiveInvalidate: () => void;
}) {
  const { t } = useTranslation();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [rerunning, setRerunning] = useState(false);

  const l0 = archive?.l0_findings ?? [];
  const l1 = archive?.l1_findings ?? [];
  const allFindings = useMemo(
    () => [
      ...l0.map((f) => ({ key: `l0:${f.id}`, f })),
      ...l1.map((f) => ({ key: `l1:${f.id}`, f })),
    ],
    [l0, l1],
  );
  // 派生过滤而非 effect 清状态：切章/档案刷新后旧选中自动失效，
  // 不会把上一章的发现项带进下一章的 chat 上下文（零 setState-in-effect）。
  const selectedKeys = useMemo(() => {
    const valid = new Set(allFindings.map(({ key }) => key));
    return new Set([...selected].filter((k) => valid.has(k)));
  }, [selected, allFindings]);

  const toggle = useCallback((key: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const onSendToChat = useCallback(() => {
    const rows = allFindings.filter(({ key }) => selectedKeys.has(key));
    if (rows.length === 0 || !archive) {
      toastInfo(t("journey.quality.detect.selectFindingsHint"));
      return;
    }
    const items = rows
      .map(
        ({ f }, i) =>
          `${i + 1}. [${f.check}] ${f.desc}` +
          (f.ledger_refs && f.ledger_refs.length > 0
            ? `\n    ${t("journey.quality.detect.fixPromptLedger", {
                list: f.ledger_refs.join(" · "),
              })}`
            : ""),
      )
      .join("\n\n");
    const chapterText = archive.reading_number
      ? t("sidebar.chapterN", { n: archive.reading_number })
      : t("journey.quality.detect.currentChapter");
    // #114：把选中发现对应的确定性验收处方一并发出——阈值/实测值/目标/
    // 位置/避免动作与检测侧同源，LLM 不再只拿到一句人读描述凭语感重写。
    const criteria = (archive.prescriptions ?? [])
      .filter((line) => rows.some(({ f }) => line.startsWith(`[${f.check}]`)))
      .filter((line, i, arr) => arr.indexOf(line) === i);
    const criteriaBlock =
      criteria.length > 0
        ? `\n${t("journey.quality.detect.acceptanceTitle")}\n${criteria
            .map((line) => `- ${line}`)
            .join("\n")}\n`
        : "";
    // #114 P1.5：连续 N 轮未消除的条件不再让模型「再改一次」，先请人工判定
    // 是判定器阈值冲突还是改动不足（纯本地确定性，零后台派发）。
    const guardKeys = archive.guard_keys ?? [];
    const guardedChecks = guardKeys
      .map((key) => key.split("|")[1])
      .filter(Boolean);
    const hitGuard = rows.some(({ f }) => guardedChecks.includes(f.check));
    const guardBlock =
      hitGuard && guardKeys.length > 0
        ? `\n${t("journey.quality.detect.fixPromptRoundGuard", {
            keys: guardKeys.join(" · "),
          })}\n`
        : "";
    // #114 P2：把选中项对应的 P1 单源稳定 key 一并发出，app 侧据此把本轮
    // 目标发现项绑进技能账本；不自行造 key、不作为给模型的修改指令。
    const targetKeys = (archive.finding_keys ?? []).filter((key) =>
      rows.some(({ f }) => key.split("|")[1] === f.check),
    );
    const keysBlock =
      targetKeys.length > 0
        ? `\n${t("journey.quality.detect.findingKeysTitle")}\n${targetKeys
            .map((key) => `- ${key}`)
            .join("\n")}\n`
        : "";
    const content =
      `${t("journey.quality.detect.fixPromptTitle", { chapter: chapterText })}\n\n` +
      `${items}\n\n` +
      `${t("journey.quality.detect.fixPromptWords", {
        words:
          archive.words_target_label ||
          t("journey.quality.detect.wordsTargetFallback"),
      })}\n` +
      criteriaBlock +
      guardBlock +
      keysBlock +
      t("journey.quality.detect.fixPromptFooter");
    const send = useChatLaunchStore.getState().send;
    if (!send) {
      toastError(t("journey.quality.detect.chatNotReady"));
      return;
    }
    send(content);
    toastSuccess(t("journey.quality.detect.fixSent", { count: rows.length }));
    setSelected(new Set());
  }, [allFindings, selectedKeys, archive, t]);

  const onRerun = useCallback(async () => {
    if (!novelId || chapterId == null || rerunning) return;
    setRerunning(true);
    try {
      trackFeature(ACTION_FEATURE_KEYS.detect);
      await RunDetectionTask(novelId, "chapter", chapterId);
      onArchiveInvalidate();
      toastSuccess(t("journey.quality.detect.rerunSent"));
    } catch (err) {
      toastError(toErrorMessage(err, t("signability.launchFailed")));
    } finally {
      setRerunning(false);
    }
  }, [novelId, chapterId, rerunning, onArchiveInvalidate, t]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="w-3.5 h-3.5 animate-spin" />
        {t("journey.quality.detect.loading")}
      </div>
    );
  }
  if (!archive || !archive.exists) {
    return (
      <p className="text-xs text-muted-foreground">
        {t("journey.quality.detect.emptyArchive")}
      </p>
    );
  }
  const m = archive.l0_metrics;
  return (
    <div className="rounded-lg border bg-muted/30 p-3 max-w-3xl">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium text-foreground">
          {t("journey.quality.detect.qualityArchive")}
          {archive.reading_number ? (
            <span className="ml-1 text-xs text-muted-foreground">
              {t("sidebar.chapterN", { n: archive.reading_number })}
            </span>
          ) : null}
        </span>
        {archive.checked_at ? (
          <span className="text-[11px] text-muted-foreground">
            {t("journey.quality.detect.archiveCheckedAt", {
              at: fmtTime(archive.checked_at),
            })}
          </span>
        ) : null}
      </div>

      {/* 生效字数口径（与检测 L0 / 修复处方 / 前后对比同源，杜绝两套数字） */}
      {archive.words_target_label ? (
        <div className="mt-2 rounded-md border border-border bg-background px-2 py-1.5 text-[11px] text-muted-foreground">
          {t("journey.quality.detect.wordsTarget")}：
          {archive.words_target_label}
        </div>
      ) : null}

      {/* #114 P1：跨轮对齐结果——本轮消掉/仍在/新增；旧档案无快照时三者恒 0 */}
      {archive.resolved_count > 0 ||
      archive.persisted_count > 0 ||
      archive.new_count > 0 ? (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {archive.resolved_count > 0 ? (
            <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary">
              {t("journey.quality.detect.findingCounts.resolved", {
                count: archive.resolved_count,
              })}
            </span>
          ) : null}
          {archive.persisted_count > 0 ? (
            <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-foreground/80">
              {t("journey.quality.detect.findingCounts.persisted", {
                count: archive.persisted_count,
              })}
            </span>
          ) : null}
          {archive.new_count > 0 ? (
            <span className="rounded bg-destructive/10 px-1.5 py-0.5 text-[11px] text-destructive">
              {t("journey.quality.detect.findingCounts.new", {
                count: archive.new_count,
              })}
            </span>
          ) : null}
        </div>
      ) : null}

      {/* 闭环操作条：勾选发现项 → 发到 chat → 改稿后重新检测 */}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={onSendToChat}
          disabled={selectedKeys.size === 0}
          className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Send className="h-3.5 w-3.5" />
          {t("journey.quality.detect.sendToChat", { count: selectedKeys.size })}
        </button>
        <button
          type="button"
          onClick={() => void onRerun()}
          disabled={rerunning || !novelId || chapterId == null}
          className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
        >
          {rerunning ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RotateCw className="h-3.5 w-3.5" />
          )}
          {t("journey.quality.detect.rerunChapter")}
        </button>
        <span className="text-[11px] text-muted-foreground">
          {t("journey.quality.detect.closureHint")}
        </span>
      </div>

      {/* #114 P1.5 轮次保护：连续 N 轮未消除，升级为人工判定，不再「再改一次」 */}
      {(archive.guard_keys ?? []).length > 0 ? (
        <p className="mt-2 rounded-md border border-border bg-background px-2 py-1.5 text-[11px] leading-relaxed text-foreground/80">
          {t("journey.quality.detect.roundGuardHint", {
            keys: (archive.guard_keys ?? []).join(" · "),
          })}
        </p>
      ) : null}

      {/* L0 指标 */}
      <div className="mt-3">
        <div className="text-xs font-medium text-muted-foreground">
          {t("journey.quality.detect.l0Metrics")}
        </div>
        <div className="mt-1 flex flex-wrap gap-2 text-xs">
          <span className="rounded-md border border-border bg-background px-2 py-1">
            {t("journey.quality.detect.wordCount")}：{m?.word_count ?? 0}
          </span>
          <span className="rounded-md border border-border bg-background px-2 py-1">
            {t("journey.quality.detect.tailHook")}：
            {m?.has_tail_hook
              ? t("journey.quality.detect.yes")
              : t("journey.quality.detect.no")}
          </span>
          {m?.style_metrics
            ? Object.entries(m.style_metrics).map(([k, v]) => (
                <span
                  key={k}
                  className="rounded-md border border-border bg-background px-2 py-1 font-mono"
                >
                  {k}：{v}
                </span>
              ))
            : null}
        </div>
      </div>

      {/* L0 发现 */}
      <div className="mt-3">
        <div className="text-xs font-medium text-muted-foreground">
          {t("journey.quality.detect.l0Findings")}
        </div>
        {l0.length > 0 ? (
          <ul className="mt-1 flex flex-col gap-1.5">
            {l0.map((f) => (
              <FindingRow
                key={f.id}
                f={f}
                selected={selectedKeys.has(`l0:${f.id}`)}
                onToggle={() => toggle(`l0:${f.id}`)}
              />
            ))}
          </ul>
        ) : (
          <p className="mt-1 text-xs text-muted-foreground">
            {t("journey.quality.detect.noFindings")}
          </p>
        )}
      </div>

      {/* L1 发现 + L1 状态徽章 */}
      <div className="mt-3">
        <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
          <span>{t("journey.quality.detect.l1Findings")}</span>
          <span className="rounded bg-muted px-1.5 py-0.5 text-[11px]">
            {VERDICT_KEYS[archive.l1_status]
              ? t(VERDICT_KEYS[archive.l1_status])
              : archive.l1_status || t("journey.quality.detect.l1Status")}
          </span>
        </div>
        {l1.length > 0 ? (
          <ul className="mt-1 flex flex-col gap-1.5">
            {l1.map((f) => (
              <FindingRow
                key={f.id}
                f={f}
                selected={selectedKeys.has(`l1:${f.id}`)}
                onToggle={() => toggle(`l1:${f.id}`)}
              />
            ))}
          </ul>
        ) : (
          <p className="mt-1 text-xs text-muted-foreground">
            {t("journey.quality.detect.noFindings")}
          </p>
        )}
      </div>
    </div>
  );
}
