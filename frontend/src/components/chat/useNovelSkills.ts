import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { GetEnabledSkills, SetEnabledSkills } from "@/lib/wailsjs/go/app/App";
import { skillKeys } from "@/lib/queryKeys";

// useEnabledSkills: 当前小说勾选启用的技能名清单（存于小说技能目录 .enabled.json）。
// enabled: !!novelId 守卫，novelId=0 时不 fetch。
// GetEnabledSkills 是旧风格 API（直接返回数组，非 apperr.Result），无需 unwrapResult。
export function useEnabledSkills(novelId: number) {
  return useQuery({
    queryKey: skillKeys.enabled(novelId),
    queryFn: async () => {
      const names = await GetEnabledSkills(novelId);
      return names ?? [];
    },
    enabled: !!novelId,
  });
}

// useSetEnabledSkills: PUT 全量覆盖启用清单。
// onSuccess 直接 setQueryData 同步本地缓存（后端写入成功即事实，无需 refetch）。
// 错误处理由调用方 catch + toast。
export function useSetEnabledSkills(novelId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (names: string[]) =>
      SetEnabledSkills({ novel_id: novelId, names }),
    onSuccess: (_data, names) => {
      qc.setQueryData(skillKeys.enabled(novelId), names);
    },
  });
}
