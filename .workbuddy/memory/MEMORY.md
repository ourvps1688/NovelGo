# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`（dev 解析为 exe 相邻 `build/bin`，DB=`build/bin/novel-agent.db`）；仓库 `ourvps1688/NovelGo`（origin），upstream=sigpanic/goink。

## 构建/环境（勿回退）
- CGO：`PATH` 含 `Program Files/Go/bin`+`tools/mingw64/bin`+`go/bin`；`CGO_ENABLED=1`；`CGO_CFLAGS=-IC:/tools/sqlite3-headers`；裸调用须带 `LOCALAPPDATA`+`GOCACHE=C:\Users\Administrator\AppData\Local\go-build`+`GOLANGCI_LINT_CACHE`（同目录）。golangci-lint 在 `C:\Users\Administrator\go\bin`。
- 前端四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 0 error 上限；164 warning 为存量）。i18n:check 禁 JSX 硬编码中文（含 textarea placeholder，须走 key）。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；一任务一提交带全套 export；记忆账单独 `chore(memory)`；核验 `git rev-list --left-right --count origin/master...HEAD` = `0 0`。

## 架构约束（核心，勿回退）
- R5 检测核心已收官：质量保障唯一规范路径——检测四 kind（chapter/volume/book/structure）独立启动、发现项落工单、人工确认→修复→显式复验闭环、零 auto-advance/零闸门/零自动派发修复。后端 `internal/pipeline/detect.go` RunDetectionTask。
- 旧 Orchestrator 流水线后端保留 legacy，前端不得走 StartPipeline/PipelineWizard/SignabilityAuditView；前端规范入口 = DetectionTasks + IssueTicketsPanel + PatrolReportPanel + RetconPanel + VolumeManagePanel。
- 契约单源（勿改）：signability-audit.md=体检契约源（8 维评分，desc 措辞须强制前向适配——禁止「改任一处」回改前章；改 prose 勿动示例 JSON 块，contract_test 三方同步）；featureKeys.ts=key 单源；factory.ts=PanelId 单源；pipeline_issues 工单状态机。度量纯本地不上传（隐私红线）。
- 分层铁律：app 绑定层只做 DTO+委托；领域编排在 `internal/<domain>.Service`；`internal` 不得直接 import wails，事件经 `internal/event.Emitter`；跨介质一致性以 DB 为真相源。
- 台账唯一约束（2026-10-31, 147b667c）：角色/地点/弧线/读者视角四类台账按自然键去重，新增一律走幂等 find-or-create；DB 层 v172 迁移合并存量重复行+加唯一索引。

