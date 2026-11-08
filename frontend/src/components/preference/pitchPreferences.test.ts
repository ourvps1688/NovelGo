import { describe, expect, it, vi } from "vitest";

// 覆盖 setup.ts 的全局 Proxy mock：本测试只验证纯读取逻辑，明确给出空实现。
vi.mock("@/lib/wailsjs/go/app/App", () => ({
  UpsertPitchPreferences: vi.fn().mockResolvedValue(undefined),
}));

import {
  EMPTY_PITCH_PREFERENCES,
  readPitchPreferences,
  type PitchPreferences,
} from "./pitchPreferences";
import type { app } from "@/lib/wailsjs/go/models";

const pref = (
  id: number,
  category: string,
  content: string,
  isGlobal = false,
) =>
  ({
    id,
    novel_id: 23,
    is_global: isGlobal,
    category,
    content,
    created_at: "",
    updated_at: "",
  }) as unknown as app.PreferenceResult["novel"][number];

describe("readPitchPreferences", () => {
  it("returns empty fields when no preferences exist", () => {
    expect(readPitchPreferences(undefined)).toEqual(EMPTY_PITCH_PREFERENCES);
    const empty = {
      global: [],
      novel: [],
    } as unknown as app.PreferenceResult;
    expect(readPitchPreferences(empty)).toEqual(EMPTY_PITCH_PREFERENCES);
  });

  it("maps the five pitch categories to input fields", () => {
    const result = {
      global: [],
      novel: [
        pref(1, "平台约束", "七猫"),
        pref(2, "创作约束", "爽文"),
        pref(3, "标签", "玄幻、进化流"),
        pref(4, "方向", "从死牢杀到大荒"),
        pref(5, "一句话卖点", "万兽入我身"),
        pref(6, "自定义分类", "不参与"),
      ],
    } as unknown as app.PreferenceResult;

    const expected: PitchPreferences = {
      tags: "玄幻、进化流",
      direction: "从死牢杀到大荒",
      platform: "七猫",
      style: "爽文",
      one_liner: "万兽入我身",
    };
    expect(readPitchPreferences(result)).toEqual(expected);
  });

  it("keeps the latest row per category and ignores global rows", () => {
    // 后端 GetPreferences 按 created_at ASC 返回；同类后者覆盖前者（= 最近终稿）。
    const result = {
      global: [pref(90, "平台约束", "全局平台", true)],
      novel: [
        pref(1, "平台约束", "番茄"),
        pref(2, "平台约束", "七猫"),
        pref(3, "标签", "旧标签"),
        pref(4, "标签", "新标签"),
      ],
    } as unknown as app.PreferenceResult;

    const got = readPitchPreferences(result);
    expect(got.platform).toBe("七猫");
    expect(got.tags).toBe("新标签");
    expect(got.direction).toBe("");
  });

  it("skips long manual writing rules but keeps long directions", () => {
    const longRule = "禁状态面板体；".repeat(20);
    const longDirection = "从死牢杀到大荒，".repeat(20);
    const result = {
      global: [],
      novel: [
        pref(1, "创作约束", "爽文"),
        pref(2, "创作约束", longRule),
        pref(3, "平台约束", "七猫"),
        pref(4, "方向", longDirection),
      ],
    } as unknown as app.PreferenceResult;

    const got = readPitchPreferences(result);
    // 手写长纪律不应显示进风格输入框；方向允许长文本，不做手写保护。
    expect(got.style).toBe("爽文");
    expect(got.platform).toBe("七猫");
    expect(got.direction).toBe(longDirection);
  });
});
