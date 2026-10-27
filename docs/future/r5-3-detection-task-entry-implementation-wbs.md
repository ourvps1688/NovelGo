# R5-3「检测任务独立入口」—— 卷体检/书体检/巡检统一为按 scope 启动的检测任务 · 流水线降级为编排预设 —— 分步实施计划细化（WBS）

> 承接 `pipeline-core-redesign-proposal.md` §10.4「环节独立启动」+ §10.5b「签约体检地位澄清」，R5 迁移路线第三步（R5-1 工单化 / R5-2 摘除自动修复 已销）。
> 目标：**检测环节可脱离流水线任意时刻单独启动**（"只体检第 X 卷""只查伏笔""从第 N 章续写"皆一等公民），流水线降级为"写→检测"的便捷串行壳（编排预设）。
> 顺序铁律：先落「检测任务统一入口 + 检测-only 执行核心」→ 前端旅程面板加入口 → 流水线编排预设定位收尾 → 验收收尾。
> 每个任务标注「文件触点 / 交付物 / 验收 / 依赖 / 风险」，未过验收不得进下一任务，一任务一提交一销账。
> **范围铁律**：R5 全程 **P0–P2 工件零触碰**（`internal/contextasm`/`internal/voice`/`internal/snapshot`/`internal/retcon` 不进本 WBS）；**降级逐字一致**（检测能力缺失=零提示零报错，绝不改检测口径）；**只报告不改写**（检测任务零写入正文/台账）；**度量纯本地**；**desktop-first**（桌面优先，不做移动端 parity）；**PanelId 零删除**（前端只加不删）。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 检测任务统一形态契约（§10.4）

- **勘察结论（现状耦合）**：章节 CI 是唯一的"独立检测"原型（`mcp_tools/chapter_ci.go` `RunChapterCIForChapter`，按章、直写质量档案、不绑 run）；卷体检（`pipeline.executeAudit`）、书体检（`pipeline.executeFinalAudit` / `runStockAudit`）、巡检·结构（`pipeline.runConsistencyPatrol`）**全部绑 pipeline run**——结果写 `run.ContextJSON`、发现项工单 keyed by `runID`（`ReplaceIssues`）、`executeAudit` 还内嵌 auto-advance/fix/gate 自动裁决。前端 JourneyQuality 只导航不启动。
- **契约**：引入**统一检测任务入口** `RunDetectionTask(novelID, kind, scopeID)`，`kind ∈ {chapter, volume, book, structure}`，各带独立 scope（某章/某卷/全书/全书结构），任意时刻可单独启动、不依赖流水线阶段。执行体仍走 **R3 任务队列**（复用 `taskKindAudit`/`taskKindPatrol`/`taskKindFinalAudit` kind；章节检测走既有 `RunChapterCIForChapter` 无需队列）。
- **检测任务 = 「scoped 检测 run + 队列派发执行体」**（方案 A，spec-aligned）：
  - 卷/书/结构检测：建/复用一个 **scoped 检测 run** 作结果容器（工单 keyed by runID 依赖它；卷=VolumeID 置位的 scoped sub-run，书/结构=book-run 语义），派发既有执行体。"独立性"=**随时可启动、不卡流水线阶段**，而非脱离 run 存储（§10.4 明示复用队列 kind）。
  - 章节检测：直接 `RunChapterCIForChapter`（写质量档案 `chapter_quality`，无 run 依赖）——保持现状零改，作为其余三类的形态基准。
- **检测-only 语义（本步核心）**：独立启动的检测**只跑「体检 skill + 解析 + 落工单/报告」**，**跳过 auto-advance/fix/gate 自动裁决**——这正是 §10.5b「S4 体检本体→检测任务（契约/三层输入/goldenN 零改动）」的映射。流水线内的 executeAudit 行为不变（auto-run 裁决保留，属编排预设的自动节奏）。

### 0.2 scope → 执行体/技能映射契约

