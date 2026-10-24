// P3 T5.1 双验收（WBS §5 T5.1）：
// ①任意功能入口能回答「喂给谁」：FACTORY_MAP 全键覆盖 + 面包屑渲染断言 + ActivityBar 全条目有分组归属；
// ②新用户按旅程走通吸收→创作→质检：旅程视图卡片链路逐跳可达。
// 另含 PanelId 零删除、18 面板全可达回归断言（源码级，与 WorkspaceView.test 运行时断言互补）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { render, screen, fireEvent, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";
import {
  FACTORY_MAP,
  FACTORY_ORDER,
  ALL_SIDEBAR_PANEL_IDS,
  factoryOf,
  factoryTitleKey,
  factoryFeedsKey,
} from "@/lib/factory";
import { FactoryBreadcrumb } from "@/components/journey/JourneyHeader";
import ActivityBar from "@/components/shell/ActivityBar";
import JourneyAbsorb from "@/components/journey/JourneyAbsorb";
import JourneyCreate from "@/components/journey/JourneyCreate";
import JourneyQuality from "@/components/journey/JourneyQuality";
import { usePanelStore } from "@/stores/usePanelStore";
import { useNovelStore } from "@/components/novel/useNovelStore";

const mocks = vi.hoisted(() => ({
  GetVoiceLedger: vi.fn(),
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

// 定位 frontend/src：vitest 下 import.meta 形态不稳，按候选逐一探测（防真空扫描）
function findSrcDir(): string {
  const selfDir = (import.meta as { dirname?: string }).dirname ?? ".";
  const candidates = [
    resolve(process.cwd(), "src"),
    resolve(process.cwd(), "frontend/src"),
    resolve(dirname(selfDir), ".."),
    resolve(dirname(selfDir), "..", ".."),
  ];
  for (const dir of candidates) {
    if (existsSync(resolve(dir, "types/panel.ts"))) return dir;
  }
  throw new Error(`src dir not found in candidates: ${candidates.join(", ")}`);
}

describe("T5.1 双验收①：任意功能入口能回答「喂给谁」", () => {
  it("FACTORY_MAP 全键覆盖：16 条目 + 3 旅程视图全归属，缺键零容忍", () => {
    for (const id of ALL_SIDEBAR_PANEL_IDS) {
      const factory = FACTORY_MAP[id];
      expect(factory, `FACTORY_MAP 缺键: ${id}`).toBeDefined();
      expect(FACTORY_ORDER).toContain(factory);
    }
    // 旅程视图归属本工厂
    expect(factoryOf("journey-absorb")).toBe("absorb");
    expect(factoryOf("journey-create")).toBe("create");
    expect(factoryOf("journey-quality")).toBe("quality");
    // §0.1 定案表抽查：quality=signability/pipeline/reader；absorb=提取三面板
    expect(factoryOf("signability")).toBe("quality");
    expect(factoryOf("pipeline")).toBe("quality");
    expect(factoryOf("reader")).toBe("quality");
    expect(factoryOf("style-samples")).toBe("absorb");
    expect(factoryOf("skills")).toBe("absorb");
    expect(factoryOf("skill-matrix")).toBe("absorb");
  });

  it("归属面包屑渲染断言：逐入口显示正确工厂与「喂给谁」", () => {
    for (const id of ALL_SIDEBAR_PANEL_IDS) {
      const { unmount } = render(<FactoryBreadcrumb panelId={id} />);
      const bar = screen.getByTestId("factory-breadcrumb");
      expect(bar.getAttribute("data-factory")).toBe(FACTORY_MAP[id]);
      expect(bar.textContent).toContain(factoryTitleKey(FACTORY_MAP[id]));
      expect(bar.textContent).toContain(factoryFeedsKey(FACTORY_MAP[id]));
      unmount();
    }
  });

  it("ActivityBar 全条目有分组归属（四工厂分组，16 条目无一丢失）", () => {
    render(<ActivityBar onSelect={() => {}} />);
    // 三工厂组标题=旅程中枢入口 + base 组标题（T1.2）
    expect(screen.getAllByTestId("factory-journey-entry")).toHaveLength(3);

    /** 组容器=nav 的直接子 div（组标题文本向上回溯） */
    function groupOf(titleKey: string): HTMLElement {
      let el: HTMLElement | null = screen.getByText(titleKey);
      while (el && el.parentElement && el.parentElement.tagName !== "NAV") {
        el = el.parentElement;
      }
      expect(el, `分组容器未找到: ${titleKey}`).toBeTruthy();
      return el as HTMLElement;
    }

    const groups: [string, string[]][] = [
      [
        factoryTitleKey("absorb"),
        ["shell.extract", "shell.skills", "shell.skillMatrix"],
      ],
      [
        factoryTitleKey("create"),
        [
          "shell.chapters",
          "shell.chapterManagement",
          "shell.preference",
          "shell.novelSetting",
          "shell.characters",
          "shell.locations",
          "shell.arcs",
          "shell.timeline",
        ],
      ],
      [factoryTitleKey("quality"), ["shell.readerView", "shell.workflow"]],
      [
        factoryTitleKey("base"),
        ["shell.search", "shell.bookshelf", "shell.gitHistory"],
      ],
    ];
    let total = 0;
    for (const [titleKey, labels] of groups) {
      const group = groupOf(titleKey);
      for (const label of labels) {
        expect(within(group).getByText(label)).toBeInTheDocument();
        total += 1;
      }
    }
    expect(total).toBe(16);
  });
});

describe("T5.1 双验收②：新用户按旅程走通吸收→创作→质检", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.GetVoiceLedger.mockResolvedValue([
      { dimension: "voice", present: true },
    ]);
    mocks.GetWritingConfig.mockResolvedValue({
      auto_score_enabled: true,
      score_threshold: 60,
      score_max_retries: 3,
    });
    useNovelStore.setState({ activeNovelId: 0 });
    usePanelStore.setState({ activePanel: "journey-create" });
  });

  it("旅程卡链路逐跳可达：吸收提取卡 → 创作双速写作卡 → 质检体检卡", () => {
    // 吸收：提取卡 → style-samples 提取工作区
    const absorb = render(<JourneyAbsorb />);
    fireEvent.click(screen.getByText("journey.absorb.stages.extract"));
    expect(usePanelStore.getState().activePanel).toBe("style-samples");
    absorb.unmount();

    // 创作：双速两卡各直达原工作面（自由区→chapters，质检区→pipeline）
    const create = render(<JourneyCreate />);
    fireEvent.click(screen.getByText("journey.create.stages.writingFree"));
    expect(usePanelStore.getState().activePanel).toBe("chapters");
    fireEvent.click(screen.getByText("journey.create.stages.writingPipeline"));
    expect(usePanelStore.getState().activePanel).toBe("pipeline");
    create.unmount();

    // 质检：体检卡 → signability 签约工作流
    const quality = render(<JourneyQuality />, { wrapper });
    fireEvent.click(screen.getByText("journey.quality.stages.audit"));
    expect(usePanelStore.getState().activePanel).toBe("signability");
    quality.unmount();
  });
});

