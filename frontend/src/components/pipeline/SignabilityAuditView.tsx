import { useTranslation } from "react-i18next";
import {
  CheckCircle2,
  Clock,
  Hourglass,
  Loader2,
  ShieldAlert,
  ShieldCheck,
  Wrench,
  XCircle,
} from "lucide-react";
import type { pipeline } from "@/lib/wailsjs/go/models";
import type {
  GatePayload,
  GateDecision,
  FixProgressState,
  FixActivityItem,
  FixIssuePayload,
} from "@/types/pipeline";
import PipelineGateCard from "./PipelineGateCard";
import StageActivityPanel from "./StageActivityPanel";

export interface AuditIssue {
  id: string;
  level: string;
  area: string;
  desc: string;
}

export interface AuditData {
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
  // S5 修复进度（可选）：修复清单 + 实时活动流，由 usePipeline 订阅事件维护。
  fixProgress?: FixProgressState;
  fixActivity?: FixActivityItem[];
}

// S4 签约体检 / S5 修复闭环 / S6 完本终检视图：
// - 后端异步跑 signability-audit（只读体检）期间显示「体检中」；
// - 完成后经 G4/G终 闸门携带 issue 清单（gate.summary 或 run.ContextJSON.audit）渲染 verdict + 问题列表；
// - S5 修复期间显示「修复中」，修复完成自动回到 S4 复评（仍由 G4 闸门交回用户）；
// - S6 完本终检复用相同样式，终检闸门（G终）提供 finish/fix/abort。
// 非 S4/S4b/S5/S6 阶段返回 null，由父组件按原逻辑渲染。
export default function SignabilityAuditView({
  run,
  stage,
  status,
  gate,
  onResume,
  fixProgress,
  fixActivity = [],
}: Props) {
  const { t } = useTranslation();

  if (stage !== "S4" && stage !== "S4b" && stage !== "S5" && stage !== "S6")
    return null;

  const isFix = stage === "S5";
  const isFinal = stage === "S6";

  // 生成中 / 修复中：对应阶段 running 且无闸门。
  // 与 S5 修复视图保持同一界面语言：spinner + 正在干什么 + 实时动态面板。
  if (status === "running" && !gate) {
    if (isFix) {
      // S5 修复进度视图：修复清单（什么问题、涉及哪些元素）+ 实时活动流 + 结果。
      return (
        <FixProgressView fixProgress={fixProgress} fixActivity={fixActivity} />
      );
    }
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span className="text-sm font-medium">
            {t(
              isFinal
                ? "pipeline.signability.finalAuditing"
                : "pipeline.signability.auditing",
            )}
          </span>
        </div>
        <p className="text-muted-foreground text-xs">
          {t("pipeline.stageActivity.hint")}
        </p>
        <StageActivityPanel activity={fixActivity} />
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
          {t(
            isFinal
              ? "pipeline.signability.finalTitle"
              : "pipeline.signability.title",
          )}
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

// StockAuditPanel 以只读方式展示「全量对齐」触发的全本现状体检结果（区别于 S4 流水线体检），
// 不挂载任何闸门按钮，仅用于让用户快速查看存量书的签约级健康度。
export function StockAuditPanel({ data }: { data: AuditData }) {
  const { t } = useTranslation();
  const issues = data?.issues ?? [];
  const passed = data?.verdict === "pass";
  return (
    <div className="flex flex-col gap-3 rounded border bg-card p-4">
      <div className="flex items-center gap-2">
        {passed ? (
          <ShieldCheck className="h-4 w-4 text-warning-foreground" />
        ) : (
          <ShieldAlert className="h-4 w-4 text-danger-foreground" />
        )}
        <span className="text-sm font-medium">
          {t("pipeline.stockAudit.title")}
        </span>
        {data?.score != null ? (
          <span className="text-xs text-muted-foreground">
            {t("pipeline.signability.score", { n: data.score })}
          </span>
        ) : null}
      </div>

      {data?.summary ? (
        <p className="whitespace-pre-wrap text-sm text-muted-foreground">
          {data.summary}
        </p>
      ) : null}

      <div className="flex flex-col gap-2">
        <div className="text-xs font-medium text-muted-foreground">
          {t("pipeline.stockAudit.issues")}
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
            {t("pipeline.stockAudit.noIssues")}
          </p>
        )}
      </div>
    </div>
  );
}

