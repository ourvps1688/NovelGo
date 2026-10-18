import { useTranslation } from "react-i18next";
import { Check, Edit3, X } from "lucide-react";
import type { GatePayload, GateDecision } from "@/types/pipeline";

interface Props {
  gate: GatePayload;
  onResume: (decision: GateDecision, feedback?: string) => void;
}

// 闸门确认卡：监听 pipeline:gate 事件后渲染「继续 / 修改 / 终止」。
export default function PipelineGateCard({ gate, onResume }: Props) {
  const { t } = useTranslation();
  return (
    <div className="rounded border border-amber-300 bg-amber-50 p-3">
      <div className="font-medium text-amber-800">
        {t("pipeline.gate.title")}
      </div>
      {gate.summary ? (
        <p className="mt-1 text-sm text-gray-600">{gate.summary}</p>
      ) : null}
      <div className="mt-3 flex gap-2">
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded bg-black px-3 py-1 text-white text-sm"
          onClick={() => onResume("continue")}
        >
          <Check className="w-4 h-4" />
          {t("pipeline.gate.continue")}
        </button>
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded border px-3 py-1 text-sm"
          onClick={() => onResume("modify")}
        >
          <Edit3 className="w-4 h-4" />
          {t("pipeline.gate.modify")}
        </button>
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded border border-red-300 px-3 py-1 text-sm text-red-600"
          onClick={() => onResume("abort")}
        >
          <X className="w-4 h-4" />
          {t("pipeline.gate.abort")}
        </button>
      </div>
    </div>
  );
}
