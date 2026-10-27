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
  act,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import JourneyQuality from "./JourneyQuality";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { usePanelStore } from "@/stores/usePanelStore";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";

const mocks = vi.hoisted(() => ({
  GetWritingConfig: vi.fn(),
  SaveWritingConfig: vi.fn(),
  SetActiveNovel: vi.fn(),
  // T2.1 检测任务卡：检测状态查询/启动 + scope 列表 + 埋点
  GetChapters: vi.fn(),
  GetVolumes: vi.fn(),
  ListDetectionTaskStatuses: vi.fn(),
  RunDetectionTask: vi.fn(),
  RecordFeatureUsage: vi.fn(),
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
    mocks.GetChapters.mockResolvedValue([]);
    mocks.GetVolumes.mockResolvedValue([]);
    mocks.ListDetectionTaskStatuses.mockResolvedValue([]);
    mocks.RunDetectionTask.mockResolvedValue({ dispatched: true });
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
    // 配置卡与检测任务卡同引 settings.selectNovelFirstHint 单源提示（两处命中）
    expect(
      screen.getAllByText("settings.selectNovelFirstHint").length,
    ).toBeGreaterThan(0);
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
    mocks.GetChapters.mockResolvedValue([]);
    mocks.GetVolumes.mockResolvedValue([]);
    mocks.ListDetectionTaskStatuses.mockResolvedValue([]);
    mocks.RunDetectionTask.mockResolvedValue({ dispatched: true });
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

  it("T3.1 编排预设定位：pipeline 直达入口 tooltip 带预设表述", () => {
    render(<JourneyQuality />, { wrapper });
    const nav = within(screen.getByTestId("journey-quality-panel-nav"));
    expect(nav.getByText("pipeline.title").closest("button")).toHaveAttribute(
      "title",
      "pipeline.presetHint",
    );
  });
});

// T2.1 验收（R5-3 契约 0.1/0.4）：检测任务四入口独立可点、不依赖流水线阶段；
// 点选 scope → RunDetectionTask；状态行消费 T1.3 检测状态查询；降级零提示。
describe("JourneyQuality：T2.1 检测任务卡（R5-3 检测任务独立入口）", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.GetWritingConfig.mockResolvedValue({
      auto_score_enabled: true,
      score_threshold: 60,
      score_max_retries: 3,
    });
    mocks.GetChapters.mockResolvedValue([
      {
        id: 101,
        novel_id: 1,
        sort_order: 1,
        title: "初入江湖",
        reading_number: 1,
      },
      {
        id: 102,
        novel_id: 1,
        sort_order: 2,
        title: "再战",
        reading_number: 2,
      },
    ]);
    mocks.GetVolumes.mockResolvedValue([
      { id: 201, novel_id: 1, name: "第一卷", sort_order: 1 },
      { id: 202, novel_id: 1, name: "第二卷", sort_order: 2 },
    ]);
    mocks.ListDetectionTaskStatuses.mockResolvedValue([]);
    mocks.RunDetectionTask.mockResolvedValue({
      kind: "book",
      novel_id: 1,
      dispatched: true,
    });
    useNovelStore.setState({ activeNovelId: 1 });
    usePanelStore.setState({ activePanel: "journey-quality" });
  });

  it("四入口独立可点、不依赖流水线阶段：点选 scope 后各调 RunDetectionTask", async () => {
    render(<JourneyQuality />, { wrapper });
    // 无活跃流水线（本面板不接 usePipeline）四入口仍可启动（契约 0.1）。
    // scope 缺省取列表首项，列表回填后按钮才可点。
    const chapterEntry = await screen.findByTestId("detection-task-chapter");
    await within(chapterEntry).findByRole("combobox");
    fireEvent.click(within(chapterEntry).getByRole("button"));
    const volumeEntry = await screen.findByTestId("detection-task-volume");
    await within(volumeEntry).findByRole("combobox");
    fireEvent.click(within(volumeEntry).getByRole("button"));
    for (const kind of ["book", "structure"] as const) {
      const entry = await screen.findByTestId(`detection-task-${kind}`);
      fireEvent.click(within(entry).getByRole("button"));
    }
    await waitFor(() =>
      expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "chapter", 101),
    );
    expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "volume", 201);
    expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "book", null);
    expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "structure", null);
    // 检测启动埋点（单源 action.detect）
    expect(mocks.RecordFeatureUsage).toHaveBeenCalledWith("action.detect");
  });

  it("scope 点选生效：切换章节/卷后启动参数随之变化", async () => {
    render(<JourneyQuality />, { wrapper });
    const chapterEntry = await screen.findByTestId("detection-task-chapter");
    const chapterSelect = await within(chapterEntry).findByRole("combobox");
    fireEvent.change(chapterSelect, { target: { value: "102" } });
    fireEvent.click(within(chapterEntry).getByRole("button"));
    await waitFor(() =>
      expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "chapter", 102),
    );

    const volumeEntry = await screen.findByTestId("detection-task-volume");
    const volumeSelect = await within(volumeEntry).findByRole("combobox");
    fireEvent.change(volumeSelect, { target: { value: "202" } });
    fireEvent.click(within(volumeEntry).getByRole("button"));
    await waitFor(() =>
      expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "volume", 202),
    );
  });

  it("状态行消费 T1.3 检测状态查询：进行态/结论徽标/未检测三态", async () => {
    mocks.ListDetectionTaskStatuses.mockResolvedValue([
      {
        kind: "chapter",
        novel_id: 1,
        running: false,
        last_verdict: "checked",
        last_issue_count: 3,
        last_run_at: "2026-10-27T10:00:00Z",
      },
      { kind: "volume", novel_id: 1, running: true, last_issue_count: 0 },
      { kind: "book", novel_id: 1, running: false, last_issue_count: 0 },
      { kind: "structure", novel_id: 1, running: false, last_issue_count: 0 },
    ]);
    render(<JourneyQuality />, { wrapper });
    // 章节：结论徽标 + 上次检测行
    const chapter = await screen.findByTestId("detection-task-chapter");
    expect(
      await within(chapter).findByText(
        "journey.quality.detect.verdict.checked",
      ),
    ).toBeInTheDocument();
    expect(
      within(chapter).getByText("journey.quality.detect.lastRun"),
    ).toBeInTheDocument();
    // 卷：进行态（状态行 + 按钮同文且收口禁用）
    const volume = await screen.findByTestId("detection-task-volume");
    await within(volume).findByRole("button", {
      name: "journey.quality.detect.running",
    });
    expect(within(volume).getByRole("button")).toBeDisabled();
    // 书：未检测
    const book = await screen.findByTestId("detection-task-book");
    expect(
      await within(book).findByText("journey.quality.detect.never"),
    ).toBeInTheDocument();
  });

  it("未选书降级：提示先选书且零查询零启动", () => {
    useNovelStore.setState({ activeNovelId: 0 });
    render(<JourneyQuality />, { wrapper });
    expect(
      screen.getAllByText("settings.selectNovelFirstHint").length,
    ).toBeGreaterThan(0);
    expect(mocks.GetChapters).not.toHaveBeenCalled();
    expect(mocks.GetVolumes).not.toHaveBeenCalled();
    expect(mocks.ListDetectionTaskStatuses).not.toHaveBeenCalled();
    expect(mocks.RunDetectionTask).not.toHaveBeenCalled();
  });

  it("空列表降级：章节/卷无 scope 时禁用并提示，书/结构仍可点", async () => {
    mocks.GetChapters.mockResolvedValue([]);
    mocks.GetVolumes.mockResolvedValue([]);
    render(<JourneyQuality />, { wrapper });
    const chapter = await screen.findByTestId("detection-task-chapter");
    expect(
      await within(chapter).findByText("journey.quality.detect.emptyChapters"),
    ).toBeInTheDocument();
    expect(within(chapter).getByRole("button")).toBeDisabled();
    const volume = await screen.findByTestId("detection-task-volume");
    expect(
      await within(volume).findByText("journey.quality.detect.emptyVolumes"),
    ).toBeInTheDocument();
    expect(within(volume).getByRole("button")).toBeDisabled();

    const book = await screen.findByTestId("detection-task-book");
    fireEvent.click(within(book).getByRole("button"));
    await waitFor(() =>
      expect(mocks.RunDetectionTask).toHaveBeenCalledWith(1, "book", null),
    );
  });
});

