# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；仓库 `ourvps1688/NovelGo`（origin），upstream=sigpanic/goink。

## 构建/环境（勿回退）
- 前端四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）；改前端须 build 后重启 wails dev。
- CGO：`export PATH="/c/Program Files/Go/bin:/c/tools/mingw64/bin:$PATH"; export CGO_ENABLED=1; export CGO_CFLAGS="-IC:/tools/sqlite3-headers"`（gcc 在 /c/tools/mingw64/bin）。bash 无 LocalAppData 须带 GOCACHE+GOLANGCI_LINT_CACHE。golangci-lint 在 /c/Users/Administrator/go/bin。gofmt 须 LF 归一化后查。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；commit&&push 链式偶显 nothing to commit 但已落盘→核 `git rev-list --left-right --count origin/master...HEAD` = `0 0`。PAT 在仓库根 github_pat.txt。钩子=scripts/hooks/pre-commit。
- 一任务一提交带全套 export；记忆账单独 `chore(memory)`。npm 一律 `npm --prefix frontend run X`（cwd 不跨调用）。
- 测试瞬态：真实 LLM E2E 偶发 FAIL 复跑绿即收口；无 key Skip（NOVELGO_EVAL_KEY）。

## 架构状态（核心约束，勿回退）
- **R5 检测核心已落地收官**（R0–R4 + R5-1/2/3/4 全销），是创作后质量保障的**唯一规范路径**：检测任务四 kind（chapter/volume/book/structure）独立启动、发现项落工单、人工确认→去修复→显式复验闭环、**零 auto-advance / 零闸门 / 零自动派发修复**。后端入口 `internal/pipeline/detect.go` RunDetectionTask。
- 旧 `Orchestrator` 流水线（S0–S6/G1/G1b/G3b/G4/GEnd）后端保留作 legacy，但**前端不得再走 StartPipeline / PipelineWizard / SignabilityAuditView**（S4/S5/S6 + G4 闸门硬卡）。前端规范入口 = DetectionTasks（四 kind 启动）+ IssueTicketsPanel（工单人工修复闭环）+ PatrolReportPanel + RetconPanel + VolumeManagePanel。
- 文案纪律：检测核心/检测任务「只跑检测不跑流水线」完全合法；零「唯一入口」宣称；pipeline 框仅为「写→检测」便捷壳（标注「编排预设」）。降级逐字一致是最高验收。
- 契约单源（勿改）：signability-audit.md=体检契约源；featureKeys.ts=三类 key 单源；factory.ts=PanelId 单源（零删除）；internal/structure 长程结构索引（恒 P1 只报告）；internal/quality chapter_quality 质量档案；pipeline_issues 工单九态。度量纯本地不上传（隐私红线）。

## 排障/坑（勿回退）
- LF 归一化只对本次改动文件做（全树 find+sed 误伤 50+ 文件）；读文件做逐字/正则断言的测试须 CRLF 归一化或钉 eol=lf。
- 重启 wails dev 要点：①`/c/Users/Administrator/go/bin/wails.exe dev`（带全套 Go/CGO export）；②重启前 `rm -rf frontend/dist/assets`（否则 vite emptyDir 触发 SAFE_DELETE_BULK_CONFIRM_REQUIRED 失败）；③结束旧进程用 PowerShell `Stop-Process -Id <pid> -Force`。wails watcher 会卡死不重建，修完须核验运行中构建（grep 二进制文案实证新代码进进程）。
- 队列测试「入队后断言不被执行」须先停/延后消费循环。MCP read 工具 start_line 越界=窗口平移收敛+note；破坏性参数才 required。grep 零回退口径：P0–P2 工件、theme token、desktop-first、PanelId 零删除。

