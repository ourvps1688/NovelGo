import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ClipboardCheck,
  Loader2,
  Wrench,
  XCircle,
  AlertTriangle,
} from "lucide-react";
import type { pipeline } from "@/lib/wailsjs/go/models";
import {
  CancelIssueVerify,
  ConfirmIssueTickets,
  DismissIssueTickets,
  ListIssueTickets,
  StartIssueFixTickets,
  VerifyIssueTickets,
} from "@/lib/wailsjs/go/app/App";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { useChatLaunchStore } from "@/stores/useChatLaunchStore";
import { useNavigationStore } from "@/stores/useNavigationStore";
import { useSettingsOpenStore } from "@/stores/useSettingsOpenStore";
import { IssueScopeTags } from "./IssueTags";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// 工单状态徽章配色（theme token，与 RetconPanel 同口径）：
// 待行动态（pending/rewrite/retcon）警示色、进行态（confirmed/fixing/fixed）主色、
// 终态里 verified 成功色、dismissed/skipped 弱化。
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
  verifying: "bg-primary/10 text-primary",
};

const TERMINAL = new Set(["verified", "dismissed"]);

function chaptersOf(row: pipeline.PipelineIssue): number[] {
  if (!row.chapter_ids) return [];
  try {
    const v = JSON.parse(row.chapter_ids);
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

function refOf(row: pipeline.PipelineIssue): pipeline.IssueTicketRef {
  return { run_id: row.run_id, issue_key: row.issue_key };
}

// 是否「供应商疑似挂起」：后端熔断哨兵错误（errProviderTripped）文案统一含
// 「疑似供应商挂起」并提示已跳过派发。与真错误区分——挂起可重试/切模型，真错误才纯提示。
function isProviderTrip(msg: string): boolean {
  return msg.includes("疑似供应商挂起") || msg.includes("已跳过派发");
}

// IssueTicketsPanel 是发现项工单面板（R5-1，提案 §10.1/§10.2）：检测产出的问题
// 统统沉淀为工单，**人工修复入口**。按钮显式驱动状态机：发现 → 确认 →（人工）
// 修复中 → 复验通过/驳回。修复动作永远由人触发；「复验」显式点击后按增量复验契约
// 定点核验并落复验回执（只记结论，不自动回流修复）。
export default function IssueTicketsPanel({ novelId }: { novelId: number }) {
  const { t } = useTranslation();
  const [rows, setRows] = useState<pipeline.PipelineIssue[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [showAll, setShowAll] = useState(false);
  const [busy, setBusy] = useState<
    "" | "confirm" | "fix" | "verify" | "dismiss"
  >("");
  const [dismissOpen, setDismissOpen] = useState(false);
  const [dismissReason, setDismissReason] = useState("");
  // 供应商疑似挂起弹窗（#271 中期方案）：复验遇熔断时给可操作弹窗，区分可重试与真错误。
  const [tripOpen, setTripOpen] = useState(false);
  const [tripError, setTripError] = useState("");
  // 复验进行态（审计 P0：后台异步、逐工单、可取消）：按 run_id 跟踪在飞复验与逐工单进度。
  const [verifyingRuns, setVerifyingRuns] = useState<Set<string>>(new Set());
  const [verifyProgress, setVerifyProgress] = useState<
    Record<string, { done: number; total: number }>
  >({});

  const load = useCallback(() => {
    ListIssueTickets(novelId)
      .then((list) => setRows(list ?? []))
      .catch((error) => toastError(toErrorMessage(error)))
      .finally(() => setLoading(false));
  }, [novelId]);

  useEffect(() => {
    load();
  }, [load]);

  // 订阅复验事件流（审计 P0）：progress 推逐工单进度、done 收口并刷新、cancelled 取消清态。
  // 置于 load 声明之后，避免 effect 依赖在 TDZ 内被提前引用（React Compiler/eslint 约束）。
  useEffect(() => {
    if (!novelId) return;
    const offProgress = EventsOn(
      "pipeline:verify_progress",
      (p: {
        novel_id?: number;
        run_id?: string;
        done?: number;
        total?: number;
      }) => {
        if (p?.novel_id !== novelId || !p.run_id) return;
        setVerifyingRuns((prev) => {
          const next = new Set(prev);
          next.add(p.run_id!);
          return next;
        });
        setVerifyProgress((prev) => ({
          ...prev,
          [p.run_id!]: { done: p.done ?? 0, total: p.total ?? 0 },
        }));
      },
    );
    const offDone = EventsOn(
      "pipeline:verify_done",
      (p: {
        novel_id?: number;
        run_id?: string;
        confirmed?: number;
        rejected?: number;
        unverified?: number;
      }) => {
        if (p?.novel_id !== novelId || !p.run_id) return;
        setVerifyingRuns((prev) => {
          const next = new Set(prev);
          next.delete(p.run_id!);
          return next;
        });
        setVerifyProgress((prev) => {
          const next = { ...prev };
          delete next[p.run_id!];
          return next;
        });
        toastSuccess(
          t("pipeline.tickets.verifyOk", {
            confirmed: p.confirmed ?? 0,
            rejected: p.rejected ?? 0,
            unverified: p.unverified ?? 0,
          }),
        );
        load();
      },
    );
    const offCancel = EventsOn(
      "pipeline:verify_cancelled",
      (p: { novel_id?: number; run_id?: string }) => {
        if (p?.novel_id !== novelId || !p.run_id) return;
        setVerifyingRuns((prev) => {
          const next = new Set(prev);
          next.delete(p.run_id!);
          return next;
        });
        setVerifyProgress((prev) => {
          const next = { ...prev };
          delete next[p.run_id!];
          return next;
        });
        toastInfo(t("pipeline.tickets.verifyCancelled"));
        load();
      },
    );
    return () => {
      offProgress?.();
      offDone?.();
      offCancel?.();
    };
  }, [novelId, t, load]);

  // 默认只看未收口工单（verified/dismissed 终态收起），可切「全部」回看历史留痕。
  const visible = useMemo(
    () => (showAll ? rows : rows.filter((r) => !TERMINAL.has(r.status))),
    [rows, showAll],
  );
  const selectedRefs = useMemo(
    () => visible.filter((r) => selected.has(r.id)).map((r) => refOf(r)),
    [visible, selected],
  );
  // 选中工单整行（供「去修复」构造发给聊天的上下文）。
  const selectedRows = useMemo(
    () => visible.filter((r) => selected.has(r.id)),
    [visible, selected],
  );

  const toggle = useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const toggleAll = useCallback(() => {
    setSelected((prev) => {
      const all = visible.map((r) => r.id);
      const done = all.length > 0 && all.every((id) => prev.has(id));
      return done ? new Set() : new Set(all);
    });
  }, [visible]);

  const runAction = useCallback(
    async (
      kind: "confirm" | "fix" | "verify" | "dismiss",
      action: () => Promise<unknown>,
      okMsg: string,
    ) => {
      if (selectedRefs.length === 0) {
        toastInfo(t("pipeline.tickets.selectHint"));
        return;
      }
      setBusy(kind);
      try {
        await action();
        // 复验等动作自携结果 toast（okMsg 为空），避免再弹空成功提示。
        if (okMsg) toastSuccess(okMsg);
        setSelected(new Set());
        setDismissOpen(false);
        setDismissReason("");
        load();
      } catch (error) {
        toastError(toErrorMessage(error));
      } finally {
        setBusy("");
      }
    },
    [selectedRefs.length, load, t],
  );

  const onDismiss = useCallback(() => {
    if (!dismissReason.trim()) {
      toastInfo(t("pipeline.tickets.dismissReasonRequired"));
      return;
    }
    void runAction(
      "dismiss",
      () => DismissIssueTickets(novelId, selectedRefs, dismissReason.trim()),
      t("pipeline.tickets.dismissOk", { count: selectedRefs.length }),
    );
  }, [dismissReason, runAction, novelId, selectedRefs, t]);

  // 「去修复」的下一步（#270）：把选中工单的问题上下文发到中央聊天窗口，给人工明确
  // 的修复入口；同时把状态翻为「修复中」以记录进度。即使状态标记失败（如供应商挂起），
  // 上下文已发到聊天，不丢失下一步。
  const onFix = useCallback(async () => {
    const rows = selectedRows;
    if (rows.length === 0) {
      toastInfo(t("pipeline.tickets.selectHint"));
      return;
    }
    setBusy("fix");
    try {
      const items = rows
        .map((r, i) => {
          const ch = chaptersOf(r);
          const chText = ch.length
            ? `  ${t("pipeline.tickets.chapters", { list: ch.join(", ") })}`
            : "";
          return `${i + 1}. [${r.issue_key}] ${r.area}\n    ${t(
            "pipeline.tickets.fixPromptDesc",
          )} ${r.desc}${chText}`;
        })
        .join("\n\n");
      const content =
        `${t("pipeline.tickets.fixPromptTitle")}\n\n${items}\n\n` +
        t("pipeline.tickets.fixPromptFooter");
      // 先发聊天（最重要的下一步），再标记状态，避免状态标记失败吞掉下一步。
      useChatLaunchStore.getState().send?.(content);
      await StartIssueFixTickets(
        novelId,
        rows.map((r) => refOf(r)),
      );
      toastSuccess(t("pipeline.tickets.fixSent", { count: rows.length }));
      setSelected(new Set());
      setDismissOpen(false);
      setDismissReason("");
      load();
    } catch (error) {
      // 状态标记失败仍保留选中以便重试；聊天上下文已发出。
      toastError(toErrorMessage(error));
      load();
    } finally {
      setBusy("");
    }
  }, [selectedRows, novelId, t, load]);

  // 「复验」审计 P0：入口只派发后台复验（立即返回已派发数），不再整段阻塞 LLM 调用；
  // 进度/结果经 pipeline:verify_progress / verify_done 事件推送，进行态由 verifyingRuns 呈现，
  // 不再全局 busy 冻结面板。供应商熔断不再发生在同步入口（延后到后台逐工单，且逐工单故障隔离）。
  const onVerify = useCallback(async () => {
    const refs = selectedRefs;
    if (refs.length === 0) {
      toastInfo(t("pipeline.tickets.selectHint"));
      return;
    }
    setBusy("verify");
    try {
      await VerifyIssueTickets(novelId, refs);
      // 入口即时返回：把涉及的 run 标记为在飞复验，提示后台执行中。
      const runs = new Set(refs.map((r) => r.run_id));
      setVerifyingRuns((prev) => {
        const next = new Set(prev);
        runs.forEach((r) => next.add(r));
        return next;
      });
      toastInfo(t("pipeline.tickets.verifyStarted", { count: refs.length }));
      setSelected(new Set());
      load();
    } catch (error) {
      const msg = toErrorMessage(error);
      if (isProviderTrip(msg)) {
        setTripError(msg);
        setTripOpen(true);
      } else {
        toastError(msg);
      }
    } finally {
      setBusy("");
    }
  }, [selectedRefs, novelId, t, load]);

  // 取消在飞复验（审计 P0）：best-effort——取消成功或在队列未起执行体均静默；
  // 后端成功取消会发 pipeline:verify_cancelled 事件最终清态（此处乐观清态，幂等安全）。
  const onCancelAllVerify = useCallback(() => {
    if (!novelId || verifyingRuns.size === 0) return;
    Array.from(verifyingRuns).forEach((runID) =>
      CancelIssueVerify(novelId, runID).catch(() => {}),
    );
    setVerifyingRuns(new Set());
    setVerifyProgress({});
  }, [novelId, verifyingRuns]);

  return (
    <div className="flex flex-col gap-3 rounded border bg-card p-4">
      <div className="flex items-center gap-2">
        <ClipboardCheck className="h-4 w-4 text-primary" />
        <span className="text-sm font-medium">
          {t("pipeline.tickets.title")}
        </span>
        {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            className={`rounded border px-2 py-1 text-xs hover:bg-muted ${
              !showAll ? "bg-muted" : ""
            }`}
            onClick={() => setShowAll(false)}
          >
            {t("pipeline.tickets.filterOpen")}
          </button>
          <button
            type="button"
            className={`rounded border px-2 py-1 text-xs hover:bg-muted ${
              showAll ? "bg-muted" : ""
            }`}
            onClick={() => setShowAll(true)}
          >
            {t("pipeline.tickets.filterAll")}
          </button>
        </div>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("pipeline.tickets.hint")}
      </p>

      {visible.length > 0 ? (
        <>
          <div className="sticky top-0 z-10 flex flex-wrap items-center gap-2 border-b border-border/60 bg-card py-2">
            <button
              type="button"
              className="rounded border px-2 py-1 text-xs hover:bg-muted"
              onClick={toggleAll}
            >
              {t("pipeline.tickets.selectAll")}
            </button>
            <span className="text-muted-foreground text-xs">
              {t("pipeline.tickets.selected", { count: selectedRefs.length })}
            </span>
            <div className="ml-auto flex flex-wrap items-center gap-2">
              <button
                type="button"
                disabled={busy !== "" || selectedRefs.length === 0}
                onClick={() =>
                  void runAction(
                    "confirm",
                    () => ConfirmIssueTickets(novelId, selectedRefs),
                    t("pipeline.tickets.confirmOk", {
                      count: selectedRefs.length,
                    }),
                  )
                }
                className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
              >
                {busy === "confirm" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : null}
                {t("pipeline.tickets.confirm")}
              </button>
              <button
                type="button"
                disabled={busy !== "" || selectedRefs.length === 0}
                onClick={() => void onFix()}
                className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
              >
                {busy === "fix" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Wrench className="h-3 w-3" />
                )}
                {t("pipeline.tickets.fix")}
              </button>
              <button
                type="button"
                disabled={busy !== "" || selectedRefs.length === 0}
                title={t("pipeline.tickets.verifyHint")}
                onClick={() => void onVerify()}
                className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
              >
                {busy === "verify" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <ClipboardCheck className="h-3 w-3" />
                )}
                {busy === "verify"
                  ? t("pipeline.tickets.verifying")
                  : t("pipeline.tickets.verify")}
              </button>
              <button
                type="button"
                disabled={busy !== "" || selectedRefs.length === 0}
                onClick={() => setDismissOpen((v) => !v)}
                className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
              >
                <XCircle className="h-3 w-3" />
                {t("pipeline.tickets.dismiss")}
              </button>
            </div>
          </div>

          {dismissOpen ? (
            <div className="flex flex-col gap-2 rounded border bg-muted/40 p-2">
              <span className="text-muted-foreground text-xs">
                {t("pipeline.tickets.dismissReason")}
              </span>
              <textarea
                value={dismissReason}
                onChange={(e) => setDismissReason(e.target.value)}
                rows={2}
                placeholder={t("pipeline.tickets.dismissPlaceholder")}
                className="w-full rounded border bg-card px-2 py-1 text-sm"
              />
              <div className="flex gap-2">
                <button
                  type="button"
                  disabled={busy !== ""}
                  onClick={onDismiss}
                  className="inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
                >
                  {busy === "dismiss" ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : null}
                  {t("pipeline.tickets.dismissConfirm")}
                </button>
                <button
                  type="button"
                  disabled={busy !== ""}
                  onClick={() => {
                    setDismissOpen(false);
                    setDismissReason("");
                  }}
                  className="rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
                >
                  {t("pipeline.tickets.dismissCancel")}
                </button>
              </div>
            </div>
          ) : null}

          {verifyingRuns.size > 0 ? (
            <div className="flex flex-wrap items-center gap-2 rounded border bg-primary/5 p-2">
              <Loader2 className="h-3 w-3 animate-spin text-primary" />
              <span className="text-xs text-primary">
                {t("pipeline.tickets.verifying")}
                {Object.values(verifyProgress)
                  .map((p) => ` ${p.done}/${p.total}`)
                  .join(" · ")}
              </span>
              <button
                type="button"
                onClick={() => onCancelAllVerify()}
                className="rounded border border-destructive/30 bg-destructive/10 px-2 py-0.5 text-xs text-foreground/90 transition-colors hover:bg-destructive/20"
              >
                {t("pipeline.tickets.cancelVerify")}
              </button>
            </div>
          ) : null}

          <ul className="flex flex-col gap-2">
            {visible.map((row) => {
              const chapters = chaptersOf(row);
              return (
                <li
                  key={row.id}
                  className="flex flex-col gap-1 rounded border bg-card px-3 py-2 text-sm"
                >
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={selected.has(row.id)}
                      onChange={() => toggle(row.id)}
                      aria-label={row.issue_key}
                    />
                    <span
                      className={`rounded px-1.5 py-0.5 text-xs ${
                        STATUS_CLS[row.status] ??
                        "bg-muted text-muted-foreground"
                      }`}
                    >
                      {t(`pipeline.tickets.status.${row.status}`)}
                    </span>
                    <span className="shrink-0 font-medium text-xs">
                      {row.issue_key}
                    </span>
                    <span className="truncate font-medium">{row.area}</span>
                    <span className="ml-auto shrink-0 text-muted-foreground text-xs">
                      {fmtTime(row.updated_at)}
                    </span>
                  </div>
                  <p className="whitespace-pre-wrap break-words text-muted-foreground text-xs">
                    {row.desc}
                  </p>
                  <div className="flex flex-wrap items-center gap-2">
                    {chapters.length > 0 ? (
                      <>
                        <span className="text-muted-foreground text-xs">
                          {t("pipeline.tickets.chapters", {
                            list: chapters.join("、"),
                          })}
                          ：
                        </span>
                        {chapters.map((n) => (
                          <button
                            key={n}
                            type="button"
                            title={t("pipeline.tickets.jumpToChapter", { n })}
                            onClick={() =>
                              useNavigationStore
                                .getState()
                                .openChapterByReadingNumber?.(novelId, n)
                            }
                            className="rounded border px-1.5 py-0.5 text-xs text-primary hover:bg-primary/10"
                          >
                            {t("pipeline.tickets.chapterN", { n })}
                          </button>
                        ))}
                      </>
                    ) : null}
                    <IssueScopeTags issue={row} />
                  </div>
                  {row.verify_receipt ? (
                    <p className="text-success-foreground text-xs">
                      {t("pipeline.tickets.verifyReceipt")}（
                      {fmtTime(row.verify_at)}）：{row.verify_receipt}
                    </p>
                  ) : null}
                  {row.note ? (
                    <p className="text-muted-foreground text-xs">
                      {t("pipeline.tickets.note")}：{row.note}
                    </p>
                  ) : null}
                </li>
              );
            })}
          </ul>
        </>
      ) : (
        !loading && (
          <p className="text-sm text-muted-foreground">
            {t("pipeline.tickets.empty")}
          </p>
        )
      )}

      {/* 供应商疑似挂起弹窗（#271 中期方案）：复验遇熔断时给可操作弹窗，
          区分可重试的挂起与真错误；重试复用 onVerify，切换模型经全局设置通道。 */}
      {tripOpen ? (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
          onClick={() => setTripOpen(false)}
        >
          <div
            className="w-full max-w-md rounded-lg border bg-card p-4 shadow-lg"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-2 mb-2">
              <AlertTriangle className="w-4 h-4 text-warning" />
              <span className="text-sm font-semibold">
                {t("pipeline.tickets.tripTitle")}
              </span>
            </div>
            <p className="text-xs text-muted-foreground mb-3">
              {t("pipeline.tickets.tripHint")}
            </p>
            <pre className="mb-3 max-h-32 overflow-auto whitespace-pre-wrap break-words rounded bg-muted/50 p-2 text-[11px] text-muted-foreground">
              {tripError}
            </pre>
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                onClick={() => setTripOpen(false)}
                className="rounded border px-3 py-1.5 text-xs hover:bg-muted"
              >
                {t("pipeline.tickets.tripClose")}
              </button>
              <button
                type="button"
                onClick={() => {
                  setTripOpen(false);
                  useSettingsOpenStore.getState().requestOpen("model");
                }}
                className="rounded border px-3 py-1.5 text-xs hover:bg-muted"
              >
                {t("pipeline.tickets.switchModel")}
              </button>
              <button
                type="button"
                disabled={busy !== ""}
                onClick={() => {
                  setTripOpen(false);
                  void onVerify();
                }}
                className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
              >
                {busy === "verify" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : null}
                {t("pipeline.tickets.retry")}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}
