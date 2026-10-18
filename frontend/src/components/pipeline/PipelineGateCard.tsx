import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Check, Edit3, Stethoscope, Wrench, X } from "lucide-react";
import type { GatePayload, GateDecision } from "@/types/pipeline";

interface Props {
  gate: GatePayload;
  onResume: (decision: GateDecision, feedback?: string) => void;
}

// 闸门确认卡：监听 pipeline:gate 事件后按后端 options 渲染决策按钮。
// 不同闸门的选项可能不同（如 G4 是 continue/fix/abort），不再写死 modify。
export default function PipelineGateCard({ gate, onResume }: Props) {
  const { t } = useTranslation();

  const options =
    gate.options.length > 0 ? gate.options : ["continue", "modify", "abort"];

  const config: Record<
    string,
    { label: string; icon: ReactNode; variant: string }
  > = {
    continue: {
      label: t("pipeline.gate.continue"),
      icon: <Check className="w-4 h-4" />,
      variant:
        "inline-flex items-center gap-1 rounded bg-primary px-3 py-1 text-primary-foreground text-sm",
    },
    fix: {
      label: t("pipeline.gate.fix"),
      icon: <Wrench className="w-4 h-4" />,
      variant:
        "inline-flex items-center gap-1 rounded border px-3 py-1 text-sm",
    },
    audit_now: {
      label: t("pipeline.gate.auditNow"),
      icon: <Stethoscope className="w-4 h-4" />,
      variant:
        "inline-flex items-center gap-1 rounded bg-primary px-3 py-1 text-primary-foreground text-sm",
    },
    modify: {
      label: t("pipeline.gate.modify"),
      icon: <Edit3 className="w-4 h-4" />,
      variant:
        "inline-flex items-center gap-1 rounded border px-3 py-1 text-sm",
    },
    abort: {
      label: t("pipeline.gate.abort"),
      icon: <X className="w-4 h-4" />,
      variant:
        "inline-flex items-center gap-1 rounded border border-danger-border px-3 py-1 text-sm text-red-600",
    },
  };

  return (
    <div className="rounded border border-warning-border bg-warning p-3">
      <div className="font-medium text-warning-foreground">
        {t("pipeline.gate.title")}
      </div>
      {gate.summary ? (
        <p className="mt-1 text-sm text-muted-foreground">{gate.summary}</p>
      ) : null}
      <div className="mt-3 flex flex-wrap gap-2">
        {options.map((opt) => {
          const c = config[opt] ?? {
            label: opt,
            icon: null,
            variant:
              "inline-flex items-center gap-1 rounded border px-3 py-1 text-sm",
          };
          return (
            <button
              key={opt}
              type="button"
              className={c.variant}
              onClick={() => onResume(opt as GateDecision)}
            >
              {c.icon}
              {c.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
