// 平台数据对账面板（P1-2 真实数据对账前端消费面）：人工粘贴七猫后台逐章完读率/
// 追读率（解析口径单源在 quality.ParsePlatformMetrics，预览确认后同章 upsert 落库），
// 与判官评分、质量档案发现交叉对照，输出「流失章 × 低分维」诊断（对账口径单源在
// 后端 GetPlatformReconciliation：流失=相邻有数据章率下降 ≥5pp、低分维=判官维 <60
// 经 6↔8 映射投影、lift=流失章低分率−稳定章低分率降序）。前端只呈现不重算；
// 维标签复用 signability.dimensions.* 与 scoreTrend.judgeDims.* 键族
// （未知键回落后端 Label/键名，宁缺勿错）。挂载点：签约体检视图（书级质检语义）。
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Database, Loader2, Star, Trash2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DeletePlatformMetrics,
  GetPlatformReconciliation,
  ListPlatformMetricsView,
  PreviewPlatformMetrics,
  SavePlatformMetricsText,
} from "@/lib/wailsjs/go/app/App";
import type { pipeline } from "@/lib/wailsjs/go/models";
import ConfirmDialog from "@/components/ui/ConfirmDialog";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

const NS = "journey.quality.platformData";

function fmtRate(v?: number): string {
  return typeof v === "number" ? `${v.toFixed(1)}%` : "—";
}

function fmtTime(v?: string): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// lift 是小数率差（0.25=25pp），展示为百分点。
function fmtLift(v: number): string {
  const pp = Math.round(v * 100);
  return pp > 0 ? `+${pp}pp` : `${pp}pp`;
}

const TH_CLS =
  "border-b border-border py-1.5 pr-2 text-left font-normal text-muted-foreground whitespace-nowrap";
const TD_CLS =
  "border-b border-border/50 py-1.5 pr-2 text-foreground align-top";
const BTN_CLS =
  "inline-flex items-center gap-1.5 rounded-md border border-border bg-background px-3 py-1.5 text-xs text-foreground transition-colors hover:bg-muted/50 disabled:opacity-50 disabled:cursor-not-allowed";
const BTN_PRIMARY_CLS =
  "inline-flex items-center gap-1.5 rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed";
const BTN_DANGER_CLS =
  "inline-flex items-center gap-1.5 rounded-md border border-destructive/40 px-3 py-1.5 text-xs text-destructive transition-colors hover:bg-destructive/10 disabled:opacity-50 disabled:cursor-not-allowed";
const INPUT_CLS =
  "rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

interface Props {
  novelId: number;
}

