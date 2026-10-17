import { toast } from "sonner";

/** 显示错误 toast，带复制按钮，方便用户报告具体错误信息。 */
export function toastError(msg: string) {
  return toast.error(msg, {
    action: {
      label: "复制",
      onClick: () => navigator.clipboard.writeText(msg),
    },
    actionButtonStyle: {
      backgroundColor: "var(--primary)",
      color: "var(--primary-foreground)",
      border: "none",
      padding: "2px 10px",
      borderRadius: "4px",
      fontSize: "12px",
    },
  });
}

/** 显示成功 toast，用于非错误的正向反馈（如 URL 自动补全）。 */
export function toastSuccess(msg: string) {
  return toast.success(msg);
}

/** 显示中性信息 toast，用于非错误也非强成功的提示（如字段冲突提示）。 */
export function toastInfo(msg: string) {
  return toast.info(msg);
}
