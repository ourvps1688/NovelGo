import { useTranslation } from "react-i18next";
import { Target, Check, Sparkles } from "lucide-react";
import type { LucideIcon } from "lucide-react";

// 工作流注册表：后续新增工作流时，只需在此数组追加一项（id / labelKey /
// descKey / icon），SidePanel 会自动列出，主区按 activeWorkflow 渲染对应视图。
// id 同时是 WorkspaceView 中 activeWorkflow 子状态的取值。
interface WorkflowDef {
  id: string;
  labelKey: string;
  descKey: string;
  icon: LucideIcon;
}

const WORKFLOWS: WorkflowDef[] = [
  {
    id: "signability",
    labelKey: "workflow.signability",
    descKey: "workflow.signabilityDesc",
    icon: Target,
  },
  {
    id: "bookpitch",
    labelKey: "workflow.bookpitch",
    descKey: "workflow.bookpitchDesc",
    icon: Sparkles,
  },
];

interface Props {
  activeWorkflow: string;
  onSelectWorkflow: (id: string) => void;
}

export default function WorkflowSidebar({
  activeWorkflow,
  onSelectWorkflow,
}: Props) {
  const { t } = useTranslation();

  return (
    <div className="flex flex-col h-full overflow-hidden bg-sidebar">
      <div className="px-4 py-3 border-b shrink-0">
        <h2 className="text-sm font-semibold flex items-center gap-2 text-foreground">
          {t("workflow.title")}
        </h2>
        <p className="text-xs text-muted-foreground mt-1 leading-relaxed">
          {t("workflow.subtitle")}
        </p>
      </div>

      <div className="flex-1 overflow-y-auto overscroll-contain p-2 space-y-1">
        {WORKFLOWS.map((wf) => {
          const isActive = wf.id === activeWorkflow;
          const Icon = wf.icon;
          return (
            <button
              key={wf.id}
              type="button"
              onClick={() => onSelectWorkflow(wf.id)}
              className={`w-full flex items-start gap-2 rounded-lg px-3 py-2 text-left transition-colors ${
                isActive
                  ? "bg-muted text-foreground"
                  : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"
              }`}
            >
              <Icon className="w-4 h-4 mt-0.5 shrink-0" strokeWidth={1.75} />
              <span className="min-w-0 flex-1">
                <span className="block text-sm font-medium truncate">
                  {t(wf.labelKey)}
                </span>
                <span className="block text-[11px] text-muted-foreground/80 truncate">
                  {t(wf.descKey)}
                </span>
              </span>
              {isActive && (
                <Check className="w-4 h-4 mt-0.5 text-primary shrink-0" />
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}
