import { useMemo } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ExternalLink, Check, X, Loader2 } from "lucide-react";
import type { skill } from "@/lib/wailsjs/go/models";
import { useSkills } from "./useSkills";
import {
  useEnabledSkills,
  useSetEnabledSkills,
} from "@/components/chat/useNovelSkills";

export type SourceFilter = "all" | "builtin" | "user" | "novel";
export type ModeFilter = "all" | "auto" | "manual" | "always";

interface Props {
  novelId: number;
  onOpenSkill: (name: string, source: string) => void;
  search: string;
  sourceFilter: SourceFilter;
  modeFilter: ModeFilter;
}

function modeBadgeClass(mode: string): string {
  switch (mode) {
    case "manual":
      return "bg-tag-blue text-tag-blue-foreground";
    case "always":
      return "bg-tag-green text-tag-green-foreground";
    default:
      return "bg-tag-amber text-tag-amber-foreground";
  }
}

function modeLabel(
  t: (k: string, opts?: Record<string, unknown>) => string,
  mode: string,
): string {
  if (mode === "manual") return t("skill.modeCommand");
  if (mode === "always") return t("skill.modePermanent");
  return t("skill.modeSmart");
}

function sourceLabel(
  t: (k: string, opts?: Record<string, unknown>) => string,
  source: string,
): string {
  if (source === "builtin") return t("skill.builtin");
  if (source === "user") return t("skill.userLevel");
  return t("skill.currentNovel");
}

export default function SkillMatrixView({
  novelId,
  onOpenSkill,
  search,
  sourceFilter,
  modeFilter,
}: Props) {
  const { t } = useTranslation();
  const { data: skills = [], isLoading, isError } = useSkills(novelId);
  const enabledQuery = useEnabledSkills(novelId);
  const setEnabled = useSetEnabledSkills(novelId);

  const enabledSet = useMemo(
    () => new Set(enabledQuery.data ?? []),
    [enabledQuery.data],
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return skills.filter((s) => {
      if (sourceFilter !== "all" && s.source !== sourceFilter) return false;
      if (modeFilter !== "all" && s.mode !== modeFilter) return false;
      if (
        q &&
        !s.name.toLowerCase().includes(q) &&
        !s.description.toLowerCase().includes(q)
      )
        return false;
      return true;
    });
  }, [skills, search, sourceFilter, modeFilter]);

  const grouped = useMemo(() => {
    const map = new Map<string, skill.SkillMeta[]>();
    for (const s of filtered) {
      const cat = s.category?.trim() || "";
      const key = cat || "__uncategorized__";
      if (!map.has(key)) map.set(key, []);
      map.get(key)!.push(s);
    }
    return [...map.entries()].sort((a, b) => {
      if (a[0] === "__uncategorized__") return 1;
      if (b[0] === "__uncategorized__") return -1;
      return a[0].localeCompare(b[0]);
    });
  }, [filtered]);

  const toggleEnabled = (name: string, current: boolean) => {
    const base = new Set(enabledSet);
    if (current) base.delete(name);
    else base.add(name);
    setEnabled.mutate([...base]);
  };

  if (isLoading) {
    return (
      <CenterMsg>
        <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
        <span className="text-sm text-muted-foreground">
          {t("skillMatrix.loading")}
        </span>
      </CenterMsg>
    );
  }
  if (isError) {
    return (
      <CenterMsg>
        <span className="text-sm text-destructive">
          {t("skillMatrix.loadFailed")}
        </span>
      </CenterMsg>
    );
  }

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <div className="flex-1 overflow-y-auto overscroll-contain px-6 py-4">
        {skills.length === 0 ? (
          <CenterMsg>
            <span className="text-sm text-muted-foreground">
              {t("skillMatrix.noSkills")}
            </span>
          </CenterMsg>
        ) : filtered.length === 0 ? (
          <CenterMsg>
            <span className="text-sm text-muted-foreground">
              {t("skillMatrix.noResults")}
            </span>
          </CenterMsg>
        ) : (
          <div className="space-y-6">
            {grouped.map(([key, items]) => (
              <section key={key}>
                <div className="flex items-center gap-2 mb-2">
                  <h2 className="text-sm font-semibold">
                    {key === "__uncategorized__"
                      ? t("skill.uncategorized")
                      : key}
                  </h2>
                  <span className="text-xs text-muted-foreground">
                    ({items.length})
                  </span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-3">
                  {items.map((s) => {
                    const enabled = enabledSet.has(s.name);
                    return (
                      <div
                        key={`${s.source}:${s.name}`}
                        className="flex flex-col rounded-lg border bg-card p-3 hover:border-primary/40 transition-colors"
                      >
                        <div className="flex items-start justify-between gap-2">
                          <button
                            onClick={() => onOpenSkill(s.name, s.source)}
                            className="text-sm font-medium text-left hover:text-primary transition-colors truncate"
                            title={s.name}
                          >
                            {s.name}
                          </button>
                          <button
                            onClick={() => toggleEnabled(s.name, enabled)}
                            disabled={setEnabled.isPending}
                            title={t("skillMatrix.enabledAria", {
                              name: s.name,
                            })}
                            className={`shrink-0 inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] border transition-colors ${
                              enabled
                                ? "bg-primary/10 text-primary border-primary/30"
                                : "bg-muted text-muted-foreground border-border"
                            }`}
                          >
                            {enabled ? (
                              <Check className="w-3 h-3" />
                            ) : (
                              <X className="w-3 h-3" />
                            )}
                            {enabled
                              ? t("skillMatrix.enabledLabel")
                              : t("skillMatrix.disabledLabel")}
                          </button>
                        </div>

                        <p className="mt-1 text-[11px] text-muted-foreground line-clamp-2 min-h-[2rem]">
                          {s.description || t("skillMatrix.emptyDesc")}
                        </p>

                        {s.use_when && s.use_when.length > 0 && (
                          <p className="mt-1 text-[10px] text-muted-foreground line-clamp-2">
                            <span className="text-primary/70">
                              {t("skillMatrix.useWhen")}：
                            </span>
                            {s.use_when.map((e) => e.intent).join("；")}
                          </p>
                        )}

                        <div className="mt-2 flex flex-wrap items-center gap-1">
                          <span
                            className={`rounded px-1.5 py-0.5 text-[10px] ${modeBadgeClass(s.mode)}`}
                          >
                            {modeLabel(t, s.mode)}
                          </span>
                          <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                            {sourceLabel(t, s.source)}
                          </span>
                          {s.aggregated && (
                            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                              {t("skill.authorProfiles")}
                            </span>
                          )}
                          <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground font-mono">
                            /{s.name}
                          </span>
                        </div>

                        <button
                          onClick={() => onOpenSkill(s.name, s.source)}
                          className="mt-2 inline-flex items-center gap-1 text-[11px] text-primary hover:underline self-start"
                        >
                          <ExternalLink className="w-3 h-3" />
                          {t("skillMatrix.open")}
                        </button>
                      </div>
                    );
                  })}
                </div>
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function CenterMsg({ children }: { children: ReactNode }) {
  return (
    <div className="h-full flex items-center justify-center">{children}</div>
  );
}
