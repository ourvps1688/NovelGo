import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ClipboardCheck,
  Loader2,
  Radar,
  ShieldAlert,
  ShieldCheck,
  Wrench,
} from "lucide-react";
import type { pipeline } from "@/lib/wailsjs/go/models";
import {
  CloseIssueFixTickets,
  ListIssueTickets,
  StartManualIssueFixTickets,
  VerifyIssueTickets,
} from "@/lib/wailsjs/go/app/App";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import type { AuditData, AuditIssue } from "@/types/pipeline";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { IssueScopeTags } from "./IssueTags";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// 工单状态徽章配色（与 IssueTicketsPanel STATUS_CLS 同口径）。
const TICKET_STATUS_CLS: Record<string, string> = {
  pending: "bg-warning/10 text-warning-foreground",
  confirmed: "bg-primary/10 text-primary",
  fixing: "bg-primary/10 text-primary",
  fixed: "bg-primary/10 text-primary",
  verified: "bg-success/10 text-success-foreground",
  dismissed: "bg-muted text-muted-foreground",
  skipped: "bg-muted text-muted-foreground",
  rewrite: "bg-warning/10 text-warning-foreground",
  retcon: "bg-warning/10 text-warning-foreground",
  verifying: "bg-primary/10 text-primary",
};

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
  // 事件回调闭包读不到最新 state，用 ref 判定「本次复验是否本面板派发」（谁派发谁提示）。
  const verifyingIdRef = useRef<string | null>(null);
  // 本 run 的工单行（issue_key → 行）：卡片直接呈现工单状态与复验回执，
  // 让「复验中 → 复验通过/未通过」的收口在卡片上有可见反馈（修复前 spinner 清空即无痕）。
  const [tickets, setTickets] = useState<
    Record<string, pipeline.PipelineIssue>
  >({});

  const loadTickets = useCallback(() => {
    if (!novelId || !runId) return;
    ListIssueTickets(novelId)
      .then((list) => {
        const map: Record<string, pipeline.PipelineIssue> = {};
        for (const r of list ?? [])
          if (r.run_id === runId) map[r.issue_key] = r;
        setTickets(map);
      })
      .catch(() => {
        // 展示增强失败静默降级（不打断主流程）。
      });
  }, [novelId, runId]);

  // tick 变化（巡检完成事件驱动）也重载工单徽章：新张力项落工单后立即可见——
  // run_id 复用时 runId prop 不变，仅靠 loadTickets 依赖不会重拉（陈旧徽章缺口）。
  useEffect(() => {
    loadTickets();
  }, [loadTickets, tick]);

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

  // 订阅工单闭环事件（run 级）：复验收口清「复验中」并**在卡片上落反馈**（状态+回执经
  // loadTickets 刷新呈现）；结果 toast 只在本面板派发的复验时弹出（与 IssueTicketsPanel
  // 「谁派发谁提示」分工，避免双 toast，也避免检测工作流页（无工单面板）零反馈）。
  // fix_start/fix_done 一并刷新：去修复标记/修复收口时卡片状态徽章同步翻转。
  useEffect(() => {
    if (!novelId || !runId) return;
    const match = (p: { novel_id?: number; run_id?: string }) =>
      p?.novel_id === novelId && p.run_id === runId;
    const offDone = EventsOn(
      "pipeline:verify_done",
      (p: {
        novel_id?: number;
        run_id?: string;
        confirmed?: number;
        rejected?: number;
        unverified?: number;
      }) => {
        if (!match(p)) return;
        const mine = verifyingIdRef.current !== null;
        verifyingIdRef.current = null;
        setVerifyingId(null);
        void loadTickets();
        if (mine) {
          toastSuccess(
            t("pipeline.tickets.verifyOk", {
              confirmed: p.confirmed ?? 0,
              rejected: p.rejected ?? 0,
              unverified: p.unverified ?? 0,
            }),
          );
        }
      },
    );
    const offCancel = EventsOn(
      "pipeline:verify_cancelled",
      (p: { novel_id?: number; run_id?: string }) => {
        if (!match(p)) return;
        verifyingIdRef.current = null;
        setVerifyingId(null);
        void loadTickets();
      },
    );
    const offFixStart = EventsOn(
      "pipeline:fix_start",
      (p: { novel_id?: number; run_id?: string }) => {
        if (match(p)) void loadTickets();
      },
    );
    const offFixDone = EventsOn(
      "pipeline:fix_done",
      (p: { novel_id?: number; run_id?: string }) => {
        if (match(p)) void loadTickets();
      },
    );
    return () => {
      offDone?.();
      offCancel?.();
      offFixStart?.();
      offFixDone?.();
    };
  }, [novelId, runId, t, loadTickets]);

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
  // #270 双轨拆分：走人工路径，不派发后台自动修复，chat 修完可直接收口/复验。
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
        )}${t("pipeline.tickets.fixPromptReachability")}`;
        useChatLaunchStore.getState().send?.(content);
        const ref = { run_id: runId, issue_key: it.id };
        const rows = (await ListIssueTickets(novelId)) ?? [];
        const hit = rows.some(
          (r) => r.run_id === ref.run_id && r.issue_key === ref.issue_key,
        );
        if (hit) {
          await StartManualIssueFixTickets(novelId, [ref]);
          toastSuccess(t("pipeline.patrol.fixSent"));
        } else {
          toastInfo(t("pipeline.patrol.fixNoTicket"));
        }
        void loadTickets();
      } catch (error) {
        toastError(toErrorMessage(error));
      } finally {
        setBusyKey(null);
      }
    },
    [novelId, runId, t, loadTickets],
  );

  // 单条「标记已修复」：chat 里人工修完后显式收口（修复中 → 已修复待复验），
  // 之后即可点复验；自动修复执行体在途时由后端拒绝。
  const onCloseOne = useCallback(
    async (it: AuditIssue) => {
      if (!it.id || !novelId || !runId) return;
      const key = `close:${it.id}`;
      setBusyKey(key);
      try {
        await CloseIssueFixTickets(novelId, [
          { run_id: runId, issue_key: it.id },
        ]);
        toastSuccess(t("pipeline.tickets.closeFixOk", { count: 1 }));
        void loadTickets();
      } catch (error) {
        toastError(toErrorMessage(error));
      } finally {
        setBusyKey(null);
      }
    },
    [novelId, runId, t, loadTickets],
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
        verifyingIdRef.current = it.id;
        setVerifyingId(it.id);
        await VerifyIssueTickets(novelId, [ref]);
        toastInfo(t("pipeline.tickets.verifyStarted", { count: 1 }));
      } catch (error) {
        verifyingIdRef.current = null;
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
              : progress && progress.phase === "locating"
                ? t("pipeline.patrol.progress.locating")
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
                    ) : it.id && tickets[it.id] ? (
                      <span
                        className={`rounded px-1.5 py-0.5 text-xs ${
                          TICKET_STATUS_CLS[tickets[it.id].status] ??
                          "bg-muted text-muted-foreground"
                        }`}
                      >
                        {t(`pipeline.tickets.status.${tickets[it.id].status}`)}
                      </span>
                    ) : null}
                  </div>
                  {it.desc ? (
                    <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">
                      {it.desc}
                    </p>
                  ) : null}
                  {/* 复验回执（卡片级反馈）：复验收口后结论直接可见，
                      避免「复验中」消失后卡片无痕（用户须翻工单面板才能看到结果）。 */}
                  {it.id && tickets[it.id]?.verify_receipt ? (
                    <p className="mt-1 whitespace-pre-wrap break-words rounded border border-border bg-muted/40 px-2 py-1 text-xs text-muted-foreground">
                      {t("pipeline.tickets.verifyReceipt")}
                      {tickets[it.id].verify_at
                        ? `（${fmtTime(tickets[it.id].verify_at)}）`
                        : ""}
                      ：{tickets[it.id].verify_receipt}
                    </p>
                  ) : null}
                  {/* 工单备注（P1-C 明示）：复验超时未验证/执行失败、启动清扫中断等后端
                      note 原文在卡片级可见（与工单面板同口径），避免关键状态只藏在面板里。 */}
                  {it.id && tickets[it.id]?.note ? (
                    <p className="mt-1 whitespace-pre-wrap break-words rounded border border-border bg-muted/40 px-2 py-1 text-xs text-muted-foreground">
                      {t("pipeline.tickets.note")}：{tickets[it.id].note}
                    </p>
                  ) : null}
                  <IssueScopeTags issue={it} />
                  {/* 单条人工入口（复用工单闭环）：去修复=发聊天+标修复中（不派自动修复）；
                      标记已修复=人工收口；复验=定点核验。仅在能定位工单时渲染，
                      动作均人工触发，不自动改稿。 */}
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
                        title={t("pipeline.tickets.closeFixHint")}
                        disabled={busyKey !== null || verifyingId !== null}
                        onClick={() => void onCloseOne(it)}
                      >
                        {busyKey === `close:${it.id}` ? (
                          <Loader2 className="h-3 w-3 animate-spin" />
                        ) : (
                          <ClipboardCheck className="h-3 w-3" />
                        )}
                        {t("pipeline.tickets.closeFix")}
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
