import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ClipboardCheck, Loader2, Wrench, XCircle } from "lucide-react";
import type { pipeline } from "@/lib/wailsjs/go/models";
import {
  ConfirmIssueTickets,
  DismissIssueTickets,
  ListIssueTickets,
  StartIssueFixTickets,
  VerifyIssueTickets,
} from "@/lib/wailsjs/go/app/App";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
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

  const load = useCallback(() => {
    ListIssueTickets(novelId)
      .then((list) => setRows(list ?? []))
      .catch((error) => toastError(toErrorMessage(error)))
      .finally(() => setLoading(false));
  }, [novelId]);

  useEffect(() => {
    load();
  }, [load]);

  // 默认只看未收口工单（verified/dismissed 终态收起），可切「全部」回看历史留痕。
  const visible = useMemo(
    () => (showAll ? rows : rows.filter((r) => !TERMINAL.has(r.status))),
    [rows, showAll],
  );
  const selectedRefs = useMemo(
    () => visible.filter((r) => selected.has(r.id)).map((r) => refOf(r)),
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
          <div className="flex flex-wrap items-center gap-2">
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
                onClick={() =>
                  void runAction(
                    "fix",
                    () => StartIssueFixTickets(novelId, selectedRefs),
                    t("pipeline.tickets.fixOk", { count: selectedRefs.length }),
                  )
                }
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
                onClick={() =>
                  void runAction(
                    "verify",
                    async () => {
                      const sum = await VerifyIssueTickets(
                        novelId,
                        selectedRefs,
                      );
                      toastSuccess(
                        t("pipeline.tickets.verifyOk", {
                          confirmed: sum?.confirmed ?? 0,
                          rejected: sum?.rejected ?? 0,
                          unverified: sum?.unverified ?? 0,
                        }),
                      );
                    },
                    "",
                  )
                }
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
                      <span className="text-muted-foreground text-xs">
                        {t("pipeline.tickets.chapters", {
                          list: chapters.join("、"),
                        })}
                      </span>
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
    </div>
  );
}
