# NovelGo 代码审计报告（前端 / 后端设计合理性）

- **审计日期**：2026-10-29
- **审计范围**：`app/`、`internal/`（Go 后端）、`frontend/src`（React 19 前端）
- **审计目标**：找出当前代码在**设计 / 分层 / 契约**层面的不合理之处，并给出可落地的解决方案（非逐行 bug 排查）
- **方法**：只读静态分析 + 以 `AGENTS.md`、单源契约（`signability-audit.md`）、#257–#263 既定重构意图为基准，逐条核对实际代码是否偏离意图
- **结论概览**：熔断/重试、体检契约、度量本地化、cgo/singleton、前端 #257–#263 主重构均已正确落地；需整改项为**少数分层违规、契约漂移与重构未收尾残留**

---

## 一、执行摘要

| 维度 | 结论 |
|------|------|
| 后端核心设计 | 熔断/重试、体检契约、度量本地化、cgo/singleton 均正确；主要欠账是 `app/` 绑定层越界承载多介质编排、跨介质一致性无补偿、文档与代码对「internal 能否依赖 Wails」自相矛盾 |
| 前端核心设计 | #257–#263「检测核心」重构主体已收敛；唯一真实违反单源契约的活代码是 `NewBookWizard` 仍调用 `StartPipeline`；另有死类型/孤儿 i18n 键/契约字段缺失等未收尾残留 |
| 最高优先级 | 前端 A1（`NewBookWizard` 改 `RunDetectionTask`）+ 后端 A1（抽 `ContentService` 收口 `SaveContent`） |
| 已达标（无需改） | breaker/retry 自洽、signability-audit 契约一致、structure/quality 恒 P1、度量纯本地、零删除纪律、featureKeys 单源、顶层面板 ErrorBoundary、桌面优先 |

---

## 二、后端设计问题

### A1【高】`SaveContent` 在绑定层编排多介质且直连 DB —— 违反分层

- **位置**：`app/content.go:58-114`（尤其 `:93-110`）
- **证据**：`SaveContent` 同时编排 5 个介质：
  1. `git.WriteFileIfUnchanged / git.WriteFile`（Git 文件）
  2. `rag.SubmitRefresh(...)`（向量刷新队列 singleton）
  3. `svc.UpdateCachedChapter(...)`（搜索缓存）
  4. `text.ComputeStats(...) / a.writing.LogChanges(...)`（写作统计）
  5. `a.chapter.DB.WithContext(...).Model(...).Where(...).Update("word_count", ...)`（**直连 `chapter.Store` 内部 DB 句柄**）
- **为什么不合理**：
  - `AGENTS.md` 规定 `app/` 仅做 DTO 转换/调用/错误适配，**不承载领域编排**；跨多介质用例必须 `App → internal/<domain>.Service → Store`。
  - 此处把典型的多介质跨域编排放在 Wails 绑定层，且穿透 `chapter.Store` 直连其 DB 句柄（绕过 Store API 与数据一致性逻辑）。
  - 各步失败无事务/补偿：DB `word_count` 更新失败仅 warn，造成「文件是新内容、DB/字数/向量滞后」的不一致（见 E2）。
- **解决方案**（需用户授权）：
  - 抽 `internal/content`（或扩展 `internal/chapter`）的 `ContentService`，方法如 `SaveChapterContent(ctx, novelID, chapterID, content)` 内部统一协调：Git 写 → DB 字数/内容更新（同一事务或带补偿）→ 向量 `SubmitRefresh` → 缓存 `UpdateCachedChapter` → 写作统计，并定义失败回退（向量/缓存失败重试 + 启动期孤儿清理）。
  - `app.SaveContent` 改为仅做路径校验 + DTO 转换 + 调用 `ContentService` + 错误适配。
  - 收益：跨介质语义集中在领域层，DB 句柄不再外泄，便于单测（注入 fake emitter/vector）。