## 章节寻址约定（聊天 LLM 防找错章，勿回退）
- **chapter_id 是稳定主键（DB 自增），阅读序号是实时计算（volumes.sort_order+chapters.sort_order），二者不等价；换卷/重排后对应关系会变。** 前端严格用后端 reading_number，编号本身没错。
- `get_chapter_list` **升序**返回（第1章在最前、未分卷置末尾）；Description 明示 chapter_id≠阅读序号，并提示用 `no_{N}` 定位。
- 章节物理路径仍按 id 落盘（`chapters/id_{id}.md` / `outlines/id_{id}.md`）。新增**按阅读序号确定性寻址** `chapters/no_{N}.md` / `outlines/no_{N}.md`：`git.ChapterPathRef` 增 `ByReadingNumber`/`ReadingNumber`，`ParseChapterLikePath` 增 `no_(\d+)` 正则；read/edit 经 `resolveChapterRef`→`chapter.Store.GetChapterIDByReadingNumber` 反查再落物理路径；越界返回明确业务错误。`agent/chapterID` 同步解析 `no_{N}` 保证展示标题正确。
- 工具描述铁律：让 LLM 精修「第N章」必须用 `no_{N}`，严禁把 get_chapter_list 里的「第N章」直接当 id 推算（错误示例：精修第1章却用 id_1）。

## 当前任务（2026-10 末）：前端切到检测核心
- #257 新建 DetectionCorePanel（组合 IssueTicketsPanel+PatrolReportPanel+RetconPanel+VolumeManagePanel，react-query 取 GetPatrolReport + ListDetectionTaskStatuses；为 PatrolReportPanel 供 data/tick/onRun(structure 检测)）。
- #258 WorkspaceView 的 pipeline 面板切到 DetectionCorePanel（移除 PipelineWizard 导入与渲染分支）。
- #259 删旧流水线/闸门视图与孤立文件：PipelineWizard.tsx / SignabilityAuditView.tsx / PipelineControlBar.tsx / PipelineGateCard.tsx / BlueprintWorkflowView.tsx / usePipeline.ts / PipelineWizard.test.tsx。
- #260 SignabilityWorkflowView 改用 RunDetectionTask(novelId,book,null) 独立启动（移除 StartPipeline/GetPipelineState）。
- #261 PIPELINE_STAGES 移除 S4/S5/S4b/S6/GEnd（保留 S0/S1/G1/S2/S3/G3b）。
- #262 i18n（pipeline.title→检测核心，zh/en）+ featureKeys.test.tsx 移除 PipelineGateCard 引用。
- #263 四道闸 + vitest 验证（format:check/i18n:check/lint 0 error/build/vitest 全绿）→ 一任务一提交推送核 0 0。

## 复验 L0 快路径（P1 已落地，commit a16b1305）
- 目的：复验去 LLM 化——机械类工单（字数/章尾钩子/AI味/称呼混用/死人出镜/战力数值序/文风漂移）走 quality.RunL0Checks
  确定性判定，修复即自动确认（免 LLM）；仅语义矛盾（矛盾/冲突/逻辑/人设/情节/崩/伏笔…）与依赖台账缺失无法判定者退回 LLM。
- 核心文件：internal/pipeline/l0_verify.go（classifyMechanicalChecks / l0Verdict / tryL0Verify / defaultL0ChapterReader）。
  service.go 加 l0Reader 接缝；issue_tickets.go verifyOneTicket 顶部插入 L0 快路径。复用 propagation.go 的 l0Characters/novelGenre，勿重复定义。
- 安全口径：l0Verdict 三分支——有效项仍命中→退回待修；零命中且 allVerifiable→自动确认；零命中但有依赖缺失项→保守退回 LLM。
  依赖齐备性过滤：style_drift 需 voice 基线、name_drift/dead_appearance 需角色台账、power_upset/crush 需境界表，缺失降级移除，全缺失→退回 LLM。

