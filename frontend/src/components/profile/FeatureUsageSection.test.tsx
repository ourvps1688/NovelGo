// 功能使用率消费面测试（P4 WBS T2.3 验收）：
// ①区块渲染近 30 天计数；②无数据=静默空态零报错；
// ③加载失败=静默不渲染零报错；④静默口径只对 feature-usage 生效（对照）。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, act } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";

const { getFeatureUsage, toastError } = vi.hoisted(() => ({
  getFeatureUsage: vi.fn(),
  toastError: vi.fn(),
}));
vi.mock("@/lib/wailsjs/go/app/App", () => ({
  GetFeatureUsage: getFeatureUsage,
}));
vi.mock("@/utils/toast", () => ({
  toastError,
  toastSuccess: vi.fn(),
}));

import FeatureUsageSection from "./FeatureUsageSection";
import { installQueryErrorToast } from "@/lib/queryErrorToast";

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const unsub = installQueryErrorToast(qc);
  const result = render(ui, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  return {
    ...result,
    cleanup: () => {
      unsub();
      qc.clear();
    },
  };
}

/** 冲刷异步 query settle，避免 act 告警。 */
async function flushEffects() {
  await act(async () => {});
}

beforeEach(() => {
  getFeatureUsage.mockReset();
  toastError.mockReset();
});

describe("功能使用率区块（P4 T2.3）", () => {
  it("验收①有度量才显示区块：近 30 天计数渲染，GetFeatureUsage(30) 驱动", async () => {
    getFeatureUsage.mockResolvedValue([
      { feature_key: "panel.chapters", count: 12 },
      { feature_key: "action.freeze", count: 3 },
      { feature_key: "journey.quality.stages.audit", count: 1 },
    ]);
    const { cleanup } = renderWithClient(<FeatureUsageSection />);
    expect(await screen.findByTestId("feature-usage-section")).toBeTruthy();
    expect(getFeatureUsage).toHaveBeenCalledWith(30);
    const rows = screen.getAllByTestId("feature-usage-row");
    expect(rows).toHaveLength(3);
    expect(rows.map((r) => r.getAttribute("data-count"))).toEqual([
      "12",
      "3",
      "1",
    ]);
    expect(toastError).not.toHaveBeenCalled();
    cleanup();
  });

  it("top 10 克制呈现：超过 10 行只显示前 10（不做运营大屏）", async () => {
    getFeatureUsage.mockResolvedValue(
      Array.from({ length: 15 }, (_, i) => ({
        feature_key: `panel.p${i}`,
        count: 100 - i,
      })),
    );
    const { cleanup } = renderWithClient(<FeatureUsageSection />);
    await screen.findByTestId("feature-usage-section");
    expect(screen.getAllByTestId("feature-usage-row")).toHaveLength(10);
    cleanup();
  });

  it("验收②无数据=静默不渲染（降级逐字一致，零报错）", async () => {
    getFeatureUsage.mockResolvedValue([]);
    const { cleanup } = renderWithClient(<FeatureUsageSection />);
    await flushEffects();
    expect(screen.queryByTestId("feature-usage-section")).toBeNull();
    expect(toastError).not.toHaveBeenCalled();
    cleanup();
  });

  it("验收③加载失败=静默不渲染（零弹窗零报错，业务零感知）", async () => {
    getFeatureUsage.mockRejectedValue(new Error("bridge down"));
    const { cleanup } = renderWithClient(<FeatureUsageSection />);
    await flushEffects();
    expect(screen.queryByTestId("feature-usage-section")).toBeNull();
    expect(toastError).not.toHaveBeenCalled();
    cleanup();
  });

  it("验收④静默口径只对 feature-usage 生效：其他前缀错误仍 toast（对照）", async () => {
    function OtherQuery() {
      useQuery({
        queryKey: ["writing-stats"],
        queryFn: () => Promise.reject(new Error("boom")),
      });
      return null;
    }
    const { cleanup } = renderWithClient(<OtherQuery />);
    await waitFor(() => expect(toastError).toHaveBeenCalled());
    cleanup();
  });
});
