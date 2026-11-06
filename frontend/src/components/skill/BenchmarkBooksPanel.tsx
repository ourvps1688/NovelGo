// 对标书库面板（P2-3 对标书库前端消费面）：人工粘贴赛道标杆书结构特征样本
// （钩子类型/爽点类型/节奏形态，解析口径单源在 quality.ParseBenchmarkBooks，
// 预览确认后同书名 upsert 落库）。样本摘要随签约体检提示词注入，dim2（题材契合）
// 评分证据从纯模型记忆变样本依据。挂载点：签约体检视图（书级质检语义）。
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { BookMarked, Loader2, Trash2 } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  DeleteBenchmarkBook,
  DeleteBenchmarkBooks,
  ListBenchmarkBooksView,
  PreviewBenchmarkBooks,
  SaveBenchmarkBooksText,
} from "@/lib/wailsjs/go/app/App";
import type { pipeline } from "@/lib/wailsjs/go/models";
import ConfirmDialog from "@/components/ui/ConfirmDialog";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

const NS = "journey.quality.benchmarkBooks";

function fmtTime(v?: string): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
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

// pendingDelete 单删/全清共用一个确认框：kind 区分语义。
type PendingDelete =
  { kind: "one"; id: number; title: string } | { kind: "all" } | null;

interface Props {
  novelId: number;
}

