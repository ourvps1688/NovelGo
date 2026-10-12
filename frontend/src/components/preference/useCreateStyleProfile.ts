import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CreateStyleProfile } from "@/lib/wailsjs/go/app/App";
import type { app } from "@/lib/wailsjs/go/models";
import { styleProfileKeys } from "@/lib/queryKeys";
import { preferenceKeys } from "@/lib/queryKeys";

// useCreateStyleProfile: 将一次风格提取结果沉淀为「风格画像」mutation。
// onSuccess 失效风格画像库与偏好列表（启用后该书偏好随之刷新）。
export function useCreateStyleProfile(novelId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: app.CreateStyleProfileInput) =>
      CreateStyleProfile(input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: styleProfileKeys.list(novelId) });
      qc.invalidateQueries({ queryKey: preferenceKeys.list(novelId) });
    },
  });
}