### A2【中】`chat.go` 直连 DB 且 git 提交编排落在 app 层

- **位置**：`app/chat.go:89,145-173,230,279,299,543-598`
- **证据**：`:145-173`/`:230`/`:279`/`:299` 用 `a.session.DB.Create(...)` 直接写 session 消息；`:592` `a.db.Create(tc)` 直接写 `turn_commits`（绕过 `rollback.Store`）；`:543-598` `commitUserChanges/commitAIChanges/doCommit` 在 app 层协调 git add/commit + `turn_commits` 落库。
- **为什么不合理**：同上分层规则——app 层直接持有并操作 DB 句柄与 git 仓库，绕过 `session.Store`/`rollback.Store` 封装，把「对话 turn 收尾 → git 提交 → 记录 turn_commits」的跨介质补偿流程放在绑定层。
- **解决方案**（需授权）：session 消息落库统一走 `session.Store`，turn_commits 走 `rollback.Store`，git 自动提交逻辑抽到 `internal/git` 或 `internal/session` 的收尾方法，app 仅编排调用顺序。**注意**：chat 的「AI 改动落 git」涉及 append-only 与回滚语义，改动需配套测试。

### A3 + D3【中】领域包直接依赖 Wails，且两份文档自相矛盾

- **位置**：`internal/agent/tokens.go:9,108`、`internal/agent/compress.go:12,314`、`internal/agent/agent.go:15,210`、`internal/pipeline/events.go:6,53-111`、`internal/mcp_tools/rw_tools.go:17,184-194,975-1005`
- **证据**：上述 `internal` 包直接 `import wails` 并 `wails.EventsEmit`；但 `AGENTS.md:60` 明文「Service 不得反向依赖 `app/` 或 Wails」；而 `internal/agent/DESIGN.md:67,71,86,360` 又写着「`internal/` 可以 import Wails，无需回调」。**两份文档直接矛盾（契约漂移）**。
- **为什么不合理**：领域包无法脱离 Wails 运行时做纯单测（必须 mock 全局事件总线）；事件发射逻辑与 Wails 生命周期/线程模型绑定。
- **解决方案**（需授权）：
  - 定义 `type EventEmitter interface { Emit(ctx, name string, payload any) }`，由 app 层注入 Wails 实现；`agent`/`pipeline`/`mcp_tools` 改为依赖该接口。
  - 同步修订 `AGENTS.md` 或 `internal/agent/DESIGN.md`，统一口径（要么「领域包不得直接 import Wails；事件经注入的 EventEmitter」，要么「事件发射是显式例外且需在 DESIGN.md 登记」）。

### A4【低】legacy 流水线入口仍作为前端可见 API 暴露

- **位置**：`app/pipeline.go:10,26,34`（`StartPipeline`/`ResumePipeline`/`ControlPipeline`）
- **证据**：设计意图要求「旧 Orchestrator 流水线前端不得再走 `StartPipeline`/`PipelineWizard`」，但后端仍把这些 legacy 入口作为前端可用 API 暴露，构成「前端可能误用 legacy 路径」的契约泄漏面（前端类型由 `wails generate module` 暴露）。
- **解决方案**（需授权）：将 legacy 入口从前端可见方法集中隔离（加 `// deprecated` + 仅内部/测试可达，或通过构建标签/显式开关控制），并在前端绑定清单中剔除，使「前端不得走 legacy」有硬约束而非口头约定。

### E1【中】`chapter.Service.Delete` 跨介质一致性窗口

- **位置**：`internal/chapter/service.go:113-150`
- **证据**：
  - `git.RemoveFile` 在 **DB 事务闭包内、提交前**执行：若 DB 提交失败，`Transaction` 回滚 DB，但**文件已被删除且无法随 DB 回滚** → 「文件没了、DB 记录还在」。
  - 向量/缓存清理在**事务提交之后** best-effort（`cleaner.DeleteChapterChunks` 与 `invalidator.DeleteCachedChapter` 失败仅 `logger.Warn`）→ 进程崩溃留孤儿向量分块，无补偿、无回滚。
  - 第 121 行 `TODO` 仅意识到反向（文件原子移到暂存），忽略正向窗口。