| kind | scope | 执行体 | 技能 | 结果容器 | 产出 |
|---|---|---|---|---|---|
| `chapter` | chapterID | `RunChapterCIForChapter`（现有） | chapter-ci | `chapter_quality`（质量档案） | L0/L1 findings |
| `volume` | volumeID | `executeAudit`（检测-only 皮） | signability-audit | scoped 检测 run | verdict + 发现项工单 |
| `book` | 全书 | `executeFinalAudit`（检测-only 皮）/`runStockAudit` | signability-audit | book 检测 run | verdict + 发现项工单 |
| `structure` | 全书结构 | `runConsistencyPatrol`（现有） | consistency-patrol | 检测 run 的 `PatrolReport` | 张力报告（只读） |

- **契约/三层输入零改动**：signability-audit.md 方法论、三层输入装配（`structureBlock`/`qualityLedgerBlock`/`keyChapterBlock`）、goldenN 逻辑、`signabilityPrompt` 均**复用现状**，检测-only 只是"调用后不接裁决"。文风漂移 V 系合并（`runStyleDriftCheck`）随 scope 生效（卷=scopeVolumeID，书=nil 全书）。

### 0.3 流水线降级为「编排预设」契约（§10.4/§10.6）

- **契约**：全书流水线（S0–S6）**降级为可选壳**——"写→检测"按序串起的便捷预设。只跑检测不跑流水线、只跑流水线不手动跑检测皆合法。`Orchestrator.Advance` 仍是"下一步去哪"唯一裁决点（不变）；**G 闸门降级属 R5-4**（本步不动闸门/冻结/retcon 边界语义）。
- **改动量（低）**：前端定位/文案把流水线框为"编排预设"（一键串起写→检测的便捷壳），检测任务入口并列于旅程面板（不再藏在流水线阶段里）。**不删流水线能力**，只是"不再是唯一入口"。

### 0.4 触发策略红线（§10.5b，勿回退）

- **保留自动检测节奏**：写后章节 CI（不变）+ 编排预设内自动触发（黄金三章首检/每 5 章快审/卷收口/完本终检）**不删**——自动是签约体检存活的机制保障。本步只**扩充手动入口**（检测任务随时单独启动），**绝不把自动节奏改纯手动**。

---

## 1. 后端：检测任务统一入口 + 检测-only 执行核心

### T1.1 检测-only 执行核心抽取（契约 0.1）

- **文件触点**：`internal/pipeline/service.go`（executeAudit/executeFinalAudit）、`internal/pipeline/detect.go`（新建，检测任务入口与检测-only 皮）。
- **交付物**：抽出「跑体检 skill + 解析 + 合并 drift + 落工单/报告 + 广播」的**检测-only 核心**（供卷/书/结构检测任务复用），与 `executeAudit` 共享提示词/契约单源；`executeAudit`/`executeFinalAudit` 重构为"调检测核心 + （仅流水线内）auto-advance/gate"，**流水线行为零变化**（既有 pipeline 测试零回退）。
- **验收**：①检测-only 核心跑完落工单/报告但**零 auto-advance/fix/gate**（断言 run 状态不被推进）；②`executeAudit` 流水线路径行为逐字一致（既有 `TestServiceExecuteAudit*`/`TestOrchestratorS4*` 零回退）；③降级逐字一致（检测失败=静默零提示，不改口径）。
- **依赖**：无（第一步）。**风险**：中——触及 executeAudit 主链路，须保证流水线行为零变化（用既有测试回归锁定）。

### T1.2 统一检测任务入口 `RunDetectionTask`（契约 0.1/0.2）

> **实施定案补记（T1.2 落地时）**：队列 kind 取 Option B——新增 `taskKindDetect`（volume/book 检测-only 执行体）+ 字面复用 `taskKindPatrol`（structure）+ chapter 不排队走注入 `chapterCIRunner`。不复用 `taskKindAudit`/`taskKindFinalAudit` 的原因：其 handler 是带闸门裁决的 executeAudit/executeFinalAudit，与检测-only 语义相悖（§10.4「检测任务是队列的一类 kind」本义=执行体走 R3 队列，非强制同 kind 字符串）。检测容器 run = `ModeDetect` + 出生即 `StatusFinished`（借既有 status 过滤零改动隔离 GetByNovel/GetActiveVolumeRun/RecoverInterruptedRuns/abortRelatives，绝不污染 book-run）；复用走新增 `GetDetectRun`（mode='detect' + scope 匹配）。完成广播：volume/book 走新增 `pipeline:detect_done` 纯增量通道（既有事件协议零改动），structure 沿用 `pipeline:consistency_patrol` 既有通道。书体检口径=signabilityPrompt 全书通读（focused=false，契约/三层输入/goldenN 零改动）；S6 终检 final 口味留 R5-4 触发路由迁移。