// T2.2 验收（R5-3）：检测启动→进行态→结果可达闭环——完成事件（事件协议零改动）
// 驱动新结果徽标+状态刷新；查看结果跳既有面板（工单/张力报告/质量档案）；降级零提示。
describe("JourneyQuality：T2.2 检测结果反馈接线", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.GetWritingConfig.mockResolvedValue({
      auto_score_enabled: true,
      score_threshold: 60,
      score_max_retries: 3,
    });
    mocks.GetChapters.mockResolvedValue([
      {
        id: 101,
        novel_id: 1,
        sort_order: 1,
        title: "初入江湖",
        reading_number: 1,
      },
    ]);
    mocks.GetVolumes.mockResolvedValue([
      { id: 201, novel_id: 1, name: "第一卷", sort_order: 1 },
    ]);
    mocks.ListDetectionTaskStatuses.mockResolvedValue([
      {
        kind: "chapter",
        novel_id: 1,
        running: false,
        last_verdict: "checked",
        last_issue_count: 3,
        last_run_at: "2026-10-27T10:00:00Z",
      },
      {
        kind: "book",
        novel_id: 1,
        running: false,
        last_verdict: "pass",
        last_issue_count: 0,
        last_run_at: "2026-10-27T11:00:00Z",
      },
    ]);
    mocks.RunDetectionTask.mockResolvedValue({ dispatched: true });
    useNovelStore.setState({ activeNovelId: 1 });
    usePanelStore.setState({ activePanel: "journey-quality" });
  });

  /** 取 useDetectionFeedback 订阅的事件回调（setup 全局 mock 的 EventsOn）。 */
  const handlerOf = (event: string) => {
    const hit = vi.mocked(EventsOn).mock.calls.find((c) => c[0] === event);
    expect(hit, `event ${event} subscribed`).toBeTruthy();
    return hit![1] as (p: unknown) => void;
  };

  it("检测完成事件→新结果徽标+状态查询失效；查看结果跳正确面板并消隐", async () => {
    render(<JourneyQuality />, { wrapper });
    const book = await screen.findByTestId("detection-task-book");
    await within(book).findByText("journey.quality.detect.verdict.pass");

    // volume/book 完成事件（T1.2 pipeline:detect_done 增量通道）
    const before = mocks.ListDetectionTaskStatuses.mock.calls.length;
    await act(async () => {
      handlerOf("pipeline:detect_done")({
        novel_id: 1,
        kind: "book",
        verdict: "pass",
        issue_count: 2,
      });
    });
    expect(
      await within(book).findByTestId("detection-fresh-book"),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(mocks.ListDetectionTaskStatuses.mock.calls.length).toBeGreaterThan(
        before,
      ),
    );

    // 查看结果 → 跳流水线面板（工单/张力报告所在），新结果徽标消隐
    fireEvent.click(within(book).getByTestId("detection-result-book"));
    expect(usePanelStore.getState().activePanel).toBe("pipeline");
    await waitFor(() =>
      expect(within(book).queryByTestId("detection-fresh-book")).toBeNull(),
    );
  });

  it("巡检完成沿用既有 consistency_patrol 通道；载荷 novel 不符静默忽略", async () => {
    render(<JourneyQuality />, { wrapper });
    await screen.findByTestId("detection-task-structure");
    await act(async () => {
      handlerOf("pipeline:consistency_patrol")({ novel_id: 999 });
    });
    expect(screen.queryByTestId("detection-fresh-structure")).toBeNull();
    await act(async () => {
      handlerOf("pipeline:consistency_patrol")({ novel_id: 1 });
    });
    expect(
      await screen.findByTestId("detection-fresh-structure"),
    ).toBeInTheDocument();
  });

  it("结果跳转门控：有结果才可达——章节→chapters、书→pipeline；未检测入口零跳转按钮", async () => {
    render(<JourneyQuality />, { wrapper });
    // 等检测状态查询回填后再交互（回填前无结果跳转按钮）
    const chapter = await screen.findByTestId("detection-task-chapter");
    await within(chapter).findByText("journey.quality.detect.verdict.checked");
    fireEvent.click(within(chapter).getByTestId("detection-result-chapter"));
    expect(usePanelStore.getState().activePanel).toBe("chapters");
    const book = await screen.findByTestId("detection-task-book");
    await within(book).findByText("journey.quality.detect.verdict.pass");
    fireEvent.click(within(book).getByTestId("detection-result-book"));
    expect(usePanelStore.getState().activePanel).toBe("pipeline");
    // volume/structure 未检测（无 last_run_at）→ 零跳转按钮
    await waitFor(() =>
      expect(mocks.ListDetectionTaskStatuses).toHaveBeenCalled(),
    );
    expect(screen.queryByTestId("detection-result-volume")).toBeNull();
    expect(screen.queryByTestId("detection-result-structure")).toBeNull();
  });
});
