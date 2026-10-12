import { describe, expect, it } from "vitest";
import {
  GENRE_TEMPLATES,
  GENRE_GROUPS,
  detectTemplateId,
  findTemplateItems,
} from "./genreTemplates";

describe("genreTemplates", () => {
  it("every template has valid id/marker/label and unique id", () => {
    const ids = new Set<string>();
    for (const tpl of GENRE_TEMPLATES) {
      expect(tpl.id).toBeTruthy();
      expect(ids.has(tpl.id)).toBe(false);
      ids.add(tpl.id);
      expect(tpl.content.trimStart().startsWith(tpl.marker)).toBe(true);
      expect(tpl.marker).toMatch(/^【创作约束·.+】$/);
      expect(tpl.label).toBeTruthy();
      expect(tpl.labelEn).toBeTruthy();
      expect(tpl.group).toBeTruthy();
    }
    // P0-B 扩展：至少覆盖通用 4 套 + 多题材分组
    expect(GENRE_TEMPLATES.length).toBeGreaterThanOrEqual(27);
  });

  it("every template's group exists in GENRE_GROUPS", () => {
    const keys = new Set(GENRE_GROUPS.map((g) => g.key));
    for (const tpl of GENRE_TEMPLATES) {
      expect(keys.has(tpl.group)).toBe(true);
    }
  });

  it("detects each template by first-line marker (tolerant of leading whitespace)", () => {
    for (const tpl of GENRE_TEMPLATES) {
      expect(detectTemplateId(tpl.content)).toBe(tpl.id);
      expect(detectTemplateId("  \n" + tpl.content)).toBe(tpl.id);
    }
  });

  it("returns null for custom constraint text", () => {
    expect(
      detectTemplateId("【创作约束·自定义】\n不要写高跟鞋梗。"),
    ).toBeNull();
    expect(detectTemplateId("对话要口语化")).toBeNull();
    expect(detectTemplateId("")).toBeNull();
  });

  it("findTemplateItems only picks categorized template entries", () => {
    const tpl = GENRE_TEMPLATES[0];
    const items = [
      // 命中：分类对 + marker 对
      { id: 1, category: "创作约束", content: tpl.content },
      // 分类对但内容非模板 → 不动
      { id: 2, category: "创作约束", content: "禁止水文" },
      // marker 对但分类不对 → 不动（防误删用户手滑分类的模板文本）
      { id: 3, category: "风格", content: tpl.content },
      // 其他分类
      { id: 4, category: "世界观", content: "东方玄幻" },
    ];
    const refs = findTemplateItems(items);
    expect(refs).toEqual([{ templateId: tpl.id, id: 1 }]);
  });

  it("findTemplateItems handles multiple template entries", () => {
    const [a, b] = GENRE_TEMPLATES;
    const refs = findTemplateItems([
      { id: 10, category: "创作约束", content: a.content },
      { id: 11, category: "创作约束", content: b.content },
    ]);
    expect(refs).toHaveLength(2);
    expect(refs.map((r) => r.id)).toEqual([10, 11]);
  });
});
