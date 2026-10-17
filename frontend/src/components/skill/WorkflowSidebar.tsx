import { useTranslation } from "react-i18next";
import { Target, Check } from "lucide-react";
import type { LucideIcon } from "lucide-react";

// 工作流注册表：只保留「在当前书上运行」的工作流。
// 新书孵化（book-pitch）是书架级动作，已迁移到 BookshelfView 顶部入口，
// 不再放在工作流面板里，避免和当前 active 书混淆。
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
