// 功能使用度量埋点测试（P4 WBS T2.2 验收）：
// ①埋点触发后计数可查（key→RecordFeatureUsage 闭环）；
// ②统计失败业务零感知（不抛错零提示）；
// ③grep 断言无网络上报路径（纯本地，隐私红线）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  RecordFeatureUsage: vi.fn(() => Promise.resolve()),
}));

import { RecordFeatureUsage } from "@/lib/wailsjs/go/app/App";
import {
  panelFeatureKey,
  journeyFeatureKey,
  ACTION_FEATURE_KEYS,
  GATE_DECISION_FEATURE_KEYS,
  trackFeature,
} from "./featureKeys";
import JourneyCard from "@/components/journey/JourneyCard";

const recordMock = vi.mocked(RecordFeatureUsage);

function findSrcDir(): string {
  let dir = dirname(
    new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"),
  );
  for (let i = 0; i < 8; i++) {
    if (existsSync(resolve(dir, "src", "lib", "featureKeys.ts"))) return dir;
    dir = resolve(dir, "..");
  }
  throw new Error("src dir not found");
}

beforeEach(() => {
  recordMock.mockClear();
});

describe("featureKeys 单源 key 枚举（P4 T2.2）", () => {
  it("面板 key 按 PanelId 派生", () => {
    expect(panelFeatureKey("chapters")).toBe("panel.chapters");
    expect(panelFeatureKey("journey-quality")).toBe("panel.journey-quality");
  });

  it("旅程卡 key 天然 journey.* 命名空间", () => {
    expect(journeyFeatureKey("journey.quality.stages.audit")).toBe(
      "journey.quality.stages.audit",
    );
    expect(journeyFeatureKey("stages.custom")).toBe("journey.stages.custom");
  });

  it("关键动作枚举全覆盖（七类动作 + 检测启动 + 告警命中率族），均为 action.* 前缀", () => {
    expect(Object.values(ACTION_FEATURE_KEYS).sort()).toEqual(
      [
        "action.audit",
        "action.extract",
        "action.fix",
        "action.freeze",
        "action.patrol",
        "action.rewrite",
        "action.retcon",
        // R5-3 T2.1：检测任务手动启动
        "action.detect",
        // §9.4 告警→真问题命中率族（后端挂点记数，key 单源锁于此）
        "action.alert_raised.l2",
        "action.alert_raised.l1",
        "action.alert_raised.struct",
        "action.alert_confirmed.l2",
        "action.alert_dismissed.l2",
      ].sort(),
    );
  });

  it("闸门决策映射：fix/audit_now/finish_book/modify 各归其位，流程控制不计入", () => {
    expect(GATE_DECISION_FEATURE_KEYS.fix).toBe(ACTION_FEATURE_KEYS.fix);
    expect(GATE_DECISION_FEATURE_KEYS.audit_now).toBe(
      ACTION_FEATURE_KEYS.audit,
    );
    expect(GATE_DECISION_FEATURE_KEYS.finish_book).toBe(
      ACTION_FEATURE_KEYS.audit,
    );
    expect(GATE_DECISION_FEATURE_KEYS.modify).toBe(ACTION_FEATURE_KEYS.rewrite);
    for (const control of ["continue", "finish", "abort", "set_auto"]) {
      expect(GATE_DECISION_FEATURE_KEYS[control]).toBeUndefined();
    }
  });
});

describe("trackFeature 尽力记录与静默降级（P4 T2.2）", () => {
  it("埋点触发后计数可查：key 传给 RecordFeatureUsage", () => {
    trackFeature(ACTION_FEATURE_KEYS.freeze);
    expect(recordMock).toHaveBeenCalledTimes(1);
    expect(recordMock).toHaveBeenCalledWith("action.freeze");
  });

  it("空 key 不下发（宁缺勿错，不写脏数据）", () => {
    trackFeature("");
    expect(recordMock).not.toHaveBeenCalled();
  });

  it("统计失败业务零感知：同步抛错静默、异步拒绝静默", () => {
    recordMock.mockImplementationOnce(() => {
      throw new Error("bridge down");
    });
    expect(() => trackFeature("action.audit")).not.toThrow();
    recordMock.mockImplementationOnce(() =>
      Promise.reject(new Error("bridge down")),
    );
    expect(() => trackFeature("action.fix")).not.toThrow();
    expect(recordMock).toHaveBeenCalledTimes(2);
  });
});

describe("埋点挂点闭环（P4 T2.2）", () => {
  it("旅程卡点击：journey key 埋点后仍直达目标面板（业务零感知）", () => {
    const onClick = vi.fn();
    render(
      <JourneyCard
        titleKey="journey.quality.stages.freeze"
        onClick={onClick}
      />,
    );
    fireEvent.click(screen.getByTestId("journey-card"));
    expect(recordMock).toHaveBeenCalledWith("journey.quality.stages.freeze");
    expect(onClick).toHaveBeenCalledTimes(1);
  });
});

describe("隐私红线：度量链路无网络上报（P4 T2.2 验收③）", () => {
  it("featureKeys.ts 源码 grep 断言：无 fetch/上报/遥测端点", () => {
    const src = readFileSync(
      resolve(findSrcDir(), "src", "lib", "featureKeys.ts"),
      "utf8",
    );
    for (const forbidden of [
      "fetch(",
      "XMLHttpRequest",
      "sendBeacon",
      "axios",
      "WebSocket",
      "navigator.",
    ]) {
      expect(src).not.toContain(forbidden);
    }
  });
});
