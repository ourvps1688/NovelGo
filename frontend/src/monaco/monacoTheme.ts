// 墨色（ink）Monaco 主题：与 frontend/src/index.css 的浅/深 token 对齐，
// 把编辑器从 VS Code 默认的蓝/青高亮切换为极简黑白灰，消除与外壳皮肤的视觉割裂。
//
// 由 monacoSetup 在 app 启动时最早注册（defineTheme 是全局单例、幂等），
// 之后各编辑器通过 theme prop 引用 INK_LIGHT / INK_DARK 即可。
import * as monaco from "monaco-editor";

export const INK_LIGHT = "ink-light";
export const INK_DARK = "ink-dark";

let registered = false;

export function registerInkThemes(): void {
  if (registered) return;
  registered = true;

  // ── 浅色：背景对齐 --background(#fafafa)，前景/行号/选区用中性灰 ──
  monaco.editor.defineTheme(INK_LIGHT, {
    base: "vs",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": "#fafafa",
      "editor.foreground": "#333333",
      "editorLineNumber.foreground": "#bdbdbd",
      "editorLineNumber.activeForeground": "#737373",
      "editor.selectionBackground": "rgba(40,40,45,0.14)",
      "editor.inactiveSelectionBackground": "rgba(40,40,45,0.10)",
      "editor.lineHighlightBackground": "rgba(0,0,0,0.025)",
      "editor.lineHighlightBorder": "transparent",
      "editorCursor.foreground": "#2a2a2e",
      "editorWhitespace.foreground": "transparent",
      "editorIndentGuide.background": "rgba(0,0,0,0.06)",
      "editorIndentGuide.activeBackground": "rgba(0,0,0,0.12)",
      "editorGutter.background": "#fafafa",
      "editor.findMatchBackground": "rgba(40,40,45,0.18)",
      "editor.findMatchHighlightBackground": "rgba(40,40,45,0.10)",
      "editorWidget.background": "#ffffff",
      "editorWidget.border": "#e5e5e5",
      "editorOverviewRuler.border": "#eeeeee",
      "editorBracketMatch.background": "transparent",
      "editorBracketMatch.border": "rgba(0,0,0,0.25)",
      "diffEditor.insertedTextBackground": "rgba(45,212,160,0.14)",
      "diffEditor.removedTextBackground": "rgba(220,90,90,0.10)",
      "diffEditor.insertedTextBorder": "transparent",
      "diffEditor.removedTextBorder": "transparent",
      "scrollbarSlider.background": "rgba(0,0,0,0.16)",
      "scrollbarSlider.hoverBackground": "rgba(0,0,0,0.26)",
    },
  });

  // ── 深色：背景对齐 --background(#292929)，前景/行号/选区同为中性灰反转 ──
  monaco.editor.defineTheme(INK_DARK, {
    base: "vs-dark",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": "#292929",
      "editor.foreground": "#ededed",
      "editorLineNumber.foreground": "#5c5c5c",
      "editorLineNumber.activeForeground": "#8a8a8a",
      "editor.selectionBackground": "rgba(237,237,237,0.18)",
      "editor.inactiveSelectionBackground": "rgba(237,237,237,0.12)",
      "editor.lineHighlightBackground": "rgba(255,255,255,0.04)",
      "editor.lineHighlightBorder": "transparent",
      "editorCursor.foreground": "#ededed",
      "editorWhitespace.foreground": "transparent",
      "editorIndentGuide.background": "rgba(255,255,255,0.07)",
      "editorIndentGuide.activeBackground": "rgba(255,255,255,0.14)",
      "editorGutter.background": "#292929",
      "editor.findMatchBackground": "rgba(237,237,237,0.22)",
      "editor.findMatchHighlightBackground": "rgba(237,237,237,0.12)",
      "editorWidget.background": "#1f1f1f",
      "editorWidget.border": "#3a3a3a",
      "editorOverviewRuler.border": "#333333",
      "editorBracketMatch.background": "transparent",
      "editorBracketMatch.border": "rgba(255,255,255,0.25)",
      "diffEditor.insertedTextBackground": "rgba(45,212,160,0.16)",
      "diffEditor.removedTextBackground": "rgba(220,90,90,0.12)",
      "diffEditor.insertedTextBorder": "transparent",
      "diffEditor.removedTextBorder": "transparent",
      "scrollbarSlider.background": "rgba(255,255,255,0.16)",
      "scrollbarSlider.hoverBackground": "rgba(255,255,255,0.26)",
    },
  });
}
