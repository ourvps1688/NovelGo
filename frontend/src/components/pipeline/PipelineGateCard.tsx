import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  Check,
  CheckCircle,
  ClipboardList,
  Edit3,
  Stethoscope,
  Wrench,
  X,
} from "lucide-react";
import type { GatePayload, GateDecision } from "@/types/pipeline";
import type { pipeline } from "@/lib/wailsjs/go/models";
import { ListIssueTickets } from "@/lib/wailsjs/go/app/App";
import { GATE_DECISION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";

interface Props {
  gate: GatePayload;
  onResume: (decision: GateDecision, feedback?: string) => void;
  // R5-4 检测汇总视图：verdict 徽标从 run 上下文解析（可选参数，对外契约向后兼容）。
  run?: pipeline.PipelineRun | null;
}

// 工单终态（与 IssueTicketsPanel 同口径）：verified/dismissed 收口后不计入未决。
const TERMINAL = new Set(["verified", "dismissed"]);

// verdictOf 从 run 上下文解析最近一次体检结论（audit 优先，蓝图轮取 blueprint_audit）。
function verdictOf(run?: pipeline.PipelineRun | null): string | null {
  if (!run?.ContextJSON) return null;
  try {
    const c = JSON.parse(run.ContextJSON);
    const raw = c.audit ?? c.blueprint_audit;
    if (!raw) return null;
    const a = typeof raw === "string" ? JSON.parse(raw) : raw;
    return (a?.verdict as string) ?? null;
  } catch {
    return null;
  }
}

// PipelineGateCard 是检测汇总视图（R5-4 G 闸门降级，契约 0.1/0.2）：
// G 闸门不再以阻塞式警告卡形态逼答，改为中性信息卡——verdict 徽标 + 结论摘要 +
// 未决工单计数（复用 ListIssueTickets）+ 建议动作按钮。按钮语义不变
// （ResumePipeline → Orchestrator.Advance 唯一裁决点），是否推进由人决定。
export default function PipelineGateCard({ gate, onResume, run }: Props) {
  const { t } = useTranslation();

  const options =
    gate.options.length > 0 ? gate.options : ["continue", "modify", "abort"];

  // 未决工单计数（R5-4 汇总视图）：复用既有工单查询，失败静默降级（不渲染计数行）。
  const [pendingCount, setPendingCount] = useState<number | null>(null);
  useEffect(() => {
    let cancelled = false;
    ListIssueTickets(gate.novel_id)
      .then((list) => {
        if (cancelled) return;
        const n = (list ?? []).filter((r) => !TERMINAL.has(r.status)).length;
        setPendingCount(n);
      })
      .catch(() => {
        if (!cancelled) setPendingCount(null);
      });
    return () => {
      cancelled = true;
    };
  }, [gate.novel_id]);

  const verdict = verdictOf(run);
  const passed = verdict === "pass";

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
    finish: {
      label: t("pipeline.gate.finish"),
      icon: <CheckCircle className="w-4 h-4" />,
      variant:
        "inline-flex items-center gap-1 rounded bg-primary px-3 py-1 text-primary-foreground text-sm",
    },
  };

  return (
    <div className="rounded border bg-muted/40 p-3">
      <div className="flex items-center gap-2">
        <span className="font-medium text-foreground">
          {t("pipeline.gate.summaryTitle")}
        </span>
        {verdict ? (
          <span
            className={
              passed
                ? "rounded bg-success/10 px-2 py-0.5 text-success-foreground text-xs"
                : "rounded bg-warning/10 px-2 py-0.5 text-warning-foreground text-xs"
            }
          >
            {passed
              ? t("pipeline.gate.verdictPass")
              : t("pipeline.gate.verdictNeedsFix")}
          </span>
        ) : null}
      </div>
      {gate.summary ? (
        <p className="mt-1 text-sm text-muted-foreground">{gate.summary}</p>
      ) : null}
      {pendingCount !== null ? (
        <p className="mt-1 flex items-center gap-1 text-muted-foreground text-xs">
          <ClipboardList className="h-3 w-3" />
          {pendingCount > 0
            ? t("pipeline.gate.foundCount", { n: pendingCount })
            : t("pipeline.gate.foundNone")}
        </p>
      ) : null}
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground text-xs">
          {t("pipeline.gate.suggestedActions")}
        </span>
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
              onClick={() => {
                // 关键动作埋点（P4 T2.2）：仅 fix/audit_now/finish_book/modify
                // 归入动作度量，流程控制不计入；失败静默零提示。
                const key = GATE_DECISION_FEATURE_KEYS[opt];
                if (key) trackFeature(key);
                onResume(opt as GateDecision);
              }}
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
