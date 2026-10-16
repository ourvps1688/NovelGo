import { useEffect, useMemo, useState } from "react";
import { Compass, Plus, ShieldCheck, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { usePreferences } from "./usePreferences";
import { useCreatePreference } from "./useCreatePreference";
import { useUpdatePreference } from "./useUpdatePreference";
import { useDeletePreference } from "./useDeletePreference";
import {
  POLAR_STAR_CATEGORY,
  parsePolarContent,
  serializePolar,
  type PolarData,
} from "./polarStarData";

// 北极星/底线 元目标层：
// - 与题材卡（怎么写）正交，管「写什么目标 / 绝不越什么界」。
// - 以一条本书偏好（category=北极星底线）持久化，走既有偏好注入管线每轮常驻注入；
//   后端 NovelProfile + FormatPreferences 已覆盖任意 category，无需改后端。

interface Props {
  novelId: number;
}

// PolarStarPanel: 本书「北极星 / 底线」编辑器，挂在偏好页约束选择器之后。
// 单条本书偏好（category=北极星底线）承载，create/update 保持一个 id，避免重复堆积。
export default function PolarStarPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const prefsQuery = usePreferences(novelId);
  const createMutation = useCreatePreference(novelId);
  const updateMutation = useUpdatePreference(novelId);
  const deleteMutation = useDeletePreference(novelId);

  const existing = useMemo(() => {
    const all = [
      ...(prefsQuery.data?.global ?? []),
      ...(prefsQuery.data?.novel ?? []),
    ];
    return all.find((p) => p.category === POLAR_STAR_CATEGORY) ?? null;
  }, [prefsQuery.data]);

  const [northStar, setNorthStar] = useState("");
  const [bottomLines, setBottomLines] = useState<string[]>([""]);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loadedId, setLoadedId] = useState<number | null>(null);

  // 仅在持久化项本身变化（首次加载 / 外部改写）时回填表单，避免覆盖用户正在进行的编辑。
  useEffect(() => {
    const id = existing?.id ?? null;
    if (id !== loadedId) {
      const parsed = existing ? parsePolarContent(existing.content) : null;
      setNorthStar(parsed?.north_star ?? "");
      setBottomLines(
        parsed && parsed.bottom_lines.length ? parsed.bottom_lines : [""],
      );
      setLoadedId(id);
      setDirty(false);
    }
  }, [existing?.id, loadedId]);

  const markDirty = () => setDirty(true);

  const updateLine = (i: number, v: string) => {
    setBottomLines((prev) => prev.map((l, idx) => (idx === i ? v : l)));
    markDirty();
  };
  const addLine = () => {
    setBottomLines((prev) => [...prev, ""]);
    markDirty();
  };
  const removeLine = (i: number) => {
    setBottomLines((prev) => prev.filter((_, idx) => idx !== i));
    markDirty();
  };

  async function handleSave() {
    if (saving) return;
    const data: PolarData = {
      north_star: northStar,
      bottom_lines: bottomLines.map((l) => l.trim()).filter(Boolean),
    };
    // 全空且已有项：删除该元目标条目。
    if (!data.north_star && data.bottom_lines.length === 0) {
      if (existing) {
        try {
          setSaving(true);
          await deleteMutation.mutateAsync(existing.id);
          toastSuccess(t("polarStar.saved"));
        } catch (e) {
          toastError(t("polarStar.saveFailed") + ": " + toErrorMessage(e));
        } finally {
          setSaving(false);
        }
      }
      return;
    }
    const content = serializePolar(data);
    try {
      setSaving(true);
      if (existing) {
        await updateMutation.mutateAsync({
          id: existing.id,
          input: { category: POLAR_STAR_CATEGORY, content, is_global: false },
        });
      } else {
        await createMutation.mutateAsync({
          is_global: false,
          category: POLAR_STAR_CATEGORY,
          content,
        });
      }
      toastSuccess(t("polarStar.saved"));
      setDirty(false);
    } catch (e) {
      toastError(t("polarStar.saveFailed") + ": " + toErrorMessage(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <section className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-start gap-3">
        <span className="shrink-0 flex h-6 w-6 items-center justify-center rounded bg-secondary text-muted-foreground">
          <Compass className="h-3.5 w-3.5" />
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-xs font-semibold text-foreground">
              {t("polarStar.title")}
            </span>
            <span className="text-[10px] text-muted-foreground">
              {t("polarStar.hint")}
            </span>
          </div>

          <div className="mt-3 space-y-3">
            <div>
              <label className="text-[11px] font-medium text-foreground flex items-center gap-1.5">
                <Compass className="h-3.5 w-3.5 text-primary" />
                {t("polarStar.northStarLabel")}
              </label>
              <textarea
                value={northStar}
                onChange={(e) => {
                  setNorthStar(e.target.value);
                  markDirty();
                }}
                placeholder={t("polarStar.northStarPlaceholder")}
                rows={2}
                className="mt-1.5 w-full resize-y rounded-md border border-border bg-background px-2.5 py-1.5 text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-ring"
              />
            </div>

            <div>
              <label className="text-[11px] font-medium text-foreground flex items-center gap-1.5">
                <ShieldCheck className="h-3.5 w-3.5 text-action-save" />
                {t("polarStar.bottomLinesLabel")}
              </label>
              <div className="mt-1.5 space-y-1.5">
                {bottomLines.map((line, i) => (
                  <div key={i} className="flex items-center gap-1.5">
                    <span className="shrink-0 text-muted-foreground text-xs">
                      {i + 1}.
                    </span>
                    <input
                      value={line}
                      onChange={(e) => updateLine(i, e.target.value)}
                      placeholder={t("polarStar.bottomLinePlaceholder")}
                      className="flex-1 h-8 rounded-md border border-border bg-background px-2.5 text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-ring"
                    />
                    <button
                      type="button"
                      onClick={() => removeLine(i)}
                      disabled={bottomLines.length <= 1}
                      className="shrink-0 flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-40 transition-colors"
                      title={t("polarStar.removeLine")}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  onClick={addLine}
                  className="inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
                >
                  <Plus className="h-3 w-3" />
                  {t("polarStar.addLine")}
                </button>
              </div>
            </div>
          </div>

          <div className="mt-3 flex items-center justify-end gap-2">
            {dirty && (
              <span className="text-[10px] text-muted-foreground">
                {t("polarStar.unsaved")}
              </span>
            )}
            <button
              type="button"
              onClick={handleSave}
              disabled={saving || !dirty}
              className="inline-flex items-center gap-1.5 h-8 px-4 rounded-lg text-xs font-medium bg-action-save text-action-save-foreground hover:bg-action-save/80 disabled:opacity-50 transition-colors"
            >
              {saving ? t("polarStar.saving") : t("polarStar.save")}
            </button>
          </div>

          {!existing && !dirty && (
            <p className="mt-2 text-[10px] text-muted-foreground/80">
              {t("polarStar.emptyHint")}
            </p>
          )}
        </div>
      </div>
    </section>
  );
}
