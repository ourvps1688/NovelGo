# NovelGo 代码审计总览（2026-10-29）

> **合并自两份审计**：
> - **维度 A · 设计合规性专项** —— 完整报告见 `docs/CODE_AUDIT_REPORT_2026-10-29.md`（含逐条证据路径/行号/解决方案）。
> - **维度 B · 产品/功能缺口专项** —— 早前「成为优秀 AI 写作工具」审计；其详细报告文件未落盘，本章为该审计的**记录结论**（含部分 `file:line` 证据），用于与维度 A 互补。
>
> **范围**：后端 `app/` + `internal/`、前端 `frontend/src`；只读静态分析。
> **方法**：并行深度审计 Agent（backend / frontend）+ 主理人交叉复核。

---

## 1. 总体结论

- **无 P0 灾难性缺陷**，代码整体健康；核心重构（检测核心 #257–#263、供应商熔断重设计）已正确落地。
- 待改进集中在两个互补维度：
  - **维度 A 设计合规**：分层越界、契约漂移、重构残留 —— 多为「架构卫生」问题，影响可维护性与前后端一致性。
  - **维度 B 产品缺口**：写作上下文智能、规模化性能、检测闭环 UX 最后一跳 —— 多为「体验/能力」缺口，影响「成为优秀工具」。
- 两份审计无冲突，可统一排期。建议先修**数据正确性 + 契约违反 + 闭环断点**三件高优项。

---

## 2. 维度 A：设计合规性（架构 / 分层 / 契约 / DRY）

> 完整证据与解决方案见 `docs/CODE_AUDIT_REPORT_2026-10-29.md`。此处为摘要。

### 2.1 后端

| 严重度 | 问题 | 位置 | 建议方案 |
|--------|------|------|----------|
| **高** | `SaveContent` 在绑定层编排 5 介质（Git/向量/缓存/写作统计/直连 DB 写字数），违反「app 不承载领域编排、跨介质应走 Service」 | `app/content.go:58-114` | 抽 `internal/content.ContentService` 统一编排；app 仅 DTO+调用 |
| 中 | chat 直连 `a.db`/`a.session.DB` 绕过 Store，git 提交编排在 app 层 | `app/chat.go:89,145-173,230,279,299,543-598` | 落库走 Store；git 收尾下沉领域服务 |
| 中 | 多个 `internal` 包直接 `import wails` 并 `EventsEmit`，且 `AGENTS.md` 禁止而 `internal/agent/DESIGN.md` 允许 —— **文档自相矛盾** | `internal/agent/*`、`internal/pipeline/events.go`、`internal/mcp_tools/rw_tools.go` | 定义 `EventEmitter` 接口由 app 注入；统一两份文档口径 |
| 中 | `chapter.Service.Delete` 跨介质一致性窗口：文件删在 DB 提交前；向量/缓存提交后 best-effort 无补偿 | `internal/chapter/service.go:113-150` | 删除走「提交后才执行+重试」；向量/缓存改可重放补偿 + 启动期孤儿清理 |
| 中 | `pipeline/service.go` 单文件 2855 行 god-service | `internal/pipeline/service.go` | 按子域拆 `issues.go`/`freeze.go`/`patrol.go`/`gate.go`（低优先） |
| 低 | legacy `StartPipeline/ResumePipeline/ControlPipeline` 仍前端可见 API | `app/pipeline.go:10,26,34` | 隔离/剔除前端可见 legacy 入口 |
| 低 | Wails 边界错误类型丢失，错误适配=字符串约定 | `app/content.go:78` | 统一错误码常量/构造器并文档化 |
| 低 | `app.App` 30+ 字段 god-object | `app/handler.go:54-111` | 装配抽到 `bootstrap.go`（低优先） |

### 2.2 前端