## 签约潜力工作流呈现与题材卡注入（已落地，commit 009407c4）
- **问题**：用户反馈「签约潜力工作流点『开始签约体检』后没按设计反馈评估内容，且与巡检（structure）长得一样」。经排查是**程序接线缺口 + 设计口径不一致**，非运行期 bug（按钮正常派发 book 检测且产出工单）。
- 三大缺口（已修）：
  1. 前端未渲染「签约潜力评估」面板——后端 `PipelineRun.Verdict/Score/Summary` 已落库，但状态接口 `DetectionTaskStatus` 只透 `last_verdict/last_issue_count/last_run_at`，无 `score/summary`；`SignabilityWorkflowView` 仅渲染静态题材卡约束文案 + 通用工单列表（与巡检同组件）→ UI 一致。
  2. 题材卡约束根本没注入体检提示词——`signabilityPrompt`(service.go:2499) 未塞题材卡；审计上下文块 `contextasm.PurposeAudit` 只含 Facts+Voice(asm.go:32)。题材卡参数（`BeatDensityMin/TailHookRequired/Taboos/WindowN`）在 `internal/quality/checker_config.go`，原仅供 L0 检测。
  3. 评估维度两套口径未对齐——设计稿列 7 个题材卡软约束维；实际技能 `signability-audit.md` v2 定义 8 维评分。**用户拍板：以技能 8 维为准**。
- **落地**（commit 009407c4）：
  - 后端 `detect_status.go`：`DetectionTaskStatus` 增 `last_score`/`last_summary`，`scopedDetectStatus` 从 `AuditData`(parseStoredAudit) 填充。
  - 后端 `service.go`：新增 `genreCardBlock(novelID)` 从 `quality.GetCheckerConfig` 取爽点密度/章尾钩子/禁忌/窗口N+题材名注入；`auditInput` 增 `genre` 字段；`signabilityPrompt` 末尾拼接该块（未配置回落通用标准、不报错中断）。
  - 前端 `SignabilityWorkflowView`：渲染专属「签约潜力评估」面板（评分/40 + 结论 + 八维评估框架），与通用工单列表区分；`models.ts` 增 `last_score/last_summary`；i18n(zh-CN/en) 增 `signability.score/summary/dimensionEight/scoreOutOf` 等键。
  - `signability-audit.md` 是契约源（8 维评分口径），勿回退。
- 四道闸：后端 gofmt/gobuild/vet/golangci-lint(webkit2_41 0 issues)/go test 全绿；前端 format:check/i18n:check/lint(0 error)/build/vitest(385 通过) 全绿。

## 检测工作流结构分支渲染张力报告（已落地，commit f301946d）
- 用户要求把 DetectionWorkflowView 的巡检/structure 分支也改为渲染 PatrolReport 专属张力报告面板，而非复用通用工单列表（与上方书体检修复同一个「界面长得一样」表层问题）。
- 落地：structure 分支经 `GetPatrolReport` 双源合并取数（活跃 run + structure 检测容器，落库时间戳最新者胜），解析 `patrol_report` JSON→`AuditData` 喂 `PatrolReportPanel`（结论徽章+摘要+张力条+作用域标签）；监听 `pipeline:consistency_patrol` 完成事件失效重拉 + tick 驱动收起「运行中」提示（与 `DetectionCorePanel` 同一套契约，事件由 `patrol.go` 广播）。结构巡检只报告张力、零发现项工单→移除「查看工单」入口；章节/卷仍走通用工单列表。复用既有 `PatrolReportPanel`（pipeline.patrol.* i18n 已齐备），未新增文案键、未改该组件。前端四道闸全绿，后端无需改动。