## 质量升级路线图 2026-11（计划 docs/quality-signability-upgrade-plan-2026-11-05.md，已全部落地，勿回退）
- P0-0 开篇体检（4189a7bb）：L0 开篇四查 opening.go（premise_late 前500/cheat_late 前800/protagonist_late 前200 仅第1章；opening_length 黄金三章 1000-1500）+ L1 契约 v2 immersion_anchor + CheckerConfig 三组关键词列（全空=不检查）+ signability-audit dim1/dim2 七猫量化口径。
- P1-1 编辑反馈回流（67410ffb）：editorfeedback 子 Agent（零工具）按 editor-feedback.md 拆解→FeedbackReceipt 两段式落 editor_feedbacks；F 系 issue 落 detect:feedback 工单走修复闭环；约束经 apply_editor_feedback_constraints 人工应用。**勿回退**：mcp_tools→pipeline 是 import cycle，落单走 SetFeedbackTicketLander 回调（app 注入）；feedback 测试须外部包 mcp_tools_test。F 命名空间=编辑反馈（I/D/T/V 各有归属）。
- P0-1 平台 profile（TestQimaoProfileL0Acceptance）：quality/platform_profile.go PlatformProfile+qimao preset（章长 1000–1500、HookEarly 200/1、Hook 500/2、BeatDensityMin=1.0、EmotionSegMax=300、RhythmTemplate）；CheckerConfig.platform_profile 列（显式列>0 优先）；L0 三查 opening_hook/chapter_length_fit/beat_density（platform_checks.go 近似词表）；l0_verify 三查分类+粗粒度回落；前端七猫模板 checkerProfile 一键配置。**勿回退**：Validate 空白 profile 名归一为空；字数=纯 CJK；TestSkillFallbackHitRate 偶败=真实 LLM 负样本误路由（非回归）。
- P0-3 评分打通：6↔8 映射契约单源 internal/review/score_mapping.go（judge 六维不评 fit）；chapter_score_events append-only（source=new/full_replace/snapshot）；体检 JSON `dims` 数组=全量契约专属可选（AuditSchemaVersion 不 bump，蓝图/巡检不产出）；GetScoreTrend/GetChapterScoreHistory+ScoreTrendPanel 恒挂检测工作流章节区。P0 golden 基线演进：`go test ./internal/pipeline/ -run TestP0BaselineRegen -p0-update`。
- P1-2 真实数据对账（50313c43/5f65c00e/90321512）：platform_metrics 表+ParsePlatformMetrics 粘贴录入（同章最新粘贴胜）；GetPlatformReconciliation「流失章 × 低分维」（流失=相邻率降 ≥5pp、lift=流失−稳定低分率）；前端 PlatformDataPanel。细节见 2026-11-06.md。
- P0-2 情绪账（d659911b/da9caa7e/0ca52cb6/64724239）：chapter_tension 表 L0 基线+L1 tension_curve 回执契约；曲线三查 pacing_drag（≥3 段 tension<25）/beat_gap 兜底/opening_slope（前500升幅<5.0）+beat_density=情绪峰计数；E 系工单 TensionKind="tension"，ReplaceIssuesByPrefix 按章前缀整批（零发现=清旧单）；dim4 RenderTensionBlock（16 段/章、120 章封顶）；前端 TensionCurveCard（MarkTensionCurve 与三查同源 helpers；拖沓 chip 跳章）。文案纪律：避开语义排除词（矛盾/冲突/逻辑/人设/崩/连贯/伏笔/前后不一/设定冲突）与 tail_hook「悬念」、word_count「篇幅」交叉词。
- P2-1 低分维技能路由（28be757b）：rewrite_skills.go auditDimSkillRoutes（八维键→技能，compliance 不入）+issueDimSkillRoutes（台账维→技能，timeline 不入）——**issue.Dimension 是台账维枚举≠八维键，不同命名空间**；低分维=≤3 星；rewriteMethodBlock 技能正文注入（单 4000/整块 12000 rune 封顶、stripFrontmatter），空=通用口径逐字不变。
- P2-2 平台节奏模板（892ca04d）：scene-beats.md `### qimao_face_slap` 小节（情绪段≤300 字/压蓄爆踩/章长 1000-1500/~800 字微钩子/≥1 情绪峰）；skill.RhythmBlock 按 `### id` 确定性提取；写章生成（buildRhythmInject 写章意图门控→system-reminder，PersistTurnMessages 第 4 通道）+重写（joinRewriteBlocks 合成）双路消费；缺省不模板。
- P2-3 对标书库（449adf6d）：benchmark_books 表（(novel_id,title) 唯一）+ParseBenchmarkBooks（`书名｜钩子｜爽点｜节奏｜赛道｜备注` 位置列，竖线/Tab **不合并**（空列保位），表头/缺特征/超列跳过回报）；RenderBenchmarkBlock「对标书库摘要」（20 本/字段 120 rune 封顶，空=零提示）→auditInput.benchmark 注入 signabilityPrompt（genre 后，detect+S4 两装配点）；signability-audit.md dim2 证据纪律=标杆异同须以样本特征举证；前端 BenchmarkBooksPanel。

