// 空态引导漏斗判定（P3 WBS T2.2/§0.3）。
// 判定源 GetVoiceLedger（P1 T3.1 既有 API）：十维 Present 全 false 或空台账 → 「未提取心智」。
// 降级最高纪律：调用失败/异常一律静默 false（宁缺勿错，零提示零报错，绝不拦截重写动作）。
// 会话级消隐：用户关闭提示后本次会话该书不再弹（不落盘，重启即清）；
// 判定缓存按 novelID 切换失效（§0.3）。
import { useEffect, useState, useSyncExternalStore } from "react";
import { GetVoiceLedger } from "@/lib/wailsjs/go/app/App";

/** 会话级消隐集合：module 级=应用会话级；两处挂点共享同一状态。 */
const dismissedNovels = new Set<number>();
const listeners = new Set<() => void>();

function emit() {
  for (const listener of [...listeners]) listener();
}

function subscribeDismissed(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

export interface VoiceExtractionState {
  /** true=确证未提取（API 成功且十维 Present 全 false/空）；进行中/失败静默 false。 */
  unextracted: boolean;
  /** 会话级消隐：本会话内该书提示已被关闭。 */
  dismissed: boolean;
  /** 关闭提示：会话内不再弹（不落盘）。 */
  dismiss: () => void;
}

/**
 * 未提取心智判定 hook（T2.2）：
 * - GetVoiceLedger 成功且全部 Present=false（或空台账）→ unextracted=true；
 * - 任一维度 Present=true（已提取）→ false；调用失败/异常 → 静默 false（降级纪律）；
 * - novelID 切换即失效重查，绝不把上一本的判定闪给这一本。
 */
export function useVoiceExtractionState(novelID: number): VoiceExtractionState {
  // verdict 按 novelID 键控：切书瞬间 id 不匹配即派生 false（零同步 setState），
  // 查询回来只信当前 id 的结果（cancelled 同护）。
  const [verdict, setVerdict] = useState<{ id: number; unextracted: boolean }>({
    id: 0,
    unextracted: false,
  });
  const dismissed = useSyncExternalStore(
    subscribeDismissed,
    () => dismissedNovels.has(novelID),
    () => dismissedNovels.has(novelID),
  );

  useEffect(() => {
    if (!novelID) return;
    let cancelled = false;
    try {
      Promise.resolve(GetVoiceLedger(novelID))
        .then((items) => {
          if (cancelled) return;
          // 坏数据（非数组）宁缺勿错：不置 verdict → 派生 false（零提示）
          if (!Array.isArray(items)) return;
          const empty = items.length === 0 || items.every((it) => !it.present);
          setVerdict({ id: novelID, unextracted: empty });
        })
        .catch(() => {
          // 失败静默：宁缺勿错，绝不弹错误/提示。verdict 保持非当前 id → 派生 false。
        });
    } catch {
      // 同步抛错（绑定缺失等）同样静默：verdict 不动 → 派生 false。
    }
    return () => {
      cancelled = true;
    };
  }, [novelID]);

  return {
    unextracted: verdict.id === novelID && verdict.unextracted,
    dismissed,
    dismiss: () => {
      dismissedNovels.add(novelID);
      emit();
    },
  };
}

/** 测试用：清空会话级消隐状态（不导出集合本体，避免外部篡改）。 */
export function resetVoiceExtractionDismissals(): void {
  dismissedNovels.clear();
  emit();
}