## 供应商熔断重设计（P0 已落地，commit 74800a1b）
- **根因修复**：原 `provider_trip.go` 用「近 10min 空闲超时 ≥10 次」当硬信号误熔断（thinking 模型长静默/代理缓冲被误杀）。P0 把「空闲超时」从熔断触发信号**剥离**，熔断只认**硬失败**。
- **三态自愈熔断器** `internal/llm/breaker.go`：`providerBreakerManager` 全局单例 `ProviderBreaker`，按 provider 隔离；三态 Closed/Open/Half-Open；阈值初值 `hardFailureTrip=5`、`hardFailureWindow=2min`、`breakerCooldown=30s`、`breakerProbeSuccess=1`；Open 冷却到期→自动 Half-Open 探针，探针成功→Closed、失败→回 Open（自愈）。
- **硬失败口径**（`retry_matrix.go` 置 `APIError.Hard`）：连接拒绝 / DNS 不可达 / 首字节超时 / `>=5xx` → `Hard=true`；瞬时 EOF/reset、429/408、其余 4xx、空闲超时 → `Hard=false`。空闲超时（idle watchdog）**只观测**（`idleFires`/`RecentIdleTimeouts`），绝不驱动熔断。
- **接线**：`stream.go` 两处错误点（`apiErrorFromNetErr`/`apiErrorFromHTTP`）`Hard` 时 `ProviderBreaker.RecordHardFailure(providerName)`；`parseSSE` 改为返回 `hadError`，仅流正常结束（`!hadError`）调 `RecordSuccess`（软错误不回血）。`provider_trip.go` `providerTripped()` 改基于 `llm.ProviderBreaker.AnyOpen` 取数接缝 `breakerOpenProviders`（替换旧 `idleTimeoutCount`），废弃 `fixProviderIdleTrip`/`providerIdleWindow` 作熔断条件。
- **测试纪律**：`breakerOpenProviders` 是可替换接缝（测试钉 `["deepseek"]` 确定触发）；`TestSkillRunnerDispatchOnlyViaTripGuard` 仍锁 `s.skillRunner(` 仅 1 处（在 `dispatchSkill` 内）。新增 `breaker_test.go` 覆盖三态迁移/阈值/按 provider 隔离/自愈/空闲不触发。
- **P1 已落地（commit `1636f9bc`，命题 §7 Step4/5）**：①**熔断下沉到 LLM 调用入口**——`breaker.go` 新增 `IsOpen(providerName)` 定向判断（冷却到期自动转 Half-Open，与 `AnyOpen` 共享自愈状态机）与取数接缝 `breakerBlockedProvider = ProviderBreaker.IsOpen`；`ChatStream`/`GenerateText` 在 provider 查找后插守卫，命中即 `breakerOpenError`（Kind=`provider_breaker_open`、`Retryable=false`）快速失败——agent 只对 `Retryable` 错误重试，故非可重试错误干净快速失败、不狂打。pipeline（`dispatchSkill`→`AnyOpen`）与 chat（入口→`IsOpen`）共用同一 `ProviderBreaker` 单例，**验收点 3（状态一致）达成**。②**空闲预算按模型放宽**——`thinkingIdleTimeout=9min`，`newIdleWatchdog` 加 `timeout` 参数，`idleTimeoutFor(provider,model)` 按 `ModelInfo.SupportsThinking` 选预算（思考模型 9min、其余 3min、未知回落 3min）。
- 测试：`breaker_test.go` 保留 P0 6 项并新增 6 项 `IsOpen`（含与 `AnyOpen` 一致性）；`stream_test.go` 新增 `ChatStream`/`GenerateText` 拦截测试（经接缝确定性驱动、不污染全局 `ProviderBreaker`）+ `idleTimeoutFor` 测试。gofmt/lint/build/test 四道闸全绿、推送核 `0 0`。
- **Step10 度量对齐（WBS 收口，commit `2f05eb6b`）**：主体已被 P0/P1 顺带完成——pipeline `tripped`(`ExecStatusTripped`，`dispatchSkill` 在 `providerTripped()` 真时记)↔`breakerOpenProviders()=AnyOpen` 已对齐；`idleFires`(`recordIdleFire`/`RecentIdleTimeouts`，`stream.go:130`)保留作长期观测（不驱动熔断）。唯一真实未对齐：前端 `tripHint`(zh-CN:1974/en:2014)仍称「空闲超时触发熔断」，已改为「硬失败(连接拒绝/5xx/首字节超时)口径 + 冷却后自动半开探针恢复」与 P0 事实一致。chat 度量**按设计排除**——`ExecStat` 是 pipeline-run 概念，chat 走独立 UX 且已与 pipeline 共用同一 `ProviderBreaker` 单例（状态一致），不补记 `tripped`。前端四道闸(含 lint 0 error)全绿。

