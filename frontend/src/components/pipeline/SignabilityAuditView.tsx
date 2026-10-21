import { useTranslation } from "react-i18next";
import {
  AlertTriangle,
  CheckCircle2,
  CircleSlash2,
  Clock,
  Hourglass,
  Loader2,
  PenLine,
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
  RewriteItem,
  RewriteProgressState,
} from "@/types/pipeline";
import PipelineGateCard from "./PipelineGateCard";
import StageActivityPanel from "./StageActivityPanel";

export interface AuditIssue {
  id: string;
  level: string;
  area: string;
  desc: string;
  // issue 三枚举（卷级作用域）：target_scope=落点卷作用域（active_volume|frozen_volume|none）、
  // dimension=四维机器归属（character|locus_arc|timeline|reader_pov|none）、
  // direction=修复方向（forward_fix|retcon_candidate）。旧版体检数据可能缺省。
  target_scope?: string;
  dimension?: string;
  direction?: string;
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
  // 自动章节重写回路进度（可选）：重写清单指向可定位章节时后端全自动重写，展示逐章进度。
  rewriteProgress?: RewriteProgressState;
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
  rewriteProgress,
}: Props) {
  const { t } = useTranslation();

  if (stage !== "S4" && stage !== "S4b" && stage !== "S5" && stage !== "S6")
    return null;

  const isFix = stage === "S5";
  const isFinal = stage === "S6";
  // 读取本次体检/修复种类（蓝图 or 签约），用于切换标题与文案。
  const ctx = run?.ContextJSON ? parseCtx(run.ContextJSON) : null;
  const auditKind: "blueprint" | "signability" =
    ctx?.audit_kind === "blueprint" ? "blueprint" : "signability";

  // 生成中 / 修复中：对应阶段 running 且无闸门。
  // 与 S5 修复视图保持同一界面语言：spinner + 正在干什么 + 实时动态面板。
  // S5 在暂停态也渲染修复进度视图：陈旧体检守卫拒绝派发后会把 run 落回
  // paused，若只在 running 渲染，stale/失败告警会一闪而过（用户感知为
  // 「点了继续没反应」）。暂停时保留最近一次修复结果与告警 + 恢复提示。
  if (!gate && isFix && (status === "running" || status === "paused")) {
    // S5 修复进度视图：修复清单（什么问题、涉及哪些元素）+ 实时活动流 + 结果。
    return (
      <FixProgressView
        fixProgress={fixProgress}
        fixActivity={fixActivity}
        fixKind={auditKind}
        rewriteItems={ctx?.rewrite_backlog as RewriteItem[] | undefined}
        rewriteProgress={rewriteProgress}
      />
    );
  }
  if (status === "running" && !gate) {
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

  return (
    <div className="flex flex-col gap-4">
      <AuditResultView
        audit={audit}
        titleKey={
          isFinal
            ? "pipeline.signability.finalTitle"
            : "pipeline.signability.title"
        }
        scoreKey={
          auditKind === "blueprint"
            ? "pipeline.blueprint.score"
            : "pipeline.signability.score"
        }
      />
      {/* 正文级问题回流 S3 重写回路清单：仅签约类体检（非蓝图）有此出口。 */}
      {auditKind === "signability" &&
      ctx?.rewrite_backlog &&
      (ctx.rewrite_backlog as RewriteItem[]).length > 0 ? (
        <RewriteBacklogPanel items={ctx.rewrite_backlog as RewriteItem[]} />
      ) : null}
      {isFix && status !== "running" ? (
        <p className="flex items-center gap-1 text-xs text-muted-foreground">
          <Wrench className="h-3 w-3" />
          {t(
            auditKind === "blueprint"
              ? "pipeline.blueprint.fixNote"
              : "pipeline.signability.fixNote",
          )}
        </p>
      ) : null}
      {gate ? <PipelineGateCard gate={gate} onResume={onResume} /> : null}
    </div>
  );
}

// AuditResultView 是体检结果（verdict + score + issue 清单）的共享渲染组件，
// S4/S4b/S6 签约体检与 S2 蓝图体检（G1b）共用，保证界面语言一致。
// 通过 titleKey / scoreKey 区分签约与蓝图的标题与分数分母（/35 vs /30）。
export function AuditResultView({
  audit,
  titleKey = "pipeline.signability.title",
  scoreKey = "pipeline.signability.score",
}: {
  audit: AuditData | null;
  titleKey?: string;
  scoreKey?: string;
}) {
  const { t } = useTranslation();
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
        <span className="text-sm font-medium">{t(titleKey)}</span>
        {audit?.score != null ? (
          <span className="text-xs text-muted-foreground">
            {t(scoreKey, { n: audit.score })}
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
        ) : audit ? (
          <p className="text-sm text-muted-foreground">
            {t("pipeline.signability.noIssues")}
          </p>
        ) : (
          // 无体检数据（体检失败/尚未跑完）时不得渲染「未发现明显问题」误导用户。
          <p className="text-sm text-muted-foreground">
            {t("pipeline.signability.noAudit")}
          </p>
        )}
      </div>
    </div>
  );
}

