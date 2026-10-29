import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, Radar, ShieldAlert, ShieldCheck } from "lucide-react";
import type { AuditData } from "@/types/pipeline";
import { IssueScopeTags } from "./IssueTags";

// PatrolReportPanel 展示全书一致性巡检（WBS 3.3「张力报告」）：
// 对照「已冻结卷既定事实」与当前台账/正文，报告前向适配累计出来的漂移张力
// （设定失真/承诺未兑现/读者认知被悄悄改写/时间线漂移/retcon 衔接裂缝）。
// **只报告不改写**：报告不进任何自动修复路由，改不改由作者定；
// 仅渲染建议方向（direction 标签），引导优先前向适配。
export default function PatrolReportPanel({
  data,
  tick,
  onRun,
}: {
  // 最近一次张力报告（run.ContextJSON.patrol_report 解析结果，可空）。
  data: AuditData | null;
  // 一致性巡检完成计数（由 DetectionCorePanel 的 pipeline:consistency_patrol 事件驱动）：变化即清除「运行中」提示。
  tick: number;
  onRun: () => void;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);

  // 每次巡检完成事件到达（tick 变化）都收起「运行中」提示：渲染期重置而非 effect，
  // 避免 set-state-in-effect 级联渲染告警。
  // 巡检失败不广播事件，提示会停留但按钮保持可点，用户可直接重试。
  const [prevTick, setPrevTick] = useState(tick);
  if (prevTick !== tick) {
    setPrevTick(tick);
    setPending(false);
  }

  const issues = data?.issues ?? [];
  return (
    <div className="flex flex-col gap-3 rounded border bg-card p-4">
      <div className="flex items-center gap-2">
        <Radar className="h-4 w-4 text-primary" />
        <span className="text-sm font-medium">
          {t("pipeline.patrol.title")}
        </span>
        {data ? (
          data.verdict === "pass" ? (
            <span className="flex items-center gap-1 rounded bg-success/10 px-1.5 py-0.5 text-success-foreground text-xs">
              <ShieldCheck className="h-3 w-3" />
              {t("pipeline.patrol.verdict.pass")}
            </span>
          ) : data.verdict === "error" ? (
            <span className="flex items-center gap-1 rounded bg-danger/10 px-1.5 py-0.5 text-danger-foreground text-xs">
              <ShieldAlert className="h-3 w-3" />
              {t("pipeline.patrol.verdict.error")}
            </span>
          ) : (
            <span className="flex items-center gap-1 rounded bg-warning/10 px-1.5 py-0.5 text-warning-foreground text-xs">
              <ShieldAlert className="h-3 w-3" />
              {t("pipeline.patrol.verdict.needs_fix")}
            </span>
          )
        ) : null}
        <button
          type="button"
          className="ml-auto inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted"
          onClick={() => {
            setPending(true);
            onRun();
          }}
        >
          {pending ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Radar className="h-3 w-3" />
          )}
          {t("pipeline.patrol.run")}
        </button>
      </div>

      <p className="text-xs text-muted-foreground">
        {t("pipeline.patrol.hint")}
      </p>

      {pending ? (
        <p className="flex items-center gap-1 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          {t("pipeline.patrol.running")}
        </p>
      ) : null}

      {data?.summary ? (
        <p className="whitespace-pre-wrap break-words text-sm text-muted-foreground">
          {data.summary}
        </p>
      ) : null}

      <div className="flex flex-col gap-2">
        <div className="text-xs font-medium text-muted-foreground">
          {t("pipeline.patrol.issues")}
        </div>
        {issues.length > 0 ? (
          <ul className="flex flex-col gap-2">
            {issues
              .filter((it) => it && (it.desc || it.area || it.id))
              .map((it) => (
                <li
                  key={it.id ?? `${it.area}-${it.desc?.slice(0, 12)}`}
                  className="rounded border bg-card px-3 py-2 text-sm"
                >
                  <div className="flex items-center gap-2">
                    {it.level ? (
                      <span
                        className={`rounded px-1.5 py-0.5 text-xs ${
                          it.level === "P0"
                            ? "bg-danger text-danger-foreground"
                            : "bg-warning text-warning-foreground"
                        }`}
                      >
                        {it.level}
                      </span>
                    ) : null}
                    {it.area ? (
                      <span className="font-medium">{it.area}</span>
                    ) : null}
                    {it.id ? (
                      <span className="text-muted-foreground">{it.id}</span>
                    ) : null}
                  </div>
                  {it.desc ? (
                    <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">
                      {it.desc}
                    </p>
                  ) : null}
                  <IssueScopeTags issue={it} />
                </li>
              ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">
            {t(data ? "pipeline.patrol.noIssues" : "pipeline.patrol.noReport")}
          </p>
        )}
      </div>
    </div>
  );
}
