import { useState } from "react";
import { MonitorPlay } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { usePreferences } from "./usePreferences";
import { useCreatePreference } from "./useCreatePreference";
import { useDeletePreference } from "./useDeletePreference";
import {
  PLATFORM_TEMPLATES,
  PLATFORM_CONSTRAINT_CATEGORY,
  findPlatformTemplateItems,
} from "./platformTemplates";

interface Props {
  novelId: number;
}

// PlatformConstraintPicker: 签约平台约束模板选择器。
// 选中 = 创建一条本书偏好（category=平台约束），关闭 = 删除模板条目。
// 只动能按 marker 识别的模板条目，用户自定义平台约束不受影响。
// 与 GenreConstraintPicker 同为 single-select：选中新项会删除旧平台条目。
export default function PlatformConstraintPicker({ novelId }: Props) {
  const { t, i18n } = useTranslation();
  const preferencesQuery = usePreferences(novelId);
  const createMutation = useCreatePreference(novelId);
  const deleteMutation = useDeletePreference(novelId);

  const items = (preferencesQuery.data?.global ?? []).concat(
    preferencesQuery.data?.novel ?? [],
  );

  const templateItems = findPlatformTemplateItems(items);
  const selectedId = templateItems[0]?.templateId ?? null;
  const pending = createMutation.isPending || deleteMutation.isPending;
  const [expanded, setExpanded] = useState(true);
  const activeTemplate = selectedId
    ? (PLATFORM_TEMPLATES.find((x) => x.id === selectedId) ?? null)
    : null;

  const isEn = i18n.language.startsWith("en");
  const nameOf = (tpl: (typeof PLATFORM_TEMPLATES)[number]) =>
    isEn ? tpl.labelEn : tpl.label;

  async function apply(templateId: string | null) {
    if (pending) return;
    if (templateId !== null && templateId === selectedId) return;
    try {
      if (templateId !== null) {
        const tpl = PLATFORM_TEMPLATES.find((x) => x.id === templateId);
        if (!tpl) return;
        await createMutation.mutateAsync({
          is_global: false,
          category: PLATFORM_CONSTRAINT_CATEGORY,
          content: tpl.content,
        });
      }
      for (const ref of templateItems) {
        if (templateId !== null && ref.templateId === templateId) continue;
        await deleteMutation.mutateAsync(ref.id);
      }
    } catch (err) {
      toastError(
        t("preference.platformApplyFailed") + ": " + toErrorMessage(err),
      );
    }
  }

  const activeLabel = selectedId
    ? nameOf(PLATFORM_TEMPLATES.find((x) => x.id === selectedId)!)
    : null;

  return (
    <section className="rounded-lg border border-border bg-card px-4 py-3">
      <div className="flex items-start gap-3">
        <span className="shrink-0 flex h-6 w-6 items-center justify-center rounded bg-secondary text-muted-foreground">
          <MonitorPlay className="h-3.5 w-3.5" />
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-xs font-semibold text-foreground">
              {t("preference.platformConstraint")}
            </span>
            <span className="text-[10px] text-muted-foreground truncate">
              {t("preference.platformConstraintHint")}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-2 mt-2">
            <select
              value={selectedId ?? ""}
              disabled={pending}
              onChange={(e) => apply(e.target.value || null)}
              className="h-8 max-w-full rounded border border-border bg-background px-2 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
            >
              <option value="">{t("preference.platformNone")}</option>
              {PLATFORM_TEMPLATES.map((tpl) => (
                <option key={tpl.id} value={tpl.id}>
                  {nameOf(tpl)}
                </option>
              ))}
            </select>
            {activeLabel && (
              <span className="text-[10px] text-muted-foreground truncate">
                {t("preference.platformApplied", { name: activeLabel })}
              </span>
            )}
          </div>

          {activeTemplate && (
            <div className="mt-3 rounded-md border border-border bg-background/60">
              <div className="flex items-center justify-between gap-2 px-3 py-2">
                <span className="min-w-0 truncate text-[11px] font-medium text-foreground">
                  {t("preference.platformActiveConstraint")}
                  <span className="text-primary">
                    ：{nameOf(activeTemplate)}
                  </span>
                  <span className="ml-1 text-muted-foreground">
                    （
                    {t("preference.platformWords", {
                      count: activeTemplate.content.length,
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
                    ? t("preference.platformCollapse")
                    : t("preference.platformExpand")}
                </button>
              </div>
              {expanded && (
                <div className="max-h-80 overflow-auto border-t border-border px-3 py-2">
                  <p className="whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">
                    {activeTemplate.content}
                  </p>
                  <p className="mt-2 text-[10px] text-muted-foreground/80">
                    {t("preference.platformListHint")}
                  </p>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
