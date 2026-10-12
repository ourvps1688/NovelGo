import { useMemo } from "react";
import { BookOpenCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { usePreferences } from "./usePreferences";
import { useCreatePreference } from "./useCreatePreference";
import { useDeletePreference } from "./useDeletePreference";
import {
  GENRE_TEMPLATES,
  GENRE_CONSTRAINT_CATEGORY,
  findTemplateItems,
} from "./genreTemplates";

interface Props {
  novelId: number;
}

// GenreConstraintPicker: 题材约束模板选择器（档B）。
// 从 docs/genre-constraints.md 的四套模板中一键选用，
// 选中 = 创建一条本书偏好（category=创作约束），关闭 = 删除模板条目。
// 只动能按 marker 识别的模板条目，用户自定义创作约束不受影响。
export default function GenreConstraintPicker({ novelId }: Props) {
  const { t } = useTranslation();
  const preferencesQuery = usePreferences(novelId);
  const createMutation = useCreatePreference(novelId);
  const deleteMutation = useDeletePreference(novelId);

  const items = useMemo(
    () => [
      ...(preferencesQuery.data?.global ?? []),
      ...(preferencesQuery.data?.novel ?? []),
    ],
    [preferencesQuery.data],
  );

  const templateItems = useMemo(() => findTemplateItems(items), [items]);

  // 当前选中：取第一个命中的模板条目（多套并存时以最新一条为准不现实，
  // 偏好列表按 updated_at 排序，这里简单取首个命中用于回显）。
  const selectedId = templateItems[0]?.templateId ?? null;
  const pending = createMutation.isPending || deleteMutation.isPending;

  async function apply(templateId: string | null) {
    if (pending) return;
    // 选中同一套 → 无操作
    if (templateId !== null && templateId === selectedId) return;
    try {
      // 先建新（失败时旧约束仍在），再删旧模板条目；自定义条目不在 templateItems 内，不会被删。
      if (templateId !== null) {
        const tpl = GENRE_TEMPLATES.find((x) => x.id === templateId);
        if (!tpl) return;
        await createMutation.mutateAsync({
          is_global: false,
          category: GENRE_CONSTRAINT_CATEGORY,
          content: tpl.content,
        });
      }
      for (const ref of templateItems) {
        if (templateId !== null && ref.templateId === templateId) continue;
        await deleteMutation.mutateAsync(ref.id);
      }
    } catch (err) {
      toastError(t("preference.genreApplyFailed") + ": " + toErrorMessage(err));
    }
  }

  return (
    <section className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-start gap-3">
        <span className="shrink-0 flex h-6 w-6 items-center justify-center rounded bg-secondary text-muted-foreground">
          <BookOpenCheck className="h-3.5 w-3.5" />
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-xs font-semibold text-foreground">
              {t("preference.genreConstraint")}
            </span>
            <span className="text-[10px] text-muted-foreground truncate">
              {t("preference.genreConstraintHint")}
            </span>
          </div>
          <div className="flex flex-wrap gap-1.5 mt-2">
            <button
              type="button"
              disabled={pending || selectedId === null}
              onClick={() => apply(null)}
              className={`px-2.5 py-1 rounded text-xs transition-colors disabled:opacity-50 ${
                selectedId === null
                  ? "bg-primary text-primary-foreground"
                  : "bg-secondary text-muted-foreground hover:text-foreground"
              }`}
            >
              {t("preference.genreNone")}
            </button>
            {GENRE_TEMPLATES.map((tpl) => {
              const active = selectedId === tpl.id;
              return (
                <button
                  key={tpl.id}
                  type="button"
                  disabled={pending}
                  title={tpl.marker}
                  onClick={() => apply(tpl.id)}
                  className={`px-2.5 py-1 rounded text-xs transition-colors disabled:opacity-50 ${
                    active
                      ? "bg-primary text-primary-foreground"
                      : "bg-secondary text-muted-foreground hover:text-foreground"
                  }`}
                >
                  {t(`preference.genre_${tpl.id}`)}
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </section>
  );
}
