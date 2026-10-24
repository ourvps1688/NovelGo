import { useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import {
  AlertTriangle,
  CheckCircle2,
  History,
  Loader2,
  XCircle,
} from "lucide-react";
import type { retcon, snapshot } from "@/lib/wailsjs/go/models";
import {
  ApproveRetconCase,
  BeginRetconEdit,
  GetRetconCase,
  GetRetconEditDiff,
  RecordRetconEdit,
  RejectRetconCase,
  RollbackRetconCase,
} from "@/lib/wailsjs/go/app/App";
import ConfirmDialog from "@/components/ui/ConfirmDialog";
import { useVolumes } from "@/components/volume/useVolumes";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { IssueScopeTags } from "./IssueTags";

// retcon 裁决/执行人标识（单机单作者场景的固定署名，落 decided_by/applied_by/rolled_back_by 留痕）。
const RETCON_ACTOR = "author";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// RetconCaseDialog 是 retcon 案件的审批/执行弹窗（WBS 3.1/3.2）。
// 按状态机自适应展示操作区（状态迁移只能经这些显式 API，绝不自动）：
//   candidate  → 风险提示 + 裁决理由（必填）→ 批准 / 驳回
//   approved   → 绑定目标冻结卷 → 开始受控编辑（固化回滚点）
//   editing    → 查看差异（回滚点 vs 当前）→ 留痕收口
//   recorded   → 差异摘要 + 回滚理由（必填）→ 回滚到回滚点（危险确认）
//   rejected / rolled_back → 终态只读
export default function RetconCaseDialog({
  item,
  onClose,
  onChanged,
}: {
  item: retcon.RetconCase;
  onClose: () => void;
  onChanged: () => void;
}) {
  const { t } = useTranslation();
  const { data: volumes = [] } = useVolumes(item.novel_id);
  const [current, setCurrent] = useState(item);
  const [note, setNote] = useState("");
  const [volumeId, setVolumeId] = useState<number | null>(
    item.volume_id ?? null,
  );
  const [busy, setBusy] = useState(false);
  const [delta, setDelta] = useState<snapshot.Delta | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);
  const [confirm, setConfirm] = useState<"record" | "rollback" | null>(null);

  // item 变化（列表侧刷新后传入新对象）时同步本地状态：渲染期重置而非 effect，
  // 避免 set-state-in-effect 级联渲染告警。
  const [prevItem, setPrevItem] = useState(item);
  if (prevItem !== item) {
    setPrevItem(item);
    setCurrent(item);
    setVolumeId(item.volume_id ?? null);
  }

  const frozenVolumes = volumes.filter((v) => v.frozen);
  const statusLabel = t(`pipeline.retcon.status.${current.status}`);

  async function refresh() {
    try {
      const next = await GetRetconCase(current.id);
      if (next) setCurrent(next);
    } catch {
      // 刷新失败不影响已完成的操作，列表侧 onChanged 会兜底。
    }
    onChanged();
  }

  async function act(fn: () => Promise<unknown>, okKey: string) {
    setBusy(true);
    try {
      await fn();
      toastSuccess(t(okKey));
      await refresh();
      setNote("");
    } catch (error) {
      toastError(toErrorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  const approve = () =>
    act(() => {
      // retcon 立案埋点（P4 T2.2）：立案在后端 handoff 出口沉淀候选案件，
      // 前端以「批准立案」裁决为使用信号；失败静默零提示。
      trackFeature(ACTION_FEATURE_KEYS.retcon);
      return ApproveRetconCase(current.id, RETCON_ACTOR, note.trim());
    }, "pipeline.retcon.approveOk");
  const reject = () =>
    act(
      () => RejectRetconCase(current.id, RETCON_ACTOR, note.trim()),
      "pipeline.retcon.rejectOk",
    );
  const beginEdit = () => {
    const vid = current.volume_id ?? volumeId;
    if (vid == null) return;
    return act(
      () => BeginRetconEdit(current.id, vid, RETCON_ACTOR),
      "pipeline.retcon.beginEditOk",
    );
  };
  const record = () =>
    act(
      () => RecordRetconEdit(current.id, RETCON_ACTOR),
      "pipeline.retcon.recordOk",
    );
  const rollback = () =>
    act(
      () => RollbackRetconCase(current.id, RETCON_ACTOR, note.trim()),
      "pipeline.retcon.rollbackOk",
    );

  async function loadDiff() {
    setDiffLoading(true);
    try {
      const d = await GetRetconEditDiff(current.id);
      setDelta(d ?? null);
    } catch (error) {
      toastError(toErrorMessage(error));
    } finally {
      setDiffLoading(false);
    }
  }

  const noteOk = note.trim().length > 0;

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="absolute inset-0 bg-black/40" onClick={onClose} />
      <div className="relative flex max-h-[85vh] w-[640px] max-w-[92vw] flex-col overflow-hidden rounded-xl border bg-background shadow-2xl">
        <div className="flex items-center gap-2 border-b px-5 py-3">
          <h2 className="truncate text-base font-semibold">{current.title}</h2>
          <span
            className={`shrink-0 rounded px-1.5 py-0.5 text-xs ${
              current.status === "candidate"
                ? "bg-warning/10 text-warning-foreground"
                : current.status === "recorded"
                  ? "bg-success/10 text-success-foreground"
                  : current.status === "rejected" ||
                      current.status === "rolled_back"
                    ? "bg-muted text-muted-foreground"
                    : "bg-primary/10 text-primary"
            }`}
          >
            {statusLabel}
          </span>
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="ml-auto w-7 h-7 flex items-center justify-center rounded-md text-muted-foreground hover:text-foreground hover:bg-muted disabled:opacity-50"
          >
            ✕
          </button>
        </div>

        <div className="flex-1 overflow-auto px-5 py-4">
          <div className="flex flex-col gap-3 text-sm">
            {current.description ? (
              <p className="whitespace-pre-wrap text-muted-foreground">
                {current.description}
              </p>
            ) : null}
            <IssueScopeTags issue={current} />
            <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-xs text-muted-foreground">
              <div>
                {t("pipeline.retcon.origin")}: {current.origin}
              </div>
              {current.issue_id ? (
                <div>
                  {t("pipeline.retcon.issue")}: {current.issue_id}
                </div>
              ) : null}
              <div>
                {t("pipeline.retcon.created")}: {fmtTime(current.created_at)}
              </div>
              {current.decided_by ? (
                <div>
                  {t("pipeline.retcon.decidedBy")}: {current.decided_by} (
                  {fmtTime(current.decided_at)})
                </div>
              ) : null}
              {current.applied_by ? (
                <div>
                  {t("pipeline.retcon.appliedBy")}: {current.applied_by} (
                  {fmtTime(current.applied_at)})
                </div>
              ) : null}
              {current.rolled_back_by ? (
                <div>
                  {t("pipeline.retcon.rolledBackBy")}: {current.rolled_back_by}{" "}
                  ({fmtTime(current.rolled_back_at)})
                </div>
              ) : null}
              {current.rollback_snapshot_id ? (
                <div>
                  {t("pipeline.retcon.snapshot")} #{" "}
                  {current.rollback_snapshot_id}
                </div>
              ) : null}
            </dl>
            {current.decision_note ? (
              <p className="rounded border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
                {t("pipeline.retcon.decisionNote")}: {current.decision_note}
              </p>
            ) : null}

            {/* candidate：风险提示 + 裁决理由必填 + 批准/驳回 */}
            {current.status === "candidate" ? (
              <>
                <p className="flex items-start gap-1.5 rounded border border-warning/50 bg-warning/5 px-3 py-2 text-xs text-muted-foreground">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning-foreground" />
                  {t("pipeline.retcon.riskHint")}
                </p>
                <label className="flex flex-col gap-1 text-xs">
                  <span>{t("pipeline.retcon.noteLabel")}</span>
                  <textarea
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                    rows={3}
                    disabled={busy}
                    className="rounded-md border bg-background px-2 py-1.5 text-sm disabled:opacity-50"
                  />
                </label>
                <div className="flex justify-end gap-2">
                  <button
                    type="button"
                    disabled={busy || !noteOk}
                    onClick={() => void reject()}
                    className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-sm hover:bg-muted disabled:opacity-50"
                  >
                    <XCircle className="h-3.5 w-3.5" />
                    {t("pipeline.retcon.reject")}
                  </button>
                  <button
                    type="button"
                    disabled={busy || !noteOk}
                    onClick={() => void approve()}
                    className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-primary-foreground text-sm disabled:opacity-50"
                  >
                    {busy ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <CheckCircle2 className="h-3.5 w-3.5" />
                    )}
                    {t("pipeline.retcon.approve")}
                  </button>
                </div>
              </>
            ) : null}

            {/* approved：绑定目标冻结卷（固化回滚点）后进入受控编辑 */}
            {current.status === "approved" ? (
              <>
                <p className="text-xs text-muted-foreground">
                  {t("pipeline.retcon.beginEditHint")}
                </p>
                {current.volume_id ? (
                  <p className="text-xs">
                    {t("pipeline.retcon.targetVolume")} # {current.volume_id}
                  </p>
                ) : (
                  <label className="flex flex-col gap-1 text-xs">
                    <span>{t("pipeline.retcon.targetVolume")}</span>
                    <select
                      value={volumeId ?? ""}
                      onChange={(e) =>
                        setVolumeId(
                          e.target.value ? Number(e.target.value) : null,
                        )
                      }
                      disabled={busy}
                      className="rounded-md border bg-background px-2 py-1.5 text-sm disabled:opacity-50"
                    >
                      <option value="">
                        {t("pipeline.retcon.selectFrozenVolume")}
                      </option>
                      {frozenVolumes.map((v) => (
                        <option key={v.id} value={v.id}>
                          {v.name}
                        </option>
                      ))}
                    </select>
                  </label>
                )}
                {frozenVolumes.length === 0 && !current.volume_id ? (
                  <p className="text-xs text-muted-foreground">
                    {t("pipeline.retcon.noFrozenVolume")}
                  </p>
                ) : null}
                <div className="flex justify-end">
                  <button
                    type="button"
                    disabled={
                      busy ||
                      (current.volume_id == null &&
                        (volumeId == null || frozenVolumes.length === 0))
                    }
                    onClick={() => void beginEdit()}
                    className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-primary-foreground text-sm disabled:opacity-50"
                  >
                    {busy ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <History className="h-3.5 w-3.5" />
                    )}
                    {t("pipeline.retcon.beginEdit")}
                  </button>
                </div>
              </>
            ) : null}

            {/* editing：查看差异 + 留痕收口 */}
            {current.status === "editing" ? (
              <>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    disabled={diffLoading}
                    onClick={() => void loadDiff()}
                    className="inline-flex items-center gap-1 rounded border px-3 py-1.5 text-sm hover:bg-muted disabled:opacity-50"
                  >
                    {diffLoading ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <History className="h-3.5 w-3.5" />
                    )}
                    {t("pipeline.retcon.viewDiff")}
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => setConfirm("record")}
                    className="inline-flex items-center gap-1 rounded bg-primary px-3 py-1.5 text-primary-foreground text-sm disabled:opacity-50"
                  >
                    {t("pipeline.retcon.record")}
                  </button>
                </div>
                {delta ? <DeltaView delta={delta} /> : null}
              </>
            ) : null}

            {/* recorded：差异摘要 + 回滚（危险确认） */}
            {current.status === "recorded" ? (
              <>
                {current.diff_summary ? (
                  <div className="flex flex-col gap-1">
                    <div className="text-xs font-medium text-muted-foreground">
                      {t("pipeline.retcon.diffSummary")}
                    </div>
                    <p className="whitespace-pre-wrap rounded border bg-muted/40 px-3 py-2 text-xs text-muted-foreground">
                      {current.diff_summary}
                    </p>
                  </div>
                ) : null}
                <label className="flex flex-col gap-1 text-xs">
                  <span>{t("pipeline.retcon.rollbackNote")}</span>
                  <textarea
                    value={note}
                    onChange={(e) => setNote(e.target.value)}
                    rows={2}
                    disabled={busy}
                    className="rounded-md border bg-background px-2 py-1.5 text-sm disabled:opacity-50"
                  />
                </label>
                <div className="flex justify-end">
                  <button
                    type="button"
                    disabled={busy || !noteOk}
                    onClick={() => setConfirm("rollback")}
                    className="inline-flex items-center gap-1 rounded bg-destructive px-3 py-1.5 text-destructive-foreground text-sm disabled:opacity-50"
                  >
                    <AlertTriangle className="h-3.5 w-3.5" />
                    {t("pipeline.retcon.rollback")}
                  </button>
                </div>
              </>
            ) : null}
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={confirm !== null}
        danger={confirm === "rollback"}
        title={t(
          confirm === "rollback"
            ? "pipeline.retcon.rollback"
            : "pipeline.retcon.record",
        )}
        message={t(
          confirm === "rollback"
            ? "pipeline.retcon.rollbackConfirm"
            : "pipeline.retcon.recordConfirm",
        )}
        loading={busy}
        onConfirm={() => {
          const action = confirm;
          setConfirm(null);
          if (action === "rollback") void rollback();
          else if (action === "record") void record();
        }}
        onClose={() => {
          if (!busy) setConfirm(null);
        }}
      />
    </div>,
    document.body,
  );
}

