// 旅程视图头部 + 主区归属面包屑（P3 WBS §0.1/T1.2）。
// 「喂给谁」不靠记忆靠可视：归属一律 FACTORY_MAP 单源驱动，组件不得自造归属。
import { useTranslation } from "react-i18next";
import type { SidebarPanelId } from "@/types/panel";
import { factoryFeedsKey, factoryOf, factoryTitleKey } from "@/lib/factory";

/** 主区功能面板顶部归属条：「质检工厂 · 喂给签约下限」。 */
export function FactoryBreadcrumb({ panelId }: { panelId: SidebarPanelId }) {
  const { t } = useTranslation();
  const factory = factoryOf(panelId);
  return (
    <div
      data-testid="factory-breadcrumb"
      data-factory={factory}
      className="flex h-7 shrink-0 items-center gap-1.5 border-b bg-sidebar/60 px-3 text-xs text-muted-foreground select-none cursor-default"
    >
      <span className="font-medium text-foreground/80">
        {t(factoryTitleKey(factory))}
      </span>
      <span className="text-muted-foreground/40">·</span>
      <span className="truncate">{t(factoryFeedsKey(factory))}</span>
    </div>
  );
}

interface JourneyHeaderProps {
  /** 旅程面板 id（journey-*），归属经 FACTORY_MAP 解析 */
  panelId: SidebarPanelId;
  /** 旅程标题 i18n key */
  titleKey: string;
  /** 叙案副标题 slot（T2.1 叙案统一挂点） */
  subtitleKey?: string;
}

export default function JourneyHeader({
  panelId,
  titleKey,
  subtitleKey,
}: JourneyHeaderProps) {
  const { t } = useTranslation();
  return (
    <header data-testid="journey-header" className="shrink-0">
      <FactoryBreadcrumb panelId={panelId} />
      <div className="px-6 pt-5 pb-2">
        <h1 className="text-lg font-semibold text-foreground">{t(titleKey)}</h1>
        {subtitleKey && (
          <p
            data-testid="journey-subtitle"
            className="mt-1 text-sm text-muted-foreground"
          >
            {t(subtitleKey)}
          </p>
        )}
      </div>
    </header>
  );
}
