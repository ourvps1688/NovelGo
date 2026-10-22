import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { History, Loader2, ShieldAlert } from "lucide-react";
import type { retcon } from "@/lib/wailsjs/go/models";
import { ListRetconCases } from "@/lib/wailsjs/go/app/App";
import { toastError } from "@/utils/toast";
import { toErrorMessage } from "@/utils/error";
import { IssueScopeTags } from "./IssueTags";
import RetconCaseDialog from "./RetconCaseDialog";

function fmtTime(v?: unknown): string {
  if (!v) return "";
  const d = new Date(String(v));
  return Number.isNaN(d.getTime()) ? String(v) : d.toLocaleString();
}

// RetconPanel 是 retcon 案件清单（WBS 3.1 审批闸门入口）：
// 命中冻结卷写保护/retcon 候选的问题会沉淀为候选案件，这里按状态列出并进入
// 审批弹窗裁决。**retcon 绝不自动**：候选→裁决→受控编辑→留痕/回滚全部显式操作。
export default function RetconPanel({ novelId }: { novelId: number }) {
  const { t } = useTranslation();
  const [cases, setCases] = useState<retcon.RetconCase[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<retcon.RetconCase | null>(null);

  const load = useCallback(() => {
    ListRetconCases(novelId)
      .then((rows) => setCases(rows ?? []))
      .catch((error) => toastError(toErrorMessage(error)))
      .finally(() => setLoading(false));
  }, [novelId]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <div className="flex flex-col gap-3 rounded border bg-card p-4">
      <div className="flex items-center gap-2">
        <ShieldAlert className="h-4 w-4 text-warning-foreground" />
        <span className="text-sm font-medium">
          {t("pipeline.retcon.title")}
        </span>
        {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
      </div>
      <p className="text-xs text-muted-foreground">
        {t("pipeline.retcon.hint")}
      </p>

      {cases.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {cases.map((c) => (
            <li
              key={c.id}
              className="flex flex-col gap-1 rounded border bg-card px-3 py-2 text-sm"
            >
              <div className="flex items-center gap-2">
                <span
                  className={`rounded px-1.5 py-0.5 text-xs ${
                    c.status === "candidate"
                      ? "bg-warning/10 text-warning-foreground"
                      : c.status === "recorded"
                        ? "bg-success/10 text-success-foreground"
                        : c.status === "rejected" || c.status === "rolled_back"
                          ? "bg-muted text-muted-foreground"
                          : "bg-primary/10 text-primary"
                  }`}
                >
                  {t(`pipeline.retcon.status.${c.status}`)}
                </span>
                <span className="truncate font-medium">{c.title}</span>
                <span className="ml-auto shrink-0 text-muted-foreground text-xs">
                  {fmtTime(c.created_at)}
                </span>
                <button
                  type="button"
                  onClick={() => setSelected(c)}
                  className="inline-flex shrink-0 items-center gap-1 rounded border px-2 py-1 text-xs hover:bg-muted"
                >
                  <History className="h-3 w-3" />
                  {t("pipeline.retcon.open")}
                </button>
              </div>
              {c.description ? (
                <p className="line-clamp-2 text-muted-foreground text-xs">
                  {c.description}
                </p>
              ) : null}
              <IssueScopeTags issue={c} />
            </li>
          ))}
        </ul>
      ) : (
        !loading && (
          <p className="text-sm text-muted-foreground">
            {t("pipeline.retcon.empty")}
          </p>
        )
      )}

      {selected ? (
        <RetconCaseDialog
          item={selected}
          onClose={() => setSelected(null)}
          onChanged={load}
        />
      ) : null}
    </div>
  );
}
