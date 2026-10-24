// 空态引导 hint（P3 WBS T2.2/§0.3）：未提取心智的书在重写入口提示提取参考书心智。
// 非阻断：绝不拦截重写动作；可关：会话级消隐；判定失败静默不渲染（降级最高纪律）。
// 挂点两处：①创作自由区 ChatPanel 重写动作处；②质检 rewrite_handoff 提示处（SignabilityAuditView）。
// T4.2 创作旅程重写卡复用本组件（挂点复用，勿另造变体文案）。
import { Sparkles, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { usePanelStore } from "@/stores/usePanelStore";
import { useNovelStore } from "@/components/novel/useNovelStore";
import { useVoiceExtractionState } from "@/hooks/useVoiceExtractionState";

interface Props {
  /** 书 id；缺省取 useNovelStore.activeNovelId（挂点无须传参）。 */
  novelID?: number;
}

export default function VoiceExtractionHint({ novelID }: Props = {}) {
  const { t } = useTranslation();
  const activeNovelId = useNovelStore((s) => s.activeNovelId);
  const id = novelID ?? activeNovelId;
  const setActivePanel = usePanelStore((s) => s.setActivePanel);
  const { unextracted, dismissed, dismiss } = useVoiceExtractionState(id);

  if (!unextracted || dismissed) return null;

  return (
    <div
      data-testid="voice-extraction-hint"
      role="note"
      className="mx-3 mb-2 flex shrink-0 items-start gap-2 rounded-lg border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-foreground"
    >
      <Sparkles className="mt-0.5 h-3.5 w-3.5 shrink-0 text-primary" />
      <span className="flex-1 leading-5">{t("emptyHint.voiceExtraction")}</span>
      <button
        type="button"
        data-testid="voice-extraction-hint-go"
        onClick={() => setActivePanel("style-samples")}
        className="shrink-0 cursor-pointer rounded-md border border-primary/30 px-2 py-0.5 font-medium text-primary transition-colors hover:bg-primary/10"
      >
        {t("emptyHint.goExtract")}
      </button>
      <button
        type="button"
        data-testid="voice-extraction-hint-dismiss"
        aria-label={t("emptyHint.dismiss")}
        onClick={dismiss}
        className="shrink-0 cursor-pointer rounded-md p-0.5 text-muted-foreground transition-colors hover:text-foreground"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