- **解决方案**（需授权）：
  - 删除走「DB 提交成功后才执行 + 重试」（即 `TODO` 的反向思路同样用于删除路径），或先写临时/未跟踪位置待提交成功再落地。
  - 向量/缓存改为「提交后异步 + 可重放补偿」：注册以 DB 删除为权威源的 reconcile（启动期扫描已删除章节清理孤儿分块，或利用 `RefreshQueue` 重放）。

### E2【中】`SaveContent` 无跨介质事务/补偿（与 A1 同源）

- 见 A1：Git 写 → 异步向量刷新 → 缓存更新 → DB `word_count` 更新（失败仅 warn）。下沉到 `ContentService` 后应在同一编排内定义顺序与失败回退（如 DB 失败时回滚 Git 文件或标记脏）。

### F2【低】Wails 边界错误类型丢失，错误适配=字符串约定

- **位置**：`app/content.go:78` 返回 `fmt.Errorf("CONTENT_CONFLICT: ...")`
- **证据**：Go `*error` 经 Wails binding 序列化后只剩字符串，前端拿不到结构化错误类型。当前用 `CONTENT_CONFLICT` 前缀是合理兜底，但各 binding 方法各自发明前缀，前端解析脆弱。
- **解决方案**（需授权/文档）：在 `app/` 统一一处错误码常量与构造器（如已有的 `internal/apperr`），binding 层统一转换；前端按约定错误码分支。属规范化，不影响正确性。

### G1【中】`pipeline/service.go` 2855 行 god-service

- **位置**：`internal/pipeline/service.go`
- **证据**：单文件承载编排状态机调用、检测核心装配、`issue` 工单流转、冻结、巡检验证、快审对照；与 `orchestrator.go` 状态机职责边界模糊（状态机在 orchestrator.go，但大量「进入某阶段后干什么」仍在 service.go 的 `enterAudit/enterFix/executeAudit/...`）。
- **解决方案**（需授权，低优先）：按子域拆分（detect.go 已拆出检测核心；可进一步拆 `issues.go`/`freeze.go`/`patrol.go`/`gate.go`）。

### G2【低】`app.App` 30+ 字段 god-object

- **位置**：`app/handler.go:54-111`
- **证据**：同时持有 Stores、Services、Clients（`llmClient/agent/registry/mcpServer`）、singleton 句柄（`vectorStore atomic.Pointer`、`searchService atomic.Pointer`）。
- **解决方案**（低优先）：把基础设施装配抽到 `app/bootstrap.go`，`App` 仅保留引用与 binding 方法。

### 后端正向确认（无需改）

- **C1 熔断/重试自洽**：`breaker.go` 单例 `ProviderBreaker` 三态只认硬失败；`retry_matrix.go` 硬失败口径单源；`stream.go` 两处错误点 `RecordHardFailure`；`ChatStream/GenerateText` 守卫 `IsOpen` 命中即 `breakerOpenError`（Retryable=false）；`provider_trip.go:31` `AnyOpen` 与 `breaker.go:196` `IsOpen` 同源于单例。全局检索 `fixProviderIdleTrip|providerIdleWindow|IdleTrip` **零命中**，无空闲熔断残留。
- **D1 体检契约一致**：`signability-audit.md:104-119` 契约与 `store.go:180-230` 解析实现字段/JSON tag/常量完全对应；`parseFreshAudit` 对无 `schema_version` 产出补标；V 系漂移 `level` 恒 P1、不翻转 verdict。
- **D2 恒 P1 只报告**：`internal/structure/types.go:13`、`internal/quality/types.go:21` 均默认 P1，不进 `pipeline_issues` 修复闭环。
- **F1 度量纯本地**：`alert_metrics.go` 仅本地计数，全局检索 `http.Post|Telemetry|upload|analytics` 零命中。
- **G3 cgo/singleton 正确**：`embedder_onnx.go:1`、`vector_store.go:1` 均带 `//go:build cgo`；全局单例均有 `sync.Once` 守卫，接入点单一，未引入第二实例。

