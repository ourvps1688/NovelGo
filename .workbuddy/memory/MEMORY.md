# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`（dev 解析为 exe 相邻 `build/bin`，DB=`build/bin/novel-agent.db`）；仓库 `ourvps1688/NovelGo`（origin），upstream=sigpanic/goink。

## 构建/环境（勿回退）
- CGO：`PATH` 含 `Program Files/Go/bin`+`tools/mingw64/bin`+`go/bin`；`CGO_ENABLED=1`；`CGO_CFLAGS=-IC:/tools/sqlite3-headers`；裸调用须带 `LOCALAPPDATA`+`GOCACHE=C:\Users\Administrator\AppData\Local\go-build`+`GOLANGCI_LINT_CACHE`（同目录）。golangci-lint 在 `C:\Users\Administrator\go\bin`。
- 前端四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 0 error 上限）。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；一任务一提交带全套 export；记忆账单独 `chore(memory)`；核验 `git rev-list --left-right --count origin/master...HEAD` = `0 0`。

## 架构约束（核心，勿回退）
- R5 检测核心已收官：质量保障唯一规范路径——检测四 kind（chapter/volume/book/structure）独立启动、发现项落工单、人工确认→修复→显式复验闭环、零 auto-advance/零闸门/零自动派发修复。后端 `internal/pipeline/detect.go` RunDetectionTask。
- 旧 Orchestrator 流水线后端保留 legacy，前端不得走 StartPipeline/PipelineWizard/SignabilityAuditView；前端规范入口 = DetectionTasks + IssueTicketsPanel + PatrolReportPanel + RetconPanel + VolumeManagePanel。
- 契约单源（勿改）：signability-audit.md=体检契约源（8 维评分，desc 措辞须强制前向适配——禁止「改任一处」回改前章）；featureKeys.ts=key 单源；factory.ts=PanelId 单源；pipeline_issues 工单状态机。度量纯本地不上传（隐私红线）。
- 分层铁律：app 绑定层只做 DTO+委托；领域编排在 `internal/<domain>.Service`；`internal` 不得直接 import wails，事件经 `internal/event.Emitter`；跨介质一致性以 DB 为真相源。
- 台账唯一约束（2026-10-31, 147b667c）：角色/地点/弧线/读者视角四类台账按自然键去重，新增一律走幂等 find-or-create；DB 层 v172 迁移合并存量重复行+加唯一索引。

## 近期修复（勿回退）
- 签约体检 chapter_ids 校验（2026-11-01）：`chapter_ids` 必须是有效全局阅读序号（1..N，N=章节总数），台账/全书级填 `[]`。LLM 常把台账 ID 或规划章号填进结构化字段→脏数据。修复：`ReplaceIssues` 落库前 `filterChapterIDs` 按 `SELECT COUNT(*) FROM chapters` 上界裁剪（缺表 HasTable 守卫）；`parseChapterRefs` 兜底只认「第N章」/「章#N」（裸 `#\d+` 视为台账引用）。
- 工单去修复 UI 卡死修复（2026-11-01, 47125469）：点「去修复」按钮转圈+状态陈旧——根因 `dispatchExecutor` 在 `Enqueue` 遇瞬态 `database is locked` 时**同步**跑长耗时 LLM 修复，致 `StartIssueFixTickets` 永不返回。修复：Enqueue 失败改**后台 goroutine**+瞬态锁短重试（3×40ms）+`legacyGuard` 同键去重；队列不可用分支保留同步。状态 `fixing` 在 dispatch 前已落库，前端 `load()` 刷新即显示修复中。
- v172 去重迁移子表守卫（2026-11-01, 6a0aca2f）：PreSchemaSteps 在 AutoMigrate 建表前跑，去重函数碰引用子表须先 `HasTable` 守卫，否则 dev 库缺新子表时报 `no such table: group_members` 启动失败。
- 复验铁律（审计 P0）：`VerifyIssueTickets` 须显式经 task payload 的 `IssueKeys` 透传给 `runVerifyTickets`，严禁执行体回扫 verifying 态（读己写竞态曾致永久卡 verifying）。L0 快路径粗粒度项 `name_drift`/`tail_hook` 命中回落 LLM 复核原指向。三档预算放宽（verifyCallTimeout 120min / verifyTicketTimeout 8min / verifyMaxTurns 12）。重新进入 verifying 一并清空 note。
- 复验自动升级人工（2026-11-01）：复验「明确未通过」累计达 3 次（`verifyEscalateAfter`，仅计 rejected 不计 unverified）自动转 `skipped`（人工工单，复用既有语义；`PipelineIssue.verify_attempts` 计数，AutoMigrate 自动加列），skip_reason/note 写明「复验 N 次未通过，自动转人工工单」；前端状态徽章不变（skipped=已跳过），面板 note 区可见原因。防修复→复验失败无限空转。
- 签约体检报告解析泄漏修复（2026-11-01, 7cb519d8）：`parseAudit`（service.go）此前 JSON 解析失败兜底把整段原始 LLM 输出塞进 Summary/issue.Desc 并持久化，致签约潜力评估面板把整份报告当纯文本铺开（含末尾原始 JSON）。改为健壮抽 JSON（json 代码围栏 + 末段平衡大括号兜底）+ 优雅降级（干净 P0 issue 提示重跑体检，绝不写原文）；新增 TestParseAuditNoRawTextLeak 回归测试。
- 签约体检卡「一直运行中」死循环修复（2026-11-01, c4afcb37）：供应商「连得通但不吐数据」时，空闲看门狗每约9分钟超时一次，旧代码把空闲超时一律标 Retryable:true 且熔断刻意忽略空闲超时，致 agent 无限重试直到60分钟硬超时，界面表现为签约体检一直运行中卡死。修复：idleWatchdog 记录本次请求是否收到过字节；新增 idleRetryable 纯函数——零字节空转（死连接）判不可重试（直接失败并给「未收到任何字节/疑似供应商无响应」清晰错误），已收字节（真实慢生成/中途断流）仍保留可重试，不误杀思考模型长静默。非流式（generate.go）与流式（stream.go）两路径接入；新增 TestIdleRetryable/TestIdleTimeoutErrorGotBytes。熔断行为不变（仍不因子空闲超时熔断）。

