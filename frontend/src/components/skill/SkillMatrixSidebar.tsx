import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { LayoutGrid, Search } from "lucide-react";
import type { skill } from "@/lib/wailsjs/go/models";
import { useSkills } from "./useSkills";
import { useEnabledSkills } from "@/components/chat/useNovelSkills";

type SourceFilter = "all" | "builtin" | "user" | "novel";
type ModeFilter = "all" | "auto" | "manual" | "always";

interface Props {
  novelId: number;
  search: string;
  onSearchChange: (v: string) => void;
  sourceFilter: SourceFilter;
  onSourceChange: (v: SourceFilter) => void;
  modeFilter: ModeFilter;
  onModeChange: (v: ModeFilter) => void;
}

export default function SkillMatrixSidebar({
  novelId,
  search,
  onSearchChange,
  sourceFilter,
  onSourceChange,
  modeFilter,
  onModeChange,
}: Props) {
  const { t } = useTranslation();
  const { data: skills = [], isLoading } = useSkills(novelId);
  const enabledQuery = useEnabledSkills(novelId);

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

  const stats = useMemo(() => {
    const total = skills.length;
    const enabledCount = skills.filter((s) => enabledSet.has(s.name)).length;
    const byMode = { auto: 0, manual: 0, always: 0 };
    const bySource = { builtin: 0, user: 0, novel: 0 };
    for (const s of skills) {
      if (s.mode === "auto" || s.mode === "manual" || s.mode === "always")
        byMode[s.mode]++;
      if (s.source === "builtin" || s.source === "user" || s.source === "novel")
        bySource[s.source]++;
    }
    return { total, enabledCount, byMode, bySource };
  }, [skills, enabledSet]);

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

  return (
    <div className="flex flex-col h-full overflow-hidden bg-sidebar">
      <div className="px-4 py-3 border-b shrink-0">
        <h2 className="text-sm font-semibold flex items-center gap-2 text-foreground">
          <LayoutGrid className="w-4 h-4 text-primary" />
          {t("skillMatrix.title")}
        </h2>
        <p className="text-xs text-muted-foreground mt-1 leading-relaxed">
          {t("skillMatrix.subtitle")}
        </p>
      </div>

      <div className="flex-1 overflow-y-auto overscroll-contain px-4 py-3 space-y-4">
        {isLoading ? null : (
          <>
            <div className="grid grid-cols-2 gap-2">
              <StatChip
                label={t("skillMatrix.total")}
                value={stats.total}
                full
              />
              <StatChip
                label={t("skillMatrix.enabledLabel")}
                value={stats.enabledCount}
                full
              />
              <StatChip
                label={t("skill.modeSmart")}
                value={stats.byMode.auto}
              />
              <StatChip
                label={t("skill.modeCommand")}
                value={stats.byMode.manual}
              />
              <StatChip
                label={t("skill.modePermanent")}
                value={stats.byMode.always}
              />
              <StatChip
                label={t("skill.builtin")}
                value={stats.bySource.builtin}
              />
              <StatChip
                label={t("skill.userLevel")}
                value={stats.bySource.user}
              />
              <StatChip
                label={t("skill.currentNovel")}
                value={stats.bySource.novel}
                full
              />
            </div>

            <div className="relative">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <input
                type="text"
                value={search}
                onChange={(e) => onSearchChange(e.target.value)}
                placeholder={t("skillMatrix.searchPlaceholder")}
                className="w-full pl-7 pr-2 py-1.5 text-sm bg-muted/40 rounded-md border-0 outline-none focus:ring-1 focus:ring-ring"
              />
            </div>

            <FilterGroup
              label={t("skillMatrix.sourceFilter")}
              options={[
                { value: "all", label: t("skillMatrix.filterAll") },
                { value: "builtin", label: t("skill.builtin") },
                { value: "user", label: t("skill.userLevel") },
                { value: "novel", label: t("skill.currentNovel") },
              ]}
              active={sourceFilter}
              onChange={(v) => onSourceChange(v as SourceFilter)}
            />
            <FilterGroup
              label={t("skillMatrix.modeFilter")}
              options={[
                { value: "all", label: t("skillMatrix.filterAll") },
                { value: "auto", label: t("skill.modeSmart") },
                { value: "manual", label: t("skill.modeCommand") },
                { value: "always", label: t("skill.modePermanent") },
              ]}
              active={modeFilter}
              onChange={(v) => onModeChange(v as ModeFilter)}
            />

            <div className="pt-2 border-t">
              <h3 className="text-xs font-medium text-foreground mb-2">
                {t("skillMatrix.categories")}
              </h3>
              {grouped.length === 0 ? (
                <p className="text-xs text-muted-foreground">
                  {t("skillMatrix.noResults")}
                </p>
              ) : (
                <ul className="space-y-1">
                  {grouped.map(([key, items]) => (
                    <li
                      key={key}
                      className="flex items-center justify-between text-xs py-1 px-2 rounded-md hover:bg-muted/50"
                    >
                      <span className="text-foreground truncate">
                        {key === "__uncategorized__"
                          ? t("skill.uncategorized")
                          : key}
                      </span>
                      <span className="text-muted-foreground tabular-nums">
                        {items.length}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function StatChip({
  label,
  value,
  full = false,
}: {
  label: string;
  value: number;
  full?: boolean;
}) {
  return (
    <div
      className={`flex items-center justify-between gap-1.5 rounded-md border bg-card px-2 py-1 ${
        full ? "col-span-2" : ""
      }`}
    >
      <span className="text-[11px] text-muted-foreground truncate">
        {label}
      </span>
      <span className="text-sm font-semibold tabular-nums">{value}</span>
    </div>
  );
}

function FilterGroup({
  label,
  options,
  active,
  onChange,
}: {
  label: string;
  options: { value: string; label: string }[];
  active: string;
  onChange: (v: string) => void;
}) {
  return (
    <div className="space-y-1.5">
      <span className="text-[11px] text-muted-foreground">{label}</span>
      <div className="flex flex-wrap gap-1.5">
        {options.map((opt) => (
          <button
            key={opt.value}
            onClick={() => onChange(opt.value)}
            className={`px-2 py-1 text-[11px] rounded-md border transition-colors ${
              active === opt.value
                ? "bg-primary text-primary-foreground border-primary"
                : "text-muted-foreground hover:bg-muted border-border"
            }`}
          >
            {opt.label}
          </button>
        ))}
      </div>
    </div>
  );
}