describe("T5.1 回归断言：PanelId 零删除、18 面板全可达", () => {
  it("18 既有 PanelId + 3 旅程视图：类型声明与 WorkspaceView 渲染分支全保留", () => {
    const src = findSrcDir();
    const panelTs = readFileSync(resolve(src, "types/panel.ts"), "utf8");
    const workspace = readFileSync(
      resolve(src, "views/WorkspaceView.tsx"),
      "utf8",
    );
    const preP3 = [
      "novels",
      "chapters",
      "chapter-management",
      "characters",
      "locations",
      "storyarcs",
      "timeline",
      "reader",
      "preferences",
      "novel-settings",
      "profile",
      "git",
      "skills",
      "skill-matrix",
      "style-samples",
      "signability",
      "bookpitch",
      "pipeline",
    ];
    const journeys = ["journey-absorb", "journey-create", "journey-quality"];
    expect(preP3).toHaveLength(18);
    for (const id of [...preP3, ...journeys]) {
      expect(panelTs, `PanelId 被删除: ${id}`).toContain(`"${id}"`);
      // chapters/skills 走 ContentPanel 默认分支（panel.ts 口径），其余必须有显式渲染分支
      if (id !== "chapters" && id !== "skills") {
        expect(workspace, `渲染分支缺失: ${id}`).toContain(
          `activePanel === "${id}"`,
        );
      }
    }
    // chapters/skills 经 ContentPanel 默认分支可达（回归）
    expect(workspace).toContain("ContentPanel");
  });
});
