import { useState, useMemo, useRef, useEffect } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronRight, Plus, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useFocusStore } from "@/stores/useFocusStore";
import { usePanelStore } from "@/stores/usePanelStore";
import type { character } from "@/lib/wailsjs/go/models";
import SearchInput from "@/components/shared/SearchInput";
import { useCharacters } from "./useCharacters";
import { useCharacterStore } from "./useCharacterStore";
import {
  useCharacterGroups,
  useCharacterGroupMembers,
} from "./useCharacterGroups";
import { useCharacterGroupStore } from "./useCharacterGroupStore";
import {
  indexCharacterMemberships,
  filterCharactersByGroup,
} from "./characterGrouping";
import CharacterGroupActions from "./CharacterGroupActions";

interface Props {
  novelId: number;
}

type Section = { id: number; name: string; characters: character.Character[] };

// F-5: 拍平后的可视行模型——分组可折叠列表无法直接虚拟化，先展开为「全部 / 分组头 / 角色项」三类行。
type Row =
  | { kind: "all" }
  | { kind: "header"; section: Section; key: string; expanded: boolean }
  | { kind: "empty"; key: string }
  | { kind: "item"; c: character.Character; groupId: number | null };

export default function CharacterList({ novelId }: Props) {
  const { t } = useTranslation();
  // 4.1.1: characters 数据走 useCharacters query，跨组件共享缓存（CharacterListView / CharacterGraph 同源）。
  // 删除原 useState<characters> + useEffect + useRefresh 链路；CRUD 后由 invalidateQueries 触发自动 refetch。
  const charsQuery = useCharacters(novelId);
  const characters = useMemo(() => charsQuery.data ?? [], [charsQuery.data]);
  const groupsQuery = useCharacterGroups(novelId);
  const membersQuery = useCharacterGroupMembers(novelId);
  const groups = groupsQuery.data ?? [];
  const isError =
    charsQuery.isError || groupsQuery.isError || membersQuery.isError;
  const loading =
    charsQuery.isLoading || groupsQuery.isLoading || membersQuery.isLoading;
  const selectedGroup = useCharacterGroupStore(
    (s) => s.selectedGroups[novelId] ?? null,
  );
  const selectGroup = useCharacterGroupStore((s) => s.selectGroup);
  const editGroup = useCharacterGroupStore((s) => s.editGroup);
  const deleteGroup = useCharacterGroupStore((s) => s.deleteGroup);
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  const focusedId = useFocusStore((s) => s.focusMap.characters?.id);
  // 4.1.2: 删除合并 —— 点删除只 dispatch setDeletingCharacterId，
  // ConfirmDialog + 执行集中在 CharacterListView（唯一确认入口，两处共用）。
  const setDeletingCharacterId = useCharacterStore(
    (s) => s.setDeletingCharacterId,
  );
  // 4b: 点击列表项触发 focusEntity，不区分搜索/非搜索，
  // CharacterListView 的 useEffect 收到后 scrollIntoView + 高亮（与全局搜索点击效果一致）。
  const focusEntity = useFocusStore((s) => s.focusEntity);
  const [search, setSearch] = useState("");
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>(
    {},
  );
  const memberships = useMemo(
    () => indexCharacterMemberships(membersQuery.data ?? []),
    [membersQuery.data],
  );

  const filtered = useMemo(() => {
    if (!search.trim()) return characters;
    const q = search.toLowerCase();
    return characters.filter((c) => c.name.toLowerCase().includes(q));
  }, [characters, search]);

  function handleDelete(charId: number) {
    setActivePanel("characters");
    setDeletingCharacterId(charId);
  }

  function navigateGroup(groupId: number | null) {
    selectGroup(novelId, groupId);
    setActivePanel("characters");
  }

  function renderCharacter(c: character.Character, groupId: number | null) {
    return (
      <div
        key={c.id}
        className={`group relative flex items-center gap-2 px-3 py-1.5 hover:bg-muted ${focusedId === c.id ? "bg-primary/10" : ""}`}
      >
        <button
          type="button"
          onClick={() => {
            navigateGroup(groupId);
            focusEntity("characters", c.id);
          }}
          className="flex min-w-0 flex-1 items-center gap-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span
            aria-hidden="true"
            className="w-5 h-5 rounded-full bg-tag-blue text-tag-blue-foreground text-[10px] font-medium flex items-center justify-center shrink-0"
          >
            {(c.name ?? "").charAt(0) || "?"}
          </span>
          <span className="min-w-0 flex-1 text-sm truncate">{c.name}</span>
        </button>
        <button
          type="button"
          onClick={() => handleDelete(c.id)}
          title={t("character.delete")}
          aria-label={t("characterGroup.deleteCharacter", { name: c.name })}
          className="shrink-0 p-0.5 rounded text-muted-foreground hover:text-destructive opacity-0 group-hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      </div>
    );
  }

  const sections = [
    ...groups.map((group) => ({
      id: group.id,
      name: group.name,
      characters: filterCharactersByGroup(characters, memberships, group.id),
    })),
    {
      id: 0,
      name: t("characterGroup.ungrouped"),
      characters: filterCharactersByGroup(characters, memberships, 0),
    },
  ];

  // F-5: 把分组可折叠结构拍平为可见行（仅展开的分组展开其角色项），交给虚拟滚动渲染。
  const rows = useMemo<Row[]>(() => {
    const out: Row[] = [{ kind: "all" }];
    const q = search.trim().toLowerCase();
    if (groups.length > 0) {
      for (const section of sections) {
        const matchesGroup = q && section.name.toLowerCase().includes(q);
        const items =
          q && !matchesGroup
            ? section.characters.filter((c) => c.name.toLowerCase().includes(q))
            : section.characters;
        if (q && !matchesGroup && items.length === 0) continue;
        const key = `${novelId}:${section.id}`;
        const expanded =
          !!q || (expandedGroups[key] ?? section.id === groups[0]?.id);
        out.push({ kind: "header", section, key, expanded });
        if (expanded) {
          if (items.length === 0) {
            // 展开的空分组显示占位提示（F-5 虚拟化重构时丢失，此处恢复）
            out.push({ kind: "empty", key: `${key}:empty` });
          } else {
            for (const c of items)
              out.push({ kind: "item", c, groupId: section.id });
          }
        }
      }
    } else {
      for (const c of filtered) out.push({ kind: "item", c, groupId: null });
    }
    return out;
  }, [groups, sections, filtered, search, expandedGroups, novelId]);

  const query = search.trim().toLowerCase();
  const showNoMatch =
    !!query &&
    filtered.length === 0 &&
    !groups.some((g) => g.name.toLowerCase().includes(query));
  const showEmpty = !query && characters.length === 0 && groups.length === 0;

  // F-5: 虚拟滚动——角色可能上千条，全量渲染会卡顿；仅渲染可视行。
  const parentRef = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 36,
    overscan: 10,
  });

  // 焦点进入时把对应角色项滚动到视口中央（保留原 scrollIntoView 体验；虚拟化后元素可能未挂载）。
  useEffect(() => {
    if (focusedId == null) return;
    const idx = rows.findIndex(
      (r) => r.kind === "item" && r.c.id === focusedId,
    );
    if (idx >= 0) virtualizer.scrollToIndex(idx, { align: "center" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusedId]);

  function renderRow(row: Row) {
    if (row.kind === "all") {
      return (
        <button
          type="button"
          onClick={() => navigateGroup(null)}
          aria-pressed={
            selectedGroup === null ||
            (selectedGroup > 0 && !groups.some((g) => g.id === selectedGroup))
          }
          className="flex w-full items-center justify-between border-b px-3 py-2 text-left text-xs font-medium hover:bg-muted aria-pressed:bg-primary/10 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
        >
          <span>{t("characterGroup.all")}</span>{" "}
          <span>{characters.length}</span>
        </button>
      );
    }
    if (row.kind === "header") {
      const { section, key, expanded } = row;
      return (
        <div
          className="border-b border-border/50"
          data-testid={`group-header-${section.name}`}
        >
          <div className="flex items-center">
            <button
              type="button"
              aria-expanded={expanded}
              disabled={!!query}
              aria-label={t("characterGroup.toggle", { name: section.name })}
              onClick={() =>
                setExpandedGroups((prev) => ({ ...prev, [key]: !expanded }))
              }
              className="ml-1 rounded p-1.5 text-muted-foreground hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
            >
              <ChevronRight
                className={`h-3.5 w-3.5 transition-transform ${expanded ? "rotate-90" : ""}`}
              />
            </button>
            <button
              type="button"
              onClick={() => navigateGroup(section.id)}
              aria-pressed={selectedGroup === section.id}
              className="flex min-w-0 flex-1 items-center gap-2 px-1 py-2 text-left text-xs font-medium hover:bg-muted aria-pressed:text-primary focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="flex-1 truncate" title={section.name}>
                {section.name}
              </span>{" "}
              <span className="text-[10px] text-muted-foreground">
                {section.characters.length}
              </span>
            </button>
            {section.id > 0 && (
              <CharacterGroupActions
                name={section.name}
                onEdit={() => {
                  navigateGroup(section.id);
                  editGroup(novelId, section.id);
                }}
                onDelete={() => {
                  setActivePanel("characters");
                  deleteGroup(novelId, section.id);
                }}
              />
            )}
          </div>
        </div>
      );
    }
    if (row.kind === "empty") {
      return (
        <p className="px-3 py-2 text-xs text-muted-foreground">
          {t("characterGroup.empty")}
        </p>
      );
    }
    return renderCharacter(row.c, row.groupId);
  }

  return (
    <>
      <div className="flex items-center justify-between px-3 py-2.5 border-b">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          {t("character.character")} ({characters.length})
        </span>
        <button
          type="button"
          aria-label={t("characterGroup.create")}
          title={t("characterGroup.create")}
          disabled={
            novelId <= 0 || groupsQuery.isLoading || groupsQuery.isError
          }
          onClick={() => {
            setActivePanel("characters");
            editGroup(novelId, null);
          }}
          className="rounded p-1 text-muted-foreground hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
        >
          <Plus className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="px-2 py-1.5 border-b">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("character.searchCharacter")}
        />
      </div>

      <div
        ref={parentRef}
        className="flex-1 overflow-y-auto overscroll-contain"
      >
        {loading ? (
          <p className="px-3 py-4 text-xs text-muted-foreground">
            {t("character.loading")}
          </p>
        ) : isError ? (
          <div className="flex items-center justify-center h-full">
            <p className="text-xs text-destructive">
              {t("character.charsLoadFailed")}
            </p>
          </div>
        ) : showEmpty ? (
          <div className="flex items-center justify-center h-full">
            <p className="px-3 py-4 text-xs text-muted-foreground">
              {search
                ? t("character.noMatchingCharacters")
                : t("character.noCharacters")}
            </p>
          </div>
        ) : showNoMatch ? (
          <p className="px-3 py-4 text-xs text-muted-foreground">
            {t("character.noMatchingCharacters")}
          </p>
        ) : (
          <div
            style={{
              height: virtualizer.getTotalSize(),
              position: "relative",
              width: "100%",
            }}
          >
            {virtualizer.getVirtualItems().map((vi) => (
              <div
                key={vi.key}
                data-index={vi.index}
                ref={virtualizer.measureElement}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${vi.start}px)`,
                }}
              >
                {renderRow(rows[vi.index])}
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  );
}
