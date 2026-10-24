// 创作工厂旅程中枢（P3 WBS §0.1/T1.2 骨架）：意图→双速写作→重写。
// 默认落地视图：「继续写作」主卡一跳回 chapters 原工作面（plan §7 老用户迷失对策）。
// 占位卡由 T4.2 填充目标与描述；本骨架只定旅程叙事顺序。
import { usePanelStore } from "@/stores/usePanelStore";
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";

export default function JourneyCreate() {
  const setActivePanel = usePanelStore((s) => s.setActivePanel);

  return (
    <main
      data-testid="journey-create"
      className="flex-1 min-w-0 overflow-y-auto overscroll-contain bg-background"
    >
      <JourneyHeader
        panelId="journey-create"
        titleKey="journey.create.title"
        subtitleKey="narrative.tagline"
      />
      <div className="grid grid-cols-1 gap-3 px-6 pb-8 pt-2 sm:grid-cols-2 xl:grid-cols-3">
        {/* 主卡：一跳回原工作面（T1.2 验收②，行为勿删） */}
        <JourneyCard
          primary
          titleKey="journey.continueWriting"
          descKey="journey.continueWritingDesc"
          onClick={() => setActivePanel("chapters")}
        />
        {/* 旅程卡占位：T4.2 填充直达目标与描述 */}
        <JourneyCard titleKey="journey.create.stages.intent" />
        <JourneyCard titleKey="journey.create.stages.writing" />
        <JourneyCard titleKey="journey.create.stages.rewrite" />
      </div>
    </main>
  );
}