// DeltaView 渲染受控编辑差异（snapshot.Delta）：按表列出新增/删除/修改计数，
// 修改行列出字段级 old → new（截断展示），供审批/收口前人工核对改动范围。
function DeltaView({ delta }: { delta: snapshot.Delta }) {
  const { t } = useTranslation();
  const tables = delta.tables ?? [];
  const fmt = (v: unknown) => {
    const s = v == null ? "—" : JSON.stringify(v);
    return s.length > 80 ? `${s.slice(0, 80)}…` : s;
  };
  return (
    <div className="flex flex-col gap-2 rounded border bg-muted/30 p-3 text-xs">
      <div className="text-muted-foreground">
        {t("pipeline.retcon.diffVersions", {
          base: delta.base_version,
          target: delta.target_version,
        })}
      </div>
      {tables.length === 0 ? (
        <p className="text-muted-foreground">
          {t("pipeline.retcon.diffEmpty")}
        </p>
      ) : (
        tables.map((td) => (
          <div key={td.table} className="flex flex-col gap-1">
            <div className="flex items-center gap-2 font-medium">
              <span>{td.table}</span>
              <span className="text-muted-foreground">
                +{td.added?.length ?? 0} / −{td.removed?.length ?? 0} / ~
                {td.changed?.length ?? 0}
              </span>
            </div>
            {(td.added ?? []).slice(0, 5).map((row, i) => (
              <div key={`a${i}`} className="truncate text-muted-foreground">
                {t("pipeline.retcon.added")}: {fmt(row)}
              </div>
            ))}
            {(td.removed ?? []).slice(0, 5).map((row, i) => (
              <div key={`r${i}`} className="truncate text-muted-foreground">
                {t("pipeline.retcon.removed")}: {fmt(row)}
              </div>
            ))}
            {(td.changed ?? []).slice(0, 5).map((row) => (
              <div key={`c${row.id}`} className="flex flex-col gap-0.5">
                <span className="text-muted-foreground">#{row.id}</span>
                {Object.entries(row.fields ?? {}).map(([field, delta2]) => (
                  <span key={field} className="truncate text-muted-foreground">
                    {field}: {fmt(delta2.old)} → {fmt(delta2.new)}
                  </span>
                ))}
              </div>
            ))}
          </div>
        ))
      )}
    </div>
  );
}
