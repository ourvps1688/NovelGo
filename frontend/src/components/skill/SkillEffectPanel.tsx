import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { GetSkillEffectStats } from "@/lib/wailsjs/go/app/App";
import type { activity } from "@/lib/wailsjs/go/models";

type EffectStat = activity.SkillEffectStat;

interface Props {
  novelId: number;
}

// #116 P1 技能效果面板：旧用量面板只回答「注入了多少次」，这里把
// skill_invocations.finding_keys（带【本章目标发现项】的改稿轮）与
// chapter_quality.finding_round 三态快照关联，回答「技能参与的改稿轮里，
// 目标发现项到底消掉了多少」。纯本地只读，零派发、零自动修复、零上传。
//
// 口径诚实标注：finding_round 每章只保留最新一轮，故这是「最近一轮近似」，
// 不是逐轮历史；无快照的旧档案不参与计数，绝不反推成「全部消掉」。
// 降级纪律：查询失败 / 加载中 / 无数据时静默不渲染（零报错零空态噪音）。
// 与 LLMSignalPanel 一致用 staleTime:0：改稿轮异步落库无写侧事件可订阅。
export default function SkillEffectPanel({ novelId }: Props) {
  const { t } = useTranslation();
  const {
    data: stats = [],
    isError,
    isPending,
  } = useQuery({
    // 前缀命中 queryErrorToast 静默白名单：统计不可用时静默降级。
    queryKey: ["feature-usage", "skill-effects", novelId, 30],
    queryFn: async () => (await GetSkillEffectStats(30)) as EffectStat[],
    staleTime: 0,
  });

  const rows = [...stats]
    .filter((stat) => stat?.skill)
    .sort(
      (a, b) =>
        Number(b.rounds ?? 0) - Number(a.rounds ?? 0) ||
        String(a.skill ?? "").localeCompare(String(b.skill ?? "")) ||
        String(a.channel ?? "").localeCompare(String(b.channel ?? "")),
    );

  if (isError || isPending || rows.length === 0) return null;

  return (
    <section data-testid="skill-effect-panel" className="mb-6 space-y-3">
      <div>
        <h2 className="text-sm font-medium text-foreground">
          {t("skillMatrix.effects.title")}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("skillMatrix.effects.hint")}
        </p>
      </div>

      <div className="overflow-x-auto rounded-lg border">
        <table className="w-full text-left text-xs">
          <thead className="bg-muted/50 text-muted-foreground">
            <tr>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.effects.skill")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.effects.channel")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.effects.rounds")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.effects.resolved")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.effects.persisted")}
              </th>
              <th className="px-3 py-2 font-medium">
                {t("skillMatrix.effects.rate")}
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => {
              const resolved = Number(row.resolved ?? 0);
              const persisted = Number(row.persisted ?? 0);
              const total = resolved + persisted;
              return (
                <tr
                  key={`${row.skill}/${row.channel}/${i}`}
                  data-testid="skill-effect-row"
                  className="border-t"
                >
                  <td className="px-3 py-1.5 font-mono">{row.skill}</td>
                  <td className="px-3 py-1.5 text-muted-foreground">
                    {row.channel}
                  </td>
                  <td className="px-3 py-1.5 tabular-nums">
                    {t("skillMatrix.effects.roundsTimes", {
                      count: Number(row.rounds ?? 0),
                    })}
                  </td>
                  <td className="px-3 py-1.5 tabular-nums text-primary">
                    {t("skillMatrix.effects.itemTimes", { count: resolved })}
                  </td>
                  <td className="px-3 py-1.5 tabular-nums text-destructive">
                    {t("skillMatrix.effects.itemTimes", { count: persisted })}
                  </td>
                  <td className="px-3 py-1.5 tabular-nums">
                    {total > 0
                      ? t("skillMatrix.effects.rateValue", {
                          value: Math.round((resolved / total) * 100),
                        })
                      : t("skillMatrix.effects.noSnapshot")}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}