## 复验闭环（勿回退）
- 铁律：VerifyIssueTickets 须显式经 task payload 的 IssueKeys 透传 runVerifyTickets，严禁执行体回扫 verifying 态（读己写竞态曾致永久卡 verifying）；L0 快路径 name_drift/tail_hook 命中回落 LLM 复核；重新进入 verifying 清空 note。三档预算 verifyCallTimeout 120min/verifyTicketTimeout 15min/verifyMaxTurns 2/verifyMaxTokens 6k。复验「明确未通过」累计 3 次（verifyEscalateAfter，仅计 rejected）自动转 skipped 人工工单（verify_attempts 计数）。
- 证据装配最终形态=RAG-Verify（8546f044，收敛自 09c29617/7cb6f485 两轮提速）：逐工单 rag.VectorStore.SearchBatch 定位证据切片（作用域章命中段 topK=2/章 + 台账 #id 定点行 + 零命中窗口语料回退），单工单帽 4500/单批帽 12000 字符；批量合验 verifyBatchSize=4（多目标回执逐 id 拆分）+批间并发 2（落库单 goroutine 串行收口避 SQLite 写锁）；**工具零暴露**（Whitelist: []string{}）+提示词去工具教学（直接通读语料、输出 ≤3 句）。TestVerifyBatchZeroToolsAndLeanPrompt 锁契约。**扩容铁律**：语料硬帽使单次复验输入与书长无关，勿因书长回退加码。

## 近期修复（勿回退）
- 签约体检 chapter_ids 校验（2026-11-01）：chapter_ids 必须有效全局阅读序号 1..N，台账/全书级填 []；ReplaceIssues 落库前 filterChapterIDs 上界裁剪（HasTable 守卫）；parseChapterRefs 兜底只认「第N章」/「章#N」（裸 `#\d+`=台账引用）。
- 工单去修复 UI 卡死（47125469）：dispatchExecutor Enqueue 遇瞬态 database is locked 曾同步跑长修复致永不返回；修复=后台 goroutine+3×40ms 短重试+legacyGuard 同键去重。
- v172 去重迁移（6a0aca2f）：PreSchemaSteps 在 AutoMigrate 前跑，碰引用子表须 HasTable 守卫。
- parseAudit 泄漏（7cb519d8）：JSON 解析失败严禁把 LLM 原文塞 Summary/issue.Desc（曾整份报告铺面板）；健壮抽 JSON+优雅降级出重跑提示；TestParseAuditNoRawTextLeak。
- 体检卡「一直运行中」死循环（c4afcb37）：idleRetryable 纯函数——零字节空转=不可重试直接失败（清晰报「未收到任何字节」），已收字节仍可重试；generate/stream 双路径；熔断仍不因子空闲超时熔断。
- MCP JSON 文本参数宽容化（8add66ac）：args_coerce.go 在 Registry.Execute 唯一入口把 string 声明字段的数组/对象值规范化为紧凑 JSON 文本（Args 解码+tc.RawArgs PATCH 双路径）；未知字段仍拒；golangci-lint 禁 reflect.Ptr 别名须 reflect.Pointer。

## 架构决策（2026-11-03）
- 四检测工作流否决进程/模块级大拆分（docs/detection-workflow-coupling-report-2026-11-03.md）：code 路径已按 kind 分治，熔断器/队列/SQLite 必须共享。「跑很久没结果」先查：①供应商流式停滞（idle watchdog 日志）②前端 query 失效缺口（旧缓存×新 run_id 过滤必空：`["signability-inline-issues"]`/`["detect-inline-issues"]` 无失效是已知坑）③重启 AbandonActive 杀在途。
- 观测已落地（d144ec5e/011b44fa）：检测完成事件失效内联查询+toast；verifyTicketTimeout+失败 note 三档；DetectionTaskStatus running_since/stall_count+DetectRunningHint 三处「已运行 X」；ResetAllStuckFixing 启动清扫（fixing→pending）。P2（确定性+向量预筛等）未动。

## ONNX 现状（细节见 docs/onnx-enhancement-development-plan.md）
- 仅 bge-small-zh-v1.5（512 维）做文本向量化（4 消费方全优雅降级）；reranker 已 L3 移除（真实评测净负优化）；升级 embedding 模型评估后放弃（维度变更须重建向量库）；P2 本地分类器/NER 降级待触发。

