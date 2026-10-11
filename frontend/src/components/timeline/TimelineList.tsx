import { useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Target, Lightbulb } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useFocusStore } from "@/stores/useFocusStore";
import SearchInput from "@/components/shared/SearchInput";
import { useTimelineEntries } from "./useTimelineEntries";

interface Props {
  novelId: number;
}

export default function SidebarTimelineList({ novelId }: Props) {
  const { t } = useTranslation();
  const { data: entries = [], isError } = useTimelineEntries(novelId);
  // 4b: 点击条目触发 focusEntity，TimelineView 的 useEffect 滑窗对齐到 entry 章节。
  const focusEntity = useFocusStore((s) => s.focusEntity);
  const [search, setSearch] = useState("");

  // 搜 title + content（与后端 ListByNovel(Search) 字段一致）
  const filtered = useMemo(() => {
    if (!search.trim()) return entries;
    const q = search.toLowerCase();
    return entries.filter(
      (e) =>
        e.title.toLowerCase().includes(q) ||
        (e.content || "").toLowerCase().includes(q),
    );
  }, [entries, search]);

  const catIcon = (cat: string) => {
    switch (cat) {
      case "foreshadowing":
        return (
          <Target className="h-3 w-3 text-tag-amber-foreground shrink-0" />
        );
      case "user_directive":
        return (
          <Lightbulb className="h-3 w-3 text-tag-purple-foreground shrink-0" />
        );
      default:
        return <Target className="h-3 w-3 text-muted-foreground shrink-0" />;
    }
  };

  const statusDot = (status: string) => {
    switch (status) {
      case "pending":
        return "bg-tag-blue";
      case "resolved":
        return "bg-tag-green";
      default:
        return "bg-muted";
    }
  };

  // F-5: 虚拟滚动——条目可能上千条，全量渲染会卡顿；用 @tanstack/react-virtual 仅渲染可视区。
  const parentRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: filtered.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 44,
    overscan: 8,
  });

  return (
    <>
      <div className="flex items-center justify-between px-3 py-2.5 border-b">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          {t("timeline.foreshadowingOrInstruction")} ({entries.length})
        </span>
      </div>
      <div className="px-2 py-1.5 border-b">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("timeline.searchTimeline")}
        />
      </div>
      <div
        ref={parentRef}
        className="flex-1 overflow-y-auto overscroll-contain"
      >
        {isError ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-xs text-destructive">
              {t("timeline.loadFailed")}
            </p>
          </div>
        ) : filtered.length === 0 ? (
          <div className="flex items-center justify-center h-full">
            <p className="px-3 py-4 text-xs text-muted-foreground">
              {search
                ? t("timeline.noMatchingEntries")
                : t("timeline.noEntries")}
            </p>
          </div>
        ) : (
          <div
            style={{
              height: virtualizer.getTotalSize(),
              position: "relative",
              width: "100%",
            }}
          >
            {virtualizer.getVirtualItems().map((vi) => {
              const e = filtered[vi.index];
              return (
                <div
                  key={e.id}
                  data-index={vi.index}
                  ref={virtualizer.measureElement}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${vi.start}px)`,
                  }}
                  onClick={() => focusEntity("timeline", e.id)}
                  className="w-full flex items-center gap-2 px-3 py-1.5 text-left cursor-pointer hover:bg-muted hover:rounded-md transition-colors group"
                >
                  {catIcon(e.category)}
                  <div className="flex-1 min-w-0">
                    <span className="text-xs truncate block text-foreground">
                      {e.title}
                    </span>
                    <span className="text-[10px] text-muted-foreground">
                      {t("timeline.targetChapterN2", {
                        n: e.target_reading_number,
                      })}
                    </span>
                  </div>
                  <span
                    className={`shrink-0 h-1.5 w-1.5 rounded-full ${statusDot(
                      e.status,
                    )}`}
                  />
                </div>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}