---

## 三、前端设计问题

### A1【高】`NewBookWizard` 仍调用 `StartPipeline` —— 唯一违反单源契约的活代码

- **位置**：`src/components/novel/NewBookWizard.tsx:7,198,199,201`
- **证据**：
  - `:7` `import { ... StartPipeline ... } from "@/lib/wailsjs/go/app/App"`
  - `:198` `await StartPipeline(id, "new", null, false)`
  - `:199` `setActivePanel("pipeline")`
  - `:201` `t("wizard.launchedToast", { workflow: t("workflow.signability") })`
- **为什么不合理**：既定设计明确「前端**不得**再走 `StartPipeline`/`PipelineWizard`/`SignabilityAuditView`」，且 #260 已将 `SignabilityWorkflowView.tsx:103` 改为 `RunDetectionTask(novelId, "book", null)`。本文件是**唯一残留的前端 `StartPipeline` 调用**。
  - 语义错配：`pipeline` 面板现已渲染 `DetectionCorePanel`（纯结果消费面，不含运行中的流水线）。启动后跳该面板，用户看到检测核心结果页，而非任何流水线状态——`launchedToast`（「请在流水线面板继续」）、`wizard.launchHint`（「由顶部阶段条驱动…」）描述的是**已被删除的 UI**。
- **解决方案**（需授权）：
  - 改 `handleLaunchConfirm`：`StartPipeline(...)` → `RunDetectionTask(id, "book", null)`（或 `"structure"`，取决于「含黄金三章体检」意图，需与后端确认 "book" 检测是否覆盖黄金三章体检）。
  - `setActivePanel("pipeline")` → `setActivePanel("signability")`（落地到已迁移的 `SignabilityWorkflowView`），或在 `pipeline`（DetectionCorePanel）结果页展示。
  - 同步改 `wizard.launchHint`/`launchedToast`/`launchFailed` 文案（见 D2）。
  - 不动 `App.d.ts`/`models.ts` 中后端仍保留的 `StartPipeline`（那是生成绑定，删了破坏 TS 编译；仅停前端调用即可）。

### A2【中】`types/pipeline.ts` 残留旧流水线/闸门死类型

- **位置**：`src/types/pipeline.ts`
- **证据**（排除自身后外部引用数均为 0）：`PipelineStatus`、`PipelineMode`、`ControlCmd`、`GateDecision`、`StagePayload`、`GatePayload`、`ControlAckPayload`、`FixIssuePayload`、`RewriteItem`、`IssueTicketStatus`、`FixProgressState`、`FixActivityItem`。仍被使用（保留）：`AuditData`(2)、`AuditIssue`(1)、`IssueTicketRef`(3)、`IssueVerifySummary`(2)。
- **为什么不合理**：#259 删除了 `usePipeline.ts` 与闸门 UI，但 `types/pipeline.ts` 未同步裁剪，是重构残留死代码。
- **解决方案**（需授权）：删除上述 0 引用类型，保留 `AuditData`/`AuditIssue`（并补 `fix_type`，见 D1）/`IssueTicketRef`/`IssueVerifySummary`。

### A3【中】i18n `pipeline.*` 大量孤儿键（0 代码引用）

