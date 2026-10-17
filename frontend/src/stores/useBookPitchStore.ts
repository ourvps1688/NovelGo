import { create } from "zustand";

// 新书孵化器跨组件状态：
// - incubatorId 指向当前正在孵化中的临时小说；null 表示没有进行中的孵化会话。
// - 该 id 在 BookPitchWorkflowView 被卸载后仍保留，以便用户切回 bookpitch 面板时继续。
// - 保存/取消后由 workflow 自行清空。
interface BookPitchState {
  incubatorId: number | null;
  setIncubatorId: (id: number | null) => void;
}

export const useBookPitchStore = create<BookPitchState>((set) => ({
  incubatorId: null,
  setIncubatorId: (id) => set({ incubatorId: id }),
}));
