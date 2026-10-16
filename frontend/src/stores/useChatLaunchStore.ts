import { create } from "zustand";
import type { PersonaSelection } from "@/components/chat/usePersonaSetting";

// useChatLaunchStore: 跨组件把 ChatPanel 的发送能力暴露给其它面板。
// 签约潜力工作流等"专属入口"需要把一条 kickoff 消息直接发到聊天，但 ChatPanel
// 的 handleSend 没有经 props 向上透传。ChatPanel 挂载时 register 当前 handleSend，
// 卸载时 unregister；入口面板取 send 调用即可，零侵入 ChatInput/ChatPanel 现有契约。
interface ChatLaunchState {
  send: ((content: string, persona?: PersonaSelection | null) => void) | null;
  register: (
    fn: (content: string, persona?: PersonaSelection | null) => void,
  ) => void;
  unregister: () => void;
}

export const useChatLaunchStore = create<ChatLaunchState>((set) => ({
  send: null,
  register: (fn) => set({ send: fn }),
  unregister: () => set({ send: null }),
}));
