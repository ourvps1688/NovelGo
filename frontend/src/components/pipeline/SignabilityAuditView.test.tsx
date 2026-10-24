// rewrite_handoff 空态挂点集成测试（P3 WBS T2.2 挂点②）：
// 未提取心智 + 重写交接口 → hint 出现；已提取 → 零提示（逐字回到旧行为）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { FixProgressState } from "@/types/pipeline";

const { getVoiceLedger } = vi.hoisted(() => ({ getVoiceLedger: vi.fn() }));
vi.mock("@/lib/wailsjs/go/app/App", () => ({ GetVoiceLedger: getVoiceLedger }));

import SignabilityAuditView from "./SignabilityAuditView";
import { resetVoiceExtractionDismissals } from "@/hooks/useVoiceExtractionState";
import { useNovelStore } from "@/components/novel/useNovelStore";

const REWRITE_HANDOFF_FIX: FixProgressState = {
  running: false,
  ok: null,
  issues: [],
  allIssues: [],
  batchIndex: 0,
  batchTotal: 0,
  rewriteHandoff: true,
  rewriteCount: 2,
};

describe("SignabilityAuditView rewrite_handoff 空态挂点（WBS T2.2）", () => {
  beforeEach(() => {
    getVoiceLedger.mockReset();
    resetVoiceExtractionDismissals();
    // hint 自读 activeNovelId（挂点无参），测试先立书
    useNovelStore.setState({ activeNovelId: 1 });
  });

  it("未提取 + rewrite_handoff 提示处 → hint 出现", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: false }]);
    render(
      <SignabilityAuditView
        run={null}
        stage="S5"
        status="paused"
        gate={null}
        onResume={() => {}}
        fixProgress={REWRITE_HANDOFF_FIX}
      />,
    );
    expect(await screen.findByTestId("voice-extraction-hint")).toBeTruthy();
  });

  it("已提取 → 零提示（逐字回到旧行为）", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: true }]);
    render(
      <SignabilityAuditView
        run={null}
        stage="S5"
        status="paused"
        gate={null}
        onResume={() => {}}
        fixProgress={REWRITE_HANDOFF_FIX}
      />,
    );
    // 重写交接口本身仍渲染，只有空态 hint 不出现
    expect(
      await screen.findByText("pipeline.fixProgress.rewriteHandoff"),
    ).toBeTruthy();
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();
  });

  it("非 rewrite_handoff → hint 不挂载（只在重写入口提示）", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: false }]);
    render(
      <SignabilityAuditView
        run={null}
        stage="S5"
        status="paused"
        gate={null}
        onResume={() => {}}
        fixProgress={{ ...REWRITE_HANDOFF_FIX, rewriteHandoff: false }}
      />,
    );
    await screen.findByText("pipeline.fixProgress.idle");
    expect(screen.queryByTestId("voice-extraction-hint")).toBeNull();
  });
});
