import { useTranslation } from "react-i18next";
import { Pause, Play, Square, Zap } from "lucide-react";
import type { ControlCmd } from "@/types/pipeline";

interface Props {
  status: string;
  autoRun: boolean;
  onControl: (cmd: ControlCmd) => void;
}

// 运行控制浮钮：暂停 / 恢复 / 停止 + 自动运行开关。随状态切换可点性。
export default function PipelineControlBar({
  status,
  autoRun,
  onControl,
}: Props) {
  const { t } = useTranslation();
  const running = status === "running" || status === "awaiting_gate";
  // 自动运行仅对活跃（运行中 / 等待闸门）的流水线有意义。
  const canAuto = running || status === "paused";
  return (
    <div className="flex items-center gap-1">
      <button
        type="button"
        className={`p-1 rounded hover:bg-black/5 disabled:opacity-40 ${
          autoRun ? "text-primary" : "text-muted-foreground"
        }`}
        title={t("pipeline.autoRun")}
        disabled={!canAuto}
        onClick={() => onControl("set_auto")}
      >
        <Zap className="w-4 h-4" />
      </button>
      {running ? (
        <button
          type="button"
          className="p-1 rounded hover:bg-black/5"
          title={t("pipeline.control.pause")}
          onClick={() => onControl("pause")}
        >
          <Pause className="w-4 h-4" />
        </button>
      ) : (
        <button
          type="button"
          className="p-1 rounded hover:bg-black/5 disabled:opacity-40"
          title={t("pipeline.control.resume")}
          disabled={status !== "paused"}
          onClick={() => onControl("resume")}
        >
          <Play className="w-4 h-4" />
        </button>
      )}
      <button
        type="button"
        className="p-1 rounded hover:bg-black/5"
        title={t("pipeline.control.stop")}
        onClick={() => onControl("stop")}
      >
        <Square className="w-4 h-4" />
      </button>
    </div>
  );
}
