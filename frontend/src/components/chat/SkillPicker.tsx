import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { createPortal } from "react-dom";
import { Check, Loader2, Search, Zap, Play, Star, X } from "lucide-react";
import type { skill } from "@/lib/wailsjs/go/models";
import { useSkills } from "@/components/skill/useSkills";
import { useEnabledSkills, useSetEnabledSkills } from "./useNovelSkills";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

// 来源徽标文案走 i18n：chat.skillPicker.source_novel / source_user / source_builtin

const MODE_ICON: Record<string, React.ReactNode> = {
  auto: <Zap className="w-3.5 h-3.5 text-tag-amber-foreground shrink-0" />,
  manual: <Play className="w-3.5 h-3.5 text-tag-blue-foreground shrink-0" />,
  always: <Star className="w-3.5 h-3.5 text-tag-green-foreground shrink-0" />,
};

const MENU_MAX_HEIGHT = 320;
const GAP = 8;

interface Props {
  novelId: number;
  position: { top: number; left: number; width: number };
  onClose: () => void;
}

// SkillPicker 是聊天输入区"+"菜单里的技能勾选面板：
// 勾选即对本作品常驻注入（等效临时 always），取消即恢复原 mode 行为。
// 启用清单按小说持久化（后端 SetEnabledSkills），不影响技能文件本身。
export default function SkillPicker({ novelId, position, onClose }: Props) {
  const { t } = useTranslation();
  const [query, setQuery] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  const skillsQuery = useSkills(novelId);
  const enabledQuery = useEnabledSkills(novelId);
  const setEnabled = useSetEnabledSkills(novelId);

  useEffect(() => {
    searchRef.current?.focus();
  }, []);

  // 点击面板外部关闭；面板内部点击不冒泡误关
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    // setTimeout 等一帧，避免打开面板的那次点击立即触发 onDown
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

  const enabledSet = useMemo(
    () => new Set(enabledQuery.data ?? []),
    [enabledQuery.data],
  );

  const filtered = useMemo(() => {
    const all = skillsQuery.data ?? [];
    const q = query.trim().toLowerCase();
    if (!q) return all;
    return all.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        (s.description ?? "").toLowerCase().includes(q),
    );
  }, [skillsQuery.data, query]);

  const toggle = (s: skill.SkillMeta) => {
    const next = new Set(enabledSet);
    if (next.has(s.name)) next.delete(s.name);
    else next.add(s.name);
    setEnabled.mutate([...next], {
      onError: (err) =>
        toastError(toErrorMessage(err, t("chat.skillPicker.saveFailed"))),
    });
  };

  const style = useMemo(() => {
    const spaceAbove = position.top - GAP;
    const maxH = Math.min(MENU_MAX_HEIGHT, spaceAbove);
    let left = position.left;
    const width = Math.max(280, Math.min(360, position.width));
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

  return createPortal(
    <div
      ref={rootRef}
      data-testid="skill-picker"
      className="fixed z-[9999] rounded-lg border bg-popover text-popover-foreground shadow-lg flex flex-col overflow-hidden"
      style={style}
    >
      <div className="flex items-center justify-between px-3 pt-2.5 pb-1">
        <span className="text-xs font-semibold text-muted-foreground">
          {t("chat.skillPicker.title")}
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
            placeholder={t("chat.skillPicker.search")}
            className="flex-1 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
          />
        </div>
        <p className="mt-1.5 text-[11px] leading-snug text-muted-foreground/80">
          {t("chat.skillPicker.hint")}
        </p>
      </div>
      <div className="flex-1 overflow-y-auto border-t border-border py-1">
        {skillsQuery.isLoading || enabledQuery.isLoading ? (
          <div className="flex items-center justify-center py-6 text-muted-foreground">
            <Loader2 className="w-4 h-4 animate-spin" />
          </div>
        ) : filtered.length === 0 ? (
          <div className="px-3 py-6 text-center text-xs text-muted-foreground">
            {t("chat.skillPicker.empty")}
          </div>
        ) : (
          filtered.map((s) => {
            const checked = enabledSet.has(s.name);
            const sourceLabel = t(`chat.skillPicker.source_${s.source}`, {
              defaultValue: s.source,
            });
            return (
              <button
                key={`${s.source}:${s.name}`}
                onClick={() => toggle(s)}
                disabled={setEnabled.isPending}
                className="flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-muted/60 disabled:opacity-60"
              >
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
                    {MODE_ICON[s.mode]}
                    <span className="truncate text-sm font-medium">
                      {s.name}
                    </span>
                    <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      {sourceLabel}
                    </span>
                  </span>
                  {s.description && (
                    <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">
                      {s.description}
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
