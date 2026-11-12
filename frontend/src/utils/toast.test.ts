import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock sonner before importing toastError
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
    info: vi.fn(),
    warning: vi.fn(),
  },
}));

import { toast } from "sonner";
import { toastError, toastSuccess } from "./toast";

describe("toastError", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls toast.error with the message", () => {
    toastError("something went wrong");
    expect(toast.error).toHaveBeenCalledWith(
      "something went wrong",
      expect.objectContaining({}),
    );
  });

  it("includes a copy action button", () => {
    toastError("error message");
    expect(toast.error).toHaveBeenCalledWith(
      "error message",
      expect.objectContaining({
        action: expect.objectContaining({
          label: "复制",
          onClick: expect.any(Function),
        }),
      }),
    );
  });

  it("uses CSS variables for button style (theme-compatible)", () => {
    toastError("error");
    expect(toast.error).toHaveBeenCalledWith(
      "error",
      expect.objectContaining({
        actionButtonStyle: expect.objectContaining({
          backgroundColor: "var(--primary)",
          color: "var(--primary-foreground)",
        }),
      }),
    );
  });

  it("copy action writes message to clipboard", async () => {
    const writeText = vi.fn();
    Object.assign(navigator, {
      clipboard: { writeText },
    });

    toastError("clipboard test");
    const call = (toast.error as ReturnType<typeof vi.fn>).mock.calls[0];
    const action = call[1].action;
    action.onClick();
    expect(writeText).toHaveBeenCalledWith("clipboard test");
  });

  it("#115 P0 同一 id 让完成反馈原地替换发起提示", () => {
    toastSuccess("started", "chapter-detect");
    expect(toast.success).toHaveBeenCalledWith("started", {
      id: "chapter-detect",
    });
    toastError("failed", "chapter-detect");
    expect(toast.error).toHaveBeenCalledWith(
      "failed",
      expect.objectContaining({ id: "chapter-detect" }),
    );
  });

  it("未传 id 时保持原有调用形态", () => {
    toastSuccess("plain");
    expect(toast.success).toHaveBeenCalledWith("plain");
  });
});
