import { useState } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { BookLock, Loader2, ScanSearch, Unlock } from "lucide-react";
import type { snapshot, volume } from "@/lib/wailsjs/go/models";
import { AuditSeriesBible, SetVolumeFrozen } from "@/lib/wailsjs/go/app/App";
import ConfirmDialog from "@/components/ui/ConfirmDialog";
import { useVolumes } from "@/components/volume/useVolumes";
import { volumeKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { toastError, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// VolumeManagePanel 是卷冻结管理（WBS 2.x 系列圣经快照的前端入口）：
// 冻结卷=既定事实（提交 series bible 快照，S4/S5/重写均以其为前向依据），
// 冻结前强制台账自洽审计（结构性矛盾会被拒绝），冻结后该卷写保护、改动走 retcon。
export default function VolumeManagePanel({ novelId }: { novelId: number }) {
  const { t } = useTranslation();
  const qc = useQueryClient();
  const { data: volumes = [], isLoading } = useVolumes(novelId);
  const [audit, setAudit] = useState<snapshot.AuditReport | null>(null);
  const [auditRunning, setAuditRunning] = useState(false);
  const [freezeTarget, setFreezeTarget] = useState<volume.Volume | null>(null);
  const [unfreezeTarget, setUnfreezeTarget] = useState<volume.Volume | null>(
    null,
  );
  const [busy, setBusy] = useState(false);

  function invalidate() {
    return qc.invalidateQueries({ queryKey: volumeKeys.list(novelId) });
  }

  async function runAudit() {
    setAuditRunning(true);
    try {
      const report = await AuditSeriesBible(novelId);
      setAudit(report ?? null);
    } catch (error) {
      toastError(toErrorMessage(error));
    } finally {
      setAuditRunning(false);
    }
  }

  async function confirmFreeze() {
    const target = freezeTarget;
    if (!target) return;
    setBusy(true);
    try {
      // 前置审计：与冻结事务内的审计同口径（2.4），提前把结构性矛盾亮给用户。
      const report = await AuditSeriesBible(novelId);
      if ((report?.error_count ?? 0) > 0) {
        setAudit(report);
        toastError(
          t("pipeline.volumeManage.freezeBlocked") +
            (report?.issues ?? [])
              .filter((i) => i.severity === "error")
              .slice(0, 3)
              .map((i) => `\n[${i.code}] ${i.message}`)
              .join(""),
        );
        setFreezeTarget(null);
        return;
      }
      // 冻结动作埋点（P4 T2.2）：冻结/解冻同属卷冻结操作；失败静默零提示。
      trackFeature(ACTION_FEATURE_KEYS.freeze);
      await SetVolumeFrozen(novelId, target.id, true);
      toastSuccess(t("pipeline.volumeManage.frozenOk", { name: target.name }));
      setFreezeTarget(null);
      await invalidate();
    } catch (error) {
      toastError(toErrorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function confirmUnfreeze() {
    const target = unfreezeTarget;
    if (!target) return;
    setBusy(true);
    try {
      trackFeature(ACTION_FEATURE_KEYS.freeze);
      await SetVolumeFrozen(novelId, target.id, false);
      toastSuccess(
        t("pipeline.volumeManage.unfrozenOk", { name: target.name }),
      );
      setUnfreezeTarget(null);
      await invalidate();
    } catch (error) {
      toastError(toErrorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  const errors = (audit?.issues ?? []).filter((i) => i.severity === "error");
  const warnings = (audit?.issues ?? []).filter((i) => i.severity !== "error");

  return (
    <div className="flex flex-col gap-3 rounded border bg-card p-4">
      <div className="flex items-center gap-2">
        <BookLock className="h-4 w-4 text-primary" />
        <span className="text-sm font-medium">
          {t("pipeline.volumeManage.title")}
        </span>
        {isLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
        <button
          type="button"
          disabled={auditRunning}
          onClick={() => void runAudit()}
          className="ml-auto inline-flex items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
        >
          {auditRunning ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <ScanSearch className="h-3 w-3" />
          )}
          {t("pipeline.volumeManage.audit")}
        </button>
      </div>
      <p className="text-xs text-muted-foreground">
        {t("pipeline.volumeManage.hint")}
      </p>

      {audit ? (
        <div className="flex flex-col gap-1 rounded border bg-muted/30 px-3 py-2 text-xs">
          {errors.length === 0 ? (
            <p className="text-muted-foreground">
              {t("pipeline.volumeManage.auditClean", {
                n: warnings.length,
              })}
            </p>
          ) : (
            <>
              <p className="text-danger-foreground">
                {t("pipeline.volumeManage.auditErrors", { n: errors.length })}
              </p>
              {errors.slice(0, 8).map((i, idx) => (
                <p key={idx} className="text-muted-foreground">
                  [{i.code}] {i.table}#{i.row_id}: {i.message}
                </p>
              ))}
            </>
          )}
          {warnings.length > 0 ? (
            <>
              <p className="mt-1 text-warning-foreground">
                {t("pipeline.volumeManage.auditWarnings")}
              </p>
              {warnings.slice(0, 8).map((i, idx) => (
                <p key={idx} className="text-muted-foreground">
                  [{i.code}] {i.table}#{i.row_id}: {i.message}
                </p>
              ))}
            </>
          ) : null}
        </div>
      ) : null}

      {volumes.length === 0 && !isLoading ? (
        <p className="text-sm text-muted-foreground">
          {t("pipeline.volumeManage.empty")}
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {volumes.map((v) => (
            <li
              key={v.id}
              className="flex items-center gap-2 rounded border bg-card px-3 py-2 text-sm"
            >
              <span className="truncate font-medium">{v.name}</span>
              {v.frozen ? (
                <span className="flex shrink-0 items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 text-primary text-xs">
                  <BookLock className="h-3 w-3" />
                  {t("pipeline.volumeManage.frozen")}
                </span>
              ) : (
                <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-muted-foreground text-xs">
                  {t("pipeline.volumeManage.unfrozen")}
                </span>
              )}
              {v.frozen ? (
                <span className="shrink-0 text-muted-foreground text-xs">
                  {t("pipeline.volumeManage.snapshotVersion", {
                    n: v.snapshot_version,
                  })}
                  {v.frozen_at
                    ? ` · ${t("pipeline.volumeManage.frozenAt", {
                        time: fmtTime(v.frozen_at),
                      })}`
                    : ""}
                </span>
              ) : null}
              <button
                type="button"
                disabled={busy}
                onClick={() =>
                  v.frozen ? setUnfreezeTarget(v) : setFreezeTarget(v)
                }
                className="ml-auto inline-flex shrink-0 items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted disabled:opacity-50"
              >
                {v.frozen ? (
                  <Unlock className="h-3 w-3" />
                ) : (
                  <BookLock className="h-3 w-3" />
                )}
                {t(
                  v.frozen
                    ? "pipeline.volumeManage.unfreeze"
                    : "pipeline.volumeManage.freeze",
                )}
              </button>
            </li>
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={freezeTarget !== null}
        title={t("pipeline.volumeManage.freeze")}
        message={t("pipeline.volumeManage.freezeConfirm", {
          name: freezeTarget?.name ?? "",
        })}
        confirmText={t("pipeline.volumeManage.freeze")}
        loading={busy}
        onConfirm={confirmFreeze}
        onClose={() => {
          if (!busy) setFreezeTarget(null);
        }}
      />
      <ConfirmDialog
        open={unfreezeTarget !== null}
        danger
        title={t("pipeline.volumeManage.unfreeze")}
        message={t("pipeline.volumeManage.unfreezeConfirm", {
          name: unfreezeTarget?.name ?? "",
        })}
        confirmText={t("pipeline.volumeManage.unfreeze")}
        loading={busy}
        onConfirm={confirmUnfreeze}
        onClose={() => {
          if (!busy) setUnfreezeTarget(null);
        }}
      />
    </div>
  );
}
