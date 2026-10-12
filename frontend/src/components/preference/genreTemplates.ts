// genreTemplates: 题材约束模板（档B）。
// 文案来源：docs/genre-constraints.md（借鉴 ai-novel-lab 蒸馏的四套题材约束）。
// 选中即创建一条本书偏好（category=创作约束，content=模板全文），
// 依赖既有偏好注入管线每轮常驻生效，无需后端改动。
// 识别/替换均以 content 首行 marker（【创作约束·XX】）为准，
// 用户手动写的同分类自由文本不会被误删。

export interface GenreTemplate {
  id: string;
  /** 模板全文首行标记，用于识别与展示 */
  marker: string;
  /** 偏好 content 全文 */
  content: string;
}

const SHUANGWEN = `【创作约束·爽文】
三定律：
1. 主角绝对中心——所有情节、配角、事件都服务于主角的成长、收益或爽感，不得让配角抢戏盖过主角。
2. 有仇不过夜——冲突与屈辱须在当章或最近几章内给出回应/反转，不让主角长期憋屈；拖延即流失爽感。
3. 金手指一致性——主角的能力、外挂、设定前后统一，不得为推进剧情临时无依据开挂或推翻前文设定。

禁忌（违反即劣）：
- 禁虐主：不得长期、无补偿地压制主角。
- 禁现实人名/真实机构：避免直接套用现实人物或单位。
- 禁烂尾/机械降神：结局须由前文伏笔与角色行动收束，不得外力突兀解局。
- 禁吃书：不得推翻已确立的人物关系、世界观或设定。
- 禁元叙述：不得出现"作者/读者/这本小说"等破墙表达。

爽点密度与节奏：
- 每章至少 1 个明确爽点（打脸/收获/反转/成长/被认可）。
- 章末留钩子（悬念、新危机、新目标），促使继续阅读。
- 情绪曲线完整：压抑 → 爆发 → 收获，单章内可见起伏。
- 单章正文不少于 3500 字，冲突推进不拖沓，少无意义日常流水。`;

const SUSPENSE = `【创作约束·悬疑】
核心：悬念优先，信息差驱动阅读。

三定律：
1. 线索公平——关键线索须已出现在文本中，读者凭已有信息可参与推理，不靠隐藏作者才知道的事实。
2. 误导不欺骗——红鲱鱼（假线索）须有文本依据，误导来自合理误读，而非叙述撒谎。
3. 真相唯一且可回溯——最终解须能由前文伏笔完整推导，无矛盾、无强行反转。

禁忌：
- 禁天降解谜：不得出现无铺垫的突然揭秘或外部权威直接给答案。
- 禁无敌主角：侦探/主角也会误判、也会付出代价，避免全知全能。
- 禁过度 withholding：关键证据必须在场（即使被忽略），不得长期对读者完全屏蔽。
- 禁元引用：不得出现破墙表达。

密度与节奏：
- 每章至少抛出 1 个新线索或 1 次认知反转。
- 章末抛悬念（新尸体/新矛盾/信任崩塌），保持引力。
- 维护伏笔台账：前文埋、后文收，收束前不烂尾。
- 单章正文不少于 3000 字，信息密度高，少闲笔。`;

const ROMANCE = `【创作约束·言情】
核心：情感张力与人物关系弧光，人物比事件更打动人。

三定律：
1. 双向吸引有因——感情推进须由具体事件、相处、抉择支撑，不得无铺垫突然坠入爱河。
2. 误会有时限——误会/隔阂是催化剂而非拖延工具，须在合理节点澄清，不靠长期误会吊着。
3. 人设稳定——角色性格、底线、行为逻辑前后一致，不为了戏剧性强行 OOC（脱离人设）。

禁忌：
- 禁无补偿虐身/长期精神折磨：痛苦须服务于成长或关系深化，不得为虐而虐。
- 禁第三方强行拆散无逻辑：外部阻力须有动机与代价，不靠降智反派硬拆。
- 禁扁平反派/工具人：对立面也应有可信动机。
- 禁元引用：不得破墙。

密度与节奏：
- 每章至少 1 个情感推进或关系变化（靠近/疏远/和解/抉择）。
- 章末留余韵或悬念（未说出口的话、新的错位），余味悠长。
- 情绪曲线清晰，甜虐有节，避免全程平铺。
- 单章正文不少于 3000 字，重对话与心理质感。`;

const LITERARY = `【创作约束·传统文学】
核心：人物内在真实、语言质感、主题纵深，不迎合套路化爽感。

三定律：
1. 真实高于爽感——人物可软弱、可失败、可不被理解；不以套路爽点牺牲可信度。
2. 动机可信——角色行为须有心理依据与处境逻辑，避免为情节牺牲人物一致性。
3. 留白与节制——意义靠呈现而非直说，不借角色之口长篇说教。

禁忌：
- 禁套路化爽点堆砌：不以打脸/反转替代人物与主题。
- 禁说教直给：主题须由叙事自然浮现。
- 禁扁平符号化人物：配角也应有复杂性。
- 禁元引用：不得破墙。

密度与节奏：
- 重质不重量，允许慢节奏与日常纹理，但须服务于人物或氛围。
- 章末可余味悠长，不必每段都有钩子。
- 语言密度与节奏讲究，节制使用形容词与解释。
- 单章长度不拘，以叙事完整为度。`;

export const GENRE_TEMPLATES: GenreTemplate[] = [
  { id: "shuangwen", marker: "【创作约束·爽文】", content: SHUANGWEN },
  { id: "suspense", marker: "【创作约束·悬疑】", content: SUSPENSE },
  { id: "romance", marker: "【创作约束·言情】", content: ROMANCE },
  { id: "literary", marker: "【创作约束·传统文学】", content: LITERARY },
];

/** 题材约束偏好的固定分类名（与 LLM upsert_preference 的人类习惯对齐） */
export const GENRE_CONSTRAINT_CATEGORY = "创作约束";

/**
 * detectTemplateId: 判断一条偏好 content 是否为某套模板。
 * 以去空白后的首行 marker 匹配；非模板文本（含用户自定义创作约束）返回 null。
 */
export function detectTemplateId(content: string): string | null {
  const firstLine = content.trimStart().split("\n", 1)[0]?.trim() ?? "";
  const hit = GENRE_TEMPLATES.find((t) => t.marker === firstLine);
  return hit ? hit.id : null;
}

export interface TemplateItemRef {
  templateId: string;
  id: number;
}

/**
 * findTemplateItems: 从偏好列表（global + novel 合并）中筛出模板条目。
 * 仅按 marker 识别，不动用户自定义的同分类条目。
 */
export function findTemplateItems(
  items: { id: number; category: string; content: string }[],
): TemplateItemRef[] {
  const refs: TemplateItemRef[] = [];
  for (const item of items) {
    if (item.category !== GENRE_CONSTRAINT_CATEGORY) continue;
    const templateId = detectTemplateId(item.content);
    if (templateId) refs.push({ templateId, id: item.id });
  }
  return refs;
}
