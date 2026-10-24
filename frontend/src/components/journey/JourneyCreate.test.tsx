// T4.2 验收：创作工厂旅程——双速卡并列呈现且各直达原工作面；
// 「继续写作」一跳 chapters（T1.2 行为勿删）；重写卡复用 T2.2 空态 hint 挂点；
// 十面板次级导航一跳触达（#187）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, within } from "@testing-library/react";
import JourneyCreate from "./JourneyCreate";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { usePanelStore } from "@/stores/usePanelStore";

const mocks = vi.hoisted(() => ({
  GetVoiceLedger: vi.fn(),
}));

vi.mock("@/lib/wailsjs/go/app/App", () => mocks);

describe("JourneyCreate：T4.2 旅程卡、双速并列与十面板次级导航", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // 默认已提取：hint 零渲染（逐字旧行为），空态用例单独覆写
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: true },
    ]);
    useNovelStore.setState({ activeNovelId: 1 });
    usePanelStore.setState({ activePanel: "journey-create" });
  });

  it("「继续写作」主卡保持首位且一跳 chapters（T1.2 行为勿删）", () => {
    render(<JourneyCreate />);
    const cards = screen.getAllByTestId("journey-card");
    expect(
      within(cards[0]).getByText("journey.continueWriting"),
    ).toBeInTheDocument();
    fireEvent.click(cards[0]);
    expect(usePanelStore.getState().activePanel).toBe("chapters");
  });

  it("双速卡并列呈现且各直达原工作面（自由区 chat→chapters，质检区→pipeline）", () => {
    render(<JourneyCreate />);
    // 双速两张卡刻意不合并（plan §6.1 反模式）
    const free = screen.getByText("journey.create.stages.writingFree");
    const pipeline = screen.getByText("journey.create.stages.writingPipeline");
    expect(free).toBeInTheDocument();
    expect(pipeline).toBeInTheDocument();

    fireEvent.click(free);
    expect(usePanelStore.getState().activePanel).toBe("chapters");
    fireEvent.click(pipeline);
    expect(usePanelStore.getState().activePanel).toBe("pipeline");
  });

  it("意图卡→bookpitch，重写卡→chapters（重写回写作页）", () => {
    render(<JourneyCreate />);
    fireEvent.click(screen.getByText("journey.create.stages.intent"));
    expect(usePanelStore.getState().activePanel).toBe("bookpitch");
    fireEvent.click(screen.getByText("journey.create.stages.rewrite"));
    expect(usePanelStore.getState().activePanel).toBe("chapters");
  });

  it("重写卡复用 T2.2 空态 hint 挂点：未提取出 hint 且一键跳提取，已提取零 hint", async () => {
    // 未提取：十维 Present 全 false
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: false },
    ]);
    const { unmount } = render(<JourneyCreate />);
    const hint = await screen.findByTestId("voice-extraction-hint");
    expect(hint).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("voice-extraction-hint-go"));
    expect(usePanelStore.getState().activePanel).toBe("style-samples");
    unmount();

    // 已提取：零提示（逐字旧行为）
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: true },
    ]);
    render(<JourneyCreate />);
    await screen.findByTestId("journey-create");
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();
  });

  it("十面板次级导航一跳触达（#187 验收）", () => {
    render(<JourneyCreate />);
    const nav = within(screen.getByTestId("journey-create-panel-nav"));
    const targets: [string, string][] = [
      ["shell.chapters", "chapters"],
      ["shell.chapterManagement", "chapter-management"],
      ["shell.preference", "preferences"],
      ["shell.novelSetting", "novel-settings"],
      ["workspace.profile", "profile"],
      ["workflow.bookpitch", "bookpitch"],
      ["shell.characters", "characters"],
      ["shell.locations", "locations"],
      ["shell.arcs", "storyarcs"],
      ["shell.timeline", "timeline"],
    ];
    expect(nav.getAllByRole("button")).toHaveLength(10);
    for (const [labelKey, panel] of targets) {
      fireEvent.click(nav.getByText(labelKey));
      expect(usePanelStore.getState().activePanel).toBe(panel);
    }
  });
});
