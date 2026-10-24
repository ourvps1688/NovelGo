// T3.1 验收②：设置对话框瘦身三 Tab（general/model/about），writing Tab 移除；
// 数据目录等基础设施仍在 general（GeneralConfigTab 零改动，源码断言兜底）。
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import SettingsDialog from "./SettingsDialog";

vi.mock("@/components/settings/GeneralConfigTab", () => ({
  default: () => <div data-testid="general-tab-mock" />,
}));
vi.mock("@/components/settings/ModelConfigTab", () => ({
  default: () => <div data-testid="model-tab-mock" />,
}));
vi.mock("@/components/settings/AboutTab", () => ({
  default: () => <div data-testid="about-tab-mock" />,
}));

// 定位 frontend/src：vitest 下 import.meta 形态不稳，按候选逐一探测（防真空扫描）
function findSrcDir(): string {
  const selfDir = (import.meta as { dirname?: string }).dirname ?? ".";
  const candidates = [
    resolve(process.cwd(), "src"),
    resolve(process.cwd(), "frontend/src"),
    resolve(dirname(selfDir), ".."),
    resolve(dirname(selfDir), "..", ".."),
    resolve(dirname(selfDir), "..", "..", ".."),
  ];
  for (const dir of candidates) {
    if (existsSync(resolve(dir, "components/settings/SettingsDialog.tsx"))) {
      return dir;
    }
  }
  throw new Error(`src dir not found in candidates: ${candidates.join(", ")}`);
}

describe("SettingsDialog：设置页瘦身（T3.1）", () => {
  it("仅剩三 Tab（general/model/about），writing Tab 已移除不留双轨", () => {
    render(<SettingsDialog open onClose={vi.fn()} initialTab="general" />);
    const nav = screen.getByRole("navigation");
    // 三个导航 Tab 按钮（不含右上关闭按钮）
    expect(within(nav).getAllByRole("button")).toHaveLength(3);
    expect(screen.getByText("settings.general")).toBeInTheDocument();
    expect(screen.getByText("settings.modelConfig")).toBeInTheDocument();
    expect(screen.getByText("settings.about")).toBeInTheDocument();
    expect(screen.queryByText("settings.writingScore")).toBeNull();
  });

  it("Tab 切换渲染对应内容区（general=数据目录等基础设施入口）", () => {
    render(<SettingsDialog open onClose={vi.fn()} initialTab="general" />);
    expect(screen.getByTestId("general-tab-mock")).toBeInTheDocument();
    fireEvent.click(screen.getByText("settings.modelConfig"));
    expect(screen.getByTestId("model-tab-mock")).toBeInTheDocument();
    fireEvent.click(screen.getByText("settings.about"));
    expect(screen.getByTestId("about-tab-mock")).toBeInTheDocument();
    fireEvent.click(screen.getByText("settings.general"));
    expect(screen.getByTestId("general-tab-mock")).toBeInTheDocument();
  });

  it("源码断言：SettingsDialog 无 writing Tab 残留，数据目录仍在 GeneralConfigTab", () => {
    const src = findSrcDir();
    const dialog = readFileSync(
      resolve(src, "components/settings/SettingsDialog.tsx"),
      "utf8",
    );
    const general = readFileSync(
      resolve(src, "components/settings/GeneralConfigTab.tsx"),
      "utf8",
    );
    expect(dialog).not.toContain("WritingConfigTab");
    expect(dialog).not.toContain('"writing"');
    // 数据目录等基础设施留设置 general（plan 原文）
    expect(general).toContain("settings.dataDir");
  });
});
