import { useState, useEffect, useRef, memo } from "react";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import {
  Loader2,
  CheckCircle2,
  XCircle,
  ChevronDown,
  Timer,
  Activity,
} from "lucide-react";
import type { TurnSegment } from "./types";
import ThinkingBlock from "./ThinkingBlock";
import MessageBubble from "./MessageBubble";
import ToolCallCard from "./ToolCallCard";
import CompressionBlock from "./CompressionBlock";
import "./SubagentCard.css";

interface Props {
  agentType: "memory" | "review";
  segments: TurnSegment[];
  status: "streaming" | "done" | "failed";
  // P1: 子 agent 重试状态（由 EventRetrying 携带 sub_task_id 时设置）
  retrying?: {
    attempt: number;
    maxRetries: number;
    errorMessage: string;
  } | null;
  // P1: 子 agent 最终失败原因（由 EventError 携带 sub_task_id 时设置）
  errorMessage?: string;
  // P4 T1.3: 长跑可见性——首/末事件时刻（毫秒 epoch，AgentEvent.timestamp 派生，
  // 事件协议零改动零后端）。缺时间戳则整块时钟不渲染（有则注入/无则降级）。
  startedAtMs?: number;
  lastActivityAtMs?: number;
}

// formatElapsed 把秒数格式化为 m:ss（≥1 小时 h:mm:ss），纯数值展示零 i18n。
export function formatElapsed(totalSec: number): string {
  const s = Math.max(0, Math.floor(totalSec));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${pad(m)}:${pad(sec)}`;
}

function getAgentMeta(
  t: TFunction,
): Record<string, { label: string; emoji: string }> {
  return {
    memory: { label: t("chat.memoryAnalyst"), emoji: "📝" },
    review: { label: t("chat.reviewEditor"), emoji: "🔍" },
  };
}

export default memo(function SubagentCard({
  agentType,
  segments,
  status,
  retrying,
  errorMessage,
  startedAtMs,
  lastActivityAtMs,
}: Props) {
  const { t } = useTranslation();
  const [collapsed, setCollapsed] = useState(status !== "streaming");
  const autoExpanded = useRef(false);
  const meta = getAgentMeta(t)[agentType];
  const isStreaming = status === "streaming";
  const isDone = status === "done";
  const isFailed = status === "failed";

  // ── P4 T1.3 长跑可见性（只读）：已运行时钟 + 最近活动心跳 ──
  // streaming 期间秒级刷新；完成/失败不启 interval（停表定格，以最后活动时刻为终点）。
  // interval 随卸载/状态切换清理（防泄漏）；静默期只显示数值，零弹窗零报错。
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    if (!isStreaming) return;
    setNowMs(Date.now());
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [isStreaming]);

  const hasClock = startedAtMs !== undefined;
  // 完成/失败态停表定格：终点=最近活动时刻，不再随时间走动。
  const endMs = isStreaming ? nowMs : (lastActivityAtMs ?? nowMs);
  const elapsedSec = hasClock
    ? Math.max(0, Math.floor((endMs - (startedAtMs as number)) / 1000))
    : undefined;
  // 心跳只在 streaming 期间显示（「最近活动 Xs 前」，事件流入即归零刷新）。
  const idleSec =
    isStreaming && lastActivityAtMs !== undefined
      ? Math.max(0, Math.floor((nowMs - lastActivityAtMs) / 1000))
      : undefined;

  const accentCls =
    agentType === "review" ? "subagent-review" : "subagent-memory";

  const prevStatusRef = useRef(status);

  useEffect(() => {
    const prev = prevStatusRef.current;
    prevStatusRef.current = status;

    if (isStreaming && !autoExpanded.current) {
      setCollapsed(false);
      autoExpanded.current = true;
    }
    if (prev !== "streaming") {
      autoExpanded.current = false;
    }
    if (prev === "streaming" && isDone) {
      const timer = setTimeout(() => setCollapsed(true), 1000);
      return () => clearTimeout(timer);
    }
  }, [status, isStreaming, isDone]);

  return (
    <div className="flex justify-start">
      <div
        className={`subagent-card max-w-[85%] ${accentCls} ${isStreaming ? "subagent-streaming" : ""}`}
      >
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="subagent-header"
        >
          <ChevronDown
            className={`shrink-0 transition-transform duration-200 text-muted-foreground/50 ${collapsed ? "" : "rotate-180"}`}
            size={12}
          />
          <span className="subagent-icon">{meta.emoji}</span>
          <span className="subagent-label">{meta.label}</span>

          {/* P4 T1.3 长跑可见性：已运行时钟 + 最近活动心跳（只读数值，静默期零弹窗） */}
          {hasClock && (
            <span className="subagent-clock">
              <Timer size={10} className="shrink-0" aria-hidden />
              <span
                data-testid="subagent-elapsed"
                title={t("chat.subagentElapsed", {
                  duration: formatElapsed(elapsedSec ?? 0),
                })}
              >
                {formatElapsed(elapsedSec ?? 0)}
              </span>
              {idleSec !== undefined && (
                <span
                  className="subagent-idle"
                  data-testid="subagent-idle"
                  data-seconds={idleSec}
                >
                  <Activity size={9} className="shrink-0" aria-hidden />
                  {t("chat.subagentLastActivity", { seconds: idleSec })}
                </span>
              )}
            </span>
          )}

          <span className="flex-1" />

          {/* P1: 子 agent 重试时优先显示重试 badge（覆盖 streaming 的执行中） */}
          {retrying ? (
            <span
              className="subagent-badge subagent-badge-running"
              title={retrying.errorMessage}
            >
              <Loader2 size={10} className="animate-spin" />{" "}
              {t("chat.retrying", {
                attempt: retrying.attempt,
                max: retrying.maxRetries,
              })}
            </span>
          ) : isStreaming ? (
            <span className="subagent-badge subagent-badge-running">
              <Loader2 size={10} className="animate-spin" />{" "}
              {t("chat.executing")}
            </span>
          ) : isDone ? (
            <span className="subagent-badge subagent-badge-done">
              <CheckCircle2 size={10} /> {t("chat.done")}
            </span>
          ) : isFailed ? (
            <span className="subagent-badge subagent-badge-failed">
              <XCircle size={10} /> {t("chat.failed")}
            </span>
          ) : null}
        </button>

        {isFailed && errorMessage && (
          <div className="tool-error">{errorMessage.slice(0, 60)}</div>
        )}

        <div
          className={`grid transition-all duration-300 ease-out ${
            collapsed
              ? "grid-rows-[0fr] opacity-0"
              : "grid-rows-[1fr] opacity-100"
          }`}
        >
          <div className="overflow-hidden border-t border-border/30">
            <div className="px-3 pb-3 space-y-2 pt-2">
              {segments.length === 0 && isStreaming && (
                <div className="flex items-center gap-2 text-xs text-muted-foreground py-2">
                  <Loader2 size={12} className="animate-spin" />{" "}
                  {t("chat.analyzing")}
                </div>
              )}
              {segments.length === 0 && !isStreaming && (
                <div className="text-xs text-muted-foreground py-2">
                  {t("chat.noContent")}
                </div>
              )}

              {segments.map((seg) => {
                if (seg.type === "compression") {
                  return (
                    <CompressionBlock
                      key={seg.id}
                      phase={seg.compressionPhase || "done"}
                    />
                  );
                }
                if (seg.type === "text") {
                  return (
                    <div key={seg.id} className="space-y-1">
                      {seg.thinkingContent && (
                        <ThinkingBlock
                          content={seg.thinkingContent}
                          isStreaming={isStreaming && !seg.thinkingDone}
                        />
                      )}
                      {seg.content && (
                        <div className="text-xs">
                          <MessageBubble
                            role="assistant"
                            content={seg.content}
                          />
                        </div>
                      )}
                    </div>
                  );
                }
                if (seg.type === "tool") {
                  return (
                    <ToolCallCard
                      key={seg.id}
                      toolName={seg.toolName}
                      displayText={seg.displayText}
                      status={seg.toolStatus}
                      activityKind={seg.activityKind}
                      error={seg.error}
                      result={seg.result}
                      compact
                    />
                  );
                }
                return null;
              })}
              {isFailed && errorMessage && (
                <div className="tool-error">{errorMessage.slice(0, 120)}</div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
});