| 严重度 | 问题 | 位置 | 建议方案 |
|--------|------|------|----------|
| **高** | `NewBookWizard` 仍真实调用 `StartPipeline` 并跳 `pipeline` 面板 —— **唯一违反单源契约的活代码**，且导航目标语义已错配 | `src/components/novel/NewBookWizard.tsx:7,198,199,201` | 改 `RunDetectionTask(id,"book",null)`；`setActivePanel("signability")`；同步改文案 |
| 中 | `types/pipeline.ts` 残留 12 个旧闸门/控制/修复死类型（0 引用） | `src/types/pipeline.ts` | 删除死类型，保留 `AuditData/AuditIssue/IssueTicketRef/IssueVerifySummary` |
| 中 | i18n `pipeline.*` 大量孤儿键（gate/control/blueprint… 0 引用）；`i18n-check.mjs` 不解析代码 `t()` 故能静默过闸 | `zh-CN.json:1826+` / `en.json:1866+` | 删除孤儿子树；`i18n-check.mjs` 加孤儿键检测 |
| 中 | `AuditIssue` 缺契约强制的 `fix_type` 字段（契约源 `signability-audit.md:96-103`，后端已产出） | `src/types/pipeline.ts:48-62` | 补 `fix_type?: string` 并对齐解析 |
| 中 | 活跃文案仍写「流水线/阶段条/Start Pipeline」，与「检测核心」语义冲突 | `wizard.launchHint/launchedToast/launchFailed`、`pipeline.presetHint`、`narrative.tagline` | 统一「检测核心/质检」语义 |
| 中 | 5 个关键面板无专属测试（DetectionCorePanel/IssueTicketsPanel/PatrolReportPanel/RetconPanel/VolumeManagePanel） | 各面板目录 | 补 IssueTicketsPanel/DetectionCorePanel 状态机与渲染测试 |
| 中 | `fmtTime` 复制 5 份、`STATUS_CLS` 复制 2 份 | 多面板 | 抽 `utils/datetime.ts` 与 `pipeline/ticketStatus.ts` |
| 低/中 | 列表加载范式不统一（react-query vs 手写 `useState+load`） | `IssueTicketsPanel:86` / `RetconPanel:26` / `VolumeManagePanel:26` | 统一 `useQuery` + 新增 `queryKeys` |
| 低 | structure 检测双入口 | `DetectionCorePanel.tsx:91-97` / `DetectionTasks.tsx:270` | 抽统一 `launch()` hook |
| 低 | DetectionCorePanel 内 4 子面板未各自包 ErrorBoundary | `DetectionCorePanel.tsx:88-101` | 各包一层 ErrorBoundary |
| 低 | 陈旧注释引用已删 `usePipeline` | `PatrolReportPanel.tsx:19` | 改注释（无需授权） |

### 2.3 维度 A 已达标（无需改）

breaker/retry 接线自洽、无空闲熔断残留；`signability-audit.md` 契约与解析一致；structure/quality 恒 P1 只报告；度量纯本地不上传；cgo tag 与 singleton 正确；#257–#263 主重构已收敛；`factory.ts` 零删除纪律未被违反；`featureKeys.ts` 单源一致；顶层面板 ErrorBoundary 完善；桌面优先无移动端反模式。

---

## 3. 维度 B：产品 / 功能缺口（记录结论）

> 来源：早前「成为优秀 AI 写作工具」审计的记录。总体缺口集中在三类：**① 写作上下文智能 ② 规模化性能 ③ 检测闭环 UX 最后一跳**。

### 3.1 P1 关键项

| 问题 | 位置 | 影响维度 |
|------|------|----------|
| 消息排序无 id tiebreaker | `internal/session/store.go:206` | 数据正确性（同秒消息顺序不定） |
| SQLite 单连接读写全串行 | `internal/storage/sqlite.go:36` | 规模化性能瓶颈 |
| 写章无自动前情注入 | `app/chat.go:203` | 写作上下文智能缺失 |
| NovelState 仅新会话注入、跨天过期 | 会话生命周期 | 长会话上下文衰减 |
| 检测任务无取消/超时 | 检测核心 | 大书检测可能卡死无退出 |
| 检测发现项无法跳转章节原文 | `IssueTicketsPanel.tsx:428` | 闭环断点（看得到改不了） |
| 失败 turn 无重试 | chat 流程 | 偶发失败需手动重发 |
| 熔断错误原始串呈现 | 前端错误展示 | 体验（暴露内部错误细节） |
| `detect_done` 不刷新工单 | 检测核心事件 | 需手动刷新 UX 断点 |

