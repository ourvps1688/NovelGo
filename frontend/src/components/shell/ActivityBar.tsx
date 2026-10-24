import { useState } from "react";
import type { LucideIcon } from "lucide-react";
import {
  Library,
  ScrollText,
  FolderTree,
  Search,
  SlidersHorizontal,
  BookOpen,
  UsersRound,
  MapPin,
  Spline,
  History,
  GitGraph,
  Glasses,
  Puzzle,
  LayoutGrid,
  Sparkles,
  ChevronsLeft,
  ChevronsRight,
  Target,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { SidebarPanelId } from "@/types/panel";
import { usePanelStore } from "@/stores/usePanelStore";
import {
  FACTORY_ORDER,
  JOURNEY_BY_FACTORY,
  factoryFeedsKey,
  factoryOf,
  factoryTitleKey,
  type FactoryId,
} from "@/lib/factory";

interface Activity {
  id: SidebarPanelId;
  icon: LucideIcon;
  labelKey: string;
  disabled?: boolean;
}

// 现有 16 项功能条目全保留（plan §7 功能型入口不硬移除），
// 归属由 factory.ts FACTORY_MAP 单源决定，此处不自造归属。
const activities: Activity[] = [
  { id: "search", icon: Search, labelKey: "shell.search" },
  { id: "novels", icon: Library, labelKey: "shell.bookshelf" },
  { id: "chapters", icon: ScrollText, labelKey: "shell.chapters" },
  {
    id: "chapter-management",
    icon: FolderTree,
    labelKey: "shell.chapterManagement",
  },
  { id: "preferences", icon: SlidersHorizontal, labelKey: "shell.preference" },
  { id: "novel-settings", icon: BookOpen, labelKey: "shell.novelSetting" },
  { id: "characters", icon: UsersRound, labelKey: "shell.characters" },
  { id: "locations", icon: MapPin, labelKey: "shell.locations" },
  { id: "storyarcs", icon: Spline, labelKey: "shell.arcs" },
  { id: "timeline", icon: History, labelKey: "shell.timeline" },
  { id: "git", icon: GitGraph, labelKey: "shell.gitHistory" },
  { id: "reader", icon: Glasses, labelKey: "shell.readerView" },
  { id: "skills", icon: Puzzle, labelKey: "shell.skills" },
  { id: "skill-matrix", icon: LayoutGrid, labelKey: "shell.skillMatrix" },
  { id: "style-samples", icon: Sparkles, labelKey: "shell.extract" },
  { id: "signability", icon: Target, labelKey: "shell.workflow" },
];

interface Props {
  onSelect: (id: SidebarPanelId) => void;
}

const COLLAPSE_KEY = "ng-activitybar-collapsed";

function getInitialCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSE_KEY) === "1";
  } catch {
    return false;
  }
}

// 按价值链顺序（吸收→创作→质检→基础）分组，组内保持既有相对顺序。
function groupActivities(): { factory: FactoryId; items: Activity[] }[] {
  return FACTORY_ORDER.map((factory) => ({
    factory,
    items: activities.filter((a) => factoryOf(a.id) === factory),
  })).filter((g) => g.items.length > 0);
}

export default function ActivityBar({ onSelect }: Props) {
  const { t } = useTranslation();
  const activeId = usePanelStore((s) => s.sidebarPanel ?? s.activePanel);
  const [collapsed, setCollapsed] = useState(getInitialCollapsed);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(COLLAPSE_KEY, next ? "1" : "0");
      } catch {
        /* ignore */
      }
      return next;
    });
  };

  const groups = groupActivities();

  return (
    <nav
      className={`flex flex-col py-3 gap-1.5 border-r bg-sidebar select-none cursor-default transition-all duration-200 ${
        collapsed ? "w-12 items-center" : "w-56 px-2 items-stretch"
      }`}
    >
      <button
        type="button"
        onClick={toggleCollapsed}
        title={collapsed ? t("shell.expand") : t("shell.collapse")}
        className={`mb-1 flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground/70 transition-colors hover:bg-muted hover:text-foreground ${
          collapsed ? "mx-auto" : "self-end"
        }`}
      >
        {collapsed ? (
          <ChevronsRight className="h-4 w-4" />
        ) : (
          <ChevronsLeft className="h-4 w-4" />
        )}
      </button>

      {groups.map((group, gi) => {
        const journey = JOURNEY_BY_FACTORY[group.factory];
        const heading = (
          <>
            <div className="text-xs font-semibold text-muted-foreground/80 truncate">
              {t(factoryTitleKey(group.factory))}
            </div>
            <div className="text-[10px] leading-tight text-muted-foreground/50 truncate">
              {t(factoryFeedsKey(group.factory))}
            </div>
          </>
        );
        return (
          <div key={group.factory}>
            {gi > 0 && <div className="w-6 h-px bg-border my-1 mx-auto" />}
            {!collapsed &&
              (journey ? (
                // 组标题=旅程中枢入口（T1.2 验收①：旅程面板可从 ActivityBar 进入）
                <button
                  type="button"
                  data-testid="factory-journey-entry"
                  data-journey={journey}
                  onClick={() => onSelect(journey)}
                  title={t(factoryTitleKey(group.factory))}
                  className="w-full rounded-md px-3 pt-1 pb-0.5 text-left transition-colors hover:bg-muted/60"
                >
                  {heading}
                </button>
              ) : (
                <div className="px-3 pt-1 pb-0.5">{heading}</div>
              ))}
            {group.items.map((a) => {
              const isActive = a.id === activeId;
              return (
                <button
                  key={a.id}
                  disabled={a.disabled}
                  onClick={() => onSelect(a.id)}
                  title={`${t(a.labelKey)}${a.disabled ? t("shell.comingSoon") : ""}`}
                  className={`relative flex h-10 items-center rounded-lg transition-all duration-200
                focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring
                ${
                  collapsed
                    ? "w-10 justify-center"
                    : "w-full gap-3 px-3 justify-start"
                }
                ${
                  a.disabled
                    ? "text-muted-foreground/40 cursor-not-allowed"
                    : isActive
                      ? "text-foreground bg-muted"
                      : "text-muted-foreground hover:text-foreground hover:bg-muted/60"
                }`}
                >
                  {isActive && !a.disabled && (
                    <span className="absolute left-0 top-1/2 -translate-y-1/2 w-0.5 h-5 bg-primary rounded-r-full" />
                  )}
                  <a.icon
                    className="h-[18px] w-[18px] shrink-0"
                    strokeWidth={1.75}
                  />
                  {!collapsed && (
                    <span className="truncate text-sm font-medium">
                      {t(a.labelKey)}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        );
      })}
    </nav>
  );
}
