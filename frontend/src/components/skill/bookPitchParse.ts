// book-pitch 终稿 JSON 解析：从助手消息文本中提取结构化外包装数据。
// book-pitch 技能在对话定稿后会输出一个 ```bookpitch 围栏代码块（亦兼容 ```json），
// 内含 title/description/tags/direction/genre/platform/style/oneLiner 八个字段。
// 该模块负责把任意文本中的这段 JSON 抽出来并归一化为表单可用的字段。

export interface PitchData {
  title: string;
  description: string;
  tags: string;
  direction: string;
  genre: string;
  platform: string;
  style: string;
  oneLiner: string;
}

// 每个目标字段支持的中英文键名别名（LLM 可能用中文键，须兼容）。
const KEY_ALIASES: Record<keyof PitchData, string[]> = {
  title: ["title", "书名"],
  description: ["description", "简介", "summary"],
  tags: ["tags", "标签"],
  direction: ["direction", "方向"],
  genre: ["genre", "题材"],
  platform: ["platform", "平台"],
  style: ["style", "风格"],
  oneLiner: ["oneLiner", "一句话卖点", "hook", "卖点"],
};

function pick(raw: Record<string, unknown>, aliases: string[]): unknown {
  for (const k of aliases) {
    if (raw[k] !== undefined && raw[k] !== null) return raw[k];
  }
  return undefined;
}

function asString(v: unknown): string {
  if (typeof v === "string") return v.trim();
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  return "";
}

function asTags(v: unknown): string {
  if (Array.isArray(v)) {
    return v
      .map((x) => (typeof x === "string" ? x.trim() : String(x)))
      .filter((x) => x.length > 0)
      .join("、");
  }
  return asString(v);
}

// 尝试把一个候选字符串解析为 PitchData；无效（缺必备字段）返回 null。
function tryParse(candidate: string): PitchData | null {
  let obj: unknown;
  // 先直接解析；失败再截取首个 {...} 块（兼容围栏内混有解释文字的情况）。
  try {
    obj = JSON.parse(candidate);
  } catch {
    const m = candidate.match(/\{[\s\S]*\}/);
    if (!m) return null;
    try {
      obj = JSON.parse(m[0]);
    } catch {
      return null;
    }
  }
  if (typeof obj !== "object" || obj === null) return null;
  const raw = obj as Record<string, unknown>;

  const title = asString(pick(raw, KEY_ALIASES.title));
  const description = asString(pick(raw, KEY_ALIASES.description));
  // title / description 为必备字段，缺失则视为非终稿 JSON。
  if (!title || !description) return null;

  return {
    title,
    description,
    tags: asTags(pick(raw, KEY_ALIASES.tags)),
    direction: asString(pick(raw, KEY_ALIASES.direction)),
    genre: asString(pick(raw, KEY_ALIASES.genre)),
    platform: asString(pick(raw, KEY_ALIASES.platform)),
    style: asString(pick(raw, KEY_ALIASES.style)),
    oneLiner: asString(pick(raw, KEY_ALIASES.oneLiner)),
  };
}

// 从一段文本中提取第一个可用的终稿 JSON。
// 搜索顺序：围栏代码块（任意语言）→ 整段文本 → 任意 {...} 片段。
export function extractPitchJSON(text: string): PitchData | null {
  if (!text) return null;

  const candidates: string[] = [];

  // 1) 所有三反引号围栏块
  const fenceRe = /```[a-zA-Z]*\n?([\s\S]*?)```/g;
  let fm: RegExpExecArray | null;
  while ((fm = fenceRe.exec(text)) !== null) {
    if (fm[1]) candidates.push(fm[1]);
  }

  // 2) 整段文本（无围栏时）
  candidates.push(text);

  // 3) 任意 {...} 片段
  const braceRe = /\{[\s\S]*\}/g;
  let bm: RegExpExecArray | null;
  while ((bm = braceRe.exec(text)) !== null) {
    candidates.push(bm[0]);
  }

  for (const c of candidates) {
    const data = tryParse(c);
    if (data) return data;
  }
  return null;
}

// 归一化签名：仅由字段值组成，用于判断「同一段 JSON 是否已回填过」。
export function pitchSignature(d: PitchData): string {
  return [
    d.title,
    d.description,
    d.tags,
    d.direction,
    d.genre,
    d.platform,
    d.style,
    d.oneLiner,
  ].join("");
}