- **位置**：`src/i18n/locales/zh-CN.json:1826+`、`en.json:1866+`
- **证据**（在 `src` 中 `t()` 引用数均为 0）：`pipeline.gate.*`、`pipeline.control.*`、`pipeline.blueprint.*`、`pipeline.signability.*`、`pipeline.fixProgress.*`、`pipeline.fixStatus.*`、`pipeline.rewriteBacklog.*`、`pipeline.stageActivity.*`、`pipeline.stockAudit.*`，以及 `pipeline.empty`/`start`/`continue`/`autoRun`/`alignFull`/`checkProgress`/`reaudit`/`hint`。存活键（`pipeline.title`/`detectCoreHint`/`patrol.*`/`tickets.*`/`retcon.*`/`volumeManage.*`/`issueTags.*`）均被使用，保留。
- **为什么不合理**：这些键来自已删组件，是死重；且「Start Pipeline」「流水线」「阶段条」等文案若被不慎渲染会直接与「检测核心」语义冲突。
- **关键限制**：`scripts/i18n-check.mjs` **不解析代码里的 `t()` 调用**（只校验中英文键结构一致/重复键/空翻译/源码硬中文），故孤儿键能静默过四道闸。
- **解决方案**（需授权）：删除上述孤儿子树；建议给 `i18n-check.mjs` 增「孤儿键」检测（用 `i18next-parser` 抽取代码用键比对 locale）防再犯。

### B1【中】DRY 违规：`fmtTime` 复制 5 份、`STATUS_CLS` 复制 2 份

- **位置**：
  - `fmtTime`：`VolumeManagePanel.tsx:14-18`、`RetconPanel.tsx:11-15`、`DetectionTasks.tsx:41-45`、`IssueTicketsPanel.tsx:24-28`、`SignabilityWorkflowView.tsx:43-47`（实现完全相同）
  - `STATUS_CLS`（工单状态徽章配色）：`IssueTicketsPanel.tsx:33-43`、`SignabilityWorkflowView.tsx:31-41`（完全一致）
- **为什么不合理**：同一逻辑多处重复，任一调整（时区/格式、新增状态色）需改多处，违反 DRY 与单源精神。
- **解决方案**（需授权）：抽 `src/utils/datetime.ts` 导出 `fmtTime`；抽 `src/components/pipeline/ticketStatus.ts` 导出 `STATUS_CLS` 与 `TERMINAL`；各面板改为 import。

### C1【低/中】列表加载范式不统一

- **位置**：`DetectionCorePanel`/`DetectionTasks` 用 `useQuery` + `queryKeys`（规范）；`IssueTicketsPanel.tsx:86`、`RetconPanel.tsx:26`、`VolumeManagePanel.tsx:26` 用本地 `useState` + 手写 `load()`。
- **为什么不合理**：同为「拉取列表」两种范式并存，loading/缓存/失效语义不一致、样板重复。
- **解决方案**（需授权）：将三处改为 `useQuery({ queryKey, queryFn })`，复用 `queryKeys`（如新增 `issueTicketKeys.list(novelId)`、`retconKeys.list(novelId)`）。

### D1【中】`AuditIssue` 缺契约强制的 `fix_type` 字段（契约漂移）

- **位置**：`src/types/pipeline.ts:48-62`（对照 `internal/skill/builtin/signability-audit.md:96-103`、`src/lib/wailsjs/go/models.ts:2874`）
- **证据**：契约源要求每条 issue 必须含 `fix_type`（`"body"`/`"ledger"`，决定修复出口 S3 重写 / 自动修复）；后端 `models.ts` 的 `PipelineIssue.fix_type?: string` 已含该字段，但前端解析类型 `AuditIssue` 没有 → 与契约单源不对齐。
- **解决方案**（需授权）：给 `AuditIssue` 增加 `fix_type?: string`，并在 `IssueTags.tsx` 的 `issue` 入参类型与 `PatrolReportPanel` 解析处对齐。

### D2【中】活跃文案仍写「流水线/阶段条/Start Pipeline」，与「检测核心」语义冲突

