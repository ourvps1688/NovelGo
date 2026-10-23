import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ActivityBar from "./ActivityBar";
import { usePanelStore } from "@/stores/usePanelStore";

vi.mock("react-i18next", async () => {
  const { createInstance } = await import("i18next");
  const { default: zhCN } = await import("@/i18n/locales/zh-CN.json");
  const i18n = createInstance();
  await i18n.init({
    lng: "zh-CN",
    resources: { "zh-CN": { translation: zhCN } },
    interpolation: { escapeValue: false },
  });
  return { useTranslation: () => ({ t: i18n.t.bind(i18n) }) };
});

const FACTORY_TITLES = ["吸收工厂", "创作工厂", "质检工厂", "基础设施"];

// 现有 16 项功能条目 label（一比一对照 ActivityBar，防丢失回归）
const ALL_LABELS = [
  "搜索",
  "书架",
  "章节",
  "章节管理",
  "偏好",
  "小说设定",
  "角色",
  "地点",
  "弧线",
  "时间线",
  "创作历史",
  "读者视角",
  "技能",
  "技能矩阵",
  "提取",
  "工作流",
];

describe("ActivityBar 四工厂分组（WBS T1.1）", () => {
  beforeEach(() => {
    localStorage.clear();
    usePanelStore.setState({ activePanel: "chapters", sidebarPanel: null });
  });

  it("渲染四个工厂分组标题，顺序=价值链顺序（吸收→创作→质检→基础）", () => {
    render(<ActivityBar onSelect={() => {}} />);
    const texts = FACTORY_TITLES.map((title) => {
      const el = screen.getByText(title);
      expect(el).toBeTruthy();
      return el.textContent;
    });
    expect(texts).toEqual(FACTORY_TITLES);
    // 价值链顺序：DOM 中分组标题先后顺序断言
    const nav = screen.getByRole("navigation");
    const navText = nav.textContent ?? "";
    let last = -1;
    for (const title of FACTORY_TITLES) {
      const idx = navText.indexOf(title);
      expect(idx).toBeGreaterThan(last);
      last = idx;
    }
  });

  it("16 条目无一丢失，全部渲染且可点击", () => {
    const onSelect = vi.fn();
    render(<ActivityBar onSelect={onSelect} />);
    for (const label of ALL_LABELS) {
      expect(screen.getByText(label)).toBeTruthy();
    }
    fireEvent.click(screen.getByText("提取"));
    expect(onSelect).toHaveBeenCalledWith("style-samples");
  });

  it("每组显示「喂给谁」叙案短语", () => {
    render(<ActivityBar onSelect={() => {}} />);
    expect(screen.getByText("喂给文风上限")).toBeTruthy();
    expect(screen.getByText("喂给正文与真理层")).toBeTruthy();
    expect(screen.getByText("喂给签约下限")).toBeTruthy();
    expect(screen.getByText("喂给全部")).toBeTruthy();
  });

  it("折叠态只显示图标不显示分组标题（保持现有折叠行为）", () => {
    render(<ActivityBar onSelect={() => {}} />);
    fireEvent.click(screen.getByTitle("收起导航"));
    for (const title of FACTORY_TITLES) {
      expect(screen.queryByText(title)).toBeNull();
    }
  });
});