export default function PlatformDataPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  // 批次备注默认取当天日期（惰性初始化，避开 render 纯度约束），可改可清。
  const [batch, setBatch] = useState(() =>
    new Date().toISOString().slice(0, 10),
  );
  const [preview, setPreview] =
    useState<pipeline.PlatformMetricsPreview | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [clearOpen, setClearOpen] = useState(false);
  const [clearing, setClearing] = useState(false);

  const { data: metrics, isLoading: metricsLoading } = useQuery({
    queryKey: ["platform-metrics", novelId],
    queryFn: async () => (await ListPlatformMetricsView(novelId)) ?? [],
    enabled: !!novelId,
  });
  const { data: recon, isLoading: reconLoading } = useQuery({
    queryKey: ["platform-reconcile", novelId],
    queryFn: async () => (await GetPlatformReconciliation(novelId)) ?? null,
    enabled: !!novelId,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["platform-metrics", novelId] });
    queryClient.invalidateQueries({
      queryKey: ["platform-reconcile", novelId],
    });
  };

  // 维标签：体检维走 signability.dimensions.*；判官维走 scoreTrend.judgeDims.*，
  // 未知键回落后端 judge_dims 快照 Label/键名（宁缺勿错不造词）。
  const judgeLabel = (key: string, fallback?: string): string => {
    const k = `journey.quality.detect.scoreTrend.judgeDims.${key}`;
    const s = t(k);
    return s === k ? (fallback ?? key) : s;
  };
  const judgeFallback = (key: string): string | undefined =>
    recon?.judge_dims?.find((d) => d.key === key)?.label;
  const auditLabel = (key: string): string =>
    t(`signability.dimensions.${key}`);

  const handlePreview = async () => {
    if (!novelId || !text.trim()) return;
    setPreviewing(true);
    try {
      setPreview((await PreviewPlatformMetrics(novelId, text)) ?? null);
    } catch (err) {
      toastError(toErrorMessage(err, t(`${NS}.previewFailed`)));
    } finally {
      setPreviewing(false);
    }
  };

  const handleSave = async () => {
    if (!novelId || !text.trim()) return;
    setSaving(true);
    try {
      // 同章 upsert（最新粘贴胜）；返回的 saved 是保存后全书清单，toast 用总数如实表述。
      const res = await SavePlatformMetricsText(novelId, text, batch.trim());
      toastSuccess(t(`${NS}.savedToast`, { count: res?.saved?.length ?? 0 }));
      setPreview(null);
      setText("");
      invalidate();
    } catch (err) {
      toastError(toErrorMessage(err, t(`${NS}.saveFailed`)));
    } finally {
      setSaving(false);
    }
  };

  const handleClear = async () => {
    if (!novelId) return;
    setClearing(true);
    try {
      await DeletePlatformMetrics(novelId);
      toastSuccess(t(`${NS}.clearedToast`));
      setClearOpen(false);
      invalidate();
    } catch (err) {
      toastError(toErrorMessage(err, t(`${NS}.clearFailed`)));
    } finally {
      setClearing(false);
    }
  };

  const rows = metrics ?? [];
  const matchedCount = preview?.rows.filter((r) => r.matched).length ?? 0;

  return (
    <div className="mt-4 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Database className="w-4 h-4 text-primary" />
        <span className="text-sm font-medium text-foreground">
          {t(`${NS}.title`)}
        </span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">{t(`${NS}.desc`)}</p>

      {/* 粘贴录入：解析预览确认后才落库（宁缺勿错，坏行/越界章号如实回报） */}
      <div className="mt-3">
        <div className="text-xs font-medium text-muted-foreground">
          {t(`${NS}.pasteTitle`)}
        </div>
        <textarea
          aria-label={t(`${NS}.pasteTitle`)}
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={5}
          spellCheck={false}
          placeholder={"1 78.2 65.0\n2 74.5 61.8\n3 70.1 58.3"}
          className={`mt-1 w-full font-mono ${INPUT_CLS}`}
        />
        <p className="mt-1 text-[11px] text-muted-foreground">
          {t(`${NS}.pasteHint`)}
        </p>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground">
            {t(`${NS}.batchLabel`)}
            <input
              aria-label={t(`${NS}.batchLabel`)}
              value={batch}
              onChange={(e) => setBatch(e.target.value)}
              placeholder={t(`${NS}.batchPlaceholder`)}
              className={`w-48 ${INPUT_CLS}`}
            />
          </label>
          <button
            type="button"
            onClick={() => void handlePreview()}
            disabled={previewing || !text.trim()}
            className={BTN_PRIMARY_CLS}
          >
            {previewing ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : null}
            {t(`${NS}.previewBtn`)}
          </button>
        </div>
      </div>

      {/* 解析预览：可入库行/跳过行/越界章号，确认后落库 */}
      {preview ? (
        <div className="mt-3 rounded-md border border-border bg-background p-2.5">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="font-medium text-foreground">
              {t(`${NS}.previewTitle`)}
            </span>
            <span className="text-muted-foreground">
              {t(`${NS}.previewMatched`, { count: matchedCount })}
            </span>
            {(preview.skipped_lines?.length ?? 0) > 0 ? (
              <span
                className="text-muted-foreground"
                title={(preview.skipped_lines ?? []).join("\n")}
              >
                {t(`${NS}.previewSkipped`, {
                  count: preview.skipped_lines.length,
                })}
              </span>
            ) : null}
            {(preview.unmatched_numbers?.length ?? 0) > 0 ? (
              <span className="text-destructive">
                {t(`${NS}.previewUnmatched`, {
                  numbers: (preview.unmatched_numbers ?? []).join(", "),
                })}
              </span>
            ) : null}
          </div>
          {matchedCount === 0 ? (
            <p className="mt-1.5 text-xs text-destructive">
              {t(`${NS}.previewEmpty`)}
            </p>
          ) : (
            <div className="mt-1.5 max-h-56 overflow-y-auto">
              <table className="w-full border-collapse text-xs">
                <thead>
                  <tr>
                    <th className={TH_CLS}>{t(`${NS}.colChapter`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colCompletion`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colRetention`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colStatus`)}</th>
                  </tr>
                </thead>
                <tbody>
                  {(preview.rows ?? []).map((r) => (
                    <tr
                      key={`${r.reading_number}-${r.matched}`}
                      className={r.matched ? "" : "opacity-60"}
                    >
                      <td className={TD_CLS}>
                        <span className="text-muted-foreground">
                          {t("sidebar.chapterN", { n: r.reading_number })}
                        </span>{" "}
                        {r.title}
                      </td>
                      <td className={TD_CLS}>{fmtRate(r.completion_rate)}</td>
                      <td className={TD_CLS}>{fmtRate(r.retention_rate)}</td>
                      <td className={TD_CLS}>
                        {r.matched
                          ? t(`${NS}.matchedYes`)
                          : t(`${NS}.matchedNo`)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={() => void handleSave()}
              disabled={saving || matchedCount === 0}
              className={BTN_PRIMARY_CLS}
            >
              {saving ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
              {t(`${NS}.saveBtn`)}
            </button>
            <button
              type="button"
              onClick={() => setPreview(null)}
              disabled={saving}
              className={BTN_CLS}
            >
              {t("common.cancel")}
            </button>
          </div>
        </div>
      ) : null}

      {/* 已录入数据清单（按全局阅读序号升序） */}
      <div className="mt-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">
            {t(`${NS}.listTitle`)}
          </span>
          {rows.length > 0 ? (
            <button
              type="button"
              onClick={() => setClearOpen(true)}
              className={`${BTN_DANGER_CLS} ml-auto`}
            >
              <Trash2 className="w-3.5 h-3.5" />
              {t(`${NS}.clearBtn`)}
            </button>
          ) : null}
        </div>
        {metricsLoading ? (
          <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            {t("journey.quality.detect.loading")}
          </div>
        ) : rows.length === 0 ? (
          <p className="mt-1 text-xs text-muted-foreground">
            {t(`${NS}.listEmpty`)}
          </p>
        ) : (
          <div className="mt-1.5 max-h-64 overflow-y-auto">
            <table className="w-full border-collapse text-xs">
              <thead>
                <tr>
                  <th className={TH_CLS}>{t(`${NS}.colChapter`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colCompletion`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colRetention`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colBatch`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colUpdatedAt`)}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.chapter_id}>
                    <td className={TD_CLS}>
                      <span className="text-muted-foreground">
                        {t("sidebar.chapterN", { n: r.reading_number })}
                      </span>{" "}
                      {r.title}
                    </td>
                    <td className={TD_CLS}>{fmtRate(r.completion_rate)}</td>
                    <td className={TD_CLS}>{fmtRate(r.retention_rate)}</td>
                    <td className={`${TD_CLS} text-muted-foreground`}>
                      {r.batch || "—"}
                    </td>
                    <td className={`${TD_CLS} text-muted-foreground`}>
                      {fmtTime(r.updated_at)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* 流失章 × 低分维交叉诊断（口径后端单源，lift 降序） */}
      <div className="mt-3">
        <div className="text-xs font-medium text-muted-foreground">
          {t(`${NS}.diagTitle`)}
        </div>
        {reconLoading && !recon ? (
          <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
            {t("journey.quality.detect.loading")}
          </div>
        ) : (
          <>
            {recon?.summary ? (
              <p className="mt-1 text-xs leading-relaxed text-foreground/80">
                {recon.summary}
              </p>
            ) : null}
            {(recon?.chapters?.length ?? 0) > 0 ? (
              <>
                {/* 交叉统计：lift=流失章低分率−稳定章低分率，显著关联维在前 */}
                <div className="mt-2 max-h-64 overflow-y-auto">
                  <table className="w-full border-collapse text-xs">
                    <thead>
                      <tr>
                        <th className={TH_CLS}>{t(`${NS}.dimCol`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.dimChurn`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.dimStable`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.dimLift`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.dimSources`)}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(recon?.dim_stats ?? []).map((st) => (
                        <tr key={st.dim}>
                          <td className={TD_CLS}>{auditLabel(st.dim)}</td>
                          <td className={TD_CLS}>
                            {st.churn_low}/{st.churn_total}
                          </td>
                          <td className={TD_CLS}>
                            {st.stable_low}/{st.stable_total}
                          </td>
                          <td
                            className={`${TD_CLS} font-medium ${
                              st.lift > 0
                                ? "text-destructive"
                                : st.lift < 0
                                  ? "text-primary"
                                  : "text-muted-foreground"
                            }`}
                          >
                            {fmtLift(st.lift)}
                          </td>
                          <td className={`${TD_CLS} text-muted-foreground`}>
                            {(st.judge_sources ?? [])
                              .map((k) => judgeLabel(k, judgeFallback(k)))
                              .join(", ") || "—"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="mt-1 text-[11px] text-muted-foreground/70">
                  {t(`${NS}.liftHint`)}
                </p>

                {/* 逐章诊断行：率+降幅、流失徽章、判官分、低分维、未修复发现 */}
                <div className="mt-2 max-h-72 overflow-y-auto">
                  <table className="w-full border-collapse text-xs">
                    <thead>
                      <tr>
                        <th className={TH_CLS}>{t(`${NS}.colChapter`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.colCompletion`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.colRetention`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.colDrop`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.colScore`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.colLowDims`)}</th>
                        <th className={TH_CLS}>{t(`${NS}.colFindings`)}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(recon?.chapters ?? []).map((c) => (
                        <tr
                          key={c.chapter_id}
                          className={c.churn ? "bg-destructive/5" : ""}
                        >
                          <td className={TD_CLS}>
                            <span className="text-muted-foreground">
                              {t("sidebar.chapterN", { n: c.reading_number })}
                            </span>{" "}
                            {c.title}
                            {c.churn ? (
                              <span className="ml-1 rounded bg-destructive/10 px-1.5 py-0.5 text-[10px] font-medium text-destructive">
                                {t(`${NS}.churnBadge`)}
                              </span>
                            ) : null}
                          </td>
                          <td className={TD_CLS}>
                            {fmtRate(c.completion_rate)}
                          </td>
                          <td className={TD_CLS}>
                            {fmtRate(c.retention_rate)}
                          </td>
                          <td className={TD_CLS}>
                            <span className="flex flex-col gap-0.5">
                              {c.completion_drop > 0 ? (
                                <span className="text-destructive">
                                  {t(`${NS}.dropCompletion`, {
                                    pp: c.completion_drop.toFixed(1),
                                  })}
                                </span>
                              ) : null}
                              {c.retention_drop > 0 ? (
                                <span className="text-destructive">
                                  {t(`${NS}.dropRetention`, {
                                    pp: c.retention_drop.toFixed(1),
                                  })}
                                </span>
                              ) : null}
                              {c.completion_drop <= 0 &&
                              c.retention_drop <= 0 ? (
                                <span className="text-muted-foreground">—</span>
                              ) : null}
                            </span>
                          </td>
                          <td className={TD_CLS}>
                            {c.scored ? (
                              c.score
                            ) : (
                              <span className="text-muted-foreground">
                                {t(`${NS}.unscored`)}
                              </span>
                            )}
                          </td>
                          <td className={TD_CLS}>
                            {(c.low_audit_dims ?? []).length > 0 ? (
                              <span
                                className="flex flex-wrap gap-1"
                                title={(c.low_judge_dims ?? [])
                                  .map((k) => judgeLabel(k, judgeFallback(k)))
                                  .join(", ")}
                              >
                                {(c.low_audit_dims ?? []).map((k) => (
                                  <span
                                    key={k}
                                    className="rounded bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive"
                                  >
                                    {auditLabel(k)}
                                  </span>
                                ))}
                              </span>
                            ) : (
                              <span className="text-muted-foreground">—</span>
                            )}
                          </td>
                          <td
                            className={`${TD_CLS} font-mono text-[10px] text-muted-foreground`}
                          >
                            {(c.open_findings ?? []).length > 0
                              ? (c.open_findings ?? []).join(" · ")
                              : "—"}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </>
            ) : null}

            {/* 书体检八维星级快照（最近一次书体检产物；未跑为空） */}
            <div className="mt-3">
              <p className="text-xs font-medium text-foreground">
                {t(`${NS}.bookDimsTitle`)}
              </p>
              {(recon?.book_audit_dims?.length ?? 0) > 0 ? (
                <ul className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
                  {(recon?.audit_dim_order?.length
                    ? recon.audit_dim_order
                    : (recon?.book_audit_dims ?? []).map((d) => d.key)
                  ).map((key) => {
                    const d = (recon?.book_audit_dims ?? []).find(
                      (x) => x.key === key,
                    );
                    return (
                      <li
                        key={key}
                        className="flex items-center gap-1 text-[11px] text-muted-foreground"
                      >
                        <span className="w-28 shrink-0 truncate">
                          {auditLabel(key)}
                        </span>
                        <span className="flex items-center gap-0.5 text-primary">
                          {[1, 2, 3, 4, 5].map((s) => (
                            <Star
                              key={s}
                              className={
                                s <= (d?.score ?? 0)
                                  ? "w-2.5 h-2.5 fill-current"
                                  : "w-2.5 h-2.5 text-muted-foreground/40"
                              }
                            />
                          ))}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              ) : (
                <p className="mt-1 text-xs text-muted-foreground">
                  {t(`${NS}.bookDimsEmpty`)}
                </p>
              )}
            </div>
          </>
        )}
      </div>

      <ConfirmDialog
        open={clearOpen}
        title={t(`${NS}.clearConfirmTitle`)}
        message={t(`${NS}.clearConfirmMessage`)}
        danger
        loading={clearing}
        onConfirm={() => void handleClear()}
        onClose={() => setClearOpen(false)}
      />
    </div>
  );
}
