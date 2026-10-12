import { useQuery } from "@tanstack/react-query";
import { ListStyleProfiles } from "@/lib/wailsjs/go/app/App";
import { styleProfileKeys } from "@/lib/queryKeys";

// useStyleProfiles: 本书「风格画像」库（提取结果的沉淀）列表 query。
// 作为偏好面板中「写作风格」选择项的知识库。novelId<=0 时不拉取（绑定按书）。
export function useStyleProfiles(novelId: number) {
  return useQuery({
    queryKey: styleProfileKeys.list(novelId),
    queryFn: async () => {
      const r = await ListStyleProfiles({ novel_id: novelId });
      return r ?? [];
    },
    enabled: novelId > 0,
  });
}
