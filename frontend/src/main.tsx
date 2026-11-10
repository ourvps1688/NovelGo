import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import "./i18n";
import "./monaco/monacoSetup";
import App from "./App.tsx";
import ErrorBoundary from "@/components/shared/ErrorBoundary";

// 根级兜底：任何未被面板级 ErrorBoundary 捕获的渲染异常都显示可见错误页，
// 而不是让 React 清空 #root 留下纯白窗口（历史子代理回放曾触发此类白屏）。
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </StrictMode>,
);
