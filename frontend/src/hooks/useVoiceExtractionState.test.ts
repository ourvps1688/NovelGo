// 空态判定 hook 契约测试（P3 WBS T2.2/§0.3）。
// 验收：①未提取判定 ②已提取=false ③API 失败静默 false（降级最高纪律）④会话级消隐。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

const { getVoiceLedger } = vi.hoisted(() => ({ getVoiceLedger: vi.fn() }));
vi.mock("@/lib/wailsjs/go/app/App", () => ({ GetVoiceLedger: getVoiceLedger }));

import {
  useVoiceExtractionState,
  resetVoiceExtractionDismissals,
} from "./useVoiceExtractionState";

describe("useVoiceExtractionState（WBS T2.2 判定源）", () => {
  beforeEach(() => {
    getVoiceLedger.mockReset();
    resetVoiceExtractionDismissals();
  });

  it("十维 Present 全 false → 确证未提取 true", async () => {
    getVoiceLedger.mockResolvedValue(
      Array.from({ length: 10 }, (_, i) => ({
        dimension: `dim-${i}`,
        present: false,
      })),
    );
    const { result } = renderHook(() => useVoiceExtractionState(1));
    await waitFor(() => expect(result.current.unextracted).toBe(true));
  });

  it("空台账 → 确证未提取 true", async () => {
    getVoiceLedger.mockResolvedValue([]);
    const { result } = renderHook(() => useVoiceExtractionState(1));
    await waitFor(() => expect(result.current.unextracted).toBe(true));
  });

  it("任一维度 Present=true（已提取）→ false（零提示，逐字旧行为）", async () => {
    getVoiceLedger.mockResolvedValue([
      { dimension: "dim-0", present: false },
      { dimension: "dim-1", present: true },
    ]);
    const { result } = renderHook(() => useVoiceExtractionState(1));
    await waitFor(() => expect(result.current.unextracted).toBe(false));
  });

  it("API 失败 → 静默 false（降级最高纪律：宁缺勿错）", async () => {
    getVoiceLedger.mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useVoiceExtractionState(1));
    await waitFor(() => expect(getVoiceLedger).toHaveBeenCalledWith(1));
    expect(result.current.unextracted).toBe(false);
  });

  it("绑定返回非数组坏数据 → 静默 false（宁缺勿错，不误报未提取）", async () => {
    getVoiceLedger.mockResolvedValue(undefined);
    const { result } = renderHook(() => useVoiceExtractionState(1));
    await waitFor(() => expect(getVoiceLedger).toHaveBeenCalledWith(1));
    expect(result.current.unextracted).toBe(false);
  });

  it("绑定同步抛错（绑定缺失等）→ 静默 false 不崩溃", async () => {
    getVoiceLedger.mockImplementation(() => {
      throw new TypeError("GetVoiceLedger is not a function");
    });
    const { result } = renderHook(() => useVoiceExtractionState(1));
    await waitFor(() => expect(getVoiceLedger).toHaveBeenCalledWith(1));
    expect(result.current.unextracted).toBe(false);
  });

  it("无 novelID → 不查询且 false", () => {
    const { result } = renderHook(() => useVoiceExtractionState(0));
    expect(result.current.unextracted).toBe(false);
    expect(getVoiceLedger).not.toHaveBeenCalled();
  });

  it("novelID 切换即失效重查（缓存按 novelID 切换失效）", async () => {
    getVoiceLedger.mockImplementation((id: number) =>
      Promise.resolve(
        id === 1
          ? [{ dimension: "dim-0", present: false }]
          : [{ dimension: "dim-0", present: true }],
      ),
    );
    const { result, rerender } = renderHook(
      ({ id }: { id: number }) => useVoiceExtractionState(id),
      { initialProps: { id: 1 } },
    );
    await waitFor(() => expect(result.current.unextracted).toBe(true));
    rerender({ id: 2 });
    await waitFor(() => expect(result.current.unextracted).toBe(false));
    expect(getVoiceLedger).toHaveBeenCalledWith(2);
  });

  it("dismiss 会话级消隐：同会话重挂仍消隐，reset 后恢复", async () => {
    getVoiceLedger.mockResolvedValue([{ dimension: "dim-0", present: false }]);
    const { result } = renderHook(() => useVoiceExtractionState(3));
    await waitFor(() => expect(result.current.unextracted).toBe(true));
    expect(result.current.dismissed).toBe(false);

    act(() => result.current.dismiss());
    expect(result.current.dismissed).toBe(true);

    // 同会话（module 级状态）重新挂载仍消隐
    const second = renderHook(() => useVoiceExtractionState(3));
    expect(second.result.current.dismissed).toBe(true);

    // 会话结束（reset）后可再提示
    act(() => resetVoiceExtractionDismissals());
    const third = renderHook(() => useVoiceExtractionState(3));
    expect(third.result.current.dismissed).toBe(false);
  });
});