export default function BenchmarkBooksPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [text, setText] = useState("");
  const [preview, setPreview] = useState<pipeline.BenchmarkBooksPreview | null>(
    null,
  );
  const [previewing, setPreviewing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<PendingDelete>(null);
  const [deleting, setDeleting] = useState(false);

  const { data: books, isLoading } = useQuery({
    queryKey: ["benchmark-books", novelId],
    queryFn: async () => (await ListBenchmarkBooksView(novelId)) ?? [],
    enabled: !!novelId,
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["benchmark-books", novelId] });

  const handlePreview = async () => {
    if (!novelId || !text.trim()) return;
    setPreviewing(true);
    try {
      setPreview((await PreviewBenchmarkBooks(novelId, text)) ?? null);
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
      // 同书名 upsert（最新粘贴胜）；返回的 saved 是保存后全书清单。
      const res = await SaveBenchmarkBooksText(novelId, text);
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

  const handleDelete = async () => {
    if (!novelId || !pendingDelete) return;
    setDeleting(true);
    try {
      if (pendingDelete.kind === "one") {
        await DeleteBenchmarkBook(novelId, pendingDelete.id);
        toastSuccess(t(`${NS}.deletedToast`));
      } else {
        await DeleteBenchmarkBooks(novelId);
        toastSuccess(t(`${NS}.clearedToast`));
      }
      setPendingDelete(null);
      invalidate();
    } catch (err) {
      toastError(toErrorMessage(err, t(`${NS}.deleteFailed`)));
    } finally {
      setDeleting(false);
    }
  };

  const rows = books ?? [];
  const parsedCount = preview?.rows.length ?? 0;

  return (
    <div className="mt-4 rounded-lg border bg-muted/30 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <BookMarked className="w-4 h-4 text-primary" />
        <span className="text-sm font-medium text-foreground">
          {t(`${NS}.title`)}
        </span>
      </div>
      <p className="mt-1 text-xs text-muted-foreground">{t(`${NS}.desc`)}</p>

      {/* 粘贴录入：解析预览确认后才落库（宁缺勿错，坏行/缺特征行如实回报） */}
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
          placeholder={t(`${NS}.pastePlaceholder`)}
          className={`mt-1 w-full font-mono ${INPUT_CLS}`}
        />
        <p className="mt-1 text-[11px] text-muted-foreground">
          {t(`${NS}.pasteHint`)}
        </p>
        <div className="mt-2 flex flex-wrap items-center gap-2">
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

      {/* 解析预览：可入库行 + 跳过行，确认后落库 */}
      {preview ? (
        <div className="mt-3 rounded-md border border-border bg-background p-2.5">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="font-medium text-foreground">
              {t(`${NS}.previewTitle`)}
            </span>
            <span className="text-muted-foreground">
              {t(`${NS}.previewCount`, { count: parsedCount })}
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
          </div>
          {parsedCount === 0 ? (
            <p className="mt-1.5 text-xs text-destructive">
              {t(`${NS}.previewEmpty`)}
            </p>
          ) : (
            <div className="mt-1.5 max-h-56 overflow-y-auto">
              <table className="w-full border-collapse text-xs">
                <thead>
                  <tr>
                    <th className={TH_CLS}>{t(`${NS}.colTitle`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colHook`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colPleasure`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colRhythm`)}</th>
                    <th className={TH_CLS}>{t(`${NS}.colTrack`)}</th>
                  </tr>
                </thead>
                <tbody>
                  {(preview.rows ?? []).map((r, i) => (
                    <tr key={`${r.title}-${i}`}>
                      <td className={TD_CLS}>《{r.title}》</td>
                      <td className={TD_CLS}>{r.hook_type || "—"}</td>
                      <td className={TD_CLS}>{r.pleasure_type || "—"}</td>
                      <td className={TD_CLS}>{r.rhythm_shape || "—"}</td>
                      <td className={`${TD_CLS} text-muted-foreground`}>
                        {r.track || "—"}
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
              disabled={saving || parsedCount === 0}
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

      {/* 已录入样本清单（按录入顺序） */}
      <div className="mt-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">
            {t(`${NS}.listTitle`)}
          </span>
          {rows.length > 0 ? (
            <button
              type="button"
              onClick={() => setPendingDelete({ kind: "all" })}
              className={`${BTN_DANGER_CLS} ml-auto`}
            >
              <Trash2 className="w-3.5 h-3.5" />
              {t(`${NS}.clearBtn`)}
            </button>
          ) : null}
        </div>
        {isLoading ? (
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
                  <th className={TH_CLS}>{t(`${NS}.colTitle`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colHook`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colPleasure`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colRhythm`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colTrack`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colNotes`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colUpdatedAt`)}</th>
                  <th className={TH_CLS}>{t(`${NS}.colActions`)}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.id}>
                    <td className={TD_CLS}>《{r.title}》</td>
                    <td className={TD_CLS}>{r.hook_type || "—"}</td>
                    <td className={TD_CLS}>{r.pleasure_type || "—"}</td>
                    <td className={TD_CLS}>{r.rhythm_shape || "—"}</td>
                    <td className={`${TD_CLS} text-muted-foreground`}>
                      {r.track || "—"}
                    </td>
                    <td className={`${TD_CLS} text-muted-foreground`}>
                      {r.notes || "—"}
                    </td>
                    <td className={`${TD_CLS} text-muted-foreground`}>
                      {fmtTime(r.updated_at)}
                    </td>
                    <td className={TD_CLS}>
                      <button
                        type="button"
                        aria-label={t(`${NS}.deleteBtn`)}
                        title={t(`${NS}.deleteBtn`)}
                        onClick={() =>
                          setPendingDelete({
                            kind: "one",
                            id: r.id,
                            title: r.title,
                          })
                        }
                        className="inline-flex items-center rounded-md border border-destructive/40 p-1 text-destructive transition-colors hover:bg-destructive/10"
                      >
                        <Trash2 className="w-3 h-3" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={
          pendingDelete?.kind === "one"
            ? t(`${NS}.deleteConfirmTitle`)
            : t(`${NS}.clearConfirmTitle`)
        }
        message={
          pendingDelete?.kind === "one"
            ? t(`${NS}.deleteConfirmMessage`, {
                title: pendingDelete.title,
              })
            : t(`${NS}.clearConfirmMessage`)
        }
        danger
        loading={deleting}
        onConfirm={() => void handleDelete()}
        onClose={() => setPendingDelete(null)}
      />
    </div>
  );
}