- **文件触点**：`internal/pipeline/detect.go`、`internal/pipeline/task_dispatch.go`（队列 kind 复用/新增）、`app/`（绑定）。
- **交付物**：`RunDetectionTask(novelID int64, kind string, scopeID *int64) (*DetectionTaskResult, error)`——按 kind 分流（chapter→RunChapterCIForChapter；volume/book→建/复用 scoped 检测 run + dispatchExecutor 派发检测-only 核心；structure→runConsistencyPatrol）。建/复用 scoped 检测 run 语义对齐既有卷级 scoped sub-run（`currentVolumeScope`/`GetActiveVolumeRun`）。app 层暴露 `RunDetectionTask` 绑定（手工补 wailsjs 生成物）。
- **验收**：①四种 kind 各能独立启动、不依赖流水线阶段（无活跃流水线也能跑）；②结果落正确容器（chapter→质量档案；volume/book→工单；structure→PatrolReport）；③队列不可用时降级 legacy 直跑（逐字一致，参照 `dispatchExecutor`）；④幂等重入（同 kind+run 由队列 dedup）。
- **依赖**：T1.1。**风险**：中——scoped run 建/复用须对齐卷级 sub-run 语义，避免污染 book-run。

### T1.3 检测任务结果消费面（报告视图接线）

> **实施定案补记（T1.3 落地时）**：两个开放设计点定案——①**检测容器按 kind 分化**：mode 由 `'detect'` 改为 `'detect:<kind>'`（`detectModeOf`，GetDetectRun 加 kind 参数 + 新增 `GetLatestDetectRun`）。book 与 structure 共享全书容器有两害：按 kind 的「最近一次检测」粒度分不开 updated_at；detect/patrol 是不同队列 dedup key 可并发执行，各自 read-modify-write 同一容器 ContextJSON 会互相覆盖丢结果——分容器一并根治。②**张力报告双源合并**：`GetPatrolReport(novelID)` 合并「活跃流水线 run」（G_freeze 自动巡检/control "patrol" 落点）与「structure 检测容器」（检测任务落点）两源，按落库时间戳最新者胜——GateContext 新增 `PatrolReportAt *time.Time`（storePatrolReport 打戳；time.Time 是 struct 不能 omitempty 必须指针；旧数据无戳回退 run.UpdatedAt，因活跃 run 的 UpdatedAt 会被其他写入推高只作兜底）。PatrolReportPanel 数据源换此口（渲染条件 `run || patrolReport`，onRun 无 run 时回退 `RunDetectionTask(structure)`），组件 props 契约零改动。检测状态查询：`DetectionTaskStatus{Kind, NovelID, ScopeID, Running, LastRunID, LastVerdict, LastIssueCount, LastRunAt}` + `GetDetectionTaskStatus`（scopeID 空=最近一次概览）+ `ListDetectionTaskStatuses`（四 kind 全量供 T2.1）；chapter 结果取质量档案（L1Status+L0/L1 发现数），volume/book 取容器 Audit，structure 取 PatrolReport。**进行态=进程内登记表**（Service.detectRunning：派发即登记、执行体收口含失败注销，覆盖排队/执行/legacy 全阶段）——不回查 task 表：队列启动清扫 AbandonActive 使进行态与执行体生命周期同进程，单一源免双判据。测试坑：GateContext.Audit/PatrolReport 是 string 字段存 JSON 文本，夹具必须 JSON 串字面量（塞裸对象→Unmarshal 整体失败静默降级零结果）；gorm 默认 logger 只打报错查询，成功 SQL 不留痕（勿以日志缺失推断未执行）。

