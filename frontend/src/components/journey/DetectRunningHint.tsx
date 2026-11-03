// DetectRunningHint 是检测「运行中」态的可观测提示（P1-D）：已运行时长 + 供应商停滞提示。
// 三处运行态（旅程检测卡 DetectionTasks / 检测工作流 DetectionWorkflowView / 签约潜力
// SignabilityWorkflowView）共用本组件；status 用结构化最小形状（不依赖 wails 绑定重生成
// 时点，DTO 新字段 running_since/stall_count 直接透传即可）；时长单位经 Intl.NumberFormat
// unit 格式本地化，源码零硬编码文案（i18n:check 红线）。
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

type DetectRunningStatusLike = {
  running?: boolean;
  running_since?: unknown;
  stall_count?: number;
};

// fmtDuration 把毫秒差格式化为本地化时长（秒/分钟/小时词干经 Intl unit 格式随语言变化；
// ≥1 小时拼「时 分」）。容错：非法时间输入由调用方过滤，此处只管格式化。
function fmtDuration(ms: number, locale: string): string {
  const sec = Math.max(0, Math.floor(ms / 1000));
  const unit = (u: "second" | "minute" | "hour", v: number) =>
    new Intl.NumberFormat(locale, {
      style: "unit",
      unit: u,
      unitDisplay: "long",
    }).format(v);
  if (sec < 60) return unit("second", sec);
  const min = Math.floor(sec / 60);
  if (min < 60) return unit("minute", min);
  return `${unit("hour", Math.floor(min / 60))} ${unit("minute", min % 60)}`;
}

export default function DetectRunningHint({
  status,
}: {
  status?: DetectRunningStatusLike;
}) {
  const { t, i18n } = useTranslation();
  const running = !!status?.running;
  // 时钟走针（SubagentCard 同款模式）：render 纯函数不碰 Date.now()（purity 规则红线），
  // 走针只在 interval 回调里更新 state；运行中 1s 一跳，收口即停表。
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [running]);
  if (!running) return null;
  const sinceMs = status?.running_since
    ? Date.parse(String(status.running_since))
    : NaN;
  const elapsedMs = Number.isNaN(sinceMs) ? null : Math.max(0, nowMs - sinceMs);
  const stalls = status?.stall_count ?? 0;
  return (
    <>
      {elapsedMs !== null ? (
        <span data-testid="detect-running-elapsed">
          {t("journey.quality.detect.runningSince", {
            duration: fmtDuration(elapsedMs, i18n.language),
          })}
        </span>
      ) : null}
      {stalls > 0 ? (
        <span
          data-testid="detect-running-stall"
          className="rounded bg-tag-amber px-1.5 py-0.5 text-tag-amber-foreground"
        >
          {t("journey.quality.detect.slowProvider", { count: stalls })}
        </span>
      ) : null}
    </>
  );
}
