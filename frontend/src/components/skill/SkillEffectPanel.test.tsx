// 技能效果面板（#116 P1）测试：把「技能参与的改稿轮」与「目标发现项收敛」
// 关联起来展示；口径诚实（无快照不反推成全部消掉）；失败/空数据静默降级。
// 范式照 SkillUsagePanel.test.tsx（QueryClientProvider + wails 绑定 mock）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { getSkillEffectStats } = vi.hoisted(() => ({
  getSkillEffectStats: vi.fn(),
}));

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  GetSkillEffectStats: getSkillEffectStats,
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, number>) => {
      if (!opts) return key;
      const [k, v] = Object.entries(opts)[0] ?? ["", 0];
      return `${key}:${k}=${v}`;
    },
  }),
}));

import SkillEffectPanel from "./SkillEffectPanel";

function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(<SkillEffectPanel novelId={7} />, {
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

beforeEach(() => {
  getSkillEffectStats.mockReset();
});

describe("技能效果面板（#116 P1）", () => {
  it("按轮数降序展示参与轮/已消除/仍在/收敛率，并调用 30 天出口", async () => {
    getSkillEffectStats.mockResolvedValue([
      {
        skill: "deai-flavor",
        channel: "chapter_ci_selfcheck",
        rounds: 1,
        resolved: 0,
        persisted: 0,
      },
      {
        skill: "scene-beats",
        channel: "writing_bundle",
        rounds: 4,
        resolved: 3,
        persisted: 1,
      },
      {
        skill: "revision-pass",
        channel: "chat_intent",
        rounds: 2,
        resolved: 2,
        persisted: 0,
      },
    ]);
    const { cleanup } = renderPanel();
    expect(await screen.findByTestId("skill-effect-panel")).toBeTruthy();
    expect(getSkillEffectStats).toHaveBeenCalledWith(30);
    const rows = screen.getAllByTestId("skill-effect-row");
    expect(rows).toHaveLength(3);
    // 按参与轮数降序：scene-beats 第一行。
    expect(rows[0].textContent).toContain("scene-beats");
    expect(
      screen.getByText("skillMatrix.effects.roundsTimes:count=4"),
    ).toBeTruthy();
    expect(
      screen.getByText("skillMatrix.effects.itemTimes:count=3"),
    ).toBeTruthy();
    // 3 resolved / (3+1) = 75%。
    expect(
      screen.getByText("skillMatrix.effects.rateValue:value=75"),
    ).toBeTruthy();
    // 无快照行显示占位符，不反推成 100%。
    expect(screen.getByText("skillMatrix.effects.noSnapshot")).toBeTruthy();
    cleanup();
  });

  it("空数据 / 查询失败时静默不渲染（零报错零空态噪音）", async () => {
    getSkillEffectStats.mockResolvedValue([]);
    const empty = renderPanel();
    await act(async () => {});
    expect(screen.queryByTestId("skill-effect-panel")).toBeNull();
    empty.cleanup();

    getSkillEffectStats.mockRejectedValue(new Error("table missing"));
    const failed = renderPanel();
    await act(async () => {});
    expect(screen.queryByTestId("skill-effect-panel")).toBeNull();
    failed.cleanup();
  });
});
