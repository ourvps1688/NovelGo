// 单章张力曲线卡（P0-2 情绪账前端消费面）：段落粒度的情绪/张力曲线 SVG +
// 发现标记（拖沓段区间/情绪峰/开篇窗口），标记位由后端 quality.MarkTensionCurve
// 与曲线三查判定同源计算，图上标记与工单发现不漂移。
// 拖沓段标记可点击跳转该章原文（openChapterByReadingNumber 阅读序号口径）。
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Activity, Loader2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { GetChapterTensionCurve } from "@/lib/wailsjs/go/app/App";
import { useNavigationStore } from "@/stores/useNavigationStore";
import type { quality } from "@/lib/wailsjs/go/models";

interface Props {
  novelId: number;
  /** 当前选中章（联动 scope 选择器），为空不取数 */
  chapterId: number | null;
}

const CHART_W = 680;
const CHART_H = 200;
const PAD = { left: 40, right: 16, top: 14, bottom: 34 };

export default function TensionCurveCard({ novelId, chapterId }: Props) {
  const { t } = useTranslation();

  const { data: view, isLoading } = useQuery({
    queryKey: ["chapter-tension-curve", novelId, chapterId],
    queryFn: async () => {
      if (!chapterId) return null;
      try {
        return await GetChapterTensionCurve(novelId, chapterId);
      } catch {
        return null;
      }
    },
    enabled: !!novelId && !!chapterId,
  });

  const points: quality.TensionPoint[] = useMemo(
    () => view?.points ?? [],
    [view?.points],
  );
  const markers = view?.markers;

  // 段序 → 折线下标（seg 可能不连续，标记按 seg 定位须经此映射）。
  const segIdx = useMemo(() => {
    const m = new Map<number, number>();
    points.forEach((p, i) => m.set(p.seg, i));
    return m;
  }, [points]);

  if (isLoading) {
    return (
      <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="w-3.5 h-3.5 animate-spin" />
        {t("journey.quality.detect.loading")}
      </div>
    );
  }
  if (!view?.exists || points.length === 0) {
    return (
      <div className="mt-4 rounded-lg border bg-muted/30 p-3">
        <div className="flex items-center gap-2 text-sm font-medium text-foreground">
          <Activity className="w-4 h-4 text-primary" />
          {t("journey.quality.detect.tensionCurve.title")}
        </div>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("journey.quality.detect.tensionCurve.empty")}
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
  const yAt = (v: number) =>
    PAD.top + ((100 - Math.max(0, Math.min(100, v))) / 100) * plotH;
  const step = n <= 1 ? plotW : plotW / (n - 1);
  const tensionPoly = points
    .map((p, i) => `${xAt(i).toFixed(1)},${yAt(p.tension).toFixed(1)}`)
    .join(" ");
  const emotionPoly = points
    .map((p, i) => `${xAt(i).toFixed(1)},${yAt(p.emotion).toFixed(1)}`)
    .join(" ");
  // x 轴刻度：段多时隔位标注，避免挤叠。
  const labelStep = Math.max(1, Math.ceil(n / 12));

  // 标记区间 → 折线下标区间（seg 缺失时收缩到在场段；越界/无命中返回 null）。
  const idxRange = (segs: number[]): [number, number] | null => {
    const idxs = segs
      .map((s) => segIdx.get(s))
      .filter((v): v is number => v !== undefined);
    if (idxs.length === 0) return null;
    return [Math.min(...idxs), Math.max(...idxs)];
  };
  const openingRange = markers ? idxRange(markers.opening_segs ?? []) : null;
  const dragRange =
    markers && markers.drag_start_seg >= 0
      ? idxRange([markers.drag_start_seg, markers.drag_end_seg])
      : null;

  const openChapter = () => {
    if (!view?.reading_number) return;
    useNavigationStore
      .getState()
      .openChapterByReadingNumber?.(novelId, view.reading_number);
  };

  return (
    <div className="mt-4 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Activity className="w-4 h-4 text-primary" />
        <span className="text-sm font-medium text-foreground">
          {t("journey.quality.detect.tensionCurve.title")}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("journey.quality.detect.tensionCurve.legendTension")}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("journey.quality.detect.tensionCurve.legendEmotion")}
        </span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">
        {t("journey.quality.detect.tensionCurve.desc")}
      </p>

      <svg
        viewBox={`0 0 ${CHART_W} ${CHART_H}`}
        className="mt-2 w-full select-none"
        role="img"
        aria-label={t("journey.quality.detect.tensionCurve.title")}
      >
        {/* 开篇窗口底色带（前 N 字窗口段） */}
        {openingRange ? (
          <rect
            x={xAt(openingRange[0]) - step / 2}
            y={PAD.top}
            width={(openingRange[1] - openingRange[0] + 1) * step}
            height={plotH}
            fill="currentColor"
            className="text-muted-foreground/10"
          />
        ) : null}
        {/* 拖沓段底色带（连续平坦段区间，命中 pacing_drag 判定才出现） */}
        {dragRange ? (
          <rect
            x={xAt(dragRange[0]) - step / 2}
            y={PAD.top}
            width={(dragRange[1] - dragRange[0] + 1) * step}
            height={plotH}
            fill="currentColor"
            className="text-destructive/10"
          />
        ) : null}
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
        {/* 情绪折线（虚线，次级） */}
        {n > 1 ? (
          <polyline
            points={emotionPoly}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.2}
            strokeDasharray="4 3"
            strokeLinejoin="round"
            className="text-muted-foreground/60"
          />
        ) : null}
        {/* 张力折线（主线） */}
        {n > 1 ? (
          <polyline
            points={tensionPoly}
            fill="none"
            stroke="currentColor"
            strokeWidth={1.8}
            strokeLinejoin="round"
            className="text-primary"
          />
        ) : null}
        {/* 段点 + 情绪峰标记 */}
        {points.map((p, i) => {
          const climax = (markers?.climax_segs ?? []).includes(p.seg);
          return (
            <g key={`seg-${p.seg}`}>
              <circle
                cx={xAt(i)}
                cy={yAt(p.tension)}
                r={climax ? 4.5 : 2.5}
                fill="currentColor"
                stroke="var(--color-background)"
                strokeWidth={1}
                className={
                  climax ? "text-tag-amber-foreground" : "text-primary"
                }
              />
            </g>
          );
        })}
        {/* x 轴刻度（段序） */}
        {points.map((p, i) =>
          i % labelStep === 0 ? (
            <text
              key={`x-${p.seg}`}
              x={xAt(i)}
              y={CHART_H - PAD.bottom + 14}
              textAnchor="middle"
              fontSize={9}
              fill="currentColor"
              className="text-muted-foreground"
            >
              {p.seg}
            </text>
          ) : null,
        )}
        <text
          x={CHART_W - PAD.right}
          y={CHART_H - 6}
          textAnchor="end"
          fontSize={9}
          fill="currentColor"
          className="text-muted-foreground"
        >
          {t("journey.quality.detect.tensionCurve.axisCaption")}
        </text>
      </svg>

      {/* 发现标记 chips：拖沓段可点击跳章；情绪峰计数如实呈现 */}
      <div className="mt-1 flex flex-wrap items-center gap-1.5">
        {dragRange && markers ? (
          <button
            type="button"
            onClick={openChapter}
            className="rounded bg-destructive/10 px-1.5 py-0.5 text-[11px] text-destructive transition-colors hover:bg-destructive/20"
            title={t("journey.quality.detect.tensionCurve.jumpHint")}
          >
            {t("journey.quality.detect.tensionCurve.dragChip", {
              range: `${markers.drag_start_seg}–${markers.drag_end_seg}`,
            })}
          </button>
        ) : null}
        <span className="rounded bg-tag-amber px-1.5 py-0.5 text-[11px] text-tag-amber-foreground">
          {(markers?.climax_segs ?? []).length > 0
            ? t("journey.quality.detect.tensionCurve.climaxSome", {
                count: (markers?.climax_segs ?? []).length,
              })
            : t("journey.quality.detect.tensionCurve.climaxNone")}
        </span>
        {openingRange ? (
          <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
            {t("journey.quality.detect.tensionCurve.openingChip")}
          </span>
        ) : null}
      </div>
      <p className="mt-1 text-[11px] text-muted-foreground/70">
        {t("journey.quality.detect.tensionCurve.chartHint")}
      </p>
    </div>
  );
}
