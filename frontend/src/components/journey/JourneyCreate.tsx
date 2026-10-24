// 创作工厂旅程中枢（P3 WBS §0.1/T1.2 骨架）：意图→双速写作→重写。
// 默认落地视图：「继续写作」主卡一跳回 chapters 原工作面（plan §7 老用户迷失对策，T1.2 行为勿删）。
// T4.2 旅程卡填充：意图（bookpitch/蓝图）→ 双速写作（自由区 chat 卡 + 质检区 pipeline 卡并列——
// 双速语义刻意保留的可视化，禁止合并为一张卡，plan §6.1 反模式）→ 重写（复用 T2.2 空态 hint 挂点）。
// 十面板次级导航（#187）：create 工厂功能面板一跳触达。
import { useTranslation } from "react-i18next";
import { usePanelStore } from "@/stores/usePanelStore";
import VoiceExtractionHint from "@/components/chat/VoiceExtractionHint";
import JourneyHeader from "./JourneyHeader";
import JourneyCard from "./JourneyCard";

/** 十面板次级导航：labelKey 复用既有面板标签单源（shell.* 等），禁自造变体。 */
const PANEL_LINKS: {
  id:
    | "chapters"
    | "chapter-management"
    | "preferences"
    | "novel-settings"
    | "profile"
    | "bookpitch"
    | "characters"
    | "locations"
    | "storyarcs"
    | "timeline";
  labelKey: string;
}[] = [
  { id: "chapters", labelKey: "shell.chapters" },
  { id: "chapter-management", labelKey: "shell.chapterManagement" },
  { id: "preferences", labelKey: "shell.preference" },
  { id: "novel-settings", labelKey: "shell.novelSetting" },
  { id: "profile", labelKey: "workspace.profile" },
  { id: "bookpitch", labelKey: "workflow.bookpitch" },
  { id: "characters", labelKey: "shell.characters" },
  { id: "locations", labelKey: "shell.locations" },
  { id: "storyarcs", labelKey: "shell.arcs" },
  { id: "timeline", labelKey: "shell.timeline" },
];

export default function JourneyCreate() {
  const { t } = useTranslation();
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
        {/* T4.2 旅程卡：意图→双速写作→重写（价值链顺序勿重排） */}
        <JourneyCard
          titleKey="journey.create.stages.intent"
          feedsKey="shell.factory.create.feeds"
          onClick={() => setActivePanel("bookpitch")}
        />
        {/* 双速写作卡并列：自由区 chat + 质检区 pipeline（禁止合并，plan §6.1 反模式） */}
        <JourneyCard
          titleKey="journey.create.stages.writingFree"
          feedsKey="shell.factory.create.feeds"
          onClick={() => setActivePanel("chapters")}
        />
        <JourneyCard
          titleKey="journey.create.stages.writingPipeline"
          feedsKey="shell.factory.create.feeds"
          onClick={() => setActivePanel("pipeline")}
        />
        {/* 重写卡 + T2.2 空态 hint 挂点复用（非阻断、失败静默、会话级可关） */}
        <div className="flex flex-col gap-3">
          <JourneyCard
            titleKey="journey.create.stages.rewrite"
            feedsKey="shell.factory.create.feeds"
            onClick={() => setActivePanel("chapters")}
          />
          <VoiceExtractionHint />
        </div>
        {/* 十面板次级导航（#187 验收：create 工厂十面板一跳触达） */}
        <nav
          data-testid="journey-create-panel-nav"
          aria-label={t("journey.create.panelNav")}
          className="flex flex-wrap items-center gap-2 sm:col-span-2 xl:col-span-3"
        >
          <span className="text-xs text-muted-foreground">
            {t("journey.create.panelNav")}
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
