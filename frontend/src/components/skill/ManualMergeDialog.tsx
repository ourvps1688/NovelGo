import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, GitMerge, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { MergeMentalSkills } from "@/lib/wailsjs/go/app/App";
import type { app, skill } from "@/lib/wailsjs/go/models";
import { skillKeys } from "@/lib/queryKeys";

interface Props {
  open: boolean;
  onClose: () => void;
  novelId: number;
  /** 候选 mental skill（非聚合、非内置），供用户勾选合并。 */
  mentalSkills: skill.SkillMeta[];
}

// ManualMergeDialog：手动勾选多个同作者的 mental skill，调用后端 MergeMentalSkills
// 生成跨书聚合档案。生成成功后失效技能列表，使新「作者档案」分组自动出现。
export default function ManualMergeDialog({
  open,
  onClose,
  novelId,
  mentalSkills,
}: Props) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [author, setAuthor] = useState("");
  const [pending, setPending] = useState(false);

  if (!open) return null;

  const toggle = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const canConfirm = selected.size >= 2 && !pending;

  const handleConfirm = async () => {
    if (selected.size < 2) return;
    setPending(true);
    try {
      await MergeMentalSkills({
        novel_id: novelId,
        names: Array.from(selected),
        canonical_author: author.trim(),
        alias_map: {},
      } as app.MergeMentalSkillsInput);
      toastSuccess(t("skill.mergeSuccess"));
      qc.invalidateQueries({ queryKey: skillKeys.list(novelId) });
      setSelected(new Set());
      setAuthor("");
      onClose();
    } catch (err) {
      toastError(t("skill.mergeFailed") + ": " + toErrorMessage(err));
      console.error(err);
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative bg-background border rounded-xl shadow-xl w-[520px] max-h-[80vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center gap-2 px-5 py-4 border-b">
          <GitMerge className="w-5 h-5 text-primary" />
          <h2 className="text-base font-semibold">
            {t("skill.mergeDialogTitle")}
          </h2>
          <button
            onClick={onClose}
            className="ml-auto text-muted-foreground hover:text-foreground transition-colors"
            title={t("common.close")}
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="px-5 py-4 space-y-4 overflow-y-auto">
          <p className="text-sm text-muted-foreground leading-relaxed">
            {t("skill.mergeDialogDesc")}
          </p>

          {/* 规范作者名（可选） */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              {t("skill.mergeAuthor")}
            </label>
            <input
              type="text"
              value={author}
              onChange={(e) => setAuthor(e.target.value)}
              placeholder={t("skill.mergeAuthorPlaceholder")}
              className="w-full px-2 py-1 text-sm bg-background border rounded outline-none focus:ring-1 focus:ring-ring"
            />
          </div>

          {/* 候选列表 */}
          <div className="space-y-1">
            <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
              {t("skill.mergeSelected", { count: selected.size })}
            </div>
            {mentalSkills.length === 0 ? (
              <div className="px-2 py-3 text-xs text-muted-foreground">
                {t("skill.mergeNoMental")}
              </div>
            ) : (
              <div className="max-h-56 overflow-y-auto divide-y border rounded">
                {mentalSkills.map((s) => {
                  const checked = selected.has(s.name);
                  return (
                    <button
                      key={`${s.source}:${s.name}`}
                      onClick={() => toggle(s.name)}
                      className={`w-full flex items-center gap-2 px-2 py-1.5 text-left hover:bg-muted/50 transition-colors ${
                        checked ? "bg-muted/40" : ""
                      }`}
                    >
                      <span
                        className={`flex items-center justify-center w-4 h-4 rounded border shrink-0 ${
                          checked
                            ? "bg-primary border-primary text-primary-foreground"
                            : "border-muted-foreground/40"
                        }`}
                      >
                        {checked && <Check className="w-3 h-3" />}
                      </span>
                      <span className="text-sm truncate flex-1">{s.name}</span>
                      <span className="text-[10px] text-muted-foreground shrink-0">
                        {s.source}
                      </span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-5 py-4 border-t">
          <button
            onClick={onClose}
            className="px-3 py-1.5 text-sm rounded-lg border hover:bg-muted transition-colors"
          >
            {t("common.cancel")}
          </button>
          <button
            onClick={handleConfirm}
            disabled={!canConfirm}
            className="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
          >
            {t("skill.mergeConfirm")}
          </button>
        </div>
      </div>
    </div>
  );
}
