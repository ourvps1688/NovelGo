import { useMutation, useQueryClient } from "@tanstack/react-query";
import { DeleteStyleProfile } from "@/lib/wailsjs/go/app/App";
import { styleProfileKeys } from "@/lib/queryKeys";
import { preferenceKeys } from "@/lib/queryKeys";

// useDeleteStyleProfile: 删除一条「风格画像」mutation。
// 后端级联移除其激活的本书偏好；onSuccess 失效画像库与偏好列表。
export function useDeleteStyleProfile(novelId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => DeleteStyleProfile({ id }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: styleProfileKeys.list(novelId) });
      qc.invalidateQueries({ queryKey: preferenceKeys.list(novelId) });
    },
  });
}
