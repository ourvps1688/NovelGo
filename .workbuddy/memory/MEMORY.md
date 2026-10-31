# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`（dev 模式 DataDir 解析为 exe 相邻 `build/bin`，DB=`build/bin/novel-agent.db`）；仓库 `ourvps1688/NovelGo`（origin），upstream=sigpanic/goink。

## 构建/环境（勿回退）
- CGO：`PATH` 含 `Program Files/Go/bin`+`tools/mingw64/bin`+`go/bin`；`CGO_ENABLED=1`；`CGO_CFLAGS=-IC:/tools/sqlite3-headers`；bare 调用须带 `LOCALAPPDATA`+`GOCACHE=C:\Users\Administrator\AppData\Local\go-build`+`GOLANGCI_LINT_CACHE`（同目录）。golangci-lint 在 `C:\Users\Administrator\go\bin`。
- 前端四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）；npm 一律带前缀、cwd 不跨调用。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；commit&&push 链式偶显 nothing to commit 但已落盘→核 `git rev-list --left-right --count origin/master...HEAD` = `0 0`。PAT 在仓库根 github_pat.txt。钩子=scripts/hooks/pre-commit。一任务一提交带全套 export；记忆账单独 `chore(memory)`。

## 架构约束（核心，勿回退）
- **R5 检测核心已收官**：创作后质量保障唯一规范路径——检测四 kind（chapter/volume/book/structure）独立启动、发现项落工单、人工确认→修复→显式复验闭环、**零 auto-advance / 零闸门 / 零自动派发修复**。后端入口 `internal/pipeline/detect.go` RunDetectionTask。
- 旧 Orchestrator 流水线（S0–S6/G1/G1b/G3b/G4/GEnd）后端保留作 legacy，但**前端不得走 StartPipeline / PipelineWizard / SignabilityAuditView**。前端规范入口 = DetectionTasks + IssueTicketsPanel（工单人工修复闭环）+ PatrolReportPanel + RetconPanel + VolumeManagePanel。文案纪律：检测核心「只跑检测不跑流水线」合法；零「唯一入口」宣称；pipeline 框标「编排预设」。
- 契约单源（勿改）：signability-audit.md=体检契约源（8 维评分）；featureKeys.ts=三类 key 单源；factory.ts=PanelId 单源（零删除）；internal/structure 长程结构索引（恒 P1 只报告）；internal/quality chapter_quality 质量档案；pipeline_issues 工单状态机。度量纯本地不上传（隐私红线）。
- 分层铁律：app 绑定层只做 DTO+委托；领域编排在 `internal/<domain>.Service`；`internal` 领域包不得直接 import wails，事件经 `internal/event.Emitter`；跨介质一致性以 DB 为真相源，文件/向量清理移出事务+启动期孤儿回收。
- **台账唯一约束（2026-10-31 落地，commit 147b667c）**：角色/地点/弧线/读者视角四类台账按自然键去重，禁止无条件 INSERT 产生重复记录。代码层 find-or-create——app 层 4 个创建入口（`CreateCharacter`/`CreateLocation`/`CreateStoryArc`/`CreateReaderPerspective`）+ MCP 工具事务内循环，经 `Store.GetByName` / `Store.GetByTypeContentHash` 命中即复用、不覆盖其字段；DB 层 `internal/migrate/v172` 迁移（`Destructive` 触发自动备份）在 AutoMigrate 前按 (novel_id,name) / (novel_id,type,content_hash) 合并存量重复行（保留内容最丰、id 最大者）、重指并折叠外键、补齐回填 reader content_hash，再加 GORM 唯一索引。读者哈希单源 `reader.ContentHash`/`NormalizeContent`（SHA1 over 归一化内容）。location_relations 重指对 `uk_location_pair` 碰撞安全（先删撞对冗余边再 UPDATE）。**新增这四类实体一律走幂等路径**。

