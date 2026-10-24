// 功能使用度量 key 单源（P4 WBS T2.2 / §0.4）。
// 只记 key + 次数，不记用户内容；纯本地不上传（隐私红线：禁任何 fetch/上报/遥测端点）。
// key 三类命名空间（单源枚举，组件不得自造字面量）：
//   panel.*   面板打开（以 activePanel 变化为准，同面板重复切换不刷量）
//   journey.* 旅程卡点击（titleKey 即 key，天然 journey.* 命名空间）
//   action.*  关键动作（提取/重写/体检/修复/冻结/巡检/retcon 立案）
import { RecordFeatureUsage } from "@/lib/wailsjs/go/app/App";

/** 面板打开 key：PanelId 直接嵌入。 */
export function panelFeatureKey(panelId: string): string {
  return `panel.${panelId}`;
}

/** 旅程卡点击 key：titleKey 本身即 journey.* 命名空间，缺前缀则补。 */
export function journeyFeatureKey(titleKey: string): string {
  return titleKey.startsWith("journey.") ? titleKey : `journey.${titleKey}`;
}

/** 关键动作 key 枚举（单源；对应 WBS §0.4 关键动作清单）。 */
export const ACTION_FEATURE_KEYS = {
  /** 提取：文风样本提取 / 套路·心智提取（吸收提取工作区两路） */
  extract: "action.extract",
  /** 重写：闸门「修改后继续」按反馈重做；自由区聊天写/重写请求无法与普通
   *  写作区分，宁缺勿错不计入 */
  rewrite: "action.rewrite",
  /** 体检：全量体检 / 闸门「进入签约体检」/「完本终检」 */
  audit: "action.audit",
  /** 修复：闸门「进入修复闭环」 */
  fix: "action.fix",
  /** 冻结：卷冻结/解冻（SetVolumeFrozen） */
  freeze: "action.freeze",
  /** 巡检：手动触发全书一致性巡检 */
  patrol: "action.patrol",
  /** retcon 立案：立案在后端 handoff 出口沉淀为候选案件，前端以「批准立案」
   *  裁决（candidate→approved 进入受控编辑）为使用信号 */
  retcon: "action.retcon",
} as const;

export type ActionFeatureKey =
  (typeof ACTION_FEATURE_KEYS)[keyof typeof ACTION_FEATURE_KEYS];

/**
 * 闸门决策 → 关键动作 key 映射（单源）。
 * 其余决策（continue/finish/abort 等）是流程控制，不计入动作度量。
 */
export const GATE_DECISION_FEATURE_KEYS: Readonly<
  Record<string, ActionFeatureKey>
> = {
  fix: ACTION_FEATURE_KEYS.fix,
  audit_now: ACTION_FEATURE_KEYS.audit,
  finish_book: ACTION_FEATURE_KEYS.audit,
  modify: ACTION_FEATURE_KEYS.rewrite,
};

/**
 * 尽力记录一次功能使用（纯本地，只记 key+次数）。
 * 失败静默零提示零抛错（降级纪律：统计失败=业务零感知）。
 */
export function trackFeature(key: string): void {
  if (!key) return;
  try {
    const pending = RecordFeatureUsage(key) as unknown;
    if (pending && typeof (pending as Promise<void>).catch === "function") {
      void (pending as Promise<void>).catch(() => {});
    }
  } catch {
    // 统计失败业务零感知：静默吞掉，绝不影响业务链路
  }
}
