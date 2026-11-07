# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25+Wails v2+React 19+SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 github.com/novelgo/novelgo；DB=build/bin/novel-agent.db（dev）；仓库 ourvps1688/NovelGo（origin）。

## 构建/环境（勿回退）
- CGO：PATH 含 `Program Files/Go/bin`+`tools/mingw64/bin`+`go/bin`；`CGO_ENABLED=1`；`CGO_CFLAGS=-IC:/tools/sqlite3-headers`；裸调用带 `LOCALAPPDATA`+`GOCACHE`/`GOLANGCI_LINT_CACHE`=`C:\Users\Administrator\AppData\Local\go-build`。golangci-lint 在 go\bin。
- 前端四道闸 `npm --prefix frontend run format:check/i18n:check/lint/build`（eslint 0 error；i18n 禁 JSX 硬编码中文）。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；一任务一提交带全套 export；记忆账单独 chore(memory)；核验 `git rev-list --left-right --count origin/master...HEAD` = 0 0。

## 架构约束（勿回退）
- 质量保障唯一规范路径：检测四 kind 独立启动→发现项落工单→人工确认→修复→显式复验；零 auto-advance/零闸门/零自动派发修复。前端入口=DetectionTasks+IssueTicketsPanel+PatrolReportPanel+RetconPanel+VolumeManagePanel，禁 StartPipeline/PipelineWizard/SignabilityAuditView。
- 契约单源勿改：signability-audit.md（8 维，desc 禁回改前章）、featureKeys.ts、factory.ts、pipeline_issues 状态机。度量纯本地不上传。
- 分层：app 绑定层只 DTO+委托；internal 禁 import wails（走 event.Emitter）；DB=真相源。
- 台账唯一约束（147b667c）：四类台账自然键去重 find-or-create；v172 迁移合并+唯一索引。

## 质量升级路线图 2026-11（全部落地勿回退）
- P0-0 开篇体检（4189a7bb）：L0 开篇四查 opening.go + L1 契约 v2 immersion_anchor + CheckerConfig 关键词列（全空=不检查）。
- P1-1 编辑反馈回流（67410ffb）：editorfeedback 子 Agent→editor_feedbacks+F 系工单。mcp_tools→pipeline 是 import cycle，落单走 SetFeedbackTicketLander 回调；feedback 测试用外部包。
- P0-1 平台 profile：platform_profile.go qimao preset+L0 三查（opening_hook/chapter_length_fit/beat_density）；Validate 空白 profile 归一为空；字数=纯 CJK。
- P0-3 评分打通：review/score_mapping.go 6↔8 映射+chapter_score_events+体检 dims+ScoreTrendPanel。golden 基线 `go test ./internal/pipeline/ -run TestP0BaselineRegen -p0-update`。
- P1-2 平台数据对账：platform_metrics 表+GetPlatformReconciliation+PlatformDataPanel。
- P0-2 情绪账：chapter_tension 表+曲线三查+E 系工单（ReplaceIssuesByPrefix ts- 独立容器）+TensionCurveCard。文案避开语义排除词（矛盾/冲突/逻辑/人设/崩/连贯/伏笔/悬念/篇幅）。
- P2-1 技能路由 rewrite_skills.go：issue.Dimension 是台账维枚举≠八维键。P2-2 节奏模板 scene-beats.md。P2-3 对标书库 benchmark_books+RenderBenchmarkBlock。

## 复验闭环（勿回退）
- VerifyIssueTickets 须经 task payload IssueKeys 透传，严禁执行体回扫 verifying 态；L0 快路径命中回落 LLM 复核；重新 verifying 清 note。预算 verifyCallTimeout 120min/verifyTicketTimeout 15min/verifyMaxTurns 2/verifyMaxTokens 10k；rejected 3 次自动转 skipped。
- RAG-Verify（8546f044）：SearchBatch 证据切片（章 topK=2+台账 #id 定点+窗口回退），单工单 4500/单批 12000 字符帽；批量合验 4+批间并发 2（落库单 goroutine）；工具零暴露（Whitelist:[]string{}）+输出≤3 句。语料硬帽=输入与书长无关，勿加码。
- 回执截断抢救（a1753e08）：parseVerifyReceipt 4 返回值（partial）+salvageVerifyEntries 流式逐条抢救；verifyPrompt 回执前置「回答第一段必须是 json 块」。

## 近期修复（勿回退）
- chapter_ids=全局阅读序号 1..N，台账/全书级填[]；filterChapterIDs 上界裁剪；parseChapterRefs 只认「第N章」/「章#N」，裸 #\d+=台账引用。
- dispatchExecutor Enqueue 失败走后台 goroutine+短重试（47125469）；PreSchemaSteps 碰子表须 HasTable 守卫（6a0aca2f）。
- parseAudit 禁把 LLM 原文塞 Summary/Desc（7cb519d8）；idleRetryable 零字节空转=不可重试（c4afcb37）。
- args_coerce.go JSON 文本参数宽容化（8add66ac）；golangci-lint 禁 reflect.Ptr 须 reflect.Pointer。
- 一致性巡检（0da8355c）：单次归并+本地预筛（余弦≥0.78）+内联语料（章2000/台账20000/总120000 帽）+patrolSynthesizeMaxTokens=48k；勿回退分块。dev 运行中改 Go 文件会杀在途任务。
- 观测（d144ec5e/011b44fa）：完成事件失效内联查询；running_since/stall_count；ResetAllStuckFixing 启动清扫。
- 工单状态回退修复（298d779c，#100）：ReplaceIssuesByPrefix 换血按「键+Area→Area 兜底」继承用户流转状态（carryIssueWorkflow）；StartIssueFixTickets 幂等容忍+部分失败不弃派发，issueFixMu/issueFixActive 互斥+排空循环（seen+32 保险丝）+dispatchExecutorUnique 唯一键防合并孤儿化；pre_verify_status 快照（verifying 捕获、重复不覆盖）+verifyRestoreStatus 恢复——未确认/清扫不打回待确认，ResetStuckVerifying 按 run 收窄；前端两视图补订 fix/verify 事件失效内联查询。

## 其他契约
- 章节寻址：chapter_id≠阅读序号；前端用 reading_number；工单 chapter_ids=全局阅读序号。
- ONNX 仅 bge-small-zh-v1.5 向量化；reranker 已 L3 移除。熔断 internal/llm/breaker.go 三态只认硬失败。

## 排障
- 重启 dev：全套 CGO export+`CODEBUDDY_SAFE_DELETE_ENABLED=0` 跑 wails dev（shim 拦 vite emptyDir；tsc -b 重吐 assets 故仅预删无效）。杀旧进程用 PowerShell Get-Process 取真实 PID。5173 白屏=正常。
- watcher 失活判据：无 [Rebuild triggered]+exe mtime 停滞→整机重启 dev。
- git commit 含 Go 必须同条命令带 export；shell 状态不跨命令。commit 输出可能谎报 "nothing to commit" 但实际成功——git log 核实后单独补 push。go build/vet 用 `-tags webkit2_41`（`--build-tags` 是 golangci-lint 旗标，go 不认）。
- render 禁 Date.now()/new Date()；vitest 偶发沙箱失败以四道闸兜底；wails 绑定文件噪声 git checkout 还原。
- 无 sqlite3 CLI：`node -e "require('node:sqlite').DatabaseSync(...,{readOnly:true})"`；tasks 非终态=status NOT IN ('done','dead')。
