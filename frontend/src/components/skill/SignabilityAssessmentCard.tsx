import { useTranslation } from "react-i18next";
import { usePanelStore } from "@/stores/usePanelStore";
import type { pipeline } from "@/lib/wailsjs/go/models";

// verdict → i18n key（book/volume=pass|needs_fix|error；章节质量档案=checked|pending|
// skipped）。未知值如实显示原值（宁缺勿错不造词）。
const VERDICT_KEYS: Record<string, string> = {
  pass: "journey.quality.detect.verdict.pass",
  needs_fix: "journey.quality.detect.verdict.needs_fix",
  checked: "journey.quality.detect.verdict.checked",
  pending: "journey.quality.detect.verdict.pending",
  skipped: "journey.quality.detect.verdict.skipped",
  error: "journey.quality.detect.verdict.error",
};

// 工单状态徽章配色（与 IssueTicketsPanel 同口径）：内联评估只读展示用。
const STATUS_CLS: Record<string, string> = {
  pending: "bg-warning/10 text-warning-foreground",
  confirmed: "bg-primary/10 text-primary",
  fixing: "bg-primary/10 text-primary",
  fixed: "bg-primary/10 text-primary",
  verified: "bg-success/10 text-success-foreground",
  dismissed: "bg-muted text-muted-foreground",
  skipped: "bg-muted text-muted-foreground",
  rewrite: "bg-warning/10 text-warning-foreground",
  retcon: "bg-warning/10 text-warning-foreground",
};

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// 签约潜力评估八维口径（与 signability-audit 技能 v2 评分维度逐字一致）：
// 开篇过签力/题材契合/人设卖点/节奏爽点/设定自洽/长线牵引/平台合规/读者视角。
const SIGN_DIMENSIONS = [
  "opening",
  "fit",
  "character",
  "pacing",
  "world",
  "longArc",
  "compliance",
  "reader",
] as const;

interface Props {
  status: pipeline.DetectionTaskStatus;
  inlineIssues: pipeline.PipelineIssue[];
  /** 作用域铭牌（卷体检传卷名，书体检留空）。仅作展示，不改变评估口径。 */
  scopeLabel?: string;
}

// 签约潜力评估卡（书/卷体检共用）：score/40 + 结论 + 八维口径 + 本 run 发现项预览 +
// 查看工单入口。书体检与卷体检跑同一 signability-audit 技能、产物形态一致，故共用一张
// 卡（仅 scope 铭牌不同）——避免重复面板，并修复卷体检此前不渲染评分/八维、只给通用
// 工单预览的缺口（数据里 last_score/last_summary/八维本就存在，只是前端没呈现）。
export default function SignabilityAssessmentCard({
  status,
  inlineIssues,
  scopeLabel,
}: Props) {
  const { t } = useTranslation();
  const setActivePanel = usePanelStore((s) => s.setActivePanel);

  return (
    <div className="mt-4 rounded-lg border p-3 max-w-3xl">
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium text-foreground">
          {t("signability.evaluation")}
        </span>
        {scopeLabel ? (
          <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary">
            {scopeLabel}
          </span>
        ) : null}
        {status.last_verdict ? (
          <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
            {VERDICT_KEYS[status.last_verdict]
              ? t(VERDICT_KEYS[status.last_verdict])
              : status.last_verdict}
          </span>
        ) : null}
      </div>

      {/* 评分 + 结论 + 八维口径：书/卷共用，仅 scope 铭牌区分 */}
      {typeof status.last_score === "number" ? (
        <div className="mt-2 flex items-baseline gap-1.5">
          <span className="text-xs text-muted-foreground">
            {t("signability.scoreLabel")}
          </span>
          <span className="text-lg font-semibold leading-none text-foreground">
            {status.last_score}
            <span className="text-xs font-normal text-muted-foreground">
              /40
            </span>
          </span>
        </div>
      ) : null}
      {status.last_summary ? (
        <p className="mt-1.5 text-sm leading-relaxed text-foreground/90">
          {status.last_summary}
        </p>
      ) : null}
      <div className="mt-3">
        <p className="text-xs font-medium text-foreground">
          {t("signability.dimensionTitle")}
        </p>
        <ul className="mt-1 grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-2">
          {SIGN_DIMENSIONS.map((d) => (
            <li
              key={d}
              className="flex items-center gap-1.5 text-xs text-muted-foreground"
            >
              <span className="h-1 w-1 shrink-0 rounded-full bg-primary/60" />
              {t(`signability.dimensions.${d}`)}
            </li>
          ))}
        </ul>
      </div>

      <p className="mt-3 text-xs text-muted-foreground">
        {t("signability.evaluationHint")}
      </p>
      <p className="mt-1 text-xs text-muted-foreground/70">
        {t("journey.quality.detect.lastRun", {
          at: fmtTime(status.last_run_at),
          count: status.last_issue_count ?? 0,
        })}
      </p>

      {inlineIssues.length > 0 ? (
        <ul className="mt-2 flex flex-col gap-1.5">
          {inlineIssues.slice(0, 12).map((r) => (
            <li
              key={r.id}
              className="rounded border bg-card px-2 py-1.5 text-xs"
            >
              <div className="flex items-center gap-2">
                <span
                  className={`rounded px-1.5 py-0.5 text-[11px] ${
                    STATUS_CLS[r.status] ?? "bg-muted text-muted-foreground"
                  }`}
                >
                  {t(`pipeline.tickets.status.${r.status}`)}
                </span>
                <span className="shrink-0 font-medium">{r.issue_key}</span>
                <span className="truncate font-medium">{r.area}</span>
              </div>
              <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">
                {r.desc}
              </p>
            </li>
          ))}
        </ul>
      ) : null}

      <div className="mt-3">
        <button
          type="button"
          onClick={() => setActivePanel("pipeline")}
          className="rounded-lg border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:border-foreground/20 hover:bg-muted/50"
        >
          {t("journey.quality.detect.viewTickets")}
        </button>
      </div>
    </div>
  );
}
