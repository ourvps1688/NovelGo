import { useState } from "react";
import { Palette, BookOpen, Brain } from "lucide-react";
import { useTranslation } from "react-i18next";
import StyleView from "@/components/style/StyleView";
import PatternExtractView from "@/components/pattern/PatternExtractView";

interface Props {
  novelId: number;
  focusSampleId?: number | null;
  onFocusSampleHandled?: () => void;
}

type ScopeKey = "style" | "struct" | "mental";

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
        ? "bg-background text-foreground shadow-sm"
        : "text-muted-foreground hover:text-foreground hover:bg-background/60"
    }`;

  return (
    <div className="flex-1 flex flex-col min-h-0 bg-background">
      {/* 统一范围选择：风格 / 套路 / 心智 */}
      <div className="px-6 pt-4 border-b shrink-0">
        <div className="inline-flex items-center gap-1 rounded-lg bg-muted/60 p-1">
          <button
            onClick={() => toggle("style")}
            className={chipClass(scope.style)}
          >
            <Palette className="w-4 h-4" />
            {t("extract.style")}
          </button>
          <button
            onClick={() => toggle("struct")}
            className={chipClass(scope.struct)}
          >
            <BookOpen className="w-4 h-4" />
            {t("extract.struct")}
          </button>
          <button
            onClick={() => toggle("mental")}
            className={chipClass(scope.mental)}
          >
            <Brain className="w-4 h-4" />
            {t("extract.mindset")}
          </button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground">
          {t("extract.unifiedHint")}
        </p>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto">
        {narrativeActive && (
          <div className={scope.style ? "border-b" : ""}>
            <PatternExtractView
              key={narrativeKey}
              currentNovelId={novelId}
              defaultFlavors={narrativeFlavors}
            />
          </div>
        )}
        {scope.style && (
          <StyleView
            focusId={focusSampleId}
            onFocusHandled={handleFocusSampleHandled}
            embedded
            novelId={novelId}
          />
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
