import { useTranslation } from "react-i18next";
import { Loader2, ShieldAlert, ShieldCheck, Wrench } from "lucide-react";
import type { pipeline } from "@/lib/wailsjs/go/models";
import type { GatePayload, GateDecision } from "@/types/pipeline";
import PipelineGateCard from "./PipelineGateCard";

interface AuditIssue {
  id: string;
  level: string;
  area: string;
  desc: string;
}

interface AuditData {
  verdict: string;
  score?: number;
  summary?: string;
  issues?: AuditIssue[];
}

interface Props {
  run: pipeline.PipelineRun | null;
  stage: string;
  status: string;
  gate: GatePayload | null;
  onResume: (decision: GateDecision, feedback?: string) => void;
}

// S4 签约体检 / S5 修复闭环视图：
// - 后端异步跑 signability-audit（只读体检）期间显示「体检中」；
// - 完成后经 G4 闸门携带 issue 清单（gate.summary 或 run.ContextJSON.audit）渲染 verdict + 问题列表；
// - S5 修复期间显示「修复中」，修复完成自动回到 S4 复评（仍由 G4 闸门交回用户）。
// 非 S4/S5 阶段返回 null，由父组件按原逻辑渲染。
export default function SignabilityAuditView({
  run,
  stage,
  status,
  gate,
  onResume,
}: Props) {
  const { t } = useTranslation();

  if (stage !== "S4" && stage !== "S5") return null;

  const isFix = stage === "S5";

  // 生成中 / 修复中：对应阶段 running 且无闸门。
  if (status === "running" && !gate) {
    return (
      <div className="flex flex-col items-center justify-center gap-3 py-10 text-muted-foreground">
        <Loader2 className="h-6 w-6 animate-spin" />
        <p className="text-sm">
          {t(
            isFix
              ? "pipeline.signability.fixing"
              : "pipeline.signability.auditing",
          )}
        </p>
      </div>
    );
  }

  // 解析 audit 数据：优先 gate.summary，回退 run.ContextJSON.audit。
  let audit: AuditData | null = null;
  const raw =
    gate?.summary ||
    (run?.ContextJSON ? JSON.parse(run.ContextJSON)?.audit : null);
  if (raw) {
    try {
      audit = typeof raw === "string" ? JSON.parse(raw) : raw;
    } catch {
      audit = null;
    }
  }

  const passed = audit?.verdict === "pass";
  const issues = audit?.issues ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        {passed ? (
          <ShieldCheck className="h-4 w-4 text-warning-foreground" />
        ) : (
          <ShieldAlert className="h-4 w-4 text-danger-foreground" />
        )}
        <span className="text-sm font-medium">
          {t("pipeline.signability.title")}
        </span>
        {audit?.score != null ? (
          <span className="text-xs text-muted-foreground">
            {t("pipeline.signability.score", { n: audit.score })}
          </span>
        ) : null}
      </div>

      {audit?.summary ? (
        <p className="whitespace-pre-wrap text-sm text-muted-foreground">
          {audit.summary}
        </p>
      ) : null}

      <div className="flex flex-col gap-2">
        <div className="text-xs font-medium text-muted-foreground">
          {t("pipeline.signability.issues")}
        </div>
        {issues.length > 0 ? (
          <ul className="flex flex-col gap-2">
            {issues.map((it) => (
              <li
                key={it.id}
                className="rounded border bg-card px-3 py-2 text-sm"
              >
                <div className="flex items-center gap-2">
                  <span
                    className={`rounded px-1.5 py-0.5 text-xs ${
                      it.level === "P0"
                        ? "bg-danger text-danger-foreground"
                        : "bg-warning text-warning-foreground"
                    }`}
                  >
                    {it.level}
                  </span>
                  <span className="font-medium">{it.area}</span>
                  <span className="text-muted-foreground">{it.id}</span>
                </div>
                <p className="mt-1 text-muted-foreground">{it.desc}</p>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">
            {t("pipeline.signability.noIssues")}
          </p>
        )}
      </div>

      {isFix && status !== "running" ? (
        <p className="flex items-center gap-1 text-xs text-muted-foreground">
          <Wrench className="h-3 w-3" />
          {t("pipeline.signability.fixNote")}
        </p>
      ) : null}

      {gate ? <PipelineGateCard gate={gate} onResume={onResume} /> : null}
    </div>
  );
}
