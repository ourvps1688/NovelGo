import { describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { contentKeys, personaSettingKeys, skillKeys } from "@/lib/queryKeys";
import { useSaveContent } from "./useSaveContent";

const { mockSaveContent } = vi.hoisted(() => ({ mockSaveContent: vi.fn() }));

vi.mock("@/lib/wailsjs/go/app/App", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/wailsjs/go/app/App")>()),
  SaveContent: mockSaveContent,
}));

describe("useSaveContent", () => {
  it("invalidates user skill content across novels and every skill list", async () => {
    mockSaveContent.mockResolvedValue(undefined);
    const qc = new QueryClient();
    const path = "~/.novelgo/skills/shared.md";
    for (const novelId of [1, 2]) {
      qc.setQueryData(contentKeys.detail(novelId, path), "old");
      qc.setQueryData(skillKeys.list(novelId), ["old"]);
    }
    // 预置技能启用清单与人格设置缓存（user 级 skill 落盘后后端会自动勾选常驻 +
    // 设为人格，前端需失效这两项缓存，故此处预置以便断言失效）。
    qc.setQueryData(skillKeys.enabled(1), ["old-enabled"]);
    qc.setQueryData(personaSettingKeys.detail(1), { enabled: false });
    qc.setQueryData(contentKeys.detail(2, "novelgo.md"), "other");
    const { result } = renderHook(() => useSaveContent(), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={qc}>{children}</QueryClientProvider>
      ),
    });

    await act(async () => {
      await result.current.mutateAsync({ novel_id: 1, path, content: "new" });
    });

    for (const novelId of [1, 2]) {
      expect(
        qc.getQueryState(contentKeys.detail(novelId, path))?.isInvalidated,
      ).toBe(true);
      expect(qc.getQueryState(skillKeys.list(novelId))?.isInvalidated).toBe(
        true,
      );
    }
    // 心智落盘后后端会自动勾选常驻 + 设为写作人格，失效启用清单与人格设置缓存。
    expect(qc.getQueryState(skillKeys.enabled(1))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(personaSettingKeys.detail(1))?.isInvalidated).toBe(
      true,
    );
    expect(
      qc.getQueryState(contentKeys.detail(2, "novelgo.md"))?.isInvalidated,
    ).toBe(false);
    qc.clear();
  });
});
