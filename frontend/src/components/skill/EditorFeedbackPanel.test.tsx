// 编辑反馈显式录入面板（#111 P2）测试：复用两段式录入、原话完整透传、raw 降级可感知。
import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  act,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { listFeedbacks, recordFeedback, toastSuccess, toastInfo, toastError } =
  vi.hoisted(() => ({
    listFeedbacks: vi.fn(),
    recordFeedback: vi.fn(),
    toastSuccess: vi.fn(),
    toastInfo: vi.fn(),
    toastError: vi.fn(),
  }));

vi.mock("@/lib/wailsjs/go/app/App", () => ({
  ListEditorFeedbacks: listFeedbacks,
  RecordEditorFeedback: recordFeedback,
}));

vi.mock("@/utils/toast", () => ({
  toastSuccess,
  toastInfo,
  toastError,
}));

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (key: string, opts?: { count?: number }) =>
      opts?.count === undefined ? key : `${key}:${opts.count}`,
  }),
}));

import EditorFeedbackPanel from "./EditorFeedbackPanel";

function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(<EditorFeedbackPanel novelId={7} />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  return {
    ...view,
    cleanup: () => {
      view.unmount();
      qc.clear();
    },
  };
}

function fillForm() {
  fireEvent.change(screen.getByLabelText("skillMatrix.feedback.workTitle"), {
    target: { value: "测试书名" },
  });
  fireEvent.change(screen.getByLabelText("skillMatrix.feedback.platform"), {
    target: { value: "七猫" },
  });
  fireEvent.change(screen.getByLabelText("skillMatrix.feedback.channel"), {
    target: { value: "男频悬疑" },
  });
  fireEvent.change(screen.getByLabelText("skillMatrix.feedback.verdict"), {
    target: { value: "开篇太慢，主角动机不清。" },
  });
}

beforeEach(() => {
  listFeedbacks.mockReset();
  recordFeedback.mockReset();
  toastSuccess.mockReset();
  toastInfo.mockReset();
  toastError.mockReset();
  listFeedbacks.mockResolvedValue([
    {
      id: 3,
      novel_id: 7,
      work_title: "旧作",
      platform: "七猫",
      channel: "",
      verdict: "节奏慢",
      parse_status: "parsed",
      summary: "开篇节奏偏慢",
      issue_count: 2,
      ticket_run_id: "run-3",
      created_at: "2026-11-09 21:00",
    },
    {
      id: 4,
      novel_id: 7,
      work_title: "新作",
      platform: "起点",
      channel: "",
      verdict: "人物脸谱化",
      parse_status: "raw",
      summary: "",
      issue_count: 0,
      ticket_run_id: "",
      created_at: "2026-11-10 01:00",
    },
  ]);
  recordFeedback.mockResolvedValue({
    feedback_id: 5,
    parse_status: "parsed",
    issue_count: 1,
    constraint_count: 0,
    tickets_landed: 1,
  });
});

describe("编辑反馈录入面板（#111 P2）", () => {
  it("列出历史反馈并区分已拆解 / 仅原话", async () => {
    const { cleanup } = renderPanel();
    expect(await screen.findByTestId("editor-feedback-panel")).toBeTruthy();
    expect(listFeedbacks).toHaveBeenCalledWith(7);
    await waitFor(() =>
      expect(screen.getAllByTestId("feedback-row")).toHaveLength(2),
    );
    const statuses = screen.getAllByTestId("feedback-status");
    expect(statuses[0].textContent).toBe("skillMatrix.feedback.parsed");
    expect(statuses[1].textContent).toBe("skillMatrix.feedback.raw");
    cleanup();
  });

  it("完整透传编辑原话与作品信息，提交后刷新列表", async () => {
    const { cleanup } = renderPanel();
    expect(await screen.findByTestId("editor-feedback-panel")).toBeTruthy();
    fillForm();
    await act(async () => {
      fireEvent.click(screen.getByTestId("feedback-submit"));
    });
    await waitFor(() => expect(recordFeedback).toHaveBeenCalledTimes(1));
    expect(recordFeedback).toHaveBeenCalledWith({
      novel_id: 7,
      work_title: "测试书名",
      platform: "七猫",
      channel: "男频悬疑",
      verdict: "开篇太慢，主角动机不清。",
    });
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        "skillMatrix.feedback.savedToast",
      ),
    );
    await waitFor(() =>
      expect(listFeedbacks.mock.calls.length).toBeGreaterThan(1),
    );
    cleanup();
  });

  it("拆解不可用只留原话时给出明确提示，不谎报成功", async () => {
    recordFeedback.mockResolvedValue({
      feedback_id: 6,
      parse_status: "raw",
      degraded: "子 Agent 能力不可用",
      issue_count: 0,
      constraint_count: 0,
      tickets_landed: 0,
    });
    const { cleanup } = renderPanel();
    expect(await screen.findByTestId("editor-feedback-panel")).toBeTruthy();
    fillForm();
    await act(async () => {
      fireEvent.click(screen.getByTestId("feedback-submit"));
    });
    await waitFor(() =>
      expect(toastInfo).toHaveBeenCalledWith("skillMatrix.feedback.rawToast"),
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    cleanup();
  });
});
