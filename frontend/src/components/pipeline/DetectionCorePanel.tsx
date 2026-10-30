import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { EventsOn } from "@/lib/wailsjs/runtime/runtime";
import { GetPatrolReport, RunDetectionTask } from "@/lib/wailsjs/go/app/App";
import type { AuditData } from "@/types/pipeline";
import { patrolKeys } from "@/lib/queryKeys";
import { ACTION_FEATURE_KEYS, trackFeature } from "@/lib/featureKeys";
import { usePanelStore } from "@/stores/usePanelStore";
import PatrolReportPanel from "./PatrolReportPanel";
import IssueTicketsPanel from "./IssueTicketsPanel";
import RetconPanel from "./RetconPanel";
import VolumeManagePanel from "./VolumeManagePanel";

interface Props {
  novelId: number;
}

// 检测核心结果枢纽（R5 检测核心前端收口，#257）：
// 取代旧 PipelineWizard 的「流水线阶段渲染 + 闸门硬卡」旧路径，仅聚合检测核心的
// **结果消费面**——四 kind 检测任务由 DetectionTasks（journey-quality）独立启动，
// 本面板只负责「汇总视图 + 人工修复入口」：
//   ① PatrolReportPanel —— 全书一致性巡查（张力报告，T1.3 双源合并取数）
//   ② IssueTicketsPanel —— 发现项工单（人工确认→去修复→复验闭环）
//   ③ RetconPanel       —— 冻结卷 retcon 候选案件审批
//   ④ VolumeManagePanel —— 卷冻结/解锁（既定事实管理）
// 与流水线阶段完全解耦：零 run 概念、零闸门、零 auto-advance；巡查报告经
// pipeline:consistency_patrol 事件失效重拉，tick 取报告 updated_at（落库时间戳）驱动
// 面板「运行中」提示收起（R5-3 T1.3 契约：finished 检测容器结果可达本面板）。
export default function DetectionCorePanel({ novelId }: Props) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [tick, setTick] = useState(0);
  // 进入来源上下文：从签约潜力工作流点「查看工单」跳来时记 "signability"，
  // 据此做澄清 + 排序，避免把顶部的「一致性巡检」误认为签约发现项。
  const panelContext = usePanelStore((s) => s.panelContext);
  const fromSignability = panelContext === "signability";

  // 全书一致性巡查报告（张力报告）：经 GetPatrolReport 双源合并取数
  // （活跃 run + structure 检测容器，按落库时间戳最新者胜）。取数失败静默
  // （降级零提示），只读展示，绝不进自动修复路由。
  const { data: patrolView } = useQuery({
    queryKey: patrolKeys.report(novelId),
    queryFn: async () => {
      const view = await GetPatrolReport(novelId);
      return view ?? null;
    },
    enabled: !!novelId,
  });

  const patrolData: AuditData | null = (() => {
    const raw = patrolView?.report;
    if (!raw) return null;
    try {
      return typeof raw === "string" ? (JSON.parse(raw) as AuditData) : raw;
    } catch {
      return null;
    }
  })();

  // 巡查完成事件到达即失效重拉；报告落库时间戳变化经 tick 驱动面板收起「运行中」。
  useEffect(() => {
    if (!novelId) return;
    const off = EventsOn(
      "pipeline:consistency_patrol",
      (p: { novel_id?: number }) => {
        if (p.novel_id !== novelId) return;
        queryClient.invalidateQueries({
          queryKey: patrolKeys.report(novelId),
        });
        setTick((prev) => prev + 1);
      },
    );
    return () => {
      off?.();
    };
  }, [novelId, queryClient]);

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 overflow-auto p-4">
        <div className="mb-2">
          <h1 className="text-base font-semibold text-foreground">
            {t("pipeline.title")}
          </h1>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {t("pipeline.detectCoreHint")}
          </p>
        </div>

        {/* 检测核心结果消费面：汇总视图 + 人工修复入口（与流水线阶段解耦）。 */}
        <div className="flex flex-col gap-3">
          {fromSignability ? (
            <div className="rounded-lg border border-primary/30 bg-primary/5 p-3 text-xs leading-relaxed text-foreground/90">
              {t("pipeline.fromSignabilityHint")}
            </div>
          ) : null}

          {fromSignability ? (
            <>
              {/* 来自签约流：优先呈现「发现项工单」（按钮承诺的内容），
                  一致性巡检降到其后并标注为独立模块，避免误判同源。 */}
              <IssueTicketsPanel novelId={novelId} />
              <PatrolReportPanel
                data={patrolData}
                tick={tick}
                independentNote={t("pipeline.patrol.independentNote")}
                onRun={() => {
                  trackFeature(ACTION_FEATURE_KEYS.patrol);
                  RunDetectionTask(novelId, "structure", null).catch(() => {});
                }}
              />
            </>
          ) : (
            <>
              <PatrolReportPanel
                data={patrolData}
                tick={tick}
                onRun={() => {
                  trackFeature(ACTION_FEATURE_KEYS.patrol);
                  RunDetectionTask(novelId, "structure", null).catch(() => {});
                }}
              />
              <IssueTicketsPanel novelId={novelId} />
            </>
          )}
          <RetconPanel novelId={novelId} />
          <VolumeManagePanel novelId={novelId} />
        </div>
      </div>
    </div>
  );
}
