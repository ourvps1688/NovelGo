import { type ReactNode } from "react";
import { Loader2 } from "lucide-react";

export type StatusVariant = "loading" | "empty" | "error";

interface StatusStateProps {
  variant: StatusVariant;
  /** 主标题（loading 时可不传，仅显示旋转图标） */
  title?: ReactNode;
  /** 次要说明文字 */
  description?: ReactNode;
  /** error 变体下可选的重试回调 */
  onRetry?: () => void;
  /** 重试按钮文案，默认「重试」 */
  retryLabel?: string;
  /** 自定义图标（empty/error 时展示在标题上方）；loading 时使用内置旋转图标 */
  icon?: ReactNode;
  className?: string;
}

/**
 * 统一的加载 / 空 / 错误占位组件。
 * 替代各视图里写法不一的 loading/empty/error 片段，保证视觉与交互一致：
 * - loading：居中旋转图标
 * - empty：可选图标 + 说明
 * - error：说明 + 重试按钮（带 onRetry 时）
 */
export function StatusState({
  variant,
  title,
  description,
  onRetry,
  retryLabel,
  icon,
  className,
}: StatusStateProps) {
  return (
    <div
      className={`flex items-center justify-center h-full ${className ?? ""}`}
    >
      <div className="text-center">
        {variant === "loading" ? (
          <Loader2 className="w-5 h-5 animate-spin text-muted-foreground mx-auto mb-3" />
        ) : icon ? (
          <div className="mx-auto mb-3 text-muted-foreground/20">{icon}</div>
        ) : null}
        {title != null && (
          <p className="text-sm text-muted-foreground mb-2">{title}</p>
        )}
        {description != null && (
          <p className="text-xs text-muted-foreground/70 mb-2">{description}</p>
        )}
        {variant === "error" && onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="text-xs text-primary underline cursor-pointer"
          >
            {retryLabel ?? "重试"}
          </button>
        )}
      </div>
    </div>
  );
}
