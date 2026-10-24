// 旅程卡原语（P3 WBS §0.1）：只做入口聚合，点击直达既有功能面板，
// 绝不内嵌/重排既有工作面（双速与闸门语义勿动，plan §6.1）。
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { journeyFeatureKey, trackFeature } from "@/lib/featureKeys";

interface Props {
  /** i18n key：卡片标题 */
  titleKey: string;
  /** i18n key：卡片描述（可选，缺则不渲染描述行） */
  descKey?: string;
  /** i18n key：「喂给谁」短语（可选） */
  feedsKey?: string;
  /** 主卡样式（「继续写作」等旅程中枢主入口） */
  primary?: boolean;
  /** 点击直达目标面板；缺省=占位卡（不可点，后续任务填充） */
  onClick?: () => void;
  children?: ReactNode;
}

export default function JourneyCard({
  titleKey,
  descKey,
  feedsKey,
  primary,
  onClick,
  children,
}: Props) {
  const { t } = useTranslation();
  const body = (
    <>
      <div className="flex items-center gap-2">
        <h3
          className={`text-sm font-semibold truncate ${primary ? "text-foreground" : "text-foreground/90"}`}
        >
          {t(titleKey)}
        </h3>
      </div>
      {descKey && (
        <p className="mt-1 text-xs text-muted-foreground leading-relaxed line-clamp-2">
          {t(descKey)}
        </p>
      )}
      {feedsKey && (
        <p className="mt-2 text-[10px] text-muted-foreground/60 truncate">
          {t(feedsKey)}
        </p>
      )}
      {children}
    </>
  );

  const className = `flex flex-col rounded-xl border bg-card text-left transition-colors ${
    primary
      ? "border-primary/40 shadow-sm hover:border-primary hover:bg-muted/40"
      : onClick
        ? "hover:bg-muted/50 hover:border-foreground/20"
        : "border-dashed opacity-70"
  }`;

  if (!onClick) {
    return (
      <div data-testid="journey-card" className={`${className} p-4`}>
        {body}
      </div>
    );
  }
  return (
    <button
      type="button"
      data-testid="journey-card"
      onClick={() => {
        // 旅程卡点击埋点（P4 T2.2）：key 由 titleKey 单源派生；失败静默零提示。
        trackFeature(journeyFeatureKey(titleKey));
        onClick();
      }}
      className={`${className} p-4 cursor-pointer`}
    >
      {body}
    </button>
  );
}
