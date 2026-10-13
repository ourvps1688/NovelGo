import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import { Check, PenLine, Search, X } from "lucide-react";
import type { PersonaOption } from "./usePersonaOptions";
import type { PersonaSelection } from "./usePersonaSetting";

const MENU_MAX_HEIGHT = 320;
const GAP = 8;

interface Props {
  position: { top: number; left: number; width: number };
  options: PersonaOption[];
  selectedPersona: PersonaSelection;
  onSelect: (p: PersonaSelection) => void;
  onClose: () => void;
}

// PersonaPicker 是聊天输入区"写作模式"菜单：
// 列出本书可用的写作人格（mental 技能 / 作者），以及一个"普通"选项（关闭 persona）。
// 选中即写入本书持久化设置（由调用方 SavePersonaSetting），并在本轮及后续对话注入。
export default function PersonaPicker({
  position,
  options,
  selectedPersona,
  onSelect,
  onClose,
}: Props) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    const timer = window.setTimeout(() => {
      document.addEventListener("mousedown", onDown);
    }, 0);
    document.addEventListener("keydown", onKey);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const normalizedSelected =
    selectedPersona != null
      ? `${selectedPersona.kind}:${selectedPersona.ref}`
      : "";

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter(
      (o) =>
        o.label.toLowerCase().includes(q) ||
        (o.description ?? "").toLowerCase().includes(q),
    );
  }, [options, query]);

  const style = useMemo(() => {
    const spaceAbove = position.top - GAP;
    const maxH = Math.min(MENU_MAX_HEIGHT, spaceAbove);
    const width = Math.max(280, Math.min(360, position.width));
    let left = position.left;
    if (left + width > window.innerWidth - GAP) {
      left = Math.max(GAP, window.innerWidth - width - GAP);
    }
    return {
      left,
      width,
      maxHeight: maxH,
      bottom: window.innerHeight - position.top + GAP,
    };
  }, [position]);

  const kindLabel = (kind: string) =>
    kind === "author"
      ? t("chat.persona.authorKind")
      : t("chat.persona.skillKind");

  return createPortal(
    <div
      ref={rootRef}
      data-testid="persona-picker"
      className="fixed z-[9999] rounded-lg border bg-popover text-popover-foreground shadow-lg flex flex-col overflow-hidden"
      style={style}
    >
      <div className="flex items-center justify-between px-3 pt-2.5 pb-1">
        <span className="text-xs font-semibold text-muted-foreground">
          {t("chat.persona.title")}
        </span>
        <button
          onClick={onClose}
          className="rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
          aria-label={t("common.close")}
        >
          <X className="w-3.5 h-3.5" />
        </button>
      </div>
      <div className="px-3 pb-2">
        <div className="flex items-center gap-1.5 rounded-md border border-border bg-muted/40 px-2 py-1.5">
          <Search className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
          <input
            ref={searchRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t("chat.persona.search")}
            className="flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
          />
        </div>
      </div>
      <div className="flex-1 overflow-y-auto border-t border-border py-1">
        {/* 普通模式（关闭 persona） */}
        <button
          onClick={() => onSelect(null)}
          className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-muted/60"
        >
          <span
            className={`flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
              normalizedSelected === ""
                ? "border-primary bg-primary text-primary-foreground"
                : "border-border"
            }`}
          >
            {normalizedSelected === "" && <Check className="w-3 h-3" />}
          </span>
          <span className="min-w-0 flex-1">
            <span className="text-sm font-medium">
              {t("chat.persona.normal")}
            </span>
            <span className="mt-0.5 block text-xs text-muted-foreground">
              {t("chat.persona.normalDesc")}
            </span>
          </span>
        </button>

        {filtered.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">
            {query ? t("chat.persona.empty") : t("chat.persona.empty")}
          </div>
        ) : (
          filtered.map((o) => {
            const key = `${o.kind}:${o.ref}`;
            const checked = key === normalizedSelected;
            return (
              <button
                key={key}
                onClick={() => onSelect({ kind: o.kind, ref: o.ref })}
                className="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-muted/60"
              >
                <PenLine className="mt-0.5 w-3.5 h-3.5 text-muted-foreground shrink-0" />
                <span
                  className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded border ${
                    checked
                      ? "border-primary bg-primary text-primary-foreground"
                      : "border-border"
                  }`}
                >
                  {checked && <Check className="w-3 h-3" />}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate text-sm font-medium">
                      {o.label}
                    </span>
                    <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      {kindLabel(o.kind)}
                    </span>
                  </span>
                  {o.description && (
                    <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">
                      {o.description}
                    </span>
                  )}
                </span>
              </button>
            );
          })
        )}
      </div>
    </div>,
    document.body,
  );
}
