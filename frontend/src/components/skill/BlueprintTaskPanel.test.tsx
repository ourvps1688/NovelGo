// S2 显式蓝图入口（#111 P2）测试：必须用户点击才派发，重复点击合并、失败可感知。
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

const { runBlueprintTask, toastSuccess, toastInfo, toastError } = vi.hoisted(
  () => ({
    runBlueprintTask: vi.fn(),
    toastSuccess: vi.fn(),
    toastInfo: vi.fn(),
    toastError: vi.fn(),
  }),
);

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  RunBlueprintTask: runBlueprintTask,
}));

vi.mock("@/utils/toast", () => ({
  toastSuccess,
  toastInfo,
  toastError,
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { id?: string }) =>
      opts?.id === undefined ? key : `${key}:${opts.id}`,
  }),
}));

import BlueprintTaskPanel from "./BlueprintTaskPanel";

beforeEach(() => {
  runBlueprintTask.mockReset();
  toastSuccess.mockReset();
  toastInfo.mockReset();
  toastError.mockReset();
});

describe("S2 显式蓝图任务入口（#111 P2）", () => {
  it("不点击就绝不派发；点击后以 novelId 启动一次并展示 runId", async () => {
    runBlueprintTask.mockResolvedValue({
      run_id: "abc12345-6789",
      novel_id: 7,
      dispatched: true,
    });
    render(<BlueprintTaskPanel novelId={7} />);
    expect(runBlueprintTask).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("blueprint-start"));
    await waitFor(() => expect(runBlueprintTask).toHaveBeenCalledWith(7));
    expect(await screen.findByTestId("blueprint-run-id")).toBeTruthy();
    expect(screen.getByTestId("blueprint-run-id").textContent).toContain(
      "abc12345",
    );
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        "skillMatrix.blueprint.startedToast",
      ),
    );
  });

  it("同书任务已在跑时幂等合并，不重复启动", async () => {
    runBlueprintTask.mockResolvedValue({
      run_id: "existing-run",
      novel_id: 7,
      dispatched: false,
    });
    render(<BlueprintTaskPanel novelId={7} />);
    fireEvent.click(screen.getByTestId("blueprint-start"));
    await waitFor(() =>
      expect(toastInfo).toHaveBeenCalledWith(
        "skillMatrix.blueprint.alreadyRunning",
      ),
    );
    expect(toastSuccess).not.toHaveBeenCalled();
  });

  it("启动失败回显错误，按钮恢复可用", async () => {
    runBlueprintTask.mockRejectedValue(new Error("bridge down"));
    render(<BlueprintTaskPanel novelId={7} />);
    fireEvent.click(screen.getByTestId("blueprint-start"));
    await waitFor(() => expect(toastError).toHaveBeenCalled());
    expect(
      (screen.getByTestId("blueprint-start") as HTMLButtonElement).disabled,
    ).toBe(false);
  });
});
