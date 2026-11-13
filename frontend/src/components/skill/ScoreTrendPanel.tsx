// 评分趋势面板（P0-3 评分打通前端消费面）：判官 6 维分与签约体检 8 维星同源对照
// （映射契约单源在 review.ScoreDimToAuditDims，前端经 GetScoreTrend 的 judge_dims 快照
// 直通展示、不硬编码映射），叠加逐章分数/未修复发现趋势曲线与单章「修复前后」历史
// （append-only 评分明细 chapter_score_events，旧章回落 quality_score 快照）。
// 回答「这次重写到底提分了没有」：历史卡逐次 attempt 给 overall+delta+来源徽章。
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, RefreshCw, Star, TrendingUp } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  GetChapterScoreHistory,
  GetScoreTrend,
  RescoreChapter,
} from "@/lib/wailsjs/go/app/App";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { VERDICT_KEYS } from "./verdictKeys";

interface Props {
  novelId: number;
  /** 当前选中章（联动 DetectionWorkflowView 的 scopeId），点击曲线节点可切换 */
  chapterId: number | null;
  onSelectChapter: (chapterId: number) => void;
}

// 评分明细来源徽章：new=新写首评 / full_replace=全文重写后重评 /
// rescore=用户显式重判分（#115 P0）/ snapshot=旧数据回填快照
const SOURCE_CLS: Record<string, string> = {
  new: "bg-primary/10 text-primary",
  full_replace: "bg-muted text-foreground/80",
  rescore: "bg-primary/10 text-primary",
  snapshot: "bg-muted text-muted-foreground",
};

// delta 配色只用语义 token（涨=正向用 text-primary，跌=text-destructive，平=muted）
function deltaCls(delta: number): string {
  if (delta > 0) return "text-primary";
  if (delta < 0) return "text-destructive";
  return "text-muted-foreground";
}

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

const CHART_W = 680;
const CHART_H = 180;
const PAD = { left: 40, right: 16, top: 14, bottom: 30 };

