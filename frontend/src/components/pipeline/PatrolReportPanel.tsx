import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ClipboardCheck,
  Loader2,
  Radar,
  ShieldAlert,
  ShieldCheck,
  Wrench,
} from "lucide-react";
import {
  ListIssueTickets,
  StartIssueFixTickets,
  VerifyIssueTickets,
} from "@/lib/wailsjs/go/app/App";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import type { AuditData, AuditIssue } from "@/types/pipeline";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { IssueScopeTags } from "./IssueTags";

// PatrolReportPanel 展示全书一致性巡检（WBS 3.3「张力报告」）：
// 对照「已冻结卷既定事实」与当前台账/正文，报告前向适配累计出来的漂移张力
// （设定失真/承诺未兑现/读者认知被悄悄改写/时间线漂移/retcon 衔接裂缝）。
// **只报告不改写**：报告不进任何自动修复路由，改不改由作者定；
// 仅渲染建议方向（direction 标签），引导优先前向适配。
// 张力项同步沉淀为发现项工单（patrol.go ReplaceIssues），卡片提供**人工触发**的
// 单条「去修复 / 复验」入口（复用工单闭环）：去修复=把该条清理内容发到中央聊天并
// 标记工单「修复中」；复验=定点核验修复效果落复验回执。均不自动改稿（人工在环）。
export default function PatrolReportPanel({
  data,
  tick,
  onRun,
  independentNote,
  novelId,
  runId,
}: {
  // 最近一次张力报告（run.ContextJSON.patrol_report 解析结果，可空）。
  data: AuditData | null;
  // 一致性巡检完成计数（由 DetectionCorePanel 的 pipeline:consistency_patrol 事件驱动）：变化即清除「运行中」提示。
  tick: number;
  onRun: () => void;
  // 可选：当本面板从其它检测（如签约潜力）入口到达时，标注其为独立模块，
  // 避免误判为同源发现项。默认不渲染。
  independentNote?: string;
  // 小说 ID：用于过滤 pipeline:patrol_progress 事件（后端批量取语料进度），
  // 以及单条「去修复/复验」工单联动。
  novelId?: number;
  // 本次张力报告的 run_id（GetPatrolReport 双源合并视图携带）：单条「去修复/复验」
  // 以 {run_id, issue_key} 定位对应发现项工单；缺省时隐藏工单联动按钮（纯只读展示）。
  runId?: string;
}) {
  const { t } = useTranslation();
  const [pending, setPending] = useState(false);
  const [progress, setProgress] = useState<{
    phase: string;
    total: number;
    done: number;
  } | null>(null);
  // 单条动作进行态：busyKey = "fix:<id>" | "verify:<id>"（派发瞬间的按钮转圈）。
  const [busyKey, setBusyKey] = useState<string | null>(null);
  // 后台复验在飞的张力项 id（verify_done/verify_cancelled 事件清空）。
  const [verifyingId, setVerifyingId] = useState<string | null>(null);

  // 订阅一致性巡检进度（后端批量取语料阶段）：读取正文 N/M、分析中，避免面板死转圈。
  useEffect(() => {
    if (!novelId) return;
    const off = EventsOn(
      "pipeline:patrol_progress",
      (p: {
        novel_id?: number;
        run_id?: string;
        phase?: string;
        total?: number;
        done?: number;
      }) => {
        if (p?.novel_id !== novelId) return;
        setProgress({
          phase: p.phase ?? "",
          total: p.total ?? 0,
          done: p.done ?? 0,
        });
      },
    );
    return off;
  }, [novelId]);

  // 订阅复验收口事件（run 级）：清「复验中」态。结果 toast 由 IssueTicketsPanel
  // 统一弹出（两面板同屏常驻，避免双 toast）；本面板只收进行态。
  useEffect(() => {
    if (!novelId || !runId) return;
    const match = (p: { novel_id?: number; run_id?: string }) =>
      p?.novel_id === novelId && p.run_id === runId;
    const offDone = EventsOn(
      "pipeline:verify_done",
      (p: { novel_id?: number; run_id?: string }) => {
        if (match(p)) setVerifyingId(null);
      },
    );
    const offCancel = EventsOn(
      "pipeline:verify_cancelled",
      (p: { novel_id?: number; run_id?: string }) => {
        if (match(p)) setVerifyingId(null);
      },
    );
    return () => {
      offDone?.();
      offCancel?.();
    };
  }, [novelId, runId]);

  // 每次巡检完成事件到达（tick 变化）都收起「运行中」提示并清进度：渲染期重置而非 effect，
  // 避免 set-state-in-effect 级联渲染告警。
  // 巡检失败也会广播完成事件（落地 verdict=error），提示与进度同样收起，面板展示失败原因。
  const [prevTick, setPrevTick] = useState(tick);
  if (prevTick !== tick) {
    setPrevTick(tick);
    setPending(false);
    setProgress(null);
  }

  // 单条「去修复」（#270 语义单条版）：把该张力项的清理内容发到中央聊天（最重要的
  // 下一步，先发不吞），并把对应工单翻为「修复中」留痕；旧报告无对应工单时退化为
  // 纯聊天发送（不丢下一步），绝不阻断人工修复入口。
  const onFixOne = useCallback(
    async (it: AuditIssue) => {
      if (!it.id || !novelId || !runId) return;
      const key = `fix:${it.id}`;
      setBusyKey(key);
      try {
        const ch = it.chapter_ids ?? [];
        const chText = ch.length
          ? `  ${t("pipeline.tickets.chapters", { list: ch.join(", ") })}`
          : "";
        const item = `1. [${it.id}] ${it.area ?? ""}\n    ${t(
          "pipeline.tickets.fixPromptDesc",
        )} ${it.desc ?? ""}${chText}`;
        const content = `${t("pipeline.patrol.fixPromptTitle")}\n\n${item}\n\n${t(
          "pipeline.tickets.fixPromptFooter",
        )}`;
        useChatLaunchStore.getState().send?.(content);
        const ref = { run_id: runId, issue_key: it.id };
        const rows = (await ListIssueTickets(novelId)) ?? [];
        const hit = rows.some(
          (r) => r.run_id === ref.run_id && r.issue_key === ref.issue_key,
        );
        if (hit) {
          await StartIssueFixTickets(novelId, [ref]);
          toastSuccess(t("pipeline.patrol.fixSent"));
        } else {
          toastInfo(t("pipeline.patrol.fixNoTicket"));
        }
      } catch (error) {
        toastError(toErrorMessage(error));
      } finally {
        setBusyKey(null);
      }
    },
    [novelId, runId, t],
  );

  // 单条「复验」（复用工单复验闭环）：定点核验该张力项的修复效果，逐条落复验回执；
  // 只记结论不自动回流修复。无对应工单（旧报告未落表）时给出明确提示。
  const onVerifyOne = useCallback(
    async (it: AuditIssue) => {
      if (!it.id || !novelId || !runId) return;
      const key = `verify:${it.id}`;
      setBusyKey(key);
      try {
        const ref = { run_id: runId, issue_key: it.id };
        const rows = (await ListIssueTickets(novelId)) ?? [];
        const hit = rows.some(
          (r) => r.run_id === ref.run_id && r.issue_key === ref.issue_key,
        );
        if (!hit) {
          toastInfo(t("pipeline.patrol.verifyNoTicket"));
          return;
        }
        // 先登记在飞态再派发，避免后端已置 verifying、前端短暂无状态（与工单面板同则）。
        setVerifyingId(it.id);
        await VerifyIssueTickets(novelId, [ref]);
        toastInfo(t("pipeline.tickets.verifyStarted", { count: 1 }));
      } catch (error) {
        setVerifyingId(null);
        toastError(toErrorMessage(error));
      } finally {
        setBusyKey(null);
      }
    },
    [novelId, runId, t],
  );

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

      {independentNote ? (
        <p className="rounded border border-border bg-muted/40 px-2 py-1 text-xs text-muted-foreground">
          {independentNote}
        </p>
      ) : null}

      {pending ? (
        <p className="flex items-center gap-1 text-xs text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" />
          {progress && progress.phase === "reading"
            ? t("pipeline.patrol.progress.reading", {
                done: progress.done,
                total: progress.total,
              })
            : progress && progress.phase === "analyzing"
              ? t("pipeline.patrol.progress.analyzing")
              : t("pipeline.patrol.running")}
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
                    {it.id && verifyingId === it.id ? (
                      <span className="flex items-center gap-1 text-primary text-xs">
                        <Loader2 className="h-3 w-3 animate-spin" />
                        {t("pipeline.tickets.verifying")}
                      </span>
                    ) : null}
                  </div>
                  {it.desc ? (
                    <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">
                      {it.desc}
                    </p>
                  ) : null}
                  <IssueScopeTags issue={it} />
                  {/* 单条人工入口（复用工单闭环）：去修复=发聊天+标修复中；复验=定点核验。
                      仅在能定位工单（runId+issue.id）时渲染；动作均人工触发，不自动改稿。 */}
                  {runId && novelId && it.id ? (
                    <div className="mt-2 flex items-center gap-2">
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
                        disabled={busyKey !== null}
                        onClick={() => void onFixOne(it)}
                      >
                        {busyKey === `fix:${it.id}` ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <Wrench className="h-3 w-3" />
                        )}
                        {t("pipeline.tickets.fix")}
                      </button>
                      <button
                        type="button"
                        className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
                        disabled={busyKey !== null || verifyingId !== null}
                        onClick={() => void onVerifyOne(it)}
                      >
                        {busyKey === `verify:${it.id}` ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <ClipboardCheck className="h-3 w-3" />
                        )}
                        {t("pipeline.tickets.verify")}
                      </button>
                    </div>
                  ) : null}
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
