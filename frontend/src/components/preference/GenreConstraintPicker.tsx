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
  GENRE_GROUPS,
  GENRE_CONSTRAINT_CATEGORY,
  findTemplateItems,
} from "./genreTemplates";

interface Props {
  novelId: number;
}

// GenreConstraintPicker: 题材约束模板选择器（档B，P0-B 扩展为分组下拉）。
// 选中 = 创建一条本书偏好（category=创作约束），关闭 = 删除模板条目。
// 只动能按 marker 识别的模板条目，用户自定义创作约束不受影响。
export default function GenreConstraintPicker({ novelId }: Props) {
  const { t, i18n } = useTranslation();
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
  const selectedId = templateItems[0]?.templateId ?? null;
  const pending = createMutation.isPending || deleteMutation.isPending;

  const isEn = i18n.language.startsWith("en");
  const nameOf = (tpl: (typeof GENRE_TEMPLATES)[number]) =>
    isEn ? tpl.labelEn : tpl.label;

  // 按 group 归类，保持 GENRE_GROUPS 顺序
  const groupMap = useMemo(() => {
    const m = new Map<string, (typeof GENRE_TEMPLATES)[number][]>();
    for (const tpl of GENRE_TEMPLATES) {
      if (!m.has(tpl.group)) m.set(tpl.group, []);
      m.get(tpl.group)!.push(tpl);
    }
    return m;
  }, []);

  async function apply(templateId: string | null) {
    if (pending) return;
    if (templateId !== null && templateId === selectedId) return;
    try {
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

  const activeLabel = selectedId
    ? nameOf(GENRE_TEMPLATES.find((x) => x.id === selectedId)!)
    : null;

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
          <div className="flex flex-wrap items-center gap-2 mt-2">
            <select
              value={selectedId ?? ""}
              disabled={pending}
              onChange={(e) => apply(e.target.value || null)}
              className="h-8 max-w-full rounded border border-border bg-background px-2 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
            >
              <option value="">{t("preference.genreNone")}</option>
              {GENRE_GROUPS.map((g) => {
                const list = groupMap.get(g.key);
                if (!list || list.length === 0) return null;
                return (
                  <optgroup key={g.key} label={isEn ? g.labelEn : g.label}>
                    {list.map((tpl) => (
                      <option key={tpl.id} value={tpl.id}>
                        {nameOf(tpl)}
                      </option>
                    ))}
                  </optgroup>
                );
              })}
            </select>
            {activeLabel && (
              <span className="text-[10px] text-muted-foreground truncate">
                {t("preference.genreApplied", { name: activeLabel })}
              </span>
            )}
          </div>
        </div>
      </div>
    </section>
  );
}
