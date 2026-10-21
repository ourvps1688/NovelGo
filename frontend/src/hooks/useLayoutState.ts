import { useState, useCallback, useEffect, useRef } from "react";

const SIDEPANEL_KEY = "novelgo_sidepanel_width";
const CHATPANEL_KEY = "novelgo_chatpanel_width";

const SIDEPANEL_DEFAULT = 224;
// AI 对话面板：默认加宽保证 LLM 输出可读；max 提到 1200 便于宽屏拖拽
const CHATPANEL_DEFAULT = 600;

function loadNumber(
  key: string,
  fallback: number,
  min: number,
  max: number,
): number {
  try {
    const raw = localStorage.getItem(key);
    if (raw !== null) {
      const v = parseInt(raw, 10);
      if (!isNaN(v) && v >= min && v <= max) return v;
    }
  } catch {
    /* ignore */
  }
  return fallback;
}

export function useLayoutState() {
  const [sidePanelWidth, setSidePanelWidthRaw] = useState(() =>
    loadNumber(SIDEPANEL_KEY, SIDEPANEL_DEFAULT, 180, 480),
  );
  const [chatPanelWidthRaw, setChatPanelWidthRaw] = useState(() =>
    loadNumber(CHATPANEL_KEY, CHATPANEL_DEFAULT, 320, 1200),
  );
  // 跟踪窗口宽度：对话面板宽度需按窗口动态封顶，否则窄窗口下
  // 固定宽（最大 1200）的对话面板会把工作区挤扁甚至挤没。
  const [winW, setWinW] = useState(() => window.innerWidth);
  useEffect(() => {
    const onResize = () => setWinW(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  // 预留活动栏 + 侧栏 + 工作区最小可读宽度（约 780px），其余才给对话面板；
  // 下限仍为 320，极窄窗口时对话面板保持最小可用宽度。
  const chatMax = Math.max(320, winW - 780);
  const chatPanelWidth = Math.min(chatPanelWidthRaw, chatMax);

  // 防抖写入 localStorage，避免拖拽时频繁同步 I/O
  const sideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const chatTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (sideTimerRef.current) clearTimeout(sideTimerRef.current);
    sideTimerRef.current = setTimeout(() => {
      localStorage.setItem(SIDEPANEL_KEY, String(sidePanelWidth));
    }, 300);
    return () => {
      if (sideTimerRef.current) clearTimeout(sideTimerRef.current);
    };
  }, [sidePanelWidth]);

  useEffect(() => {
    if (chatTimerRef.current) clearTimeout(chatTimerRef.current);
    chatTimerRef.current = setTimeout(() => {
      localStorage.setItem(CHATPANEL_KEY, String(chatPanelWidth));
    }, 300);
    return () => {
      if (chatTimerRef.current) clearTimeout(chatTimerRef.current);
    };
  }, [chatPanelWidth]);

  const setSidePanelWidth = useCallback((w: number) => {
    setSidePanelWidthRaw(Math.min(480, Math.max(180, Math.round(w))));
  }, []);

  const setChatPanelWidth = useCallback((w: number) => {
    setChatPanelWidthRaw(Math.min(1200, Math.max(320, Math.round(w))));
  }, []);

  return {
    sidePanelWidth,
    chatPanelWidth,
    setSidePanelWidth,
    setChatPanelWidth,
  };
}