- **位置/证据**（由 `NewBookWizard` 实时渲染或活跃引用）：
  - `wizard.launchHint`：`zh-CN.json:1076`「启动后将直接打开创作**流水线**面板，由顶部**阶段条**驱动…」；`en.json:1099` "…the **stage bar** drives blueprint…"
  - `wizard.launchedToast`：`zh-CN.json:1079`「已启动 {{workflow}}，请在**流水线面板**继续」；`en.json:1102` "Continue in the **pipeline panel**."
  - `wizard.launchFailed`：`en.json:1103` "Start it later from the **Workflows panel**."（与现有 `shell.workflow` 命名不一致）
  - `pipeline.presetHint`（`JourneyQuality.tsx:29-32` 用作 pipeline 卡片 `titleKey`）：`zh-CN.json:1855`「…只跑**流水线**不手动跑检测，皆合法」—— #261 已移除阶段，描述失效。
  - `narrative.tagline`：`en.json:2186`「The **pipeline** guarantees the signability floor」/ `zh-CN.json:2146`「**流水线**保证签约下限」——产品标语仍用「流水线」。
- **解决方案**（需授权）：统一改为「检测核心/质检」语义；`presetHint` 改为描述「检测任务四入口 + 结果落工单/张力报告」；`tagline` 改为「检测核心守住签约下限，心智提取抬高文风上限」。

### F1【中】5 个关键面板无专属测试

- **位置**：`DetectionCorePanel`/`IssueTicketsPanel`/`PatrolReportPanel`/`RetconPanel`/`VolumeManagePanel` 均无 `*.test.ts(x)`（`find src -iname "*DetectionCorePanel.test*"` 等全无结果）。
- **为什么不合理**：`DetectionCorePanel`（#257 收口核心）与 `IssueTicketsPanel`（人工修复闭环状态机：确认→修复→复验→驳回）是重构重点，目前仅由 `JourneyQuality.test.tsx`/`WorkspaceView.test.tsx` 间接覆盖，#257–#263 重构成果无锁定。
- **解决方案**（需授权）：补 `IssueTicketsPanel.test.tsx`（覆盖选中/确认/去修复/复验/驳回状态机与供应商挂起弹窗）、`DetectionCorePanel.test.tsx`（`GetPatrolReport` 解析 + `consistency_patrol` 事件失效 + 四子面板聚合渲染）。

### B2【低】structure 检测双入口

- **位置**：`DetectionCorePanel.tsx:91-97`（`PatrolReportPanel` `onRun` 直接 `RunDetectionTask(novelId, "structure", null)`）与 `DetectionTasks.tsx:270` 各有入口。
- **为什么不合理**：属「巡检重跑」语义可接受，但两处启动器未统一，埋点/失效策略可能不一致。
- **解决方案**（需授权）：抽出统一 `launch()`（如 `useDetectionLaunch` hook）。

### B3【低】`DetectionCorePanel` 内 4 子面板未各自包 ErrorBoundary

- **位置**：`DetectionCorePanel.tsx:88-101`
- **证据**：`WorkspaceView.tsx` 已对每个顶层面板包 `ErrorBoundary`（良好），但 `DetectionCorePanel` 内的 4 子面板共享一个边界，任一抛错会整块检测核心白屏。
- **解决方案**（需授权）：在 `DetectionCorePanel` 内给 4 子面板各包一层 `<ErrorBoundary>`。

### F2【低】孤儿键可静默过 `i18n:check`；工单列表未虚拟滚动

- **位置**：`scripts/i18n-check.mjs`、`IssueTicketsPanel.tsx:393`
- **证据**：见 A3；`@tanstack/react-virtual` 已在依赖中，若工单量大可引入虚拟滚动（低优先）。

### A4【低】陈旧注释引用已删 `usePipeline`

- **位置**：`src/components/pipeline/PatrolReportPanel.tsx:19` 注释「`tick`（usePipeline 维护）」
- **解决方案**（无需授权）：改为「由 `DetectionCorePanel` 的 `pipeline:consistency_patrol` 事件驱动」。

