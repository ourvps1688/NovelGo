// 空态引导 hint 行为测试（P3 WBS T2.2）：
// ①未提取+重写入口 → hint 出现且一键跳吸收工厂；②已提取 → 零提示；
// ③API 失败 → 静默零提示；④关闭后当次会话不再提示；另加挂点接线断言。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";

const { getVoiceLedger } = vi.hoisted(() => ({ getVoiceLedger: vi.fn() }));
vi.mock("@/lib/wailsjs/go/app/App", () => ({ GetVoiceLedger: getVoiceLedger }));

import VoiceExtractionHint from "./VoiceExtractionHint";
import { resetVoiceExtractionDismissals } from "@/hooks/useVoiceExtractionState";
import { usePanelStore } from "@/stores/usePanelStore";

/** 冲刷异步 effect（GetVoiceLedger promise settle），避免 act 告警。 */
async function flushEffects() {
  await act(async () => {});
}

describe("VoiceExtractionHint（WBS T2.2 空态漏斗）", () => {
  beforeEach(() => {
    getVoiceLedger.mockReset();
    resetVoiceExtractionDismissals();
    usePanelStore.setState({ activePanel: "chapters" });
  });

  it("验收①未提取+重写入口 → hint 出现且一键跳吸收工厂", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: false }]);
    render(<VoiceExtractionHint novelID={1} />);
    expect(await screen.findByTestId("voice-extraction-hint")).toBeTruthy();
    fireEvent.click(screen.getByTestId("voice-extraction-hint-go"));
    expect(usePanelStore.getState().activePanel).toBe("style-samples");
  });

  it("验收④关闭后当次会话不再提示", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: false }]);
    const { unmount } = render(<VoiceExtractionHint novelID={1} />);
    fireEvent.click(await screen.findByTestId("voice-extraction-hint-dismiss"));
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();

    // 同会话再次挂载（另一处重写入口）仍不提示
    unmount();
    render(<VoiceExtractionHint novelID={1} />);
    await flushEffects();
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();
  });

  it("验收②已提取 → 零提示（逐字回到旧行为）", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: true }]);
    render(<VoiceExtractionHint novelID={1} />);
    await flushEffects();
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();
  });

  it("验收③API 失败 → 静默零提示（降级最高纪律）", async () => {
    getVoiceLedger.mockRejectedValue(new Error("boom"));
    render(<VoiceExtractionHint novelID={1} />);
    await flushEffects();
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();
  });

  it("挂点接线断言：ChatPanel 与 JourneyCreate 均渲染本 hint（文件触点）", () => {
    // 挂点以现状为准：P3 原挂 SignabilityAuditView，后随重写入口 hint 语义
    // 调整迁至 ChatPanel（写作入口）+ JourneyCreate（重写卡）；旧断言失步修复。
    for (const rel of [
      "components/chat/ChatPanel.tsx",
      "components/journey/JourneyCreate.tsx",
    ]) {
      const content = readFileSync(resolve(findSrcDir(), rel), "utf-8");
      expect(content, rel).toContain("<VoiceExtractionHint");
    }
  });
});

/** 定位 frontend/src：vitest 下 import.meta.dirname 形态不稳，按候选逐一探测。 */
function findSrcDir(): string {
  const candidates = [
    resolve(process.cwd(), "src"),
    resolve(process.cwd(), "frontend/src"),
    resolve(
      dirname((import.meta as { dirname?: string }).dirname ?? "."),
      "..",
      "..",
    ),
    resolve(
      dirname((import.meta as { dirname?: string }).dirname ?? "."),
      "..",
      "..",
      "..",
    ),
  ];
  for (const dir of candidates) {
    if (existsSync(resolve(dir, "components/chat/ChatPanel.tsx"))) return dir;
  }
  throw new Error(`src dir not found in candidates: ${candidates.join(", ")}`);
}
