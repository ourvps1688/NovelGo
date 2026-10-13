import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { GetAuthorAliases, SaveAuthorAliases } from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import { skillKeys } from "@/lib/queryKeys";

// useAuthorAliases: 读取用户级「作者别名映射」配置。
// queryKey ["authorAliases"]，与技能列表缓存解耦（别名是全局用户级配置，不随 novel 变化）。
export function useAuthorAliases() {
  return useQuery({
    queryKey: ["authorAliases"],
    queryFn: async () => {
      const res = await GetAuthorAliases();
      if (!res) return { version: 1, groups: [] } as app.GetAuthorAliasesResult;
      return res;
    },
  });
}

// useSaveAuthorAliases: 保存作者别名映射，成功后失效别名配置与技能列表（让作者档案刷新）。
export function useSaveAuthorAliases() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: app.SaveAuthorAliasesInput) => SaveAuthorAliases(input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["authorAliases"] });
      qc.invalidateQueries({ queryKey: skillKeys.all });
    },
  });
}