### 前端正向确认（无需改）

- **#257–#263 主体收敛**：被删文件（`PipelineWizard.tsx`/`SignabilityAuditView.tsx`/`PipelineControlBar.tsx`/`PipelineGateCard.tsx`/`BlueprintWorkflowView.tsx`/`usePipeline.ts`/`PipelineWizard.test.tsx`）确实已物理删除；`factory.ts` 的 `PanelId` 未违反「零删除」；`featureKeys.test.tsx` 已无 `PipelineGateCard` 引用；四 kind 检测入口统一在 `DetectionTasks.tsx`。
- **A5 「零删除」纪律未被违反**：`factory.ts` 的 `ALL_SIDEBAR_PANEL_IDS` 仍含 `pipeline`，`FACTORY_MAP` 映射到 `quality`——被删的是**组件/视图**而非 **PanelId**，`pipeline` 被保留并改用途（→ `DetectionCorePanel`），符合纪律，无矛盾。
- **D3 featureKeys 一致**：`panelFeatureKey`/`journeyFeatureKey`/`ACTION_FEATURE_KEYS` 均与 PanelId/功能对齐；`alertRaised*` 等 0 前端调用但注释明确为「后端挂点记数、枚举锁 key 单源」，属有意设计，非孤儿。
- **F3 ErrorBoundary 完善**：`src/components/shared/ErrorBoundary.tsx` 存在；`WorkspaceView.tsx:563-694` 对每顶层面板包边界，单面板崩溃不拖垮整页。
- **G1 桌面优先健康**：`DetectionTasks.tsx` 用 `sm:grid-cols-2 xl:grid-cols-4` 等桌面优先渐进增强；无移动端强依赖或反模式，符合 PC/桌面偏好。

---

## 四、问题清单汇总表

### 后端

| # | 问题 | 位置 | 严重度 | 建议方案 | 需授权 |
|---|------|------|--------|----------|--------|
| A1 | `SaveContent` 绑定层编排 5 介质 + 直连 DB，违反分层与跨介质应走 Service | `app/content.go:58-114` | 高 | 抽 `ContentService` 统一编排；app 仅 DTO+调用 | 是 |
| A2 | chat 直连 DB / git 编排在 app 层 | `app/chat.go:89,145-173,230,279,299,543-598` | 中 | 走 `session.Store`/`rollback.Store`；git 收尾下沉 | 是 |
| A3/D3 | internal 包直接依赖 Wails，且 AGENTS.md 与 agent/DESIGN.md 矛盾 | `internal/agent/*`、`pipeline/events.go`、`mcp_tools/rw_tools.go` | 中 | 注入 `EventEmitter` + 统一文档口径 | 是 |
| A4 | legacy `StartPipeline` 仍前端可见 API | `app/pipeline.go:10,26,34` | 低 | 隔离/剔除前端可见 legacy 入口 | 是 |
| E1 | `chapter.Service.Delete` 向量/缓存提交后 best-effort；文件删在 DB 提交前致不一致 | `internal/chapter/service.go:113-150` | 中 | 提交后可重放补偿 + 启动期孤儿清理；删除走暂存 | 是 |
| E2 | `SaveContent` 无跨介质事务/补偿（同源 A1） | `app/content.go:73-110` | 中 | 随 A1 的 ContentService 一并定义顺序与回退 | 是 |
| F2 | Wails 边界错误类型丢失=字符串约定 | `app/content.go:78` | 低 | 统一错误码常量/构造器并文档化 | 是/文档 |
| G1 | `pipeline/service.go` 2855 行 god-service | `internal/pipeline/service.go` | 中 | 按子域拆分（低优先） | 是 |
| G2 | `app.App` 30+ 字段 god-object | `app/handler.go:54-111` | 低 | 装配抽到 `bootstrap.go`（低优先） | 是 |

