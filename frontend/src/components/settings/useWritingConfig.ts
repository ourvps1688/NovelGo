import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  GetWritingConfig,
  SaveWritingConfig,
} from "@/lib/wailsjs/go/app/App";
import type { app as appModels, writingconfig } from "@/lib/wailsjs/go/models";
import { writingConfigKeys } from "@/lib/queryKeys";

// useWritingConfig: 本书写作评分配置 query（auto_score_enabled / score_threshold /
// score_max_retries）。后端未配置时返回带缺省值的 WritingConfig，前端无需区分"未配置"。
export function useWritingConfig(novelId: number) {
  return useQuery({
    queryKey: writingConfigKeys.detail(novelId),
    queryFn: async () => {
      const cfg = await GetWritingConfig(novelId);
      return cfg;
    },
    enabled: novelId > 0,
  });
}

// useSaveWritingConfig: 保存本书写作评分配置（按 novel_id 幂等 upsert）。
// onSuccess invalidate writingConfigKeys.detail，让 WritingConfigTab 自动 refetch。
export function useSaveWritingConfig(novelId: number) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: appModels.SaveWritingConfigInput) => {
      await SaveWritingConfig(novelId, input);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: writingConfigKeys.detail(novelId),
      });
    },
  });
}

export type { writingconfig };
