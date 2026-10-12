import { describe, expect, it } from "vitest";
import {
  GENRE_TEMPLATES,
  detectTemplateId,
  findTemplateItems,
} from "./genreTemplates";

describe("genreTemplates", () => {
  it("exposes four templates with matching marker prefix", () => {
    expect(GENRE_TEMPLATES).toHaveLength(4);
    for (const tpl of GENRE_TEMPLATES) {
      expect(tpl.content.trimStart().startsWith(tpl.marker)).toBe(true);
      expect(tpl.marker).toMatch(/^【创作约束·.+】$/);
    }
  });

  it("detects each template by first-line marker", () => {
    for (const tpl of GENRE_TEMPLATES) {
      expect(detectTemplateId(tpl.content)).toBe(tpl.id);
      // 容忍前导空白
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
