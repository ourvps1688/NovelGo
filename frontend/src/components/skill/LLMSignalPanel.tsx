import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { GetLLMSignalStats } from "@/lib/wailsjs/go/app/App";
import type { activity } from "@/lib/wailsjs/go/models";

type SignalStat = activity.LLMSignalStat;

interface Props {
  novelId: number;
}

// #114 P2 供应商可观测面板：把 llm_signal_daily 的本地指标消费掉，避免重演
// 「账本已建、前端零消费」。按 provider×model×task_kind×signal 展示截断 /
// 工具参数解析失败 / 空流 / 熔断跳过的次数，用于判断「是不是模型不行」。
// 降级纪律：查询失败 / 加载中 / 无任何信号时静默不渲染（零报错零空态噪音）。
// LLM 失败信号在流式调用过程中异步落库，没有可订阅的写侧事件；staleTime=0
// 保证每次进入技能矩阵面板都重拉，避免停在旧快照（度量面零失效通道教训）。
export default function LLMSignalPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const {
    data: stats = [],
    isError,
    isPending,
  } = useQuery({
    // 前缀命中 queryErrorToast 静默白名单：指标表不可用时静默降级。
    queryKey: ["feature-usage", "llm-signals", novelId, 30],
    queryFn: async () => (await GetLLMSignalStats(30)) as SignalStat[],
    staleTime: 0,
  });

  const rows = [...stats]
    .filter((stat) => stat?.signal)
    .sort(
      (a, b) =>
        Number(b.count ?? 0) - Number(a.count ?? 0) ||
        String(a.provider ?? "").localeCompare(String(b.provider ?? "")) ||
        String(a.signal ?? "").localeCompare(String(b.signal ?? "")),
    );

  if (isError || isPending || rows.length === 0) return null;

  return (
    <section data-testid="llm-signal-panel" className="mb-6 space-y-3">
      <div>
        <h2 className="text-sm font-medium text-foreground">
          {t("skillMatrix.signals.title")}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("skillMatrix.signals.hint")}
        </p>
      </div>

      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full text-left text-xs">
          <thead className="bg-muted/50 text-muted-foreground">
            <tr>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.signals.provider")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.signals.model")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.signals.taskKind")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.signals.signal")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.signals.count")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.signals.lastDate")}
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr
                key={`${row.provider}/${row.model}/${row.task_kind}/${row.signal}/${i}`}
                data-testid="llm-signal-row"
                className="border-t"
              >
                <td className="px-3 py-1.5 font-mono">{row.provider}</td>
                <td className="px-3 py-1.5 font-mono">{row.model}</td>
                <td className="px-3 py-1.5 text-muted-foreground">
                  {row.task_kind}
                </td>
                <td className="px-3 py-1.5 font-mono text-[10px]">
                  {row.signal}
                </td>
                <td className="px-3 py-1.5 tabular-nums">
                  {t("skillMatrix.signals.times", {
                    count: Number(row.count ?? 0),
                  })}
                </td>
                <td className="px-3 py-1.5 text-muted-foreground">
                  {row.last_date}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
