import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { GetSkillUsage } from "@/lib/wailsjs/go/app/App";
import type { activity } from "@/lib/wailsjs/go/models";
import { useSkills } from "./useSkills";

type UsageStat = activity.SkillInvocationStat;

interface Props {
  novelId: number;
}

interface UsageRow {
  skill: string;
  channels: string[];
  count: number;
  lastSeen: string;
}

// 技能装配用量面板（#111 P0）：把统一账本 skill_invocations 消费掉，
// 避免重演 GetPipelineExecStats「账本已建、前端零消费」。近 30 天零记录的
// 内置技能标红——以后「哪些技能是摆设」由系统自己报警，不用再挖 DB。
// 降级纪律：查询失败 / 无任何可展示信息时静默不渲染（零报错零空态噪音）。
export default function SkillUsagePanel({ novelId }: Props) {
  const { t } = useTranslation();
  const { data: skills = [] } = useSkills(novelId);
  const {
    data: stats = [],
    isError,
    isPending,
  } = useQuery({
    // 前缀命中 queryErrorToast 静默白名单：账本不可用时静默降级。
    queryKey: ["feature-usage", "skill-invocations", novelId, 30],
    queryFn: async () => (await GetSkillUsage(30)) as UsageStat[],
  });

  const rows = useMemo<UsageRow[]>(() => {
    const grouped = new Map<string, UsageRow>();
    for (const stat of stats) {
      if (!stat?.skill) continue;
      const row = grouped.get(stat.skill) ?? {
        skill: stat.skill,
        channels: [],
        count: 0,
        lastSeen: "",
      };
      row.channels.push(stat.channel);
      row.count += Number(stat.count ?? 0);
      if (stat.last_seen > row.lastSeen) row.lastSeen = stat.last_seen;
      grouped.set(stat.skill, row);
    }
    return [...grouped.values()].sort(
      (a, b) => b.count - a.count || a.skill.localeCompare(b.skill),
    );
  }, [stats]);

  const idle = useMemo(() => {
    const used = new Set(rows.map((row) => row.skill));
    return skills
      .filter((item) => item.source === "builtin" && !used.has(item.name))
      .map((item) => item.name)
      .sort();
  }, [skills, rows]);

  // 加载中也不渲染：否则账本 query 尚未返回时会把全部技能短暂误报成「零记录」。
  if (isError || isPending || (rows.length === 0 && idle.length === 0))
    return null;

  return (
    <section data-testid="skill-usage-panel" className="mb-6 space-y-3">
      <div>
        <h2 className="text-sm font-medium text-foreground">
          {t("skillMatrix.usage.title")}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t("skillMatrix.usage.hint")}
        </p>
      </div>

      {rows.length === 0 ? (
        <p
          data-testid="skill-usage-empty"
          className="text-xs text-muted-foreground"
        >
          {t("skillMatrix.usage.empty")}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-left text-xs">
            <thead className="bg-muted/50 text-muted-foreground">
              <tr>
                <th className="px-3 py-2 font-medium">
                  {t("skillMatrix.usage.skill")}
                </th>
                <th className="px-3 py-2 font-medium">
                  {t("skillMatrix.usage.channel")}
                </th>
                <th className="px-3 py-2 font-medium">
                  {t("skillMatrix.usage.count")}
                </th>
                <th className="px-3 py-2 font-medium">
                  {t("skillMatrix.usage.lastSeen")}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr
                  key={row.skill}
                  data-testid="skill-usage-row"
                  className="border-t"
                >
                  <td className="px-3 py-1.5 font-mono">{row.skill}</td>
                  <td className="px-3 py-1.5 text-muted-foreground">
                    {row.channels.join(", ")}
                  </td>
                  <td className="px-3 py-1.5 tabular-nums">
                    {t("skillMatrix.usage.times", { count: row.count })}
                  </td>
                  <td className="px-3 py-1.5 text-muted-foreground">
                    {row.lastSeen.slice(0, 16)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {idle.length > 0 && (
        <div data-testid="skill-usage-idle">
          <h3 className="text-xs font-medium text-destructive">
            {t("skillMatrix.usage.idleTitle")}
          </h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t("skillMatrix.usage.idleHint")}
          </p>
          <div className="mt-2 flex flex-wrap gap-1">
            {idle.map((name) => (
              <span
                key={name}
                data-testid="skill-usage-idle-item"
                className="rounded border border-destructive/40 bg-destructive/10 px-1.5 py-0.5 font-mono text-[10px] text-destructive"
              >
                {name}
              </span>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
