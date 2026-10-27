// 质检工厂旅程（P3 WBS §0.1/T1.2 骨架）：体检→修复/重写→卷末冻结→巡检。
// T3.2 旅程卡填充：点击直达既有面板（signability / pipeline），卡只做入口聚合，
// 绝不内嵌/重排既有工作面（双速与闸门语义勿动，plan §6.1）。
// 三面板次级导航（#185）：pipeline/reader/signability 一跳触达——
// reader 无旅程阶段卡（§0.1 面板归属），经直达条触达。
// 写作评分配置（T3.1）自设置页 writing Tab 迁入本面板设置区：
// 只动挂载点与入口，WritingConfigTab 与 useWritingConfig 逻辑零改动。
import { useTranslation } from "react-i18next";
import { usePanelStore } from "@/stores/usePanelStore";
import WritingConfigTab from "@/components/settings/WritingConfigTab";
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";
import DetectionTasks from "./DetectionTasks";

/** 三面板次级导航：labelKey 复用既有面板标签单源（shell.* / pipeline.title），禁自造变体。
 *  R5-3 T3.1 编排预设定位：pipeline 入口 tooltip 带预设表述（不宣称唯一入口，
 *  检测任务区可只跑检测不跑流水线，流水线亦可不手动跑检测）。 */
const PANEL_LINKS: {
  id: "signability" | "pipeline" | "reader";
  labelKey: string;
  titleKey?: string;
}[] = [
  { id: "signability", labelKey: "shell.workflow" },
  {
    id: "pipeline",
    labelKey: "pipeline.title",
    titleKey: "pipeline.presetHint",
  },
  { id: "reader", labelKey: "shell.readerView" },
];

export default function JourneyQuality() {
  const { t } = useTranslation();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  return (
    <main
      data-testid="journey-quality"
      className="flex-1 min-w-0 overflow-y-auto overscroll-contain bg-background"
    >
      <JourneyHeader
        panelId="journey-quality"
        titleKey="journey.quality.title"
        subtitleKey="narrative.tagline"
      />
      <div className="grid grid-cols-1 gap-3 px-6 pb-8 pt-2 sm:grid-cols-2 xl:grid-cols-3">
        {/* T3.2 旅程卡：体检→修复/重写→卷末冻结→巡检（价值链顺序勿重排） */}
        <JourneyCard
          titleKey="journey.quality.stages.audit"
          feedsKey="shell.factory.quality.feeds"
          onClick={() => setActivePanel("signability")}
        />
        <JourneyCard
          titleKey="journey.quality.stages.fix"
          feedsKey="shell.factory.quality.feeds"
          onClick={() => setActivePanel("pipeline")}
        />
        <JourneyCard
          titleKey="journey.quality.stages.freeze"
          feedsKey="shell.factory.quality.feeds"
          onClick={() => setActivePanel("pipeline")}
        />
        <JourneyCard
          titleKey="journey.quality.stages.patrol"
          feedsKey="shell.factory.quality.feeds"
          onClick={() => setActivePanel("pipeline")}
        />
        {/* T2.1 检测任务卡区（R5-3）：四入口独立启动、不依赖流水线阶段，
            与既有旅程卡并列；价值链顺序勿重排（卡仍只做入口）。 */}
        <DetectionTasks />
        {/* 三面板次级导航（#185 验收：一跳触达 pipeline/reader/signability） */}
        <nav
          data-testid="journey-quality-panel-nav"
          aria-label={t("journey.quality.panelNav")}
          className="flex flex-wrap items-center gap-2 sm:col-span-2 xl:col-span-3"
        >
          <span className="text-xs text-muted-foreground">
            {t("journey.quality.panelNav")}
          </span>
          {PANEL_LINKS.map((p) => (
            <button
              key={p.id}
              type="button"
              title={p.titleKey ? t(p.titleKey) : undefined}
              onClick={() => setActivePanel(p.id)}
              className="h-7 rounded-md border bg-card px-3 text-xs text-foreground/90 transition-colors hover:border-foreground/20 hover:bg-muted/50"
            >
              {t(p.labelKey)}
            </button>
          ))}
        </nav>
        {/* T3.1 写作评分配置卡（自设置页迁入，组件零改动，只动挂载点） */}
        <section
          data-testid="journey-writing-config"
          aria-label={t("journey.quality.writingConfig")}
          className="flex flex-col rounded-xl border bg-card p-4 sm:col-span-2 xl:col-span-3"
        >
          <WritingConfigTab />
          <p className="mt-2 truncate text-[10px] text-muted-foreground/60">
            {t("shell.factory.quality.feeds")}
          </p>
        </section>
      </div>
    </main>
  );
}