- **文件触点**：`internal/pipeline/`（检测报告查询）、`app/`（绑定）。
- **交付物**：检测任务完成后前端可查：卷/书体检→verdict + 发现项工单（既有 `IssueTicketsPanel`/`ListIssueTickets` 跨 run 查询，天然可用）；巡检→张力报告（既有 `PatrolReportPanel`）；章节→质量档案（既有）。补检测任务状态查询（按 novel+kind 查最近一次检测结果/进行态）供旅程面板展示。
- **验收**：四类检测结果各自可达现有面板（不新建重复视图）；检测进行态可见（R0 观测 `exec_run_stats` 已有，复用）。
- **依赖**：T1.2。**风险**：低——多为查询/绑定接线。

---

## 2. 前端：旅程面板检测任务入口

### T2.1 journey-quality 检测任务卡（契约 0.1/0.4）

> **实施定案补记（T2.1 落地时）**：检测任务卡区落独立组件 `frontend/src/components/journey/DetectionTasks.tsx`，挂载于 JourneyQuality 四旅程卡之后、面板直达之前（价值链顺序勿重排；JourneyCard 语义零改动——导航卡只做入口，检测启动卡自带 scope 选择与启动按钮）。scope 缺省取列表首项（少一次必点）、显式选择覆盖；章节/卷列表空=入口禁用+空态提示，书/结构恒可点（验收①）。状态行消费 T1.3 `ListDetectionTaskStatuses`（进行态/结论徽标+上次检测时间与发现数/未检测三态）；进行态轻量轮询 5s（仅 running 时），事件接线属 T2.2。降级零提示：`detection-tasks` query 前缀入 `queryErrorToast` 静默口径白名单，启动失败/回执 dispatched=false 一律静默。埋点：四入口共用 `action.detect`（featureKeys 单源）。**存量红修两处（均已 stash 验证 HEAD 上即红，顺手修复）**：①`featureKeys.test.tsx` 期望键集漏 §9.4 五个 alert key（补齐+纳 `action.detect`）；②`VoiceExtractionHint.test.tsx` 挂点接线断言仍指旧挂点 SignabilityAuditView（hint 现挂 ChatPanel+JourneyCreate），同步现挂点。测试坑：vitest 全量必须在 frontend 目录执行（`npm --prefix frontend run test`），仓库根跑假红（alias `@` 解析失败 33 文件红）；全局 setup 对 App 模块是 Proxy mock，测试文件自带 `vi.mock` 全量覆盖后须显式补齐所消费绑定（否则 query 静默 error）；`settings.selectNovelFirstHint` 配置卡与检测卡双渲染，断言用 `getAllByText`。

- **文件触点**：`frontend/src/components/journey/JourneyQuality.tsx`（检测任务卡区）、`frontend/src/lib/factory.ts`（不动，PanelId 零删除）、`frontend/src/i18n/locales/{zh-CN,en}.json`。
- **交付物**：journey-quality 增「检测任务」区：章节检测/卷体检/书体检/巡检·结构四个**启动入口**（点选 scope→调 `RunDetectionTask`），与既有"体检→修复→冻结→巡检"旅程卡并列（价值链顺序勿重排）。卷体检可选卷（VolumeManagePanel 卷列表复用）、书体检全书、巡检全书结构、章节检测选章。
- **验收**：①四入口独立可点、不依赖流水线阶段；②PanelId 零删除（只加不删）；③i18n 双语齐（`i18n:check` 过）；④theme token（`theme:check` 过）；⑤desktop-first 布局。
- **依赖**：T1.2。**风险**：低——入口聚合，绝不内嵌/重排既有工作面（JourneyCard 只做入口）。

### T2.2 检测结果反馈接线