// RewriteBacklogPanel 展示回流到 S3 章节重写回路的正文级问题清单（与后端 RewriteBacklog 对应）。
// 这些问题无法靠 S5 台账/单章补丁修好，已被显式路由到写作回路，须由用户在 S3 重写对应章节，
// 闭合「审计→修台账→复评→正文重写→复评」的完整修复链。
function RewriteBacklogPanel({ items }: { items: RewriteItem[] }) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2 rounded border border-dashed border-warning/50 bg-warning/5 p-4">
      <div className="flex items-center gap-2">
        <PenLine className="h-4 w-4 text-warning-foreground" />
        <span className="text-sm font-medium">
          {t("pipeline.rewriteBacklog.title")}
        </span>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("pipeline.rewriteBacklog.hint")}
      </p>
      <ul className="flex flex-col gap-2">
        {items.map((it) => (
          <li key={it.id} className="rounded border bg-card px-3 py-2 text-sm">
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
            <p className="mt-1 text-xs text-muted-foreground">
              {it.chapters && it.chapters.length > 0
                ? t("pipeline.rewriteBacklog.chapters", {
                    chapters: it.chapters.join("、"),
                  })
                : t("pipeline.rewriteBacklog.noChapters")}
            </p>
          </li>
        ))}
      </ul>
    </div>
  );
}

