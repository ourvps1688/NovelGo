// platformTemplates: 签约平台约束模板（与 genreTemplates 并列的"本书持久设置"）。
// 选中即创建一条本书偏好（category=平台约束，content=模板全文），
// 依赖既有偏好注入管线每轮常驻生效，无需后端改动。
//
// 设计原则（与题材卡一致但职责正交）：
// - 题材卡管「调性/禁忌/内容 flavor」；平台卡管「时序/节奏/签约模型」。
// - 平台卡是紧凑"数据参数表"而非方法论散文，从源头压住常驻上下文体积。
// - 识别/替换均以 content 首行 marker（【平台约束·XX】）为准。
//
// 显示名（label/labelEn）数据驱动，避免为每平台维护一条 i18n 键。

export interface PlatformTemplate {
  id: string;
  /** 模板全文首行标记，用于识别与展示 */
  marker: string;
  /** 中文显示名 */
  label: string;
  /** 英文显示名 */
  labelEn: string;
  /** 偏好 content 全文（紧凑参数表） */
  content: string;
}

/** 平台约束偏好的固定分类名（与 LLM upsert_preference 的人类习惯对齐） */
export const PLATFORM_CONSTRAINT_CATEGORY = "平台约束";

const FANQIE = `【平台约束·番茄】
签约模型：免费畅读 + 广告/激励，编辑与算法以「前 3–10 章完读率 / 留存」为过签与推量生死线；首章定生死。
节奏参数（覆盖 opening-chapters / pacing-control 默认）：
- 首个冲突钩子窗口 hook_window_open = 300 字（前 300 字内必出第一个冲突/异常/悬念，几乎零铺垫）
- 单章目标字数 = 1200–2000 字
- 章内微钩子间隔 micro_hook_interval = 800 字（每约 800 字一个悬念/进展点，密度高于长付费文）
- 允许纯铺垫章 allow_setup_chapters = 否（每章必须推进剧情或给甜头，零容忍慢章）
- 付费墙 aware = 否（无付费墙，无需墙前强钩）
- 高潮节奏 arc_cadence = 章级（每章小高潮、快回报、情绪即时兑现）
- 核心指标 primary_metric = 完读率 / 留存
签约口径：编辑看前 3 章完读曲线与题材契合；书名短狠吸睛、简介首句即抛钩子。
平台卡管「时序/节奏」，题材卡管「调性/禁忌」；冲突时平台卡赢在节奏时序、题材卡赢在内容禁忌；平台卡优先级高于 opening-chapters / pacing-control 默认。`;

const QIDIAN = `【平台约束·起点】
签约模型：付费订阅 + 追读，编辑以「付费章追读率 / 长线宏大感」为过签核心；允许较长铺垫为宏大叙事服务。
节奏参数（覆盖 opening-chapters / pacing-control 默认）：
- 首个冲突钩子窗口 hook_window_open = 1000 字（前 1000 字立人设 + 抛金手指/主线线索，可含少量铺垫）
- 单章目标字数 = 3000–4000 字
- 章内微钩子间隔 micro_hook_interval = 1500 字（相对舒缓，重叙事纵深）
- 允许纯铺垫章 allow_setup_chapters = 是（蓄力章可用，服务长线）
- 付费墙 aware = 是（付费墙前一章须预留全书最大钩子位，强制拉满张力）
- 高潮节奏 arc_cadence = 卷级（每卷大高潮 + 卷内起伏，情绪曲线服务卷级而非章级）
- 核心指标 primary_metric = 付费追读率
签约口径：编辑看立意高度、世界观自洽与长线潜力；简介重世界观与主线钩子。
平台卡管「时序/节奏」，题材卡管「调性/禁忌」；冲突时平台卡赢在节奏时序、题材卡赢在内容禁忌；平台卡优先级高于 opening-chapters / pacing-control 默认。`;

const QIDIAN_NV = `【平台约束·起点女生】
签约模型：女频付费订阅 + 追读，标准同起点但受众为女性向；编辑重「情感关系推进 + 人设卖点 + 文笔质感」。
节奏参数（覆盖 opening-chapters / pacing-control 默认）：
- 首个冲突钩子窗口 hook_window_open = 800 字（前 800 字立女主人设 + 抛关系张力/悬念）
- 单章目标字数 = 2000–3500 字
- 章内微钩子间隔 micro_hook_interval = 1200 字
- 允许纯铺垫章 allow_setup_chapters = 是（情感慢热章可用，但须有微妙关系变化）
- 付费墙 aware = 是（付费墙前一章预留强情感/剧情钩子）
- 高潮节奏 arc_cadence = 卷级（情感大转折 + 卷内起伏）
- 核心指标 primary_metric = 付费追读率
签约口径：编辑看人设记忆点、CP 张力与关系推进节奏；简介重女主钩子与关系设定。
平台卡管「时序/节奏」，题材卡管「调性/禁忌」；冲突时平台卡赢在节奏时序、题材卡赢在内容禁忌；平台卡优先级高于 opening-chapters / pacing-control 默认。`;

