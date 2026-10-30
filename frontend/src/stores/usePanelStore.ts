import { create } from "zustand";
import type { PanelId, SidebarPanelId } from "@/types/panel";

interface PanelState {
  activePanel: PanelId;
  // 进入面板的来源上下文（如从签约潜力工作流点「查看工单」跳到检测核心时记
  // "signability"），供目标面板做差异化澄清/排序。每次导航都重写，默认 null。
  panelContext: string | null;
  sidebarPanel: SidebarPanelId | null;
  sidebarClosed: boolean;
  setActivePanel: (panel: PanelId, context?: string | null) => void;
  setSidebarPanel: (panel: SidebarPanelId | null) => void;
  setSidebarClosed: (closed: boolean) => void;
}

// activePanel 默认 "novels"；WorkspaceView mount 时用 initialNovelId 覆盖为 "chapters"。
// handleActivitySelect 的折叠/展开逻辑是 condition-based，由调用方用 3 个 setter 组合实现，
// 不提供 toggleSidebar（无简单 toggle 语义）。
export const usePanelStore = create<PanelState>((set) => ({
  activePanel: "novels",
  panelContext: null,
  sidebarPanel: null,
  sidebarClosed: false,
  setActivePanel: (panel, context = null) =>
    set({ activePanel: panel, panelContext: context }),
  setSidebarPanel: (panel) => set({ sidebarPanel: panel }),
  setSidebarClosed: (closed) => set({ sidebarClosed: closed }),
}));
