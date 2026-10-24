// 吸收工厂旅程（P3 WBS §0.1/T1.2 骨架）：导入参考书→提取→入库。
// 旅程卡占位由 T4.1 填充（直达目标：书架 reference 入口 / style-samples / skills·skill-matrix）。
// 提取是长跑任务：本视图不得引入会卸载 ExtractWorkspaceView 的新路由。
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";

export default function JourneyAbsorb() {
  return (
    <main
      data-testid="journey-absorb"
      className="flex-1 min-w-0 overflow-y-auto overscroll-contain bg-background"
    >
      <JourneyHeader
        panelId="journey-absorb"
        titleKey="journey.absorb.title"
        subtitleKey="narrative.tagline"
      />
      <div className="grid grid-cols-1 gap-3 px-6 pb-8 pt-2 sm:grid-cols-2 xl:grid-cols-3">
        <JourneyCard titleKey="journey.absorb.stages.import" />
        <JourneyCard titleKey="journey.absorb.stages.extract" />
        <JourneyCard titleKey="journey.absorb.stages.onboard" />
      </div>
    </main>
  );
}
