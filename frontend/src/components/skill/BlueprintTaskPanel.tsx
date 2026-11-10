import { useState } from "react";
import { useTranslation } from "react-i18next";
import { RunBlueprintTask } from "@/lib/wailsjs/go/app/App";
import { toastError, toastInfo, toastSuccess } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";

interface Props {
  novelId: number;
}

// S2 全书蓝图的唯一显式入口（#111 P2，§4.3）：用户点按钮才启动一次
// 「生成蓝图 → 蓝图体检 → 落工单」。默认绝不自动派发，也不自动修复或复评；
// 体检落工单后由用户在工单面板「去修复」把上下文发到 chat 人工修复。
// 同一入口对存量书同样可用，天然覆盖「存量书补跑」，无需第二个按钮。
export default function BlueprintTaskPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const [running, setRunning] = useState(false);
  const [runId, setRunId] = useState("");

  const start = async () => {
    if (!novelId || running) return;
    setRunning(true);
    try {
      const res = await RunBlueprintTask(novelId);
      setRunId(res?.run_id ?? "");
      if (res?.dispatched === false) {
        toastInfo(t("skillMatrix.blueprint.alreadyRunning"));
      } else {
        toastSuccess(t("skillMatrix.blueprint.startedToast"));
      }
    } catch (e) {
      toastError(toErrorMessage(e, t("skillMatrix.blueprint.failed")));
    } finally {
      setRunning(false);
    }
  };

  return (
    <section data-testid="blueprint-task-panel" className="mb-6 space-y-3">
      <div>
        <h2 className="text-sm font-medium text-foreground">
          {t("skillMatrix.blueprint.title")}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("skillMatrix.blueprint.hint")}
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          data-testid="blueprint-start"
          disabled={!novelId || running}
          onClick={start}
          className="rounded-md border border-primary/40 bg-primary/10 px-3 py-1.5 text-xs font-medium text-primary hover:bg-primary/20 disabled:opacity-50"
        >
          {running
            ? t("skillMatrix.blueprint.running")
            : t("skillMatrix.blueprint.button")}
        </button>
        {runId && (
          <span
            data-testid="blueprint-run-id"
            className="font-mono text-xs text-muted-foreground"
          >
            {t("skillMatrix.blueprint.runId", { id: runId.slice(0, 8) })}
          </span>
        )}
      </div>
      <p className="text-xs text-muted-foreground">
        {t("skillMatrix.blueprint.afterHint")}
      </p>
    </section>
  );
}
