// 三工厂归属映射单源（P3 WBS §0.1）。
// 「喂给谁」是 P3 最高验收标准：任何功能入口的归属只能来自本文件，组件不得自造归属。
import type { SidebarPanelId } from "@/types/panel";

/** 工厂标识：吸收/创作/质检 + 基础设施。单源枚举（WBS §6 枚举单源）。 */
export type FactoryId = "absorb" | "create" | "quality" | "base";

/** 价值链顺序：吸收→创作→质检→基础。分组顺序唯一依据，勿按字母/原序。 */
export const FACTORY_ORDER: readonly FactoryId[] = [
  "absorb",
  "create",
  "quality",
  "base",
] as const;

/** 全部 SidebarPanelId 的规范清单（与 types/panel.ts 同步，测试全键覆盖断言用）。 */
export const ALL_SIDEBAR_PANEL_IDS: readonly SidebarPanelId[] = [
  "novels",
  "chapters",
  "chapter-management",
  "characters",
  "locations",
  "storyarcs",
  "timeline",
  "reader",
  "preferences",
  "novel-settings",
  "profile",
  "git",
  "skills",
  "skill-matrix",
  "style-samples",
  "signability",
  "bookpitch",
  "pipeline",
  "search",
] as const;

/**
 * 面板归属映射（WBS §0.1 定案表）：
 * - absorb 吸收工厂：喂给文风上限（心智/套路/表达 DNA 入库）
 * - create 创作工厂：喂给正文与真理层（意图→章节→台账）
 * - quality 质检工厂：喂给签约下限（体检→修复→冻结→巡检）
 * - base 基础设施：喂给全部（书架/检索/版本）
 *
 * Record 全键锁定：SidebarPanelId 增删必须同步归属，缺键不过编译。
 */
export const FACTORY_MAP: Record<SidebarPanelId, FactoryId> = {
  "style-samples": "absorb",
  skills: "absorb",
  "skill-matrix": "absorb",
  chapters: "create",
  "chapter-management": "create",
  preferences: "create",
  "novel-settings": "create",
  profile: "create",
  bookpitch: "create",
  characters: "create",
  locations: "create",
  storyarcs: "create",
  timeline: "create",
  signability: "quality",
  pipeline: "quality",
  reader: "quality",
  novels: "base",
  search: "base",
  git: "base",
};

/** 归属查询。缺键兜底 base 不崩溃（§0.6 降级纪律：宁缺勿错）。 */
export function factoryOf(id: SidebarPanelId | string): FactoryId {
  return FACTORY_MAP[id as SidebarPanelId] ?? "base";
}

/** 工厂分组标题 i18n key（单源命名空间 shell.factory.*）。 */
export function factoryTitleKey(factory: FactoryId): string {
  return `shell.factory.${factory}.title`;
}

/** 「喂给谁」叙案短语 i18n key（T2.1 叙案统一挂点）。 */
export function factoryFeedsKey(factory: FactoryId): string {
  return `shell.factory.${factory}.feeds`;
}
