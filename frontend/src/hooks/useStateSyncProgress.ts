import { useEffect, useRef, useState } from "react";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";

interface StateSyncPayload {
  novel_id: number;
  chapter_id: number;
  status: "started" | "done";
  error?: string;
}

// 安全网：若后端 done 事件丢失（如进程异常退出），超时后强制清除，
// 避免「后台状态同步中…」提示永久停留。
const STALE_MS = 60_000;

/**
 * 订阅后端 statesync 子 Agent 的生命周期事件，返回是否有后台状态同步在进行中。
 * 写章（edit full_replace）后系统会自动 debounce 派发 statesync 子 Agent 同步台账，
 * 本 hook 让前端能轻量地提示「后台状态同步中…」，无需关心具体子 Agent 卡片。
 */
export function useStateSyncProgress() {
  const [activeChapters, setActiveChapters] = useState<Set<number>>(
    () => new Set(),
  );
  const timers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

  useEffect(() => {
    const unsubscribe = EventsOn(
      "statesync:progress",
      (data: StateSyncPayload) => {
        if (!data || typeof data.chapter_id !== "number") return;
        const cid = data.chapter_id;

        if (data.status === "started") {
          setActiveChapters((prev) => {
            const next = new Set(prev);
            next.add(cid);
            return next;
          });
          const existing = timers.current.get(cid);
          if (existing) clearTimeout(existing);
          timers.current.set(
            cid,
            setTimeout(() => {
              setActiveChapters((prev) => {
                const next = new Set(prev);
                next.delete(cid);
                return next;
              });
              timers.current.delete(cid);
            }, STALE_MS),
          );
        } else {
          const t = timers.current.get(cid);
          if (t) {
            clearTimeout(t);
            timers.current.delete(cid);
          }
          setActiveChapters((prev) => {
            const next = new Set(prev);
            next.delete(cid);
            return next;
          });
        }
      },
    );
    return () => {
      unsubscribe();
      timers.current.forEach((t) => clearTimeout(t));
      timers.current.clear();
    };
  }, []);

  return {
    syncing: activeChapters.size > 0,
    activeChapters,
  };
}