## 后端维度 A 收官（2026-10-29，已推送 origin/master 核 0 0）
- 四任务 E1/A1/A3,D3/A2 全落地：commit `856af2eb`/`fcc448c5`/`b83611e7`/`ef1f7593`，`git push origin master` 核 `0 0`。
- 分层铁律固化（审计整改验收点）：`app` 绑定层只做 DTO+委托；领域编排在 `internal/<domain>.Service`；`internal` 领域包**不得直接 import wails**，事件经 `internal/event.Emitter` 注入；跨介质一致性以 DB 为真相源，文件/向量清理移出事务 + 启动期孤儿回收（见 E1）。
- 新增领域包：`internal/event`(`Emitter`/`NopEmitter`，A3/D3)、`internal/content`(`ContentService.DeriveAfterSave`，A1)、`internal/commit`(`Service`：git 自动提交+turn_commits 落库，全 best-effort，A2)。
- chat.go 已无 `commitUserChanges`/`commitAIChanges`/`doCommit`/`writeSystemMessages` 与 git/gorm/rollback 直连；落库走 `session.Store`，git 收尾走 `commit.Service`。

## 维度 B 产品缺口（2026-10-29 起推进，已推送 origin/master 核 0 0）
- 总览见 `docs/CODE_AUDIT_OVERVIEW_2026-10-29.md` 维度 B（P1 关键项 9 条 + 前端工程债）。已建任务 #5–#10（B1–B6）。
- **B1 消息排序 id tiebreaker（commit `125e78db`）**：`internal/session/store.go` 的
  `GetMessagesForAPI/ForFrontend/AllMessages` 排序由 `created_at ASC` 改为 `created_at ASC, id ASC`，
  以自增 id 作同秒消息稳定排序；新增 `store_test.go` 内存 SQLite 强制同秒断言按 id 升序。
- **B2 检测发现项跳转章节原文（commit `d8f56b84`）**：闭环「检测→确认→去修复→复验」最后一跳。
  新增 `frontend/src/stores/useNavigationStore.ts`（范式同 `useChatLaunchStore`，深层面板零侵入）；
  `WorkspaceView` 经 `useChapters` 按阅读序号反查 chapter 对象并委托 `handleSelectChapter` 打开；
  `IssueTicketsPanel` 把涉及章节号渲染为可点击按钮 → `openChapterByReadingNumber`（沿用阅读序号→章节寻址约定）。
  另修维度A遗留：`narrative.test.ts` tagline 断言同步到「检测核心」文案（commit `f2e1fcbe`，此前 2a5d926f 改了源文案却漏改测试，导致 vitest 基线红）。
- 验证纪律：后端 `go build ./...`+`go test ./...`+gofmt+pre-commit 双闸；前端四道闸
  format:check/i18n:check/lint(0 error)/build/vitest 全绿（vitest 默认 threads 池偶发 worker 超时，
  用 `--pool=forks` 稳定全绿，384 测试通过）。
- **B3 放开 SQLite 连接池提升读写并发（commit `4311be43`）**：WAL 已启用，瓶颈是 `SetMaxOpenConns(1)` 强制串行。放开为 `max(4, min(NumCPU,8))`；DSN 注入 `_busy_timeout=5s` 让并发写自旋等待写锁而非报 locked；保留 idle/lifetime。新增 `sqlite_concurrency_test.go` 验证并发读写不报 locked 且自增无丢失更新。
- 待办 B4–B6（未动）：写章前情注入(B4)、检测任务取消/超时(B5)、失败 turn 重试(B6)。
- **环境坑（重要，勿回退）**：本机 `%LocalAppData%` 未定义，bare `go`/`golangci-lint` 因 GOCACHE 缺失会报 "no go files to analyze" 或 "build cache required"。提交/跑 lint 必须带 `GOCACHE=$HOME/.cache/go-build`（及 `GOLANGCI_LINT_CACHE`/`CGO_*`/`PATH`）。`run.go:"1.25"` 与实际 go1.27 不冲突——GOCACHE 一设即可。
