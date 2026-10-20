import { useTranslation } from "react-i18next";
import { CheckCircle2, XCircle } from "lucide-react";
import type { FixActivityItem } from "@/types/pipeline";

// StageActivityPanel 是所有流水线执行阶段（S2 蓝图 / S4 体检 / S5 修复 / S6 终检）
// 共用的「实时动态」面板：统一展示 pipeline-agent 事件的工具活动，
// 让每个工作环节都按同一界面语言告知用户「正在干什么、干到哪了」。
// 活动区不做过高压缩（max-h 60vh），避免工作区被挤成一条缝。
export default function StageActivityPanel({
  activity,
}: {
  activity: FixActivityItem[];
}) {
  const { t } = useTranslation();
  const items = [...activity].reverse();
  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-medium text-muted-foreground">
        {t("pipeline.stageActivity.title")}
      </div>
      {items.length > 0 ? (
        <ul className="flex max-h-[60vh] flex-col gap-1 overflow-auto rounded border bg-card px-3 py-2">
          {items.map((it, i) => (
            <li
              key={`${it.seq}-${i}`}
              className="flex items-start gap-2 text-muted-foreground text-xs"
            >
              {it.failed ? (
                <XCircle className="h-3 w-3 shrink-0 text-danger-foreground" />
              ) : (
                <CheckCircle2 className="h-3 w-3 shrink-0 text-success-foreground" />
              )}
              <span className="min-w-0 break-words">{it.text}</span>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-muted-foreground text-xs">
          {t("pipeline.stageActivity.waiting")}
        </p>
      )}
    </div>
  );
}
