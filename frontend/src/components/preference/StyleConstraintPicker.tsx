import { useMemo, useState } from "react";
import { BookType, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { usePreferences } from "./usePreferences";
import { useCreatePreference } from "./useCreatePreference";
import { useDeletePreference } from "./useDeletePreference";
import { useStyleProfiles } from "./useStyleProfiles";
import { useDeleteStyleProfile } from "./useDeleteStyleProfile";
import {
  STYLE_CONSTRAINT_CATEGORY,
  styleMarkerOf,
  detectStyleProfileId,
} from "./styleTemplates";
import ConfirmDialog from "@/components/ui/ConfirmDialog";

interface Props {
  novelId: number;
}

// StyleConstraintPicker: 写作风格选择项（P1 风格提取改造）。
// 选项来自本书「风格画像」库（提取结果的沉淀）；选中即创建一条本书偏好
// （category=写作风格，content 首行带 marker），每轮常驻注入；关闭即删除。
// 仅动带 marker 的条目，用户自定义本书偏好不受影响。
export default function StyleConstraintPicker({ novelId }: Props) {
  const { t } = useTranslation();
  const preferencesQuery = usePreferences(novelId);
  const createMutation = useCreatePreference(novelId);
  const deleteMutation = useDeletePreference(novelId);
  const profilesQuery = useStyleProfiles(novelId);
  const deleteProfileMutation = useDeleteStyleProfile(novelId);

  const items = useMemo(
    () => [
      ...(preferencesQuery.data?.global ?? []),
      ...(preferencesQuery.data?.novel ?? []),
    ],
    [preferencesQuery.data],
  );
  const profiles = useMemo(() => profilesQuery.data ?? [], [profilesQuery.data]);
  const profileIds = useMemo(
    () => new Set(profiles.map((p) => p.id)),
    [profiles],
  );

  // 当前激活的写作风格偏好：分类匹配 + marker 命中且画像仍存在。
  const activeItem = useMemo(
    () =>
      items.find((it) => {
        if (it.category !== STYLE_CONSTRAINT_CATEGORY) return false;
        const id = detectStyleProfileId(it.content);
        return id !== null && profileIds.has(id);
      }) ?? null,
    [items, profileIds],
  );
  const selectedId = activeItem
    ? detectStyleProfileId(activeItem.content)
    : null;
  const activeProfile = useMemo(
    () => (selectedId !== null ? profiles.find((p) => p.id === selectedId) ?? null : null),
    [profiles, selectedId],
  );

  const pending =
    createMutation.isPending || deleteMutation.isPending || deleteProfileMutation.isPending;
  const [expanded, setExpanded] = useState(true);
  const [deleteTarget, setDeleteTarget] = useState<number | null>(null);

  async function apply(profileId: number | null) {
    if (pending) return;
    if (profileId !== null && profileId === selectedId) return;
    try {
      // 先移除所有激活的写作风格偏好（含孤儿），保证单一激活。
      for (const it of items) {
        if (it.category === STYLE_CONSTRAINT_CATEGORY) {
          await deleteMutation.mutateAsync(it.id);
        }
      }
      if (profileId !== null) {
        const profile = profiles.find((p) => p.id === profileId);
        if (!profile) return;
        await createMutation.mutateAsync({
          is_global: false,
          category: STYLE_CONSTRAINT_CATEGORY,
          content: `${styleMarkerOf(profile.id)}\n\n${profile.content}`,
        });
      }
    } catch (err) {
      toastError(t("preference.styleApplyFailed") + ": " + toErrorMessage(err));
    }
  }

  function confirmDeleteProfile() {
    if (deleteTarget === null) return;
    deleteProfileMutation
      .mutateAsync(deleteTarget)
      .catch((err) =>
        toastError(t("preference.styleDeleteFailed") + ": " + toErrorMessage(err)),
      )
      .finally(() => setDeleteTarget(null));
  }

  return (
    <section className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-start gap-3">
        <span className="shrink-0 flex h-6 w-6 items-center justify-center rounded bg-secondary text-muted-foreground">
          <BookType className="h-3.5 w-3.5" />
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-xs font-semibold text-foreground">
              {t("preference.styleConstraint")}
            </span>
            <span className="text-[10px] text-muted-foreground truncate">
              {t("preference.styleConstraintHint")}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-2 mt-2">
            <select
              value={selectedId !== null ? String(selectedId) : ""}
              disabled={pending || profilesQuery.isLoading}
              onChange={(e) => apply(e.target.value ? Number(e.target.value) : null)}
              className="h-8 max-w-full rounded border border-border bg-background px-2 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
            >
              <option value="">{t("preference.styleNone")}</option>
              {profiles.map((p) => (
                <option key={p.id} value={String(p.id)}>
                  {p.name}
                </option>
              ))}
            </select>
            {selectedId !== null && (
              <button
                type="button"
                title={t("preference.styleDeleteConfirm", {
                  name: activeProfile?.name ?? String(selectedId),
                })}
                onClick={() => setDeleteTarget(selectedId)}
                disabled={pending}
                className="shrink-0 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors disabled:opacity-50"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            )}
          </div>

          {profiles.length === 0 && (
            <p className="mt-2 text-[10px] text-muted-foreground">
              {t("preference.styleEmpty")}
            </p>
          )}

          {activeProfile && (
            <div className="mt-3 rounded-md border border-border bg-background/60">
              <div className="flex items-center justify-between gap-2 px-3 py-2">
                <span className="min-w-0 truncate text-[11px] font-medium text-foreground">
                  {t("preference.styleActiveConstraint")}
                  <span className="text-primary">
                    ：{activeProfile.name}
                  </span>
                  <span className="ml-1 text-muted-foreground">
                    （
                    {t("preference.styleWords", {
                      count: activeProfile.content.length,
                    })}
                    ）
                  </span>
                </span>
                <button
                  type="button"
                  onClick={() => setExpanded((v) => !v)}
                  className="shrink-0 rounded px-1.5 py-0.5 text-[10px] text-muted-foreground hover:bg-secondary hover:text-foreground"
                >
                  {expanded
                    ? t("preference.styleCollapse")
                    : t("preference.styleExpand")}
                </button>
              </div>
              {expanded && (
                <div className="max-h-80 overflow-auto border-t border-border px-3 py-2">
                  <p className="whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
                    {activeProfile.content}
                  </p>
                  <p className="mt-2 text-[10px] text-muted-foreground/80">
                    {t("preference.styleListHint")}
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      <ConfirmDialog
        open={deleteTarget !== null}
        title={t("common.confirmDelete")}
        message={
          deleteTarget !== null
            ? t("preference.styleDeleteConfirm", {
                name: profiles.find((p) => p.id === deleteTarget)?.name ?? String(deleteTarget),
              })
            : ""
        }
        danger
        loading={deleteProfileMutation.isPending}
        confirmText={t("common.delete")}
        onClose={() => setDeleteTarget(null)}
        onConfirm={confirmDeleteProfile}
      />
    </section>
  );
}
