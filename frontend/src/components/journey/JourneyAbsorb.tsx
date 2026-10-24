// 吸收工厂旅程（P3 WBS §0.1/T1.2 骨架）：导入参考书→提取→入库。
// T4.1 旅程卡填充：点击直达既有面板（novels 书架 reference 入口 / style-samples / skills），
// 卡只做入口聚合，绝不内嵌/重排既有工作面；提取长跑的 ExtractWorkspaceView 常驻挂载纪律勿动。
// 三面板次级导航（#186）：style-samples/skills/skill-matrix 一跳触达。
// 空态承接（T2.2 漏斗）：「未提取」状态在提取卡上同语提示（emptyHint.voiceExtraction 单源），
// 判定失败/坏数据静默零提示（宁缺勿错）。
import { useTranslation } from "react-i18next";
import { usePanelStore } from "@/stores/usePanelStore";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { useVoiceExtractionState } from "@/hooks/useVoiceExtractionState";
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";

/** 三面板次级导航：labelKey 复用既有面板标签单源（shell.*），禁自造变体。 */
const PANEL_LINKS: {
  id: "style-samples" | "skills" | "skill-matrix";
  labelKey: string;
}[] = [
  { id: "style-samples", labelKey: "shell.extract" },
  { id: "skills", labelKey: "shell.skills" },
  { id: "skill-matrix", labelKey: "shell.skillMatrix" },
];

export default function JourneyAbsorb() {
  const { t } = useTranslation();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  const activeNovelId = useNovelStore((s) => s.activeNovelId);
  // T2.2 漏斗承接：未提取心智 → 提取卡同语提示（hook 内失败静默降级）
  const { unextracted, dismissed } = useVoiceExtractionState(activeNovelId);

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
        {/* T4.1 旅程卡：导入→提取→入库（价值链顺序勿重排） */}
        <JourneyCard
          titleKey="journey.absorb.stages.import"
          feedsKey="shell.factory.absorb.feeds"
          onClick={() => setActivePanel("novels")}
        />
        <JourneyCard
          titleKey="journey.absorb.stages.extract"
          feedsKey="shell.factory.absorb.feeds"
          onClick={() => setActivePanel("style-samples")}
        >
          {/* 空态承接（T2.2 同语提示）：未提取时落提取卡，整卡可点直达提取工作区 */}
          {unextracted && !dismissed ? (
            <span
              data-testid="absorb-voice-empty-hint"
              className="mt-2 block text-[11px] text-muted-foreground"
            >
              {t("emptyHint.voiceExtraction")}
            </span>
          ) : null}
        </JourneyCard>
        <JourneyCard
          titleKey="journey.absorb.stages.onboard"
          feedsKey="shell.factory.absorb.feeds"
          onClick={() => setActivePanel("skills")}
        />
        {/* 三面板次级导航（#186 验收：一跳触达 style-samples/skills/skill-matrix） */}
        <nav
          data-testid="journey-absorb-panel-nav"
          aria-label={t("journey.absorb.panelNav")}
          className="flex flex-wrap items-center gap-2 sm:col-span-2 xl:col-span-3"
        >
          <span className="text-xs text-muted-foreground">
            {t("journey.absorb.panelNav")}
          </span>
          {PANEL_LINKS.map((p) => (
            <button
              key={p.id}
              type="button"
              onClick={() => setActivePanel(p.id)}
              className="h-7 rounded-md border bg-card px-3 text-xs text-foreground/90 transition-colors hover:border-foreground/20 hover:bg-muted/50"
            >
              {t(p.labelKey)}
            </button>
          ))}
        </nav>
      </div>
    </main>
  );
}