export default function ScoreTrendPanel({
  novelId,
  chapterId,
  onSelectChapter,
}: Props) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [rescorePending, setRescorePending] = useState(false);

  // #115 P0：局部改稿与重新检测都不经过评分门禁，历史轴会停在唯一那次
  // full_replace。这里给一个人工在环的显式入口：点一次才调用判官、追加一条
  // source=rescore 记录，零自动派发、不改正文、不阻断任何写作路径。
  const handleRescore = async () => {
    if (!novelId || !chapterId || rescorePending) return;
    setRescorePending(true);
    try {
      const view = await RescoreChapter(novelId, chapterId);
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ["score-history", novelId, chapterId],
        }),
        queryClient.invalidateQueries({ queryKey: ["score-trend", novelId] }),
      ]);
      toastSuccess(
        t("journey.quality.detect.scoreTrend.rescoreDone", {
          score: view?.overall ?? 0,
        }),
      );
    } catch (err) {
      toastError(
        toErrorMessage(
          err,
          t("journey.quality.detect.scoreTrend.rescoreFailed"),
        ),
      );
    } finally {
      setRescorePending(false);
    }
  };

  const { data: trend, isLoading: trendLoading } = useQuery({
    queryKey: ["score-trend", novelId],
    queryFn: async () => (await GetScoreTrend(novelId)) ?? null,
    enabled: !!novelId,
  });

  const { data: history, isLoading: historyLoading } = useQuery({
    queryKey: ["score-history", novelId, chapterId],
    queryFn: async () => {
      if (!chapterId) return [];
      try {
        return (await GetChapterScoreHistory(novelId, chapterId)) ?? [];
      } catch {
        return [];
      }
    },
    enabled: !!novelId && !!chapterId,
  });

  const points = trend?.points ?? [];
  const judgeDims = useMemo(() => trend?.judge_dims ?? [], [trend]);
  const auditOrder = useMemo(() => trend?.audit_dim_order ?? [], [trend]);

  // 判官六维 i18n 标签（键族单源，未知键如实回落后端 Label，宁缺勿错不造词）
  const judgeLabel = (key: string, fallback?: string): string => {
    const k = `journey.quality.detect.scoreTrend.judgeDims.${key}`;
    const s = t(k);
    return s === k ? (fallback ?? key) : s;
  };
  const auditLabel = (key: string): string =>
    t(`signability.dimensions.${key}`);

  // 历史事件：接口正序返回，展示倒序（最新在上）；attempt 序号按正序编。
  // #114 P1：相邻两次评分的 finding key 快照做集合差，回答「这次重写消掉哪几条」。
  const events = useMemo(() => {
    const list = [...(history ?? [])];
    const asc = list.map((e, i) => {
      const prev = i > 0 ? list[i - 1] : null;
      const prevKeys = prev?.finding_keys;
      const curKeys = e.finding_keys;
      // 旧数据/快照回填一律降级为「无快照」，绝不把空集反推成「全部消掉」。
      const hasSnapshot =
        i > 0 && Array.isArray(prevKeys) && Array.isArray(curKeys);
      const prevSet = new Set(prevKeys ?? []);
      const curSet = new Set(curKeys ?? []);
      const findingDiff = hasSnapshot
        ? {
            resolved: [...prevSet].filter((k) => !curSet.has(k)).length,
            persisted: [...curSet].filter((k) => prevSet.has(k)).length,
            new: [...curSet].filter((k) => !prevSet.has(k)).length,
          }
        : null;
      return {
        e,
        attempt: i + 1,
        delta: i > 0 ? e.overall - list[i - 1].overall : 0,
        findingDiff,
      };
    });
    return asc.reverse();
  }, [history]);

  if (trendLoading) {
    return (
      <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="w-3.5 h-3.5 animate-spin" />
        {t("journey.quality.detect.loading")}
      </div>
    );
  }

  if (points.length === 0) {
    return (
      <div className="mt-4 rounded-lg border bg-muted/30 p-3">
        <div className="flex items-center gap-2 text-sm font-medium text-foreground">
          <TrendingUp className="w-4 h-4 text-primary" />
          {t("journey.quality.detect.scoreTrend.title")}
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("journey.quality.detect.scoreTrend.empty")}
        </p>
      </div>
    );
  }

  // ---- 曲线几何 ----
  const plotW = CHART_W - PAD.left - PAD.right;
  const plotH = CHART_H - PAD.top - PAD.bottom;
  const n = points.length;
  const xAt = (i: number) =>
    n <= 1 ? PAD.left + plotW / 2 : PAD.left + (i * plotW) / (n - 1);
  const yAt = (score: number) =>
    PAD.top + ((100 - Math.max(0, Math.min(100, score))) / 100) * plotH;
  const maxFind = Math.max(1, ...points.map((p) => p.open_findings));
  const barW = Math.min(18, (plotW / Math.max(1, n)) * 0.55);
  const scoredPts = points
    .map((p, i) => ({ p, i }))
    .filter(({ p }) => p.scored);
  const polyPoints = scoredPts
    .map(({ p, i }) => `${xAt(i).toFixed(1)},${yAt(p.score).toFixed(1)}`)
    .join(" ");
  // x 轴刻度：章多时隔位标注，避免挤叠
  const labelStep = Math.max(1, Math.ceil(n / 12));

  return (
    <div className="mt-4 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <TrendingUp className="w-4 h-4 text-primary" />
        <span className="text-sm font-medium text-foreground">
          {t("journey.quality.detect.scoreTrend.title")}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("journey.quality.detect.scoreTrend.chartScore")}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("journey.quality.detect.scoreTrend.chartFindings")}
        </span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        {t("journey.quality.detect.scoreTrend.desc")}
      </p>

      {/* 逐章分数曲线（判官总分 0-100）+ 未修复发现数柱 */}
      <svg
        viewBox={`0 0 ${CHART_W} ${CHART_H}`}
        className="mt-2 w-full select-none"
        role="img"
        aria-label={t("journey.quality.detect.scoreTrend.title")}
      >
        {/* y 轴刻度 */}
        {[0, 50, 100].map((v) => (
          <g key={v}>
            <line
              x1={PAD.left}
              x2={CHART_W - PAD.right}
              y1={yAt(v)}
              y2={yAt(v)}
              stroke="currentColor"
              strokeWidth={0.5}
              className="text-border"
            />
            <text
              x={PAD.left - 6}
              y={yAt(v) + 3}
              textAnchor="end"
              fontSize={9}
              fill="currentColor"
              className="text-muted-foreground"
            >
              {v}
            </text>
          </g>
        ))}
        {/* 未修复发现数柱（独立比例，越高=待修越多） */}
        {points.map((p, i) => {
          const h = (p.open_findings / maxFind) * plotH * 0.55;
          return h > 0 ? (
            <rect
              key={`bar-${p.chapter_id}`}
              x={xAt(i) - barW / 2}
              y={PAD.top + plotH - h}
              width={barW}
              height={h}
              rx={2}
              fill="currentColor"
              className="text-muted-foreground/25"
            />
          ) : null;
        })}
        {/* 分数折线（仅已评章，点选章高亮竖线） */}
        {polyPoints ? (
          <polyline
            points={polyPoints}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.8}
            strokeLinejoin="round"
            className="text-primary"
          />
        ) : null}
        {scoredPts.map(({ p, i }) => {
          const selected = p.chapter_id === chapterId;
          return (
            <g key={`pt-${p.chapter_id}`}>
              {selected ? (
                <line
                  x1={xAt(i)}
                  x2={xAt(i)}
                  y1={PAD.top}
                  y2={PAD.top + plotH}
                  stroke="currentColor"
                  strokeWidth={1}
                  strokeDasharray="3 3"
                  className="text-primary/60"
                />
              ) : null}
              <circle
                cx={xAt(i)}
                cy={yAt(p.score)}
                r={selected ? 5 : 3}
                fill="currentColor"
                stroke="var(--color-background)"
                strokeWidth={1}
                className="text-primary"
              />
            </g>
          );
        })}
        {/* x 轴刻度（阅读序号） */}
        {points.map((p, i) =>
          i % labelStep === 0 || p.chapter_id === chapterId ? (
            <text
              key={`x-${p.chapter_id}`}
              x={xAt(i)}
              y={CHART_H - PAD.bottom + 14}
              textAnchor="middle"
              fontSize={9}
              fill="currentColor"
              className={
                p.chapter_id === chapterId
                  ? "text-primary"
                  : "text-muted-foreground"
              }
            >
              {p.reading_number}
            </text>
          ) : null,
        )}
        {/* 点击热区：点选章联动左侧章选择器 */}
        {points.map((p, i) => (
          <rect
            key={`hit-${p.chapter_id}`}
            x={xAt(i) - Math.max(barW, plotW / n) / 2}
            y={PAD.top}
            width={Math.max(barW, plotW / n)}
            height={plotH}
            fill="transparent"
            className="cursor-pointer"
            onClick={() => onSelectChapter(p.chapter_id)}
          />
        ))}
      </svg>
      <p className="mt-0.5 text-[11px] text-muted-foreground/70">
        {t("journey.quality.detect.scoreTrend.chartHint")}
      </p>

      {/* 判官六维 → 体检八维映射图例（契约单源在后端，此处直通展示） */}
      {judgeDims.length > 0 ? (
        <div className="mt-3">
          <p className="text-xs font-medium text-foreground">
            {t("journey.quality.detect.scoreTrend.legendTitle")}
          </p>
          <ul className="mt-1 grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-2">
            {judgeDims.map((d) => (
              <li
                key={d.key}
                className="flex flex-wrap items-center gap-1 text-xs text-muted-foreground"
              >
                <span className="font-medium text-foreground/80">
                  {judgeLabel(d.key, d.label)}
                </span>
                <span aria-hidden>→</span>
                <span>{(d.audit ?? []).map(auditLabel).join(", ") || "—"}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {/* 单章修复前后历史（append-only 明细，最新在上） */}
      <div className="mt-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-xs font-medium text-foreground">
            {t("journey.quality.detect.scoreTrend.historyTitle")}
          </p>
          <button
            type="button"
            disabled={!chapterId || rescorePending}
            onClick={handleRescore}
            className="inline-flex items-center gap-1.5 rounded border border-border px-2 py-1 text-[11px] text-foreground transition-colors hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50"
          >
            {rescorePending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <RefreshCw className="h-3 w-3" />
            )}
            {rescorePending
              ? t("journey.quality.detect.scoreTrend.rescorePending")
              : t("journey.quality.detect.scoreTrend.rescore")}
          </button>
        </div>
        <p className="mt-1 text-[11px] text-muted-foreground">
          {t("journey.quality.detect.scoreTrend.historyHint")}
        </p>
        {historyLoading ? (
          <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            {t("journey.quality.detect.loading")}
          </div>
        ) : events.length === 0 ? (
          <p className="mt-1 text-xs text-muted-foreground">
            {t("journey.quality.detect.scoreTrend.historyEmpty")}
          </p>
        ) : (
          <ul className="mt-1.5 flex flex-col gap-2">
            {events.map(({ e, attempt, delta, findingDiff }) => (
              <li key={e.id} className="rounded border bg-card px-2.5 py-2">
                <div className="flex flex-wrap items-baseline gap-2">
                  <span className="text-[11px] text-muted-foreground">
                    {t("journey.quality.detect.scoreTrend.attempt", {
                      n: attempt,
                    })}
                  </span>
                  <span className="text-base font-semibold leading-none text-foreground">
                    {e.overall}
                    <span className="text-xs font-normal text-muted-foreground">
                      /100
                    </span>
                  </span>
                  {attempt > 1 ? (
                    <span className={`text-xs font-medium ${deltaCls(delta)}`}>
                      {delta > 0 ? `+${delta}` : delta}
                    </span>
                  ) : null}
                  <span
                    className={`rounded px-1.5 py-0.5 text-[11px] ${
                      SOURCE_CLS[e.source] ?? "bg-muted text-muted-foreground"
                    }`}
                  >
                    {t(`journey.quality.detect.scoreTrend.sources.${e.source}`)}
                  </span>
                  {e.verdict ? (
                    <span
                      title={t("journey.quality.detect.scoreTrend.verdictHint")}
                      className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground"
                    >
                      {VERDICT_KEYS[e.verdict]
                        ? t(VERDICT_KEYS[e.verdict])
                        : e.verdict}
                    </span>
                  ) : null}
                  <span className="ml-auto text-[11px] text-muted-foreground">
                    {fmtTime(e.created_at)}
                  </span>
                </div>
                {e.critique ? (
                  <p className="mt-1.5 text-xs leading-relaxed text-foreground/80">
                    {e.critique}
                  </p>
                ) : null}
                {/* #114 P1：本次重写消掉/仍在/新增的 L0/L1 发现（快照同源） */}
                {findingDiff ? (
                  <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                    {findingDiff.resolved > 0 ? (
                      <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary">
                        {t(
                          "journey.quality.detect.scoreTrend.findingDiff.resolved",
                          { count: findingDiff.resolved },
                        )}
                      </span>
                    ) : null}
                    {findingDiff.persisted > 0 ? (
                      <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-foreground/80">
                        {t(
                          "journey.quality.detect.scoreTrend.findingDiff.persisted",
                          { count: findingDiff.persisted },
                        )}
                      </span>
                    ) : null}
                    {findingDiff.new > 0 ? (
                      <span className="rounded bg-destructive/10 px-1.5 py-0.5 text-[11px] text-destructive">
                        {t(
                          "journey.quality.detect.scoreTrend.findingDiff.new",
                          {
                            count: findingDiff.new,
                          },
                        )}
                      </span>
                    ) : null}
                  </div>
                ) : attempt > 1 ? (
                  <p className="mt-1.5 text-[11px] text-muted-foreground">
                    {t(
                      "journey.quality.detect.scoreTrend.findingDiff.noSnapshot",
                    )}
                  </p>
                ) : null}
                {/* 6 维判官分 ↔ 8 维体检星同源对照 */}
                <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
                  <div>
                    <p className="text-[11px] font-medium text-foreground/80">
                      {t("journey.quality.detect.scoreTrend.judgeTitle")}
                    </p>
                    <ul className="mt-0.5 flex flex-col gap-0.5">
                      {judgeDims.map((d) => {
                        const v = e.judge_dims?.[d.key];
                        return (
                          <li
                            key={d.key}
                            className="flex items-center gap-1.5 text-[11px] text-muted-foreground"
                          >
                            <span className="w-28 shrink-0 truncate">
                              {judgeLabel(d.key, d.label)}
                            </span>
                            <span className="flex-1">
                              <span
                                className="inline-block h-1 rounded-full bg-primary/70 align-middle"
                                style={{
                                  width: `${Math.max(0, Math.min(100, v ?? 0))}%`,
                                  maxWidth: "100%",
                                }}
                              />
                            </span>
                            <span className="w-7 shrink-0 text-right text-foreground/80">
                              {typeof v === "number" ? v : "—"}
                            </span>
                          </li>
                        );
                      })}
                    </ul>
                  </div>
                  <div>
                    <p className="text-[11px] font-medium text-foreground/80">
                      {t("journey.quality.detect.scoreTrend.auditTitle")}
                    </p>
                    <ul className="mt-0.5 flex flex-col gap-0.5">
                      {(auditOrder.length > 0
                        ? auditOrder
                        : (e.audit_dims ?? []).map((a) => a.key)
                      ).map((key) => {
                        const a = (e.audit_dims ?? []).find(
                          (x) => x.key === key,
                        );
                        return (
                          <li
                            key={key}
                            className="flex items-start gap-1.5 text-[11px] text-muted-foreground"
                          >
                            <span className="w-28 shrink-0 truncate">
                              {auditLabel(key)}
                            </span>
                            <span className="flex shrink-0 items-center gap-0.5 text-primary">
                              {[1, 2, 3, 4, 5].map((s) => (
                                <Star
                                  key={s}
                                  className={
                                    s <= (a?.score ?? 0)
                                      ? "w-2.5 h-2.5 fill-current"
                                      : "w-2.5 h-2.5 text-muted-foreground/40"
                                  }
                                />
                              ))}
                            </span>
                            <span className="flex-1 text-muted-foreground/70">
                              {a?.contributed?.length
                                ? t(
                                    "journey.quality.detect.scoreTrend.contributed",
                                    {
                                      dims: a.contributed
                                        .map((k) => judgeLabel(k))
                                        .join(", "),
                                    },
                                  )
                                : null}
                            </span>
                          </li>
                        );
                      })}
                    </ul>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
