# Phase 4 · QA 报告 — book-pitch v2

## 验收标准逐条核对（对照 Phase 1）

| # | 验收标准 | 结果 | 证据 |
|---|---------|------|------|
| 1 | book-pitch.md 含完整方法论 + 终稿 JSON 契约与前端字段逐一对齐 | ✅ | book-pitch.md v2 含书名/简介/标签/方向/卖点/自检六章 + 终稿 schema；`PitchData` 8 字段 = JSON schema 8 字段完全一致 |
| 2 | 前端能解析 8 字段并写回书架 | ✅ | `extractPitchJSON` + 自动 effect 的 `UpdateNovel`(title/desc/genre) + `persistPitchPreferences`(tags/dir/oneLiner/platform/style) |
| 3 | 手动改字段不被后续 JSON 覆盖 + 冲突提示 | ✅ | `manualFieldsRef` 保护；`fillFromData` 跳过 manual 字段；冲突时 `toastInfo(conflictToast)` 列出中文字段名 |
| 4 | 「采用 AI 建议」按钮全量覆盖 | ✅ | `handleAdoptAll` → `fillFromData(data, true)` 清空 manual 后全量覆盖并保存 |
| 5 | 聊天 UI 不显示 ```bookpitch 块 | ✅（前期提交） | `MessageBubble` 经 `sanitizeAssistantContent` 过滤 |
| 6 | tsc/prettier/i18n/theme/eslint(0e)/vite build 全过 | ✅ | 见下「门禁结果」 |
| 7 | 提交并推送 origin/master | ⏳ | Phase 5 执行 |

## 契约一致性自检（专家 SOP 要求的语义判据替代项）
- 书稿 schema 字段集合：`{title, description, tags, direction, genre, platform, style, oneLiner}`
- 前端 `PitchData` 字段集合：同上 8 字段
- 结论：**完全一致**，无增删。前端解析器 `extractPitchJSON` 不引入额外字段。

## 门禁结果（替代 WorkBuddy validate_skill.py）
- `npx tsc -b`：通过（0 错误）
- `npx prettier --check "src/**/*.{ts,tsx}"`：通过
- `npm run i18n:check`：通过（无硬编码中文、key 结构一致、无空值）
- `npm run theme:check`：通过（8 warning 均为豁免模式，0 error）
- `npx eslint .`：0 error（162 warning 为存量，与本变更无关）
- `npx vite build --outDir ./distN` → `git clean -fd`：构建成功，产物已清理

## 风险与备注
- `manualFieldsRef` 仅在内存，组件卸载（切面板）后丢失：符合预期（切回面板会重新解析最新 JSON；用户手动值已落书架，不会丢）。
- 自动保存用 merged（手动字段保留其手动值），与表单显示一致，避免「保存后书架与画面不一致」。
- i18n 字段名改用 `bookpitch.fXxx` key 本地化，未引入源码硬编码中文。
