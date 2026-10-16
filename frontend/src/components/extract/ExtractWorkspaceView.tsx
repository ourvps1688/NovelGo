import { useState, type ElementType } from "react";
import { Palette, BookOpen, Brain, Check } from "lucide-react";
import { useTranslation } from "react-i18next";
import StyleView from "@/components/style/StyleView";
import PatternExtractView from "@/components/pattern/PatternExtractView";

interface Props {
  novelId: number;
  focusSampleId?: number | null;
  onFocusSampleHandled?: () => void;
}

type ScopeKey = "style" | "struct" | "mental";

function SectionHeader({
  icon: Icon,
  title,
  note,
}: {
  icon: ElementType;
  title: string;
  note: string;
}) {
  return (
    <div className="px-5 py-3 bg-muted/40 border-b flex items-start gap-3">
      <div className="mt-0.5 p-1.5 rounded-md bg-primary/10 shrink-0">
        <Icon className="w-4 h-4 text-primary" />
      </div>
      <div className="flex flex-col">
        <span className="text-sm font-semibold text-foreground">{title}</span>
        <span className="text-xs text-muted-foreground mt-0.5">{note}</span>
      </div>
    </div>
  );
}

export default function ExtractWorkspaceView({
  novelId,
  focusSampleId,
  onFocusSampleHandled,
}: Props) {
  const { t } = useTranslation();
  // 默认三类全选：风格（样本级、落偏好卡） / 套路 / 心智（全书级，一次前缀双产物）。
  const [scope, setScope] = useState<Record<ScopeKey, boolean>>({
    style: true,
    struct: true,
    mental: true,
  });
  const toggle = (k: ScopeKey) => setScope((p) => ({ ...p, [k]: !p[k] }));

  const narrativeActive = scope.struct || scope.mental;
  const narrativeFlavors = (
    [scope.struct ? "struct" : null, scope.mental ? "mental" : null] as (
      "struct" | "mental" | null
    )[]
  ).filter(Boolean) as ("struct" | "mental")[];
  // 范围变化即重挂载叙事面板，使内部产物集合与勾选一致。
  const narrativeKey = narrativeFlavors.join("+");

  const handleFocusSampleHandled = () => {
    setScope((p) => ({ ...p, style: true }));
    onFocusSampleHandled?.();
  };

  const chipClass = (active: boolean) =>
    `inline-flex items-center gap-1.5 h-8 px-3 rounded-md text-sm transition-colors ${
      active
        ? "bg-primary text-primary-foreground shadow-sm font-medium"
        : "text-muted-foreground hover:text-foreground hover:bg-background/60"
    }`;

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-background">
      {/* 统一范围选择：风格 / 套路 / 心智 */}
      <div className="px-6 pt-4 pb-3 border-b shrink-0">
        <p className="text-xs text-muted-foreground mb-2">
          {t("extract.panelSelectorLabel")}
        </p>
        <div className="inline-flex items-center gap-1 rounded-lg bg-muted/60 p-1">
          <button
            onClick={() => toggle("style")}
            className={chipClass(scope.style)}
          >
            <Palette className="w-4 h-4" />
            {t("extract.style")}
            {scope.style && <Check className="w-3.5 h-3.5" />}
          </button>
          <button
            onClick={() => toggle("struct")}
            className={chipClass(scope.struct)}
          >
            <BookOpen className="w-4 h-4" />
            {t("extract.struct")}
            {scope.struct && <Check className="w-3.5 h-3.5" />}
          </button>
          <button
            onClick={() => toggle("mental")}
            className={chipClass(scope.mental)}
          >
            <Brain className="w-4 h-4" />
            {t("extract.mindset")}
            {scope.mental && <Check className="w-3.5 h-3.5" />}
          </button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          {t("extract.unifiedHint")}
        </p>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto p-6 space-y-6">
        {narrativeActive && (
          <div className="rounded-xl border bg-card overflow-hidden">
            <SectionHeader
              icon={BookOpen}
              title={t("extract.narrativeSectionTitle")}
              note={t("extract.narrativeSectionNote")}
            />
            <PatternExtractView
              key={narrativeKey}
              currentNovelId={novelId}
              defaultFlavors={narrativeFlavors}
            />
          </div>
        )}
        {scope.style && (
          <div className="rounded-xl border bg-card overflow-hidden">
            <SectionHeader
              icon={Palette}
              title={t("extract.styleSectionTitle")}
              note={t("extract.styleSectionNote")}
            />
            <StyleView
              focusId={focusSampleId}
              onFocusHandled={handleFocusSampleHandled}
              embedded
              novelId={novelId}
            />
          </div>
        )}
        {!narrativeActive && !scope.style && (
          <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-3">
            <Palette className="w-12 h-12 opacity-20" />
            <p className="text-sm">{t("extract.selectScopeHint")}</p>
          </div>
        )}
      </div>
    </div>
  );
}
