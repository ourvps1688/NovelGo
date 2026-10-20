import { useTranslation } from "react-i18next";
import { BookOpen, Loader2 } from "lucide-react";
import type { pipeline } from "@/lib/wailsjs/go/models";
import type {
  GatePayload,
  GateDecision,
  FixActivityItem,
} from "@/types/pipeline";
import PipelineGateCard from "./PipelineGateCard";
import StageActivityPanel from "./StageActivityPanel";

interface VolumeDraft {
  id: number;
  name: string;
  sort_order: number;
}

interface BlueprintData {
  summary?: string;
  volumes?: VolumeDraft[];
}

interface Props {
  run: pipeline.PipelineRun | null;
  stage: string;
  status: string;
  gate: GatePayload | null;
  onResume: (decision: GateDecision, feedback?: string) => void;
  // 生成期间的实时活动流（与 S4/S5/S6 共用同一面板界面）。
  activity?: FixActivityItem[];
}

// S2 蓝图视图：进入 S2 后后端异步执行 story-blueprint（经 create_volume 建卷落库），
// 期间显示「生成中」+ 实时动态（与其它执行阶段一致的界面语言）；
// 完成后经 G1 闸门携带卷结构概览（run.ContextJSON.blueprint）渲染，
// 并复用 PipelineGateCard 让用户确认。非 S2 阶段返回 null，由父组件按原逻辑渲染。
export default function BlueprintWorkflowView({
  run,
  stage,
  status,
  gate,
  onResume,
  activity = [],
}: Props) {
  const { t } = useTranslation();

  if (stage !== "S2") return null;

  // 生成中：S2 running 且无闸门。
  if (status === "running" && !gate) {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-2">
          <Loader2 className="h-4 w-4 animate-spin" />
          <span className="text-sm font-medium">
            {t("pipeline.blueprint.generating")}
          </span>
        </div>
        <p className="text-muted-foreground text-xs">
          {t("pipeline.stageActivity.hint")}
        </p>
        <StageActivityPanel activity={activity} />
      </div>
    );
  }

  // G1 闸门：解析 run.ContextJSON 中的蓝图结构概览。
  let blueprint: BlueprintData | null = null;
  if (run?.ContextJSON) {
    try {
      const ctx = JSON.parse(run.ContextJSON);
      if (ctx.blueprint) blueprint = JSON.parse(ctx.blueprint) as BlueprintData;
    } catch {
      blueprint = null;
    }
  }
  const volumes = blueprint?.volumes ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        <BookOpen className="h-4 w-4 text-muted-foreground" />
        <span className="text-sm font-medium">
          {t("pipeline.blueprint.title")}
        </span>
        {volumes.length > 0 ? (
          <span className="text-xs text-muted-foreground">
            {t("pipeline.blueprint.volumeCount", { n: volumes.length })}
          </span>
        ) : null}
      </div>

      {blueprint?.summary ? (
        <p className="whitespace-pre-wrap text-sm text-muted-foreground">
          {blueprint.summary}
        </p>
      ) : null}

      {volumes.length > 0 ? (
        <ol className="flex flex-col gap-2">
          {volumes.map((v) => (
            <li key={v.id} className="rounded border bg-card px-3 py-2 text-sm">
              <span className="mr-2 text-muted-foreground">
                {v.sort_order}.
              </span>
              {v.name}
            </li>
          ))}
        </ol>
      ) : (
        <p className="text-sm text-muted-foreground">
          {t("pipeline.blueprint.empty")}
        </p>
      )}

      {gate ? <PipelineGateCard gate={gate} onResume={onResume} /> : null}
    </div>
  );
}