## 复验工作流（关键 gotcha，勿回退）
- **L0 快路径（P1 已落地，commit a16b1305；粗粒度回落 LLM 修复 2026-10-31，commit 1c9b2766）**：机械类工单走 `quality.RunL0Checks` 确定性判定。精确检查项（字数/AI味/禁忌/死人出镜/战力/文风漂移）命中仍**确定性判未修复退回**（安全）；**粗粒度启发式项 `name_drift`/`tail_hook` 命中不再臆测驳回**——其类别口径比原工单（常含机械关键词的 LLM 语义细粒度问题）更宽，会以无关理由命中导致永久通不过，故 `decided=false` **回落 LLM 复核原具体指向**。`l0_verify.go` 常量 `coarseHeuristicChecks` 维护该豁免清单。L0 仍作「放行加速器」：候选项零命中且依赖齐备→自动确认通过（免 LLM）。
- **复验三档预算（2026-10-31 放宽，commit 87670600）**：`internal/pipeline/detect.go` 三档——`verifyCallTimeout` 15min→**120min**（整轮随工单数线性兜底，21 张仍余量；原 15min 把整轮腰斩，在跑工单拿整轮 ctx 到期 deadline，剩全复位 pending）；`verifyTicketTimeout` 5min→**8min**（仍早于 thinking 9min 空闲看门狗，容慢模型单轮 1-2min + ~10万 token 章节上下文）；`verifyMaxTurns` 6→**12**（多章节工单需多次 read 才出回执）。根因实证：`mimo`/`mimo-v2.6-pro`（purpose=signability）一直正常响应（llm_usage_daily 有记录、无熔断），慢在主模型+大上下文，非断网；修前 5min/6 轮往往在出回执前被斩断。
- **复验派发铁律（读己写竞态修复，2026-10-31）**：`VerifyIssueTickets` 必须**显式把待复验工单键经 task payload 的 `IssueKeys` 透传**给 `runVerifyTickets`；**严禁依赖执行体回扫 verifying 态**——旧口径曾因读己写竞态让执行体读到 0 行而提前返回，工单永久卡 `verifying`（用户侧「复验无法走下去」）。`runVerifyTickets(runID,novelID,keys)`：keys 非空按 key 精确 `getIssueRow` 加载（跳过终态），空则回落 `ListVerifyingTickets` 旧口径。`dispatchExecutor` 末位变参 `issueKeys ...string` 仅复验用；Handler 解码 `p.IssueKeys` 透传。启动期 `ResetAllStuckVerifying` 仍兜底复位残留 verifying→pending。
- **复验备注残留（2026-10-31 修复，commit 4ad28e99）**：重新进入 `verifying` 态时 `UpdateIssueStatus` 一并清空 `note`，避免「复验中」与启动清扫残留「已复位待复验」并排显示误导（曾致用户误判"又卡死"）。仅 `VerifyIssueTickets` 把状态置 `verifying`，收窄安全。

## 供应商熔断（P0 已落地）
- 三态自愈熔断器 `internal/llm/breaker.go`（按 provider 隔离，Closed/Open/Half-Open，冷却到期自动半开探针恢复）。**熔断只认硬失败**（连接拒绝/DNS 不可达/首字节超时/≥5xx）；空闲超时（idle watchdog）**只观测绝不驱动熔断**。chat 入口与 pipeline 共用同一 `ProviderBreaker` 单例；空闲预算 thinking 模型 9min、其余 3min。

## 排障/坑（勿回退）
- 重启 wails dev：①带全套 Go/CGO export 跑 `/c/Users/Administrator/go/bin/wails.exe dev`；②重启前 `rm -rf frontend/dist/assets`（否则 vite emptyDir 触发 SAFE_DELETE_BULK_CONFIRM_REQUIRED）；③结束旧进程 `Stop-Process -Id <pid> -Force`。wails watcher 卡死须核验运行中构建（grep 二进制文案实证新代码进进程）。
- 浏览器访问 localhost:5173 白屏=正常（Wails 桥 `window.go`/`window.runtime` 仅注入桌面 webview，浏览器无桥→React 初始化抛错白屏）；必须用 wails dev 桌面窗口。
- LF 归一化只对本次改动文件做（全树 sed 误伤 50+）；读文件做断言的测试须 CRLF 归一化或钉 eol=lf。
- **vitest 沙箱内偶发失败但版本无关**（vite 8 + vitest 4 兼容）：根因资源受限（forks 池/ jsdom 渲染空 div）；脱离沙箱全量 385 测试 EXIT=0。pre-commit 只校验 Go 文件；前端用 format/i18n/lint(0 error)/build 兜底，vitest 建议脱沙箱跑。

## 章节寻址约定
- chapter_id 是稳定主键（DB 自增），阅读序号是实时计算（volumes.sort_order+chapters.sort_order），二者不等价；换卷/重排后对应关系会变。前端严格用后端 reading_number。
- 按阅读序号确定性寻址 `chapters/no_{N}.md`：`git.ChapterPathRef.ByReadingNumber`/`ParseChapterLikePath` 增 `no_(\d+)`；read/edit 经 `resolveChapterRowRef`→`GetChapterIDByReadingNumber` 反查落盘。工具铁律：让 LLM 精修「第N章」必须用 `no_{N}`，严禁拿 get_chapter_list 的「第N章」当 id 推算。
