import { create } from "zustand";
import type { PersonaSelection } from "@/components/chat/usePersonaSetting";

// useChatLaunchStore: 跨组件把 ChatPanel 的发送能力暴露给其它面板。
// 签约潜力工作流等"专属入口"需要把一条 kickoff 消息直接发到聊天，但 ChatPanel
// 的 handleSend 没有经 props 向上透传。ChatPanel 挂载时 register 当前 handleSend，
// 卸载时 unregister；入口面板取 send 调用即可，零侵入 ChatInput/ChatPanel 现有契约。
//
// pendingKickoff: 新书孵化工作流在书架为空时自动建第一本书并切换 activeNovelId，
// 此时 ChatPanel 需要一帧用新书 novelId 重新注册 send。若入口面板直接取 send 调
// 用，极可能因闭包仍是旧 novelId 而发到错误上下文。故改为"写入待发消息"，由
// ChatPanel 在新 novelId 上下文就绪后自动发送，彻底规避闭包竞态。
interface ChatLaunchState {
  send: ((content: string, persona?: PersonaSelection | null) => void) | null;
  pendingKickoff: string | null;
  register: (
    fn: (content: string, persona?: PersonaSelection | null) => void,
  ) => void;
  unregister: () => void;
  setPendingKickoff: (content: string | null) => void;
}

export const useChatLaunchStore = create<ChatLaunchState>((set) => ({
  send: null,
  pendingKickoff: null,
  register: (fn) => set({ send: fn }),
  unregister: () => set({ send: null }),
  setPendingKickoff: (content) => set({ pendingKickoff: content }),
}));