### 前端

| # | 问题 | 位置 | 严重度 | 建议方案 | 需授权 |
|---|------|------|--------|----------|--------|
| A1 | `NewBookWizard` 仍调用 `StartPipeline` 并跳 pipeline 面板 | `NewBookWizard.tsx:7,198,199,201` | 高 | 改 `RunDetectionTask` + `setActivePanel("signability")` + 文案 | 是 |
| A2 | `types/pipeline.ts` 残留旧死类型（12 个） | `src/types/pipeline.ts` | 中 | 删除死类型，保留 AuditData/AuditIssue/IssueTicketRef/IssueVerifySummary | 是 |
| A3 | i18n `pipeline.*` 孤儿键 | `zh-CN.json:1826+`/`en.json:1866+` | 中 | 删除孤儿子树；i18n-check 加孤儿键检测 | 是 |
| A4 | 陈旧注释引用已删 `usePipeline` | `PatrolReportPanel.tsx:19` | 低 | 改注释 | 否 |
| B1 | `fmtTime`×5、`STATUS_CLS`×2 重复 | 5 个面板 | 中 | 抽 `utils/datetime.ts` 与 `pipeline/ticketStatus.ts` | 是 |
| B2 | structure 检测双入口 | `DetectionCorePanel.tsx:91-97`、`DetectionTasks.tsx:270` | 低 | 抽统一 `launch()` hook | 是 |
| B3 | DetectionCorePanel 内子面板未各自包 ErrorBoundary | `DetectionCorePanel.tsx:88-101` | 低 | 各包一层 ErrorBoundary | 是 |
| C1 | 列表加载范式不统一 | IssueTicketsPanel:86 / RetconPanel:26 / VolumeManagePanel:26 | 低/中 | 改用 `useQuery` + 新增 `queryKeys` | 是 |
| D1 | `AuditIssue` 缺契约强制 `fix_type` | `types/pipeline.ts:48-62` | 中 | 补 `fix_type?: string` 并对齐解析 | 是 |
| D2 | 活跃文案仍写「流水线/阶段条/Start Pipeline」 | wizard.*、pipeline.presetHint、narrative.tagline | 中 | 统一「检测核心/质检」语义 | 是 |
| F1 | 5 个关键面板无专属测试 | 各面板目录 | 中 | 补 IssueTicketsPanel/DetectionCorePanel 测试 | 是 |
| F2 | 孤儿键静默过 i18n-check；工单列表未虚拟滚动 | `scripts/i18n-check.mjs`、`IssueTicketsPanel.tsx:393` | 低 | i18n-check 加孤儿键检测；大列表考虑虚拟滚动 | 是 |

---

## 五、修复路线建议

**第 1 批（高 + 直接违反契约，优先）**
1. 前端 **A1**：改 `NewBookWizard` 的 `StartPipeline` → `RunDetectionTask`（唯一真实违反单源契约的活代码，影响用户可见行为）。
2. 后端 **A1**：抽 `ContentService` 收口 `SaveContent`（最明确架构欠账，连带修复 E2 一致性窗口）。

**第 2 批（中，重构卫生 / 契约对齐，可一并清理）**
- 前端 A2 + A3 + B1 + D1 + D2 + F1
- 后端 A3/D3（注入 `EventEmitter` 并统一文档）+ E1（删窗口）+ A2

**第 3 批（低优先，后续重构）**
- G1/G2 拆分大文件、`i18n-check` 加孤儿键检测、`App` 装配抽 `bootstrap`、B2/B3/C1/F2 收尾。

> 按项目约定，所有「需授权」项均为代码修改/文档修订，实现前需用户确认；每个实现阶段完成后按「一任务一提交」拆分，并附验证（后端 `go build ./...` + `go test ./...`；前端四道闸 `format:check`/`i18n:check`/`lint`/`build` + `vitest` 全绿）。
