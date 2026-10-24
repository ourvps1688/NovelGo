// T3.1 验收①：写作评分配置迁入质检工厂——旅程内可完成读写。
// WritingConfigTab 与 useWritingConfig 零改动，仅挂载点自设置页迁入本面板。
// T3.2 验收：旅程卡按序渲染、点击直达正确面板；三面板一跳触达 + 评分配置。
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import JourneyQuality from "./JourneyQuality";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { usePanelStore } from "@/stores/usePanelStore";

const mocks = vi.hoisted(() => ({
  GetWritingConfig: vi.fn(),
  SaveWritingConfig: vi.fn(),
  SetActiveNovel: vi.fn(),
}));

vi.mock("@/lib/wailsjs/go/app/App", () => mocks);

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("JourneyQuality：写作评分配置卡（T3.1 挂载点迁移）", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.GetWritingConfig.mockResolvedValue({
      auto_score_enabled: true,
      score_threshold: 60,
      score_max_retries: 3,
    });
    mocks.SaveWritingConfig.mockResolvedValue(undefined);
    useNovelStore.setState({ activeNovelId: 1 });
  });

  it("配置卡挂载于质检旅程面板（组件零改动，标题与控件逐字保留）", async () => {
    render(<JourneyQuality />, { wrapper });
    expect(
      await screen.findByTestId("journey-writing-config"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("region", { name: "journey.quality.writingConfig" }),
    ).toBeInTheDocument();
    // WritingConfigTab 自带标题与三个配置项（逐字旧行为）；等 query 回填后断言
    const sw = await screen.findByRole("switch", {
      name: "settings.autoScoreEnabled",
    });
    expect(sw).toBeInTheDocument();
    expect(screen.getByText("settings.writingScore")).toBeInTheDocument();
    expect(screen.getByDisplayValue("60")).toBeInTheDocument();
    expect(screen.getByDisplayValue("3")).toBeInTheDocument();
  });

  it("旅程内读写：改开关/阈值/重试并保存 → SaveWritingConfig 落库", async () => {
    render(<JourneyQuality />, { wrapper });
    const sw = await screen.findByRole("switch", {
      name: "settings.autoScoreEnabled",
    });
    expect(sw).toBeChecked();

    fireEvent.click(sw);
    fireEvent.change(screen.getByDisplayValue("60"), {
      target: { value: "75" },
    });
    fireEvent.change(screen.getByDisplayValue("3"), {
      target: { value: "5" },
    });
    fireEvent.click(screen.getByText("common.save"));

    await waitFor(() =>
      expect(mocks.SaveWritingConfig).toHaveBeenCalledWith(1, {
        auto_score_enabled: false,
        score_threshold: 75,
        score_max_retries: 5,
      }),
    );
  });

  it("未选书降级：提示先选书且零查询（逐字旧行为）", () => {
    useNovelStore.setState({ activeNovelId: 0 });
    render(<JourneyQuality />, { wrapper });
    expect(
      screen.getByText("settings.selectNovelFirstHint"),
    ).toBeInTheDocument();
    expect(mocks.GetWritingConfig).not.toHaveBeenCalled();
    expect(mocks.SaveWritingConfig).not.toHaveBeenCalled();
  });
});

describe("JourneyQuality：T3.2 旅程卡与三面板次级导航", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.GetWritingConfig.mockResolvedValue({
      auto_score_enabled: true,
      score_threshold: 60,
      score_max_retries: 3,
    });
    useNovelStore.setState({ activeNovelId: 1 });
    usePanelStore.setState({ activePanel: "journey-quality" });
  });

  it("旅程卡按序渲染（体检→修复→冻结→巡检），每卡「喂给签约下限」短语", () => {
    render(<JourneyQuality />, { wrapper });
    const cards = screen.getAllByTestId("journey-card");
    expect(cards).toHaveLength(4);
    const order = [
      "journey.quality.stages.audit",
      "journey.quality.stages.fix",
      "journey.quality.stages.freeze",
      "journey.quality.stages.patrol",
    ];
    order.forEach((key, i) => {
      expect(within(cards[i]).getByText(key)).toBeInTheDocument();
      // 「喂给签约下限」短语经 shell.factory.quality.feeds 单源引出
      expect(
        within(cards[i]).getByText("shell.factory.quality.feeds"),
      ).toBeInTheDocument();
    });
  });

  it("旅程卡点击直达正确面板（体检→signability，修复/冻结/巡检→pipeline）", () => {
    render(<JourneyQuality />, { wrapper });
    const cards = screen.getAllByTestId("journey-card");
    fireEvent.click(cards[0]);
    expect(usePanelStore.getState().activePanel).toBe("signability");
    for (const i of [1, 2, 3]) {
      fireEvent.click(cards[i]);
      expect(usePanelStore.getState().activePanel).toBe("pipeline");
    }
  });

  it("三面板次级导航一跳触达 pipeline/reader/signability（#185 验收）", () => {
    render(<JourneyQuality />, { wrapper });
    const nav = within(screen.getByTestId("journey-quality-panel-nav"));
    fireEvent.click(nav.getByText("shell.workflow"));
    expect(usePanelStore.getState().activePanel).toBe("signability");
    fireEvent.click(nav.getByText("pipeline.title"));
    expect(usePanelStore.getState().activePanel).toBe("pipeline");
    fireEvent.click(nav.getByText("shell.readerView"));
    expect(usePanelStore.getState().activePanel).toBe("reader");
  });
});
