// R5-3 T3.1 验收：流水线=「编排预设」定位文案（写→检测一键串起的便捷壳）——
// ①文案/定位诚实（双合法用法明示、不宣称唯一入口）；②流水线功能零回退（空态仍可启动）；
// ③PanelId 零删除。纯定位/文案任务：子面板全部 stub，只验空态文案与启动链路。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

const mocks = vi.hoisted(() => ({
  start: vi.fn(),
  resume: vi.fn(),
  control: vi.fn(),
  GetPatrolReport: vi.fn(),
  RunDetectionTask: vi.fn(),
  RecordFeatureUsage: vi.fn(),
}));

vi.mock("@/hooks/usePipeline", () => ({
  usePipeline: () => ({
    run: null,
    stage: "",
    status: "",
    stageNote: "",
    gate: null,
    autoRun: false,
    fixProgress: null,
    fixActivity: [],
    patrolTick: 0,
    start: mocks.start,
    resume: mocks.resume,
    control: mocks.control,
  }),
}));

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  GetPatrolReport: mocks.GetPatrolReport,
  RunDetectionTask: mocks.RunDetectionTask,
  RecordFeatureUsage: mocks.RecordFeatureUsage,
}));

// 子面板全部 stub：本任务零 UI 重构，不验子面板内部行为。
vi.mock("./PipelineControlBar", () => ({ default: () => null }));
vi.mock("./PipelineGateCard", () => ({ default: () => null }));
vi.mock("./BlueprintWorkflowView", () => ({ default: () => null }));
vi.mock("./SignabilityAuditView", () => ({
  default: () => null,
  StockAuditPanel: () => null,
}));
vi.mock("./PatrolReportPanel", () => ({ default: () => null }));
vi.mock("./IssueTicketsPanel", () => ({ default: () => null }));
vi.mock("./RetconPanel", () => ({ default: () => null }));
vi.mock("./VolumeManagePanel", () => ({ default: () => null }));

import PipelineWizard from "./PipelineWizard";
import { ALL_SIDEBAR_PANEL_IDS } from "@/lib/factory";

describe("PipelineWizard：T3.1 编排预设定位（R5-3 契约 0.3）", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.GetPatrolReport.mockResolvedValue(null);
  });

  it("空态定位文案：presetHint 明示双合法用法（只跑检测/只跑流水线皆合法）", () => {
    render(<PipelineWizard novelId={1} />);
    expect(screen.getByText("pipeline.empty")).toBeInTheDocument();
    expect(screen.getByText("pipeline.presetHint")).toBeInTheDocument();
  });

  it("流水线功能零回退：空态启动按钮仍一触启动（start('new', null)）", () => {
    render(<PipelineWizard novelId={1} />);
    fireEvent.click(screen.getByRole("button", { name: "pipeline.start" }));
    expect(mocks.start).toHaveBeenCalledWith("new", null);
  });

  it("定位诚实（双语 JSON 断言）：明示两种用法皆合法，不宣称唯一入口", async () => {
    const { default: zh } = await import("@/i18n/locales/zh-CN.json");
    const { default: en } = await import("@/i18n/locales/en.json");
    const zhHint = (zh as { pipeline: { presetHint: string } }).pipeline
      .presetHint;
    const enHint = (en as { pipeline: { presetHint: string } }).pipeline
      .presetHint;
    // 双合法用法：只跑检测不跑流水线 + 只跑流水线不手动跑检测
    expect(zhHint).toContain("只跑检测");
    expect(zhHint).toContain("只跑流水线");
    expect(enHint.toLowerCase()).toContain("detection");
    expect(enHint.toLowerCase()).toContain("pipeline");
    // 不宣称「唯一入口」（诚实红线）
    expect(zhHint).not.toContain("唯一入口");
    expect(enHint.toLowerCase()).not.toContain("only entry");
    // 预设入口表述双语齐（JourneyCreate 流水线卡 descKey）
    const zhDesc = (
      zh as {
        journey: { create: { stages: { writingPipelineDesc: string } } };
      }
    ).journey.create.stages.writingPipelineDesc;
    const enDesc = (
      en as {
        journey: { create: { stages: { writingPipelineDesc: string } } };
      }
    ).journey.create.stages.writingPipelineDesc;
    expect(zhDesc.length).toBeGreaterThan(0);
    expect(enDesc.length).toBeGreaterThan(0);
  });

  it("PanelId 零删除：pipeline 面板仍在全量清单（22 项不缩）", () => {
    expect(ALL_SIDEBAR_PANEL_IDS).toContain("pipeline");
    expect(ALL_SIDEBAR_PANEL_IDS).toHaveLength(22);
  });
});