> **实施定案补记（T2.2 落地时）**：新 hook `frontend/src/hooks/useDetectionFeedback.ts`（不改 usePipeline——巡检面板自有 patrolTick 语义勿混）订阅 `pipeline:detect_done`（卷/书，载荷 {novel_id,kind,verdict,issue_count}）与 `pipeline:consistency_patrol`（结构，既有通道）——事件到达=检测状态查询失效（状态行刷新）+该 kind 记「新结果」徽标，点「查看结果」消隐；载荷 novel 不符/缺 kind 静默忽略（降级零提示）。**结果跳转落点复用既有面板零重复视图**：章节→`chapters` 面板（侧栏质量档案标记 SidebarChapterGroup quality_score）、卷/书→`pipeline` 面板（IssueTicketsPanel 恒渲染）、结构→`pipeline` 面板（PatrolReportPanel，run||patrolReport 渲染条件 T1.3 已保两源可达）；跳转按钮有结果（last_run_at 已落库）才亮。**章节检测无完成事件**（dispatchChapterDetect 直写质量档案，收口只注销进行态）——进行态由检测状态轮询收口即可见，结果跳转不依赖事件；不为它补事件（事件协议零改动红线）。测试坑：状态查询回填前无跳转按钮，交互前须等 verdict 行出现；EventsOn 经 setup 全局 mock，回调从 `vi.mocked(EventsOn).mock.calls` 捕获后 act() 触发；hook 清理可选调用（setup Proxy mock 无返回值）。

- **文件触点**：`frontend/src/components/journey/JourneyQuality.tsx`、`frontend/src/hooks/usePipeline.ts`（或新 hook）。
- **交付物**：启动检测后前端展示进行态 + 完成后跳结果（工单面板/巡检报告/质量档案）。复用既有事件协议（`pipeline:consistency_patrol`/`issue`/stage 事件，事件协议不变——§10.4）。
- **验收**：检测启动→进行态→结果可达闭环；降级零提示（检测失败不弹错，静默）。
- **依赖**：T2.1、T1.3。**风险**：低。

---

## 3. 流水线降级为「编排预设」定位收尾

### T3.1 编排预设定位（契约 0.3）

- **文件触点**：`frontend/src/components/pipeline/PipelineWizard.tsx`（定位文案）、i18n、`JourneyQuality`/`JourneyCreate`（预设入口表述）。
- **交付物**：流水线框为"编排预设（写→检测一键串起的便捷壳）"——入口文案/说明调整，明确"可只跑检测不跑流水线、可只跑流水线不手动跑检测"。**不删流水线能力、不动 Orchestrator.Advance/闸门语义**（G 闸门降级属 R5-4）。
- **验收**：文案/定位诚实（不宣称"唯一入口"）；流水线功能零回退；PanelId 零删除。
- **依赖**：T2.2。**风险**：低——纯定位/文案。

---

## 4. 验收收尾

### T4.1 双验收 + 全量门禁 + 提交

- **验收 A（检测独立性）**：无活跃流水线时，四类检测任务皆可单独启动并落正确结果（卷/书→工单、巡检→报告、章节→质量档案）；检测-only 零 auto-advance/fix/gate。
- **验收 B（编排预设 + 勿回退）**：流水线作为预设仍可一键跑"写→检测"；自动检测节奏保留（不改纯手动）；PanelId 零删除；P0–P2 工件零触碰（grep 断言）。
- **门禁**：Go 四道闸（gofmt 改动文件 LF 归一化 / go build / go test 全量 / golangci-lint 0 issue）+ 前端四道（format:check / i18n:check / lint 0 error / build）+ `theme:check`。`TestRewriteVoiceABE2E` 真实 LLM E2E 402 外部瞬态不作回归判据。
- **提交**：一任务一提交（feat(pipeline) R5-3）+ chore(memory) 记忆账；commit 带全套 export；推送前 fetch→rebase→提交→推送→核验 AHEAD=0。
- **依赖**：T1–T3。**风险**：低。

---

## 5. 待办衔接

- R5-4 G 闸门降级（G 闸门改汇总视图、默认不卡推进）——本步不动闸门语义，留待 R5-4。
- R5 完成后 §4.0 终态图：`创作人驱动：写（S0–S3 预设或单章）→ 检测任务（章节 CI / 卷体检 / 书体检 / 巡检，可任意单独启动）→ 质量档案 + 发现项工单 → 人工修复（显式触发）→ 显式复验 → 回到检测`。
