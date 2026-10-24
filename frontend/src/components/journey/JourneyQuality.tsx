// 质检工厂旅程（P3 WBS §0.1/T1.2 骨架）：体检→修复/重写→卷末冻结→巡检。
// 旅程卡占位由 T3.2 填充（直达目标：signability / pipeline 卷管理与巡检）；
// 写作评分配置（T3.1）自设置页 writing Tab 迁入本面板设置区：
// 只动挂载点与入口，WritingConfigTab 与 useWritingConfig 逻辑零改动。
import { useTranslation } from "react-i18next";
import WritingConfigTab from "@/components/settings/WritingConfigTab";
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";

export default function JourneyQuality() {
  const { t } = useTranslation();
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
        <JourneyCard titleKey="journey.quality.stages.audit" />
        <JourneyCard titleKey="journey.quality.stages.fix" />
        <JourneyCard titleKey="journey.quality.stages.freeze" />
        <JourneyCard titleKey="journey.quality.stages.patrol" />
        {/* T3.1 写作评分配置卡（自设置页迁入，组件零改动，只动挂载点） */}
        <section
          data-testid="journey-writing-config"
          aria-label={t("journey.quality.writingConfig")}
          className="flex flex-col rounded-xl border bg-card p-4 sm:col-span-2 xl:col-span-3"
        >
          <WritingConfigTab />
        </section>
      </div>
    </main>
  );
}
