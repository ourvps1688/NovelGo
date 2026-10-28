import { create } from "zustand";

// 全局「打开设置」通道：让任意面板（如工单面板的「切换模型」）都能从任意位置
// 打开 WorkspaceView 层级的 SettingsDialog 并定位到指定 tab，无需逐层透传 props。
// WorkspaceView 订阅 open/tab 渲染 SettingsDialog；其它面板只调用 requestOpen。
type SettingsTab = "general" | "model" | "about";

interface SettingsOpenState {
  open: boolean;
  tab: SettingsTab | null;
  requestOpen: (tab?: SettingsTab) => void;
  close: () => void;
}

export const useSettingsOpenStore = create<SettingsOpenState>((set) => ({
  open: false,
  tab: null,
  requestOpen: (tab: SettingsTab = "general") => set({ open: true, tab }),
  close: () => set({ open: false, tab: null }),
}));
