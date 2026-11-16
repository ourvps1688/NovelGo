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
 * 订阅后端 statesync 子 Agent 的生命周期事件，返回是否有状态同步在进行中。
 * A 方案后写章不再自动派发 statesync；只有作者显式点「同步本章台账」、
 * S0 导入对齐或全量对齐才会发出 started/done 事件。本 hook 让前端能轻量地
 * 提示「台账同步中…」，无需关心具体子 Agent 卡片；无事件时恒为 false。
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
