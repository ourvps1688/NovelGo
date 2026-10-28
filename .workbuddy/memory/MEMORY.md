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

## 供应商熔断重设计（P0 已落地，commit 74800a1b）
- **根因修复**：原 `provider_trip.go` 用「近 10min 空闲超时 ≥10 次」当硬信号误熔断（thinking 模型长静默/代理缓冲被误杀）。P0 把「空闲超时」从熔断触发信号**剥离**，熔断只认**硬失败**。
- **三态自愈熔断器** `internal/llm/breaker.go`：`providerBreakerManager` 全局单例 `ProviderBreaker`，按 provider 隔离；三态 Closed/Open/Half-Open；阈值初值 `hardFailureTrip=5`、`hardFailureWindow=2min`、`breakerCooldown=30s`、`breakerProbeSuccess=1`；Open 冷却到期→自动 Half-Open 探针，探针成功→Closed、失败→回 Open（自愈）。
- **硬失败口径**（`retry_matrix.go` 置 `APIError.Hard`）：连接拒绝 / DNS 不可达 / 首字节超时 / `>=5xx` → `Hard=true`；瞬时 EOF/reset、429/408、其余 4xx、空闲超时 → `Hard=false`。空闲超时（idle watchdog）**只观测**（`idleFires`/`RecentIdleTimeouts`），绝不驱动熔断。
- **接线**：`stream.go` 两处错误点（`apiErrorFromNetErr`/`apiErrorFromHTTP`）`Hard` 时 `ProviderBreaker.RecordHardFailure(providerName)`；`parseSSE` 改为返回 `hadError`，仅流正常结束（`!hadError`）调 `RecordSuccess`（软错误不回血）。`provider_trip.go` `providerTripped()` 改基于 `llm.ProviderBreaker.AnyOpen` 取数接缝 `breakerOpenProviders`（替换旧 `idleTimeoutCount`），废弃 `fixProviderIdleTrip`/`providerIdleWindow` 作熔断条件。
- **测试纪律**：`breakerOpenProviders` 是可替换接缝（测试钉 `["deepseek"]` 确定触发）；`TestSkillRunnerDispatchOnlyViaTripGuard` 仍锁 `s.skillRunner(` 仅 1 处（在 `dispatchSkill` 内）。新增 `breaker_test.go` 覆盖三态迁移/阈值/按 provider 隔离/自愈/空闲不触发。
- **P1 已落地（commit `1636f9bc`，命题 §7 Step4/5）**：①**熔断下沉到 LLM 调用入口**——`breaker.go` 新增 `IsOpen(providerName)` 定向判断（冷却到期自动转 Half-Open，与 `AnyOpen` 共享自愈状态机）与取数接缝 `breakerBlockedProvider = ProviderBreaker.IsOpen`；`ChatStream`/`GenerateText` 在 provider 查找后插守卫，命中即 `breakerOpenError`（Kind=`provider_breaker_open`、`Retryable=false`）快速失败——agent 只对 `Retryable` 错误重试，故非可重试错误干净快速失败、不狂打。pipeline（`dispatchSkill`→`AnyOpen`）与 chat（入口→`IsOpen`）共用同一 `ProviderBreaker` 单例，**验收点 3（状态一致）达成**。②**空闲预算按模型放宽**——`thinkingIdleTimeout=9min`，`newIdleWatchdog` 加 `timeout` 参数，`idleTimeoutFor(provider,model)` 按 `ModelInfo.SupportsThinking` 选预算（思考模型 9min、其余 3min、未知回落 3min）。
- 测试：`breaker_test.go` 保留 P0 6 项并新增 6 项 `IsOpen`（含与 `AnyOpen` 一致性）；`stream_test.go` 新增 `ChatStream`/`GenerateText` 拦截测试（经接缝确定性驱动、不污染全局 `ProviderBreaker`）+ `idleTimeoutFor` 测试。gofmt/lint/build/test 四道闸全绿、推送核 `0 0`。