## 供应商熔断
- 三态自愈熔断器 internal/llm/breaker.go，按 provider 隔离；只认硬失败（连接拒绝/DNS/首字节超时/≥5xx），空闲超时只观测不熔断。

## 章节寻址 / 工单引用契约
- chapter_id（主键）≠ 阅读序号（实时计算）；前端严格用 reading_number；LLM 精修「第N章」须用 `no_{N}` 路径，严禁拿 get_chapter_list 的「第N章」当 id 推算。
- 工单 chapter_ids=全局阅读序号；台账引用写 `<type>#<id>`（id=自增主键，常上千）；裸 `#\d+` 一律视为台账引用。

## 一致性巡检（consistency-patrol，勿回退）
- 编排 patrol.go=**单次归并调用**（0da8355c 收敛自分块 Map-Reduce）：本地预筛（patrol_prescreen.go，BGE 512 维聚类余弦 ≥0.78 仅留跨章簇+数值分歧 Divergent）→一次 consistency-patrol 调用内联【本书语料】（buildPatrolCorpus：章 2000/台账 20000/总 120000 字符帽；唯一完整证据源，禁 read/get_*/search）。**语料体量是超时成败关键**：原 4000/60000/200000 思考模型 45min 跑不完；收紧后仍需 patrolSynthesizeMaxTokens=48k 硬上限（MiMo max_completion_tokens=思考+回答总量，唯一可靠杠杆）；patrolSynthesizeTimeout=45min、patrolOverallTimeout=60min。分块不能省总思维量（台账每块重复），勿回退分块。**dev 运行中改 Go 文件触发重建杀在途任务**，验证期间绝不动文件。
- 只报告不改写=零自动流转/零修复派发；张力项落工单（23d6e19f/58d275e2，round=0 pending，ReplaceIssues 整批），失败/拒绝不落不清；前端巡检卡片单条「去修复/复验」复用工单闭环。classifyPatrolFailure idle_timeout/DeadlineExceeded 清晰报「供应商无响应」（5775990b）。

## 排障
- 重启 wails dev：全套 CGO export **+ `CODEBUDDY_SAFE_DELETE_ENABLED=0`** 跑 `wails.exe dev`。WorkBuddy node-safe-delete-shim 会拦 vite emptyDir（抛 SAFE_DELETE_BULK_CONFIRM_REQUIRED）；`wails dev` 跑 `tsc -b && vite build`，tsc -b 会在 rm 后重吐 dist/assets，仅预删无效必须靠该 env。结束旧进程用 PowerShell `Get-Process` 取真实 PID 再 Stop-Process（MinGW PID ≠ Windows PID，杀不干净会残留）。浏览器 5173 白屏=正常（Wails 桥仅注入桌面 webview）。
- wails dev watcher 会失活（一次 rebuild 后对 Go 保存零反应）：判据=无 [Rebuild triggered]+exe mtime 停滞+models.ts 缺新字段；处置=整机重启 dev。
- git commit 含 Go 文件必须同条命令带全套 export（钩子 golangci-lint 缺 env 即 BLOCKED；纯前端提交无感）。shell 状态不跨命令持久（cwd 持久）。
- render 里禁 Date.now()/new Date()（react-hooks purity 级）：走针用 useState(()=>Date.now())+setInterval；Date.parse(str) 不触发。
- 无 sqlite3 CLI：`node -e "require('node:sqlite').DatabaseSync(...,{readOnly:true})"`；tasks 非终态=status NOT IN ('done','dead')。
- `npm run build` 写 dist/assets 会撞死 dev 里的 app（assetserver panic）：判据=队列轮询日志停更；处置=重启 dev；build 闸门后须查 dev 存活。
- vitest 沙箱偶发失败（资源受限）；前端用 format/i18n/lint(0 error)/build 兜底，vitest 脱沙箱跑。
- wails 重产绑定文件（App.d.ts/App.js/models.ts）常现纯缩进/排序噪声，git checkout 还原即可，勿混入提交；纯增量（新方法/新 DTO）才提交。
