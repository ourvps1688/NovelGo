import { useQuery } from "@tanstack/react-query";
import { GetFeatureUsage } from "@/lib/wailsjs/go/app/App";
import { featureUsageKeys } from "@/lib/queryKeys";
import type { activity } from "@/lib/wailsjs/go/models";

// useFeatureUsage: 近 N 天功能使用率聚合 query（P4 T2.3 消费面，纯本地不上传）。
// 沿 useWritingStats 模式：queryFn 直接 import wailsjs 绑定。
// GET 错误走全局中间件，但 "feature-usage" 前缀在 queryErrorToast.ts 静默口径
// 白名单内——加载失败=静默空态零报错（降级逐字一致）。
export type FeatureUsageStat = activity.FeatureUsageStat;

export function useFeatureUsage(days = 30) {
  return useQuery({
    queryKey: featureUsageKeys.detail(days),
    queryFn: async () => (await GetFeatureUsage(days)) as FeatureUsageStat[],
  });
}