// FixIssueStatusBadge 是修复清单里单个问题的状态徽章：
// 待修复（灰） / 正在修复（主色，spinner） / 已修复（绿）。
function FixIssueStatusBadge({
  status,
}: {
  status?: FixIssuePayload["status"];
}) {
  const { t } = useTranslation();
  if (status === "fixed") {
    return (
      <span className="ml-auto flex shrink-0 items-center gap-1 rounded bg-success/10 px-1.5 py-0.5 text-success-foreground text-xs">
        <CheckCircle2 className="h-3 w-3" />
        {t("pipeline.fixStatus.fixed")}
      </span>
    );
  }
  if (status === "fixing") {
    return (
      <span className="ml-auto flex shrink-0 items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 text-primary text-xs">
        <Loader2 className="h-3 w-3 animate-spin" />
        {t("pipeline.fixStatus.fixing")}
      </span>
    );
  }
  return (
    <span className="ml-auto flex shrink-0 items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-muted-foreground text-xs">
      <Clock className="h-3 w-3" />
      {t("pipeline.fixStatus.pending")}
    </span>
  );
}

// FixProgressView 是 S5「按体检问题修复中」的进度视图：
// - 顶部：spinner + 状态文案（修复中 + 批次 / 修复完成待复评 / 修复失败）；
// - 修复清单：全量问题逐项标注 待修复 / 正在修复 / 已修复（含涉及元素 area），
//   让用户随时知道「LLM 做到哪了、在修什么、涉及角色/地点/弧线/读者视角中的哪些元素」；
// - 实时活动流：StageActivityPanel（与其它阶段共用，最新在上）。
function FixProgressView({
  fixProgress,
  fixActivity,
}: {
  fixProgress?: FixProgressState;
  fixActivity: FixActivityItem[];
}) {
  const { t } = useTranslation();
  // 注意初始态（running=false, ok=null）必须视为「未开始」而非「已完成」：
  // 应用重启后事件状态丢失，若把初始态渲染成 done 会给用户"修复完成在复评"的假象。
  const running = fixProgress?.running ?? false;
  const idle = !running && fixProgress?.ok == null;
  // 优先用全量问题清单（带逐项状态）；旧事件缺失时退化为本批清单。
  const issues =
    fixProgress?.allIssues && fixProgress.allIssues.length > 0
      ? fixProgress.allIssues
      : (fixProgress?.issues ?? []);
  const fixedCount = issues.filter((it) => it.status === "fixed").length;
  const batchTotal = fixProgress?.batchTotal ?? 0;
  const batchIndex = fixProgress?.batchIndex ?? 0;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        {fixProgress?.ok === false ? (
          <XCircle className="h-4 w-4 text-danger-foreground" />
        ) : running ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : idle ? (
          <Hourglass className="h-4 w-4 text-muted-foreground" />
        ) : (
          <CheckCircle2 className="h-4 w-4 text-warning-foreground" />
        )}
        <span className="text-sm font-medium">
          {fixProgress?.ok === false
            ? t("pipeline.fixProgress.failed")
            : running
              ? batchTotal > 1
                ? `${t("pipeline.signability.fixing")} ${t("pipeline.fixProgress.batch", { index: batchIndex, total: batchTotal })}`
                : t("pipeline.signability.fixing")
              : idle
                ? t("pipeline.fixProgress.idle")
                : t("pipeline.fixProgress.done")}
        </span>
      </div>

      {fixProgress?.ok === false && fixProgress?.error ? (
        <p className="rounded border border-danger bg-danger/10 px-3 py-2 text-danger-foreground text-sm">
          {fixProgress.error}
        </p>
      ) : null}

      {fixProgress?.ok === false ? (
        <p className="flex items-center gap-1 text-muted-foreground text-xs">
          <Wrench className="h-3 w-3" />
          {t("pipeline.fixProgress.resumeHint")}
        </p>
      ) : null}

      {!running && !idle && fixProgress?.ok !== false ? (
        <p className="flex items-center gap-1 text-muted-foreground text-xs">
          <Wrench className="h-3 w-3" />
          {t("pipeline.fixProgress.reAuditing")}
        </p>
      ) : null}

      {issues.length > 0 ? (
        <div className="flex flex-col gap-2">
          <div className="text-xs font-medium text-muted-foreground">
            {t("pipeline.fixProgress.checklist", {
              fixed: fixedCount,
              total: issues.length,
            })}
          </div>
          <ul className="flex flex-col gap-2">
            {issues.map((it) => (
              <li
                key={it.id}
                className={`rounded border bg-card px-3 py-2 text-sm ${
                  it.status === "fixing" ? "border-primary/40" : ""
                }`}
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
                  <FixIssueStatusBadge status={it.status} />
                </div>
                <p className="mt-1 text-muted-foreground">{it.desc}</p>
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      {!idle ? <StageActivityPanel activity={fixActivity} /> : null}
    </div>
  );
}
