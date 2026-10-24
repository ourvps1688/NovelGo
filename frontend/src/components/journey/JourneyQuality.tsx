// 质检工厂旅程（P3 WBS §0.1/T1.2 骨架）：体检→修复/重写→卷末冻结→巡检。
// 旅程卡占位由 T3.2 填充（直达目标：signability / pipeline 卷管理与巡检）；
// 写作评分配置卡由 T3.1 从设置页迁入。
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";

export default function JourneyQuality() {
  return (
    <main
      data-testid="journey-quality"
      className="flex-1 min-w-0 overflow-y-auto overscroll-contain bg-background"
    >
      <JourneyHeader
        panelId="journey-quality"
        titleKey="journey.quality.title"
      />
      <div className="grid grid-cols-1 gap-3 px-6 pb-8 pt-2 sm:grid-cols-2 xl:grid-cols-3">
        <JourneyCard titleKey="journey.quality.stages.audit" />
        <JourneyCard titleKey="journey.quality.stages.fix" />
        <JourneyCard titleKey="journey.quality.stages.freeze" />
        <JourneyCard titleKey="journey.quality.stages.patrol" />
      </div>
    </main>
  );
}