const JJWXC = `【平台约束·晋江】
签约模型：女频 VIP 付费 + 追读，编辑以「文笔质感 / CP 黏性 / 情感张力」为过签核心；读者重人设与关系而非爽点密度。
节奏参数（覆盖 opening-chapters / pacing-control 默认）：
- 首个冲突钩子窗口 hook_window_open = 800 字（前 800 字立人设 + 抛关系张力/悬念，重氛围而非动作）
- 单章目标字数 = 2000–3500 字（重心理与对话质感，不必长）
- 章内微钩子间隔 micro_hook_interval = 1200 字（钩子偏情感错位/未说出口的话，非强冲突）
- 允许纯铺垫章 allow_setup_chapters = 是（慢热可，但须有微妙关系或心理变化）
- 付费墙 aware = 是（VIP/入 V 前一章预留最强情感或剧情钩子）
- 高潮节奏 arc_cadence = 卷级（情感大转折 + 卷内起伏）
- 核心指标 primary_metric = 付费追读率
签约口径：编辑看文笔、人设与 CP 感；简介重人设反差与关系钩子，避免剧透结局。
平台卡管「时序/节奏」，题材卡管「调性/禁忌」；冲突时平台卡赢在节奏时序、题材卡赢在内容禁忌；平台卡优先级高于 opening-chapters / pacing-control 默认。`;

const QIMAO = `【平台约束·七猫】
签约模型：免费畅读 + 激励/广告，过签与推量逻辑类似番茄，以「完读率 / 留存」为先；节奏快、钩子密。
节奏参数（覆盖 opening-chapters / pacing-control 默认）：
- 首个冲突钩子窗口 hook_window_open = 400 字（前 400 字内出第一个钩子，极少铺垫）
- 单章目标字数 = 1000–1800 字（短章高密度）
- 章内微钩子间隔 micro_hook_interval = 800 字（每约 800 字一个悬念/进展点）
- 允许纯铺垫章 allow_setup_chapters = 否（每章必须推进或给甜头，零容忍慢章）
- 付费墙 aware = 否（无付费墙）
- 高潮节奏 arc_cadence = 章级（每章小高潮、快回报）
- 核心指标 primary_metric = 完读率 / 留存
签约口径：编辑看前 3 章完读曲线与爽点密度；书名直白吸睛、简介首句抛钩。
平台卡管「时序/节奏」，题材卡管「调性/禁忌」；冲突时平台卡赢在节奏时序、题材卡赢在内容禁忌；平台卡优先级高于 opening-chapters / pacing-control 默认。`;

export const PLATFORM_TEMPLATES: PlatformTemplate[] = [
  {
    id: "fanqie",
    marker: "【平台约束·番茄】",
    label: "番茄",
    labelEn: "Fanqie",
    content: FANQIE,
  },
  {
    id: "qidian",
    marker: "【平台约束·起点】",
    label: "起点",
    labelEn: "Qidian",
    content: QIDIAN,
  },
  {
    id: "qidian_nv",
    marker: "【平台约束·起点女生】",
    label: "起点女生",
    labelEn: "Qidian Female",
    content: QIDIAN_NV,
  },
  {
    id: "jjwxc",
    marker: "【平台约束·晋江】",
    label: "晋江",
    labelEn: "JJWXC",
    content: JJWXC,
  },
  {
    id: "qimao",
    marker: "【平台约束·七猫】",
    label: "七猫",
    labelEn: "Qimao",
    content: QIMAO,
  },
];

/**
 * detectPlatformTemplateId: 判断一条偏好 content 是否为某套平台模板。
 * 以去空白后的首行 marker 匹配；非模板文本返回 null。
 */
export function detectPlatformTemplateId(content: string): string | null {
  const firstLine = content.trimStart().split("\n", 1)[0]?.trim() ?? "";
  const hit = PLATFORM_TEMPLATES.find((t) => t.marker === firstLine);
  return hit ? hit.id : null;
}

export interface PlatformTemplateItemRef {
  templateId: string;
  id: number;
}

/**
 * findPlatformTemplateItems: 从偏好列表（global + novel 合并）中筛出平台模板条目。
 * 仅按 marker 识别，不动用户自定义的同分类条目。
 */
export function findPlatformTemplateItems(
  items: { id: number; category: string; content: string }[],
): PlatformTemplateItemRef[] {
  const refs: PlatformTemplateItemRef[] = [];
  for (const item of items) {
    if (item.category !== PLATFORM_CONSTRAINT_CATEGORY) continue;
    const templateId = detectPlatformTemplateId(item.content);
    if (templateId) refs.push({ templateId, id: item.id });
  }
  return refs;
}
