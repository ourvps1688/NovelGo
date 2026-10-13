import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { GetPersonaSetting, SavePersonaSetting } from "@/lib/wailsjs/go/app/App";
import type { agentcfg } from "@/lib/wailsjs/go/models";
import { personaSettingKeys } from "@/lib/queryKeys";

// PersonaSelection 是 UI 层的"写作模式"选择：null=普通模式，否则为 persona 引用。
// 与后端 agentcfg.PersonaRef 对齐（kind: "skill" | "author"）。
export type PersonaSelection = agentcfg.PersonaRef | null;

// usePersonaSetting: 当前小说的写作人格（写作模式）持久化设置。
// 后端 GetPersonaSetting 对未配置的书返回零值（enabled=false），前端据此判普通模式。
// enabled: !!novelId 守卫，novelId=0 时不 fetch。
// select 把原始 PersonaSetting 投影成 PersonaSelection（关闭即 null）。
export function usePersonaSetting(novelId: number) {
  return useQuery({
    queryKey: personaSettingKeys.detail(novelId),
    queryFn: () => GetPersonaSetting(novelId),
    enabled: !!novelId,
    select: (s) =>
      s && s.enabled
        ? ({ kind: s.kind, ref: s.ref } as agentcfg.PersonaRef)
        : null,
  });
}

// useSavePersonaSetting: 保存/关闭写作人格（按书幂等 upsert）。
// onSuccess 直接 setQueryData 同步本地缓存（写即事实，无需 refetch）。
export function useSavePersonaSetting(novelId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { enabled: boolean; kind: string; ref: string }) =>
      SavePersonaSetting(novelId, {
        enabled: input.enabled,
        kind: input.kind,
        ref: input.ref,
      }),
    onSuccess: (_data, input) => {
      qc.setQueryData(personaSettingKeys.detail(novelId), {
        id: 0,
        novel_id: novelId,
        enabled: input.enabled,
        kind: input.kind,
        ref: input.ref,
        created_at: null,
        updated_at: null,
      });
    },
  });
}
