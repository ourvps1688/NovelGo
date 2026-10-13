import { useEffect, useRef, useState } from "react";
import { X, Tag, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { app } from "@/lib/wailsjs/go/models";
import { useAuthorAliases, useSaveAuthorAliases } from "./useAuthorAliases";

interface Props {
  open: boolean;
  onClose: () => void;
}

// AuthorAliasDialog：管理用户级「作者别名映射」。
// 每个分组 = 一个规范名 + 若干等价署名（别名），保存后后端会把同一作者的不同署名归并到同一规范名，
// 跨书聚合（MergeMentalSkills / RebuildAggregated / reconcile）即按此归并，使「作者档案」正确聚合。
// 保存动作会触发后端清空并重建全部派生聚合档，使改动即时生效。
export default function AuthorAliasDialog({ open, onClose }: Props) {
  const { t } = useTranslation();
  const { data, isLoading } = useAuthorAliases();
  const saveMutation = useSaveAuthorAliases();
  const [groups, setGroups] = useState<app.AuthorAliasGroupDTO[]>([]);
  // 每个分组的「待添加别名」输入框草稿（键为分组下标）。
  const [aliasDrafts, setAliasDrafts] = useState<Record<number, string>>({});
  const openRef = useRef(false);

  // 打开时（且数据就绪）用后端数据初始化本地可编辑副本；关闭后重置初始化标记，下次再开重新载入。
  useEffect(() => {
    if (open && !openRef.current && data) {
      setGroups(
        data.groups.map((g) => ({
          canonical: g.canonical,
          aliases: [...g.aliases],
        })),
      );
      setAliasDrafts({});
      openRef.current = true;
    }
    if (!open) {
      openRef.current = false;
    }
  }, [open, data]);

  if (!open) return null;

  const updateGroup = (
    idx: number,
    patch: Partial<app.AuthorAliasGroupDTO>,
  ) => {
    setGroups((prev) =>
      prev.map((g, i) => (i === idx ? { ...g, ...patch } : g)),
    );
  };

  const addAlias = (idx: number) => {
    const draft = (aliasDrafts[idx] ?? "").trim();
    if (!draft) return;
    setGroups((prev) =>
      prev.map((g, i) => {
        if (i !== idx) return g;
        if (g.aliases.includes(draft)) return g;
        return { ...g, aliases: [...g.aliases, draft] };
      }),
    );
    setAliasDrafts((prev) => ({ ...prev, [idx]: "" }));
  };

  const removeAlias = (idx: number, alias: string) => {
    setGroups((prev) =>
      prev.map((g, i) =>
        i === idx ? { ...g, aliases: g.aliases.filter((a) => a !== alias) } : g,
      ),
    );
  };

  const addGroup = () => {
    setGroups((prev) => [...prev, { canonical: "", aliases: [] }]);
  };

  const removeGroup = (idx: number) => {
    setGroups((prev) => prev.filter((_, i) => i !== idx));
    setAliasDrafts((prev) => {
      const next = { ...prev };
      delete next[idx];
      return next;
    });
  };

  const handleSave = async () => {
    // 校验：忽略规范名为空的分组；检测重复规范名与「别名==某规范名」冲突。
    const cleaned = groups.filter((g) => g.canonical.trim() !== "");
    const canonSet = new Set<string>();
    for (const g of cleaned) {
      const key = g.canonical.trim();
      if (canonSet.has(key)) {
        toastError(t("skill.alias.duplicateCanonical", { name: key }));
        return;
      }
      canonSet.add(key);
    }
    for (const g of cleaned) {
      for (const al of g.aliases) {
        if (canonSet.has(al.trim()) && al.trim() !== g.canonical.trim()) {
          toastError(
            t("skill.alias.aliasEqualsCanonical", { name: al.trim() }),
          );
          return;
        }
      }
    }
    try {
      await saveMutation.mutateAsync(
        app.SaveAuthorAliasesInput.createFrom({ groups: cleaned }),
      );
      toastSuccess(t("skill.alias.saveSuccess"));
      onClose();
    } catch (err) {
      toastError(t("skill.alias.saveFailed") + ": " + toErrorMessage(err));
      console.error(err);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative bg-background border rounded-xl shadow-xl w-[560px] max-h-[82vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center gap-2 px-5 py-4 border-b">
          <Tag className="w-5 h-5 text-primary" />
          <h2 className="text-base font-semibold">{t("skill.alias.manage")}</h2>
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
            {t("skill.alias.manageDesc")}
          </p>

          {isLoading ? (
            <div className="flex items-center justify-center py-6 text-xs text-muted-foreground">
              {t("skill.loading")}
            </div>
          ) : groups.length === 0 ? (
            <div className="px-2 py-4 text-xs text-muted-foreground">
              {t("skill.alias.empty")}
            </div>
          ) : (
            <div className="space-y-3">
              {groups.map((g, idx) => (
                <div
                  key={idx}
                  className="border rounded-lg p-3 space-y-2 bg-muted/20"
                >
                  <div className="flex items-center gap-2">
                    <input
                      type="text"
                      value={g.canonical}
                      onChange={(e) =>
                        updateGroup(idx, { canonical: e.target.value })
                      }
                      placeholder={t("skill.alias.canonicalPlaceholder")}
                      className="flex-1 px-2 py-1 text-sm bg-background border rounded outline-none focus:ring-1 focus:ring-ring"
                    />
                    <button
                      onClick={() => removeGroup(idx)}
                      className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-destructive transition-colors"
                      title={t("skill.alias.removeGroup")}
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>

                  <div className="space-y-1">
                    <div className="text-[10px] font-semibold text-muted-foreground/60 uppercase tracking-wider">
                      {t("skill.alias.aliases")}
                    </div>
                    {g.aliases.length > 0 && (
                      <div className="flex flex-wrap gap-1.5">
                        {g.aliases.map((al) => (
                          <span
                            key={al}
                            className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs text-foreground"
                          >
                            {al}
                            <button
                              onClick={() => removeAlias(idx, al)}
                              className="text-muted-foreground hover:text-destructive transition-colors"
                              title={t("skill.alias.removeAlias")}
                            >
                              <X className="w-3 h-3" />
                            </button>
                          </span>
                        ))}
                      </div>
                    )}
                    <div className="flex items-center gap-1">
                      <input
                        type="text"
                        value={aliasDrafts[idx] ?? ""}
                        onChange={(e) =>
                          setAliasDrafts((prev) => ({
                            ...prev,
                            [idx]: e.target.value,
                          }))
                        }
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            addAlias(idx);
                          }
                        }}
                        placeholder={t("skill.alias.aliasPlaceholder")}
                        className="flex-1 px-2 py-1 text-xs bg-background border rounded outline-none focus:ring-1 focus:ring-ring"
                      />
                      <button
                        onClick={() => addAlias(idx)}
                        className="p-1 rounded hover:bg-muted/60 text-muted-foreground hover:text-primary transition-colors"
                        title={t("skill.alias.addAlias")}
                      >
                        <Plus className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          <button
            onClick={addGroup}
            className="w-full flex items-center justify-center gap-1 px-2 py-1.5 text-xs rounded-lg border border-dashed hover:bg-muted/40 transition-colors text-muted-foreground hover:text-foreground"
          >
            <Plus className="w-3.5 h-3.5" />
            {t("skill.alias.addGroup")}
          </button>
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
            onClick={handleSave}
            disabled={saveMutation.isPending}
            className="px-3 py-1.5 text-sm rounded-lg bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
          >
            {saveMutation.isPending
              ? t("skill.alias.saving")
              : t("skill.alias.save")}
          </button>
        </div>
      </div>
    </div>
  );
}
