import { create } from "zustand";

// 新书孵化器跨组件状态：
// - incubatorId 指向当前正在孵化中的临时小说；null 表示没有进行中的孵化会话。
// - 该 id 在 BookPitchWorkflowView 被卸载后仍保留，以便用户切回 bookpitch 面板时继续。
// - 保存/取消后由 workflow 自行清空。
// - appliedPitchSig：最近一次已回填的终稿 JSON 签名（归一化后的字段拼接）。
//   用于避免同一段 JSON 被重复回填、覆盖用户在表单里的手动修改；新建/保存孵化器时清空。
interface BookPitchState {
  incubatorId: number | null;
  setIncubatorId: (id: number | null) => void;
  appliedPitchSig: string;
  setAppliedPitchSig: (sig: string) => void;
}

export const useBookPitchStore = create<BookPitchState>((set) => ({
  incubatorId: null,
  setIncubatorId: (id) => set({ incubatorId: id, appliedPitchSig: "" }),
  appliedPitchSig: "",
  setAppliedPitchSig: (sig) => set({ appliedPitchSig: sig }),
}));
