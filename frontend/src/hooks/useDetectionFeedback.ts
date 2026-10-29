// 检测结果反馈（R5-3 T2.2）：订阅检测完成事件驱动「进行态→结果」闭环。
// 复用既有事件协议零改动（WBS §10.4）：
//   pipeline:detect_done        — 卷/书体检完成 {novel_id, kind, verdict, issue_count}（T1.2 增量通道）
//   pipeline:consistency_patrol — 巡检·结构完成 {novel_id}（既有通道）
//   pipeline:detect_cancelled   — 检测任务被取消（B5）{novel_id, kind}：即时刷新状态行（进行态已注销）
// 事件到达=结果已更新：检测状态查询失效（状态行刷新）+ 该 kind 记「新结果」，
// 用户点「查看结果」后消隐。章节检测无完成事件（直写质量档案），进行态由
// 检测状态轮询收口即可见，结果跳转不依赖事件。降级零提示：事件缺失/载荷
// 不符（无 kind 等）一律静默忽略。
import { useCallback, useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import { detectionKeys } from "@/lib/queryKeys";

export interface DetectionFeedback {
  /** 有未查看新结果的 kind 集合（volume/book/structure） */
  freshKinds: ReadonlySet<string>;
  /** 查看结果后消隐该 kind 的新结果标记 */
  markSeen: (kind: string) => void;
}

export function useDetectionFeedback(novelId: number): DetectionFeedback {
  const queryClient = useQueryClient();
  const [freshKinds, setFreshKinds] = useState<ReadonlySet<string>>(new Set());

  useEffect(() => {
    if (!novelId) return;
    const mark = (kind: string) => {
      queryClient.invalidateQueries({ queryKey: detectionKeys.list(novelId) });
      setFreshKinds((prev) => new Set(prev).add(kind));
    };
    const offDetect = EventsOn(
      "pipeline:detect_done",
      (p: { novel_id?: number; kind?: string }) => {
        if (p.novel_id !== novelId || !p.kind) return;
        mark(p.kind);
      },
    );
    const offPatrol = EventsOn(
      "pipeline:consistency_patrol",
      (p: { novel_id?: number }) => {
        if (p.novel_id !== novelId) return;
        mark("structure");
      },
    );
    // B5：取消事件=进行态已注销，仅刷新状态行即可（running→false），
    // 并清掉该 kind 的「新结果」标记（取消不产生结果）。
    const offCancel = EventsOn(
      "pipeline:detect_cancelled",
      (p: { novel_id?: number; kind?: string }) => {
        if (p.novel_id !== novelId || !p.kind) return;
        queryClient.invalidateQueries({
          queryKey: detectionKeys.list(novelId),
        });
        setFreshKinds((prev) => {
          if (!prev.has(p.kind!)) return prev;
          const next = new Set(prev);
          next.delete(p.kind!);
          return next;
        });
      },
    );
    return () => {
      // 测试环境 EventsOn 可能无返回值（setup Proxy mock），可选调用防卸载报错。
      offDetect?.();
      offPatrol?.();
      offCancel?.();
    };
  }, [novelId, queryClient]);

  const markSeen = useCallback((kind: string) => {
    setFreshKinds((prev) => {
      if (!prev.has(kind)) return prev;
      const next = new Set(prev);
      next.delete(kind);
      return next;
    });
  }, []);

  return { freshKinds, markSeen };
}
