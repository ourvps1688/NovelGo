import { create } from "zustand";

// useNavigationStore: 跨组件把 ContentPanel 的「按阅读序号打开章节」能力暴露给深层面板
// （如 IssueTicketsPanel 的检测发现项「跳转章节原文」）。WorkspaceView 挂载时 register，
// 卸载时 unregister；入口面板取 openChapterByReadingNumber 调用即可，零侵入内容/章节现有契约。
//
// 阅读序号（reading_number）是实时计算的展示序号，与 chapter_id（DB 主键）不等价；
// 调用方传入的 readingNumber 即 AuditIssue.chapter_ids 里的全局阅读序号，由 WorkspaceView
// 经 useChapters 数据反查为真实 chapter.Chapter 再委托 handleSelectChapter 打开。
interface NavigationState {
  openChapterByReadingNumber:
    ((novelId: number, readingNumber: number) => void) | null;
  register: (fn: (novelId: number, readingNumber: number) => void) => void;
  unregister: () => void;
}

export const useNavigationStore = create<NavigationState>((set) => ({
  openChapterByReadingNumber: null,
  register: (fn) => set({ openChapterByReadingNumber: fn }),
  unregister: () => set({ openChapterByReadingNumber: null }),
}));
