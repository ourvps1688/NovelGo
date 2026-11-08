import { UpsertPitchPreferences } from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import {
  CATEGORY_CREATIVE,
  CATEGORY_DIRECTION,
  CATEGORY_HOOK,
  CATEGORY_PLATFORM,
  CATEGORY_TAGS,
} from "./preferenceCategories";

// 卖点孵化引导项：书架编辑弹窗与「AI 孵化新书」采纳方案共用的读写口径。
// 五类全部落 preference_items（本书专属），后端 UpsertPitchPreferences 按
// (novel_id, category) 幂等收敛为唯一行，避免旧链路每次保存都新增一组。
export type PitchPreferences = {
  tags: string;
  direction: string;
  platform: string;
  style: string;
  one_liner: string;
};

export const EMPTY_PITCH_PREFERENCES: PitchPreferences = {
  tags: "",
  direction: "",
  platform: "",
  style: "",
  one_liner: "",
};

// 「平台约束 / 创作约束」两类和用户手写的长篇创作纪律共用分类；表单只认短引导值，
// 超过 80 字的历史行视为手写纪律（后端同样保护，不会被收敛删除）。
const MANUAL_RULE_MIN_LENGTH = 80;
const MANUAL_RULE_CATEGORIES = new Set([CATEGORY_PLATFORM, CATEGORY_CREATIVE]);

// 从 GetPreferences 结果取五类各最新一条（列表按 created_at ASC，同类后者覆盖前者；
// 与后端 intentFromPitch 的取最新口径一致）。全局偏好与手写长纪律不参与。
export function readPitchPreferences(
  result?: app.PreferenceResult | null,
): PitchPreferences {
  const latest: Record<string, string> = {};
  for (const item of result?.novel ?? []) {
    if (item.is_global) continue;
    if (
      MANUAL_RULE_CATEGORIES.has(item.category) &&
      [...item.content].length > MANUAL_RULE_MIN_LENGTH
    ) {
      continue;
    }
    latest[item.category] = item.content;
  }
  return {
    tags: latest[CATEGORY_TAGS] ?? "",
    direction: latest[CATEGORY_DIRECTION] ?? "",
    platform: latest[CATEGORY_PLATFORM] ?? "",
    style: latest[CATEGORY_CREATIVE] ?? "",
    one_liner: latest[CATEGORY_HOOK] ?? "",
  };
}

// 全量 PUT：传入五类全部字段，留空=清除该类。
export async function savePitchPreferences(
  novelID: number,
  values: PitchPreferences,
): Promise<void> {
  await UpsertPitchPreferences(
    novelID,
    values as app.UpsertPitchPreferencesInput,
  );
}