// parseCtx 安全解析 run.ContextJSON 为对象（解析失败返回 null），避免单次 JSON 错误中断渲染。
export function parseCtx(raw?: string): Record<string, unknown> | null {
  if (!raw) return null;
  try {
    return JSON.parse(raw) as Record<string, unknown>;
  } catch {
    return null;
  }
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
// 待修复（灰） / 正在修复（主色，spinner） / 已修复（绿） / 已跳过（灰黄，agent 上报无法修复）。
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
  if (status === "skipped") {
    return (
      <span className="ml-auto flex shrink-0 items-center gap-1 rounded bg-warning/10 px-1.5 py-0.5 text-warning-foreground text-xs">
        <CircleSlash2 className="h-3 w-3" />
        {t("pipeline.fixStatus.skipped")}
      </span>
    );
  }
  if (status === "rewrite") {
    return (
      <span className="ml-auto flex shrink-0 items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 text-primary text-xs">
        <PenLine className="h-3 w-3" />
        {t("pipeline.fixStatus.rewrite")}
      </span>
    );
  }
  if (status === "retcon") {
    return (
      <span className="ml-auto flex shrink-0 items-center gap-1 rounded bg-warning/10 px-1.5 py-0.5 text-warning-foreground text-xs">
        <ShieldAlert className="h-3 w-3" />
        {t("pipeline.fixStatus.retcon")}
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
  fixKind = "signability",
  rewriteItems,
  rewriteProgress,
}: {
  fixProgress?: FixProgressState;
  fixActivity: FixActivityItem[];
  fixKind?: "signability" | "blueprint";
  rewriteItems?: RewriteItem[];
  rewriteProgress?: RewriteProgressState;
}) {
  const { t } = useTranslation();
  const fixingLabel = t(`pipeline.${fixKind}.fixing`);
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
        {fixProgress?.stale ? (
          <AlertTriangle className="h-4 w-4 text-warning-foreground" />
        ) : fixProgress?.rewriteHandoff ? (
          <PenLine className="h-4 w-4 text-warning-foreground" />
        ) : fixProgress?.retconHandoff ? (
          <ShieldAlert className="h-4 w-4 text-warning-foreground" />
        ) : fixProgress?.ok === false ? (
          <XCircle className="h-4 w-4 text-danger-foreground" />
        ) : running ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : idle ? (
          <Hourglass className="h-4 w-4 text-muted-foreground" />
        ) : (
          <CheckCircle2 className="h-4 w-4 text-warning-foreground" />
        )}
        <span className="text-sm font-medium">
          {fixProgress?.stale
            ? t("pipeline.fixProgress.stale")
            : fixProgress?.rewriteHandoff
              ? t("pipeline.fixProgress.rewriteHandoff", {
                  count: fixProgress?.rewriteCount ?? 0,
                })
              : fixProgress?.retconHandoff
                ? t("pipeline.fixProgress.retconHandoff", {
                    count: fixProgress?.retconCount ?? 0,
                  })
                : fixProgress?.ok === false
                  ? t("pipeline.fixProgress.failed")
                  : running
                    ? batchTotal > 1
                      ? `${fixingLabel} ${t("pipeline.fixProgress.batch", { index: batchIndex, total: batchTotal })}`
                      : fixingLabel
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

      {fixProgress?.stale ? (
        <p className="flex items-center gap-1 text-muted-foreground text-xs">
          <AlertTriangle className="h-3 w-3" />
          {t("pipeline.fixProgress.staleHint")}
        </p>
      ) : fixProgress?.ok === false ? (
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
            {issues.map((it) => {
              // 视图层兜底（修复 B）：批次失败 / 非运行态下，后端可能残留
              // status==="fixing" 的条目（修复未真正落地）。此时按 "pending"
              // 展示，避免「修复失败」与「正在修复」徽标并存的矛盾。
              const displayStatus =
                (fixProgress?.ok === false || !running) &&
                it.status === "fixing"
                  ? "pending"
                  : it.status;
              return (
                <li
                  key={it.id}
                  className={`rounded border bg-card px-3 py-2 text-sm ${
                    displayStatus === "fixing" ? "border-primary/40" : ""
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
                    <FixIssueStatusBadge status={displayStatus} />
                  </div>
                  <p className="mt-1 text-muted-foreground">{it.desc}</p>
                </li>
              );
            })}
          </ul>
        </div>
      ) : null}

      {fixProgress?.rewriteHandoff &&
      rewriteItems &&
      rewriteItems.length > 0 ? (
        <RewriteBacklogPanel items={rewriteItems} />
      ) : null}

      {rewriteProgress && rewriteProgress.items.length > 0 ? (
        <RewriteProgressView rewriteProgress={rewriteProgress} />
      ) : null}

      {!idle ? <StageActivityPanel activity={fixActivity} /> : null}
    </div>
  );
}

// RewriteProgressView 展示自动章节重写回路的逐章进度：重写清单指向可定位章节时，
// 后端全自动派发章节重写 agent，本视图显示「正在自动重写第 X/Y 章」与每章结果（done/failed）。
// 与 FixProgressView 共用同一「正在发生什么」的界面语言，避免用户误以为流水线卡死。
function RewriteProgressView({
  rewriteProgress,
}: {
  rewriteProgress: RewriteProgressState;
}) {
  const { t } = useTranslation();
  const { running, index, total, items } = rewriteProgress;
  const doneCount = items.filter((it) => it.phase === "done").length;
  const failedCount = items.filter((it) => it.phase === "failed").length;
  return (
    <div className="flex flex-col gap-3 rounded border border-primary/40 bg-primary/5 p-4">
      <div className="flex items-center gap-2">
        {running ? (
          <Loader2 className="h-4 w-4 animate-spin text-primary" />
        ) : failedCount > 0 ? (
          <XCircle className="h-4 w-4 text-danger-foreground" />
        ) : (
          <CheckCircle2 className="h-4 w-4 text-primary" />
        )}
        <span className="text-sm font-medium">
          {running
            ? t("pipeline.rewriteProgress.rewriting", { index, total })
            : t("pipeline.rewriteProgress.done", {
                done: doneCount,
                total: items.length,
              })}
        </span>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("pipeline.rewriteProgress.hint")}
      </p>
      <ul className="flex flex-col gap-2">
        {items.map((it) => (
          <li
            key={it.chapterId}
            className="flex items-center gap-2 rounded border bg-card px-3 py-1.5 text-sm"
          >
            {it.phase === "done" ? (
              <CheckCircle2 className="h-3.5 w-3.5 text-primary" />
            ) : it.phase === "failed" ? (
              <XCircle className="h-3.5 w-3.5 text-danger-foreground" />
            ) : (
              <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
            )}
            <span className="font-medium">
              {t("pipeline.rewriteProgress.chapter", {
                number: it.readingNumber,
              })}
            </span>
            <span className="text-muted-foreground">
              {it.phase === "done"
                ? t("pipeline.rewriteProgress.chapterDone")
                : it.phase === "failed"
                  ? t("pipeline.rewriteProgress.chapterFailed")
                  : t("pipeline.rewriteProgress.chapterStarting")}
            </span>
            {it.error ? (
              <span className="text-danger-foreground text-xs">{it.error}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}
