import { toast } from "sonner";

/** 显示错误 toast，带复制按钮，方便用户报告具体错误信息。
 * id 用于覆盖同一次操作先前的「已发起」提示（sonner 同 id 会原地替换）。 */
export function toastError(msg: string, id?: string) {
  return toast.error(msg, {
    ...(id ? { id } : {}),
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

/** 显示成功 toast，用于非错误的正向反馈（如 URL 自动补全）。
 * id 用于让同一次操作的完成反馈原地替换「已发起」提示，避免两条叠加。 */
export function toastSuccess(msg: string, id?: string) {
  return id ? toast.success(msg, { id }) : toast.success(msg);
}

/** 显示中性信息 toast，用于非错误也非强成功的提示（如字段冲突提示）。 */
export function toastInfo(msg: string) {
  return toast.info(msg);
}
