// 子 Agent 长跑可见性测试（P4 WBS T1.3 / §0.3 验收）：
// ①长跑卡显示时长随时间走动、有事件流入时「最近活动」归零刷新；
// ②完成/失败态停表定格；③两卡并存互不串扰；
// ④无事件静默 ≥N 秒仅显示心跳数值，零弹窗零报错（降级纪律）。
// 另：无时间戳降级不渲染时钟、interval 随卡卸载清理（防泄漏）、接线断言。
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";

import SubagentCard, { formatElapsed } from "./SubagentCard";

const T0 = new Date("2026-02-10T12:00:00Z").getTime();

function installFakeClock() {
  vi.useFakeTimers({
    now: T0,
    toFake: [
      "setTimeout",
      "clearTimeout",
      "setInterval",
      "clearInterval",
      "Date",
    ],
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("formatElapsed", () => {
  it("mm:ss / h:mm:ss 纯数值格式化", () => {
    expect(formatElapsed(0)).toBe("00:00");
    expect(formatElapsed(65)).toBe("01:05");
    expect(formatElapsed(3723)).toBe("1:02:03");
    expect(formatElapsed(-5)).toBe("00:00");
  });
});

describe("SubagentCard 长跑可见性（P4 T1.3）", () => {
  it("验收①时长随时间走动，事件流入时「最近活动」归零刷新", () => {
    installFakeClock();
    const startedAtMs = T0 - 65_000;
    const { rerender } = render(
      <SubagentCard
        agentType="memory"
        segments={[]}
        status="streaming"
        startedAtMs={startedAtMs}
        lastActivityAtMs={T0 - 5_000}
      />,
    );
    expect(screen.getByTestId("subagent-elapsed").textContent).toBe("01:05");
    expect(
      screen.getByTestId("subagent-idle").getAttribute("data-seconds"),
    ).toBe("5");

    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    expect(screen.getByTestId("subagent-elapsed").textContent).toBe("01:15");
    expect(
      screen.getByTestId("subagent-idle").getAttribute("data-seconds"),
    ).toBe("15");

    // 事件流入（lastActivityAtMs 刷新）→ 心跳归零
    rerender(
      <SubagentCard
        agentType="memory"
        segments={[]}
        status="streaming"
        startedAtMs={startedAtMs}
        lastActivityAtMs={T0 + 10_000}
      />,
    );
    expect(
      screen.getByTestId("subagent-idle").getAttribute("data-seconds"),
    ).toBe("0");
    expect(screen.getByTestId("subagent-elapsed").textContent).toBe("01:15");
  });

  it("验收②完成/失败态停表定格（终点=最后活动时刻，不再走动）", () => {
    for (const status of ["done", "failed"] as const) {
      installFakeClock();
      const { unmount } = render(
        <SubagentCard
          agentType="review"
          segments={[]}
          status={status}
          startedAtMs={T0 - 65_000}
          lastActivityAtMs={T0 - 5_000}
        />,
      );
      // 停表值 = lastActivityAtMs - startedAtMs = 60s
      expect(screen.getByTestId("subagent-elapsed").textContent).toBe("01:00");
      // 完成/失败态不显示心跳（只读定格）
      expect(screen.queryByTestId("subagent-idle")).toBeNull();
      act(() => {
        vi.advanceTimersByTime(120_000);
      });
      expect(screen.getByTestId("subagent-elapsed").textContent).toBe("01:00");
      unmount();
      vi.useRealTimers();
    }
  });

  it("验收③两卡并存互不串扰（按 sub_task 独立计时）", () => {
    installFakeClock();
    render(
      <>
        <SubagentCard
          agentType="memory"
          segments={[]}
          status="streaming"
          startedAtMs={T0 - 10_000}
          lastActivityAtMs={T0 - 10_000}
        />
        <SubagentCard
          agentType="review"
          segments={[]}
          status="streaming"
          startedAtMs={T0 - 65_000}
          lastActivityAtMs={T0 - 65_000}
        />
      </>,
    );
    const [a, b] = screen.getAllByTestId("subagent-elapsed");
    expect(a.textContent).toBe("00:10");
    expect(b.textContent).toBe("01:05");
    act(() => {
      vi.advanceTimersByTime(5_000);
    });
    expect(a.textContent).toBe("00:15");
    expect(b.textContent).toBe("01:10");
  });

  it("验收④静默 ≥N 秒仅显示心跳数值，零弹窗零报错", () => {
    installFakeClock();
    const { container } = render(
      <SubagentCard
        agentType="memory"
        segments={[]}
        status="streaming"
        startedAtMs={T0 - 180_000}
        lastActivityAtMs={T0 - 120_000}
      />,
    );
    expect(
      screen.getByTestId("subagent-idle").getAttribute("data-seconds"),
    ).toBe("120");
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    expect(
      screen.getByTestId("subagent-idle").getAttribute("data-seconds"),
    ).toBe("180");
    // 只显示数值：无错误块、无弹窗（只读可见性，绝不骚扰）
    expect(container.querySelector(".tool-error")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("降级：无时间戳 → 时钟整块不渲染（与旧行为逐字一致）", () => {
    installFakeClock();
    render(
      <SubagentCard agentType="memory" segments={[]} status="streaming" />,
    );
    expect(screen.queryByTestId("subagent-elapsed")).toBeNull();
    expect(screen.queryByTestId("subagent-idle")).toBeNull();
  });

  it("interval 随卡卸载清理（风险条目：心跳刷新泄漏）", () => {
    installFakeClock();
    const { unmount } = render(
      <SubagentCard
        agentType="memory"
        segments={[]}
        status="streaming"
        startedAtMs={T0}
        lastActivityAtMs={T0}
      />,
    );
    expect(vi.getTimerCount()).toBeGreaterThan(0);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("接线断言：ChatPanel 透传心跳时间字段并应用 withEventTime（T1.3 文件触点）", () => {
    const content = readFileSync(
      resolve(findSrcDir(), "components/chat/ChatPanel.tsx"),
      "utf-8",
    );
    expect(content).toContain("withEventTime");
    expect(content).toContain("startedAtMs={seg.startedAtMs}");
    expect(content).toContain("lastActivityAtMs={seg.lastActivityAtMs}");
  });
});

/** 定位 frontend/src：vitest 下 import.meta.dirname 形态不稳，按候选逐一探测。 */
function findSrcDir(): string {
  const candidates = [
    resolve(process.cwd(), "src"),
    resolve(process.cwd(), "frontend/src"),
    resolve(
      dirname((import.meta as { dirname?: string }).dirname ?? "."),
      "../src",
    ),
    resolve(
      dirname((import.meta as { dirname?: string }).dirname ?? "."),
      "src",
    ),
  ];
  for (const dir of candidates) {
    if (existsSync(resolve(dir, "components/chat/ChatPanel.tsx"))) return dir;
  }
  throw new Error("cannot locate frontend/src");
}
