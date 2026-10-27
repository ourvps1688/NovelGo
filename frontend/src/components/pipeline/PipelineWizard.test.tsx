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
  // R5-4 验收 A：可变 usePipeline 状态（gate 呈现态常驻控件并存断言用）。
  pipelineState: {
    run: null as object | null,
    stage: "",
    status: "",
    stageNote: "",
    gate: null as object | null,
    autoRun: false,
    fixProgress: null,
    fixActivity: [] as string[],
    patrolTick: 0,
  },
}));

vi.mock("@/hooks/usePipeline", () => ({
  usePipeline: () => ({
    ...mocks.pipelineState,
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
vi.mock("./PipelineGateCard", () => ({
  default: () => <div data-testid="gate-card-stub" />,
}));
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
    mocks.pipelineState.run = null;
    mocks.pipelineState.stage = "";
    mocks.pipelineState.status = "";
    mocks.pipelineState.stageNote = "";
    mocks.pipelineState.gate = null;
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

  it("R5-4 验收 A：gate 呈现态常驻控件不遮蔽（S3 写作/复检/完本入口可点，不卡流程）", () => {
    mocks.pipelineState.run = { run_id: "r1", novel_id: 1 };
    mocks.pipelineState.stage = "S3";
    mocks.pipelineState.status = "awaiting_gate";
    mocks.pipelineState.gate = {
      run_id: "r1",
      novel_id: 1,
      stage: "S4",
      gate_id: "G4",
      summary: "检测完毕",
      options: ["continue", "fix", "abort"],
    };
    render(<PipelineWizard novelId={1} />);
    // 检测汇总卡与常驻控件并存（不再被 gate 遮蔽）
    expect(screen.getByTestId("gate-card-stub")).toBeInTheDocument();
    // 写作节奏入口
    fireEvent.click(
      screen.getByRole("button", { name: "pipeline.checkProgress" }),
    );
    expect(mocks.resume).toHaveBeenCalledWith("continue");
    // 复检入口（显式 audit_now，人决定推进）
    fireEvent.click(screen.getByRole("button", { name: "pipeline.reaudit" }));
    expect(mocks.resume).toHaveBeenCalledWith("audit_now");
    // 完本终检入口
    fireEvent.click(
      screen.getByRole("button", { name: "pipeline.gate.finishBook" }),
    );
    expect(mocks.resume).toHaveBeenCalledWith("finish_book");
  });

  it("R5-4 验收 A：gate 呈现态 S1「继续」不遮蔽（蓝图停点不卡流程）", () => {
    mocks.pipelineState.run = { run_id: "r1", novel_id: 1 };
    mocks.pipelineState.stage = "S1";
    mocks.pipelineState.status = "awaiting_gate";
    mocks.pipelineState.gate = {
      run_id: "r1",
      novel_id: 1,
      stage: "S1",
      gate_id: "G1",
      summary: "蓝图体检通过",
      options: ["continue", "modify", "abort"],
    };
    render(<PipelineWizard novelId={1} />);
    fireEvent.click(screen.getByRole("button", { name: "pipeline.continue" }));
    expect(mocks.resume).toHaveBeenCalledWith("continue");
  });

  it("R5-4 T2.2 诚实红线（双语 JSON 断言）：零逼答话术、检测汇总话术统一、完本保留人工确认", async () => {
    const { default: zh } = await import("@/i18n/locales/zh-CN.json");
    const { default: en } = await import("@/i18n/locales/en.json");
    const zhGate = (
      zh as {
        pipeline: {
          gate: Record<string, string>;
          hint: { s3: string };
          reaudit: string;
          reauditHint: string;
        };
      }
    ).pipeline;
    const enGate = (
      en as {
        pipeline: {
          gate: Record<string, string>;
          hint: { s3: string };
          reaudit: string;
          reauditHint: string;
        };
      }
    ).pipeline;
    // 零逼答：G3b 已退役，不再有「需要你的确认」硬卡标题（死键已删）
    expect(zhGate.gate.title).toBeUndefined();
    expect(enGate.gate.title).toBeUndefined();
    expect(JSON.stringify(zhGate.gate)).not.toContain("请确认");
    expect(JSON.stringify(enGate.gate).toLowerCase()).not.toContain(
      "your confirmation",
    );
    // 零「必须」话术（诚实红线，范围=R5-4 改造面：gate/hint/reaudit；
    // retcon 边界语义「必须走显式审批」属硬约束说明，不在改造面）
    const zhSurface =
      JSON.stringify(zhGate.gate) +
      zhGate.hint.s3 +
      zhGate.reaudit +
      zhGate.reauditHint;
    expect(zhSurface).not.toContain("必须");
    expect(zhSurface).not.toContain("请确认");
    // 检测汇总话术统一（§10.2）：「检测完毕，发现 N 项」+「由你决定」
    expect(zhGate.gate.foundCount).toContain("检测完毕，发现");
    expect(zhGate.gate.suggestedActions).toContain("由你决定");
    expect(enGate.gate.foundCount.toLowerCase()).toContain(
      "detection finished",
    );
    expect(enGate.gate.suggestedActions.toLowerCase()).toContain("you decide");
    // G3b 过期话术退役：hint.s3 零「里程碑闸」，改非阻塞复检建议
    expect(zhGate.hint.s3).not.toContain("里程碑闸");
    expect(zhGate.hint.s3).toContain("复检");
    expect(enGate.hint.s3.toLowerCase()).not.toContain("milestone gate");
    // 体检/复检常驻入口话术双语齐
    expect(zhGate.reaudit.length).toBeGreaterThan(0);
    expect(zhGate.reauditHint).toContain("由你决定");
    expect(enGate.reaudit.length).toBeGreaterThan(0);
    // 完本保留人工确认语气（契约例外）
    expect(zhGate.gate.finish).toContain("确认");
    expect(enGate.gate.finish.toLowerCase()).toContain("confirm");
  });

  it("PanelId 零删除：pipeline 面板仍在全量清单（22 项不缩）", () => {
    expect(ALL_SIDEBAR_PANEL_IDS).toContain("pipeline");
    expect(ALL_SIDEBAR_PANEL_IDS).toHaveLength(22);
  });
});
