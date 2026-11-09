// 技能装配用量面板（#111 P0）测试：统一账本有消费面，且「30 天零记录」能自动报警。
// 范式照 FeatureUsageSection.test.tsx（QueryClientProvider + wails 绑定 mock）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { getSkillUsage, listSkills } = vi.hoisted(() => ({
  getSkillUsage: vi.fn(),
  listSkills: vi.fn(),
}));

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  GetSkillUsage: getSkillUsage,
  ListSkills: listSkills,
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { count?: number }) =>
      opts?.count === undefined ? key : `${key}:${opts.count}`,
  }),
}));

import SkillUsagePanel from "./SkillUsagePanel";

const builtinSkills = [
  { name: "next", source: "builtin" },
  { name: "chapter-ci", source: "builtin" },
  { name: "memory", source: "builtin" },
  { name: "custom", source: "user" },
];

function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(<SkillUsagePanel novelId={7} />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  return {
    ...view,
    cleanup: () => {
      view.unmount();
      qc.clear();
    },
  };
}

async function flushEffects() {
  await act(async () => {});
}

beforeEach(() => {
  getSkillUsage.mockReset();
  listSkills.mockReset();
  listSkills.mockResolvedValue(builtinSkills);
});

describe("技能用量面板（#111 P0）", () => {
  it("按 skill 聚合通道与次数，并把零记录内置技能标红", async () => {
    getSkillUsage.mockResolvedValue([
      {
        skill: "next",
        channel: "chat_intent",
        count: 2,
        last_seen: "2026-11-09 21:00:00",
      },
      {
        skill: "next",
        channel: "slash",
        count: 1,
        last_seen: "2026-11-09 22:00:00",
      },
      {
        skill: "chapter-ci",
        channel: "chapter_ci",
        count: 5,
        last_seen: "2026-11-08 10:00:00",
      },
      // 通道级事件（空 skill）不得计入技能用量。
      { skill: "", channel: "state_sync", count: 1, last_seen: "" },
    ]);
    const { cleanup } = renderPanel();
    expect(await screen.findByTestId("skill-usage-panel")).toBeTruthy();
    expect(getSkillUsage).toHaveBeenCalledWith(30);
    expect(screen.getAllByTestId("skill-usage-row")).toHaveLength(2);
    expect(screen.getByText("skillMatrix.usage.times:3")).toBeTruthy();
    expect(screen.getByText("skillMatrix.usage.times:5")).toBeTruthy();
    expect(screen.getByText("chat_intent, slash")).toBeTruthy();
    expect(screen.getByText("2026-11-09 22:00")).toBeTruthy();

    const idle = screen.getAllByTestId("skill-usage-idle-item");
    expect(idle).toHaveLength(1);
    expect(idle[0].textContent).toContain("memory");
    expect(idle[0].className).toContain("text-destructive");
    cleanup();
  });

  it("账本为空时显示空态并列出全部零记录内置技能（不含用户技能）", async () => {
    getSkillUsage.mockResolvedValue([]);
    const { cleanup } = renderPanel();
    expect(await screen.findByTestId("skill-usage-panel")).toBeTruthy();
    expect(screen.getByTestId("skill-usage-empty")).toBeTruthy();
    const idle = screen.getAllByTestId("skill-usage-idle-item");
    expect(idle.map((node) => node.textContent)).toEqual([
      "chapter-ci",
      "memory",
      "next",
    ]);
    cleanup();
  });

  it("无技能且无账本记录时静默不渲染", async () => {
    listSkills.mockResolvedValue([]);
    getSkillUsage.mockResolvedValue([]);
    const { cleanup } = renderPanel();
    await flushEffects();
    expect(screen.queryByTestId("skill-usage-panel")).toBeNull();
    cleanup();
  });

  it("账本查询失败时静默降级，不误报零记录", async () => {
    getSkillUsage.mockRejectedValue(new Error("bridge down"));
    const { cleanup } = renderPanel();
    await waitFor(() => expect(getSkillUsage).toHaveBeenCalled());
    await flushEffects();
    expect(screen.queryByTestId("skill-usage-panel")).toBeNull();
    expect(screen.queryByTestId("skill-usage-idle")).toBeNull();
    cleanup();
  });
});