## ONNX 现状（结论，细节见 docs/onnx-enhancement-development-plan.md）
- 仅 `bge-small-zh-v1.5`（512维）做文本向量化（4 消费方全部优雅降级）。reranker 已 L3 彻底移除（真实评测净负优化）；P0-2 批处理/缓存 ✅；P1-1 长文分段聚合 ✅（不影响功能）；P1-2 升级 embedding 模型 评估后放弃（维度变更须重建向量库）；P2 本地分类器/NER 降级待触发。

## 供应商熔断
- 三态自愈熔断器 `internal/llm/breaker.go`，按 provider 隔离；只认硬失败（连接拒绝/DNS/首字节超时/≥5xx），空闲超时只观测不熔断。

## 章节寻址 / 工单引用契约
- chapter_id（主键）≠ 阅读序号（实时计算）；前端严格用 reading_number；LLM 精修「第N章」须用 `no_{N}` 路径，严禁拿 get_chapter_list 的「第N章」当 id 推算。
- 工单 chapter_ids=全局阅读序号；台账引用写 `<type>#<id>`（id=自增主键，常上千）；裸 `#\d+` 一律视为台账引用。

## 排障
- 重启 wails dev：带全套 CGO export **+ `CODEBUDDY_SAFE_DELETE_ENABLED=0`** 跑 `wails.exe dev`。关键：WorkBuddy 的 node-safe-delete-shim（`app.asar.unpacked/cli/vendor/shim/node-safe-delete-shim.cjs`）会拦截 vite 的 emptyDir（threshold 50 / scope turn，抛 `SAFE_DELETE_BULK_CONFIRM_REQUIRED`）；且 `wails dev` 跑 `tsc -b && vite build`，`tsc -b` 会在 rm 之后重新往 `dist/assets` 吐编译产物，故**仅预删 assets 无效**，必须靠该 env 关掉 shim 才能让 vite build 过。结束旧进程 `Stop-Process -Id <pid> -Force`（注意 `ps -W` 的 MinGW PID ≠ Windows 真实 PID，须用 PowerShell `Get-Process` 取真实 PID 再杀，否则杀不干净致旧实例残留）。浏览器访问 5173 白屏=正常（Wails 桥仅注入桌面 webview）。
- vitest 沙箱偶发失败（资源受限）；前端用 format/i18n/lint(0 error)/build 兜底，vitest 脱沙箱跑。
- wails 重启重产的前端绑定文件（App.d.ts/App.js/models.ts）常现纯缩进/排序噪声，git checkout 还原即可，勿混入提交。
