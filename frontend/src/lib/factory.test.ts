import { describe, it, expect } from "vitest";

import {
  ALL_SIDEBAR_PANEL_IDS,
  FACTORY_MAP,
  FACTORY_ORDER,
  JOURNEY_BY_FACTORY,
  factoryFeedsKey,
  factoryOf,
  factoryTitleKey,
  type FactoryId,
} from "./factory";

describe("factory map 单源（WBS §0.1）", () => {
  it("全键覆盖：SidebarPanelId 每一项都有归属，无缺键无多余键", () => {
    const mapped = Object.keys(FACTORY_MAP).sort();
    const expected = [...ALL_SIDEBAR_PANEL_IDS].sort();
    expect(mapped).toEqual(expected);
  });

  it("归属定案表逐项正确（喂给谁=消费方）", () => {
    // absorb：喂给文风上限
    expect(FACTORY_MAP["journey-absorb"]).toBe("absorb");
    expect(FACTORY_MAP["style-samples"]).toBe("absorb");
    expect(FACTORY_MAP.skills).toBe("absorb");
    expect(FACTORY_MAP["skill-matrix"]).toBe("absorb");
    // create：喂给正文与真理层
    expect(FACTORY_MAP["journey-create"]).toBe("create");
    expect(FACTORY_MAP.chapters).toBe("create");
    expect(FACTORY_MAP["chapter-management"]).toBe("create");
    expect(FACTORY_MAP.preferences).toBe("create");
    expect(FACTORY_MAP["novel-settings"]).toBe("create");
    expect(FACTORY_MAP.profile).toBe("create");
    expect(FACTORY_MAP.bookpitch).toBe("create");
    expect(FACTORY_MAP.characters).toBe("create");
    expect(FACTORY_MAP.locations).toBe("create");
    expect(FACTORY_MAP.storyarcs).toBe("create");
    expect(FACTORY_MAP.timeline).toBe("create");
    // quality：喂给签约下限
    expect(FACTORY_MAP["journey-quality"]).toBe("quality");
    expect(FACTORY_MAP.signability).toBe("quality");
    expect(FACTORY_MAP.pipeline).toBe("quality");
    expect(FACTORY_MAP.reader).toBe("quality");
    // base：喂给全部
    expect(FACTORY_MAP.novels).toBe("base");
    expect(FACTORY_MAP.search).toBe("base");
    expect(FACTORY_MAP.git).toBe("base");
  });

  it("每项归属值都是合法 FactoryId，且四个工厂非空", () => {
    const seen = new Set<FactoryId>();
    for (const id of ALL_SIDEBAR_PANEL_IDS) {
      const f = FACTORY_MAP[id];
      expect(FACTORY_ORDER).toContain(f);
      seen.add(f);
    }
    expect(seen.size).toBe(FACTORY_ORDER.length);
  });

  it("factoryOf 缺键兜底 base 不崩溃（§0.6 降级纪律）", () => {
    expect(factoryOf("chapters")).toBe("create");
    expect(factoryOf("不存在的面板" as never)).toBe("base");
  });

  it("i18n key 单源命名空间 shell.factory.*", () => {
    expect(factoryTitleKey("absorb")).toBe("shell.factory.absorb.title");
    expect(factoryFeedsKey("quality")).toBe("shell.factory.quality.feeds");
  });

  it("旅程中枢映射：三工厂各一，base 无旅程（T1.2）", () => {
    expect(JOURNEY_BY_FACTORY.absorb).toBe("journey-absorb");
    expect(JOURNEY_BY_FACTORY.create).toBe("journey-create");
    expect(JOURNEY_BY_FACTORY.quality).toBe("journey-quality");
    expect(JOURNEY_BY_FACTORY.base).toBeUndefined();
    // 旅程 PanelId 自身归属与所属工厂一致
    for (const [factory, journey] of Object.entries(JOURNEY_BY_FACTORY)) {
      expect(FACTORY_MAP[journey!]).toBe(factory);
    }
  });
});
