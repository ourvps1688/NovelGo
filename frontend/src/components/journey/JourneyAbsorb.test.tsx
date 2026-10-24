// T4.1 验收：吸收工厂旅程——旅程卡按序渲染、直达正确面板；
// 「喂给文风上限」短语落卡；三面板一跳触达（#186）；空态承接 T2.2 同语提示。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import JourneyAbsorb from "./JourneyAbsorb";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { usePanelStore } from "@/stores/usePanelStore";

const mocks = vi.hoisted(() => ({
  GetVoiceLedger: vi.fn(),
}));

vi.mock("@/lib/wailsjs/go/app/App", () => mocks);

describe("JourneyAbsorb：T4.1 旅程卡与三面板次级导航", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // 默认：已提取（任一 present=true）——空态提示用例单独覆写
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: true },
    ]);
    useNovelStore.setState({ activeNovelId: 1 });
    usePanelStore.setState({ activePanel: "journey-absorb" });
  });

  it("旅程卡按序渲染（导入→提取→入库），每卡「喂给文风上限」短语", () => {
    render(<JourneyAbsorb />);
    const cards = screen.getAllByTestId("journey-card");
    expect(cards).toHaveLength(3);
    const order = [
      "journey.absorb.stages.import",
      "journey.absorb.stages.extract",
      "journey.absorb.stages.onboard",
    ];
    order.forEach((key, i) => {
      expect(within(cards[i]).getByText(key)).toBeInTheDocument();
      // 「喂给文风上限」短语经 shell.factory.absorb.feeds 单源引出
      expect(
        within(cards[i]).getByText("shell.factory.absorb.feeds"),
      ).toBeInTheDocument();
    });
  });

  it("旅程卡点击直达正确面板（导入→novels，提取→style-samples，入库→skills）", () => {
    render(<JourneyAbsorb />);
    const cards = screen.getAllByTestId("journey-card");
    fireEvent.click(cards[0]);
    expect(usePanelStore.getState().activePanel).toBe("novels");
    fireEvent.click(cards[1]);
    expect(usePanelStore.getState().activePanel).toBe("style-samples");
    fireEvent.click(cards[2]);
    expect(usePanelStore.getState().activePanel).toBe("skills");
  });

  it("三面板次级导航一跳触达 style-samples/skills/skill-matrix（#186 验收）", () => {
    render(<JourneyAbsorb />);
    const nav = within(screen.getByTestId("journey-absorb-panel-nav"));
    fireEvent.click(nav.getByText("shell.extract"));
    expect(usePanelStore.getState().activePanel).toBe("style-samples");
    fireEvent.click(nav.getByText("shell.skills"));
    expect(usePanelStore.getState().activePanel).toBe("skills");
    fireEvent.click(nav.getByText("shell.skillMatrix"));
    expect(usePanelStore.getState().activePanel).toBe("skill-matrix");
  });

  it("空态承接：未提取 → 提取卡落 T2.2 同语提示；已提取 → 零提示（逐字旧行为）", async () => {
    // 未提取：十维 Present 全 false
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: false },
      { dimension: "tone", present: false },
    ]);
    const { unmount } = render(<JourneyAbsorb />);
    expect(
      await screen.findByTestId("absorb-voice-empty-hint"),
    ).toHaveTextContent("emptyHint.voiceExtraction");
    unmount();

    // 已提取：零提示
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: true },
    ]);
    render(<JourneyAbsorb />);
    await screen.findByTestId("journey-absorb");
    expect(screen.queryByTestId("absorb-voice-empty-hint")).toBeNull();
  });
});