### 3.2 现状优势（改动中须保留）

contextasm 纪律、六通道 SystemMessages、token 持久化记账、版本化迁移 + 备份、novel 隔离完备、git 回滚完备、审批流、Wails 事件清理、Monaco + 保存队列 + diff 审阅。

### 3.3 前端工程债

`ChatPanel` 1701 行 / `ContentPanel` 1187 行 / `ArcListView` 1280 行待拆；`ChapterManagementView` 无虚拟化（参考 `CharacterList` 已有 `react-virtual` 可复用）。

---

## 4. 合并统一优先级清单

| 优先级 | 维度 | 项 | 关键位置 |
|--------|------|----|----------|
| **P0-高** | A | `NewBookWizard` 仍走 `StartPipeline`（违反单源契约 + 语义错配） | `NewBookWizard.tsx` |
| **P0-高** | A | `SaveContent` 绑定层编排多介质 + 直连 DB（违反分层） | `app/content.go` |
| **P0-高** | B | 消息排序无 id tiebreaker（数据正确性） | `session/store.go:206` |
| **P0-高** | B | 检测发现项无法跳转章节原文（闭环断点） | `IssueTicketsPanel.tsx:428` |
| 中 | A | internal 直接依赖 Wails + 文档矛盾 | `internal/agent/*` 等 |
| 中 | A | `chapter.Service.Delete` 跨介质一致性窗口 | `internal/chapter/service.go` |
| 中 | A | `types/pipeline.ts` 死类型、`pipeline.*` 孤儿 i18n、`AuditIssue` 缺 `fix_type`、活跃文案「流水线」 | 前端多文件 |
| 中 | A | 5 关键面板无测试、`fmtTime`/`STATUS_CLS` 重复 | 前端多文件 |
| 中 | B | SQLite 单连接串行、写章无前情注入、NovelState 衰减 | `storage/sqlite.go`、`app/chat.go` |
| 中 | B | 检测任务无取消/超时、失败 turn 无重试、`detect_done` 不刷新 | 检测核心 |
| 低 | A | legacy API 暴露、错误字符串约定、god-service/god-object | `app/`、`internal/pipeline/service.go` |
| 低 | B | 大组件拆分、`ChapterManagementView` 虚拟化 | 前端多文件 |

---

## 5. 修复路线建议

**第 1 批（P0-高，先修）**
1. 前端 A：`NewBookWizard` 的 `StartPipeline` → `RunDetectionTask` + 面板/文案修正。
2. 后端 A：抽 `ContentService` 收口 `SaveContent`（连带修复跨介质一致性）。
3. 后端 B：`session/store.go` 消息排序加 id tiebreaker（数据正确性）。
4. 前端 B：`IssueTicketsPanel` 检测发现项支持跳转章节原文（闭环最后一跳）。

**第 2 批（中，可一并清理）**
- 维度 A：死类型/孤儿 i18n/契约字段/文案/`fix_type`/测试/DRY；`EventEmitter` 注入 + 文档统一；`Delete` 一致性窗口。
- 维度 B：SQLite 读写并发、写章前情注入、检测任务取消/超时、turn 重试、`detect_done` 刷新。

**第 3 批（低 / 工程债，后续）**
- 维度 A：legacy 隔离、错误码常量、god-service/god-object 拆分。
- 维度 B：ChatPanel/ContentPanel/ArcListView 拆分、`ChapterManagementView` 虚拟化。

> 按项目约定，所有代码改动需用户确认后实施；每个实现阶段按「一任务一提交」拆分，并附验证（后端 `go build ./...` + `go test ./...`；前端四道闸 `format:check`/`i18n:check`/`lint`/`build` + `vitest` 全绿）。
