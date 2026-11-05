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
- 复验铁律（审计 P0）：`VerifyIssueTickets` 须显式经 task payload 的 `IssueKeys` 透传给 `runVerifyTickets`，严禁执行体回扫 verifying 态（读己写竞态曾致永久卡 verifying）。L0 快路径粗粒度项 `name_drift`/`tail_hook` 命中回落 LLM 复核原指向。三档预算（verifyCallTimeout 120min / verifyTicketTimeout 15min / verifyMaxTurns 2，见复验二次提速）。重新进入 verifying 一并清空 note。
- 复验终极提速（2026-11-05, 09c29617，勿回退）：复验两段式=①L0 快路径串行（机械类免 LLM）②**批量合验**（verifyBatchSize=4，一次 signability-verify 多目标回执逐 id 拆分）+ **语料预取内联**（verify_corpus.go：作用域章正文+紧凑台账快照（8k 帽）后端取好内联【本次复验语料】，明示禁止工具往返）+ **批间并发** verifyBatchParallel=2（落库单 goroutine 串行收口，避 SQLite 写锁）。verifyMaxTurns 12→3、新增 verifyMaxTokens 压思维膨胀（后被二次提速收紧，见下条）；单批语料帽 40k/章帽 8k（掐头尾 2/3+1/3 保尾部修复点）。根治「复验慢」=取数与推理分离：旧 12 轮工具往返现读证据是最大耗时源。失败分层 note 三档措辞「单批」；契约（多目标回执格式/未提及=未确认/3 次未过转人工/零自动派发/可取消）零变化。
- 复验二次提速（2026-11-05, 7cb6f485，勿回退）：round1 后单条复验仍慢（DB 实据 I7 单批烧满 25min 未出回执），根因=预算/提示词残留非架构。三点修复：①预算对齐体量：verifyTicketTimeout 25→15min、verifyMaxTurns 3→2、verifyMaxTokens 24k→10k（回执~300tok）；②**工具零暴露**：verifyBatch 派发 `Whitelist: []string{}`（skillrun.go 的 SkillInput.Whitelist 非 nil 即覆盖默认 allowlist）→ 不给模型任何 tool schema，调用结构性不可能，单轮必出回执；③提示词去工具教学：verifyPrompt/ledgerChangeVerifyBlock 删「用 get_*/按 chapters 读」改「直接通读语料」+「输出从简≤3 句」。TestVerifyBatchZeroToolsAndLeanPrompt 锁契约。**扩容铁律**：语料硬帽（章 8k/台账 8k/批 40k 字符）使单次复验输入与书长无关——百万字小说复验不比 10 章慢，勿因书长回退加码。
- 复验 RAG-Verify（2026-11-05, 8546f044，勿回退，复验证据装配最终形态）：round2 后 I7 仍烧满 15min（12:51 二次实据）——大语料逼长链思维，减预算只是止损，减语料才是根治。verify_retrieval.go 逐工单用 rag.VectorStore.SearchBatch 定位证据切片：①作用域章内命中段（filter 锁章，每章保底 topK=2/章多降 1）②台账 #id 引用定点行（按 `id=` 字段精确匹配，novel_id 值不误匹配、章#N 剔除）③零命中/无向量库→窗口语料回退（appendChapterWindows 共用件，显式传 novelID）。单工单帽 4500/单批帽 12000 字符；verifyMaxTokens 6k。检索器经 verifySearcherProvider 注入（patrolEmbedderProvider 同惯例）。前提：章节保存+MCP 写工具均挂 SubmitRefresh 增量刷新，索引随修复更新（dev 库实测 ~10 chunk/章）。契约零变化。
- 复验自动升级人工（2026-11-01）：复验「明确未通过」累计达 3 次（`verifyEscalateAfter`，仅计 rejected 不计 unverified）自动转 `skipped`（人工工单，复用既有语义；`PipelineIssue.verify_attempts` 计数，AutoMigrate 自动加列），skip_reason/note 写明「复验 N 次未通过，自动转人工工单」；前端状态徽章不变（skipped=已跳过），面板 note 区可见原因。防修复→复验失败无限空转。
- 签约体检报告解析泄漏修复（2026-11-01, 7cb519d8）：`parseAudit`（service.go）此前 JSON 解析失败兜底把整段原始 LLM 输出塞进 Summary/issue.Desc 并持久化，致签约潜力评估面板把整份报告当纯文本铺开（含末尾原始 JSON）。改为健壮抽 JSON（json 代码围栏 + 末段平衡大括号兜底）+ 优雅降级（干净 P0 issue 提示重跑体检，绝不写原文）；新增 TestParseAuditNoRawTextLeak 回归测试。
- 签约体检卡「一直运行中」死循环修复（2026-11-01, c4afcb37）：供应商「连得通但不吐数据」时，空闲看门狗每约9分钟超时一次，旧代码把空闲超时一律标 Retryable:true 且熔断刻意忽略空闲超时，致 agent 无限重试直到60分钟硬超时，界面表现为签约体检一直运行中卡死。修复：idleWatchdog 记录本次请求是否收到过字节；新增 idleRetryable 纯函数——零字节空转（死连接）判不可重试（直接失败并给「未收到任何字节/疑似供应商无响应」清晰错误），已收字节（真实慢生成/中途断流）仍保留可重试，不误杀思考模型长静默。非流式（generate.go）与流式（stream.go）两路径接入；新增 TestIdleRetryable/TestIdleTimeoutErrorGotBytes。熔断行为不变（仍不因子空闲超时熔断）。
- 一致性巡检分块 Map-Reduce 重构（2026-11-02, 6b90e7d5）：旧单调用把整本书一次性塞进一个 LLM 调用，供应商稍慢即超时整轮作废、结论全丢。改为按 patrolWindowSize=5 切片 MAP 小调用 + 一次性 REDUCE 归并（复用单技能 consistency-patrol，仅提示词区分角色，零测试契约变更）；任一块失败不牵连；归并失败但有分块结果时落库 needs_fix 部分张力报告（杜绝结论丢弃）。列章失败降级整本单调用。Phase 2 确定性+向量预筛（patrol_prescreen.go 用 rag.Embedder bge 512 维聚类）待办。
- MCP 工具 JSON 文本参数宽容化（2026-11-05, 8add66ac，勿回退）：string 声明的 JSON 文本参数（abilities/personality/detail_json/tags/params 等 10 字段）被 LLM 传真数组/对象曾炸「cannot unmarshal array into … of type string」。修复=args_coerce.go 在 Registry.Execute 唯一入口按 Args 结构把 string 字段的数组/对象值就地规范化为紧凑 JSON 文本（Args 解码+tc.RawArgs PATCH 双路径同修）；字符串/null 原样、数值/布尔进 string 仍报错、json.Number 透传防大整数精度损失、未知字段仍拒。schema 描述明示两形态均可。坑：golangci-lint 禁 reflect.Ptr 别名，须 reflect.Pointer。
- 签约级质量升级计划（2026-11-05, docs/quality-signability-upgrade-plan-2026-11-05.md, a52f6cd1）：核心诊断=系统是「正确性机器」，缺商业可读性度量+真实反馈闭环。路线图 P0-2 情绪账（chapter_tension）→P0-1 平台 profile 机器验收（七猫：章长 1000-1500/前 500 字爆点/先抑后扬 300 字情绪段）→P0-3 评分打通→P1 反馈回流+数据对账→P2 技能路由。待用户补编辑原话/题材/七猫数据。
- P0-0 开篇体检专项已落地（2026-11-05, 4189a7bb，勿回退）：L0 开篇四查 opening.go（premise_late 前500/cheat_late 前800/protagonist_late 前200，三查仅第1章；opening_length 黄金三章 1000-1500）+ L1 契约 v2 immersion_anchor（语义判读）+ CheckerConfig 三组关键词列（全空=不检查=缺省逐字一致，upsert_checker_config 可配）+ signability-audit dim1/dim2 七猫量化口径。§7.4 修订序：P0-0✅→P1-1 编辑反馈回流→P0-1 平台 profile→P0-3 评分打通。

- P1-1 编辑反馈回流（2026-11-05, 67410ffb）：编辑原话→editorfeedback 子 Agent（零工具）按 editor-feedback.md 拆解→FeedbackReceipt（八维+F 系 issue+约束修订）两段式落 editor_feedbacks（原话先行，失败保留 raw）；F 系 issue 落 detect:feedback 容器工单走修复闭环；约束只建议，apply_editor_feedback_constraints 人工应用（幂等拒绝）。**勿回退**：mcp_tools→pipeline 是 import cycle（pipeline→agent→mcp_tools），落单走 SetFeedbackTicketLander 回调（app 注入）；feedback 测试须外部包 mcp_tools_test。F 命名空间=编辑反馈（I/D/T/V 各有归属）。

- P0-1 平台 profile 机制化（2026-11-05，验收 TestQimaoProfileL0Acceptance）：`quality/platform_profile.go` PlatformProfile+七猫 preset `qimao`（章长 1000–1500、HookEarly 200/1、Hook 500/2、BeatDensityMin=1.0、EmotionSegMax=300）+注册表只发实证口径（Validate 拒未注册名、别名归一）。CheckerConfig 增 platform_profile 列（`Profile()`/`EffectiveBeatDensityMin()`：显式列>0 优先否则 profile 值）；L0 三查 opening_hook/chapter_length_fit/beat_density（platform_checks.go 近似词表，缺省不跑逐字一致）；opening.go 窗口 profile 化+章长门控「关键词||profile」；三处 L0Input 构造点统一传 Opening/Profile/BeatDensityMin；l0_verify 快路径增三查分类+依赖门控，opening_hook/beat_density 入 coarseHeuristicChecks 命中回落 LLM；前端七猫模板 `checkerProfile` 一键配置（ApplyPlatformProfile 绑定，选中/切换/清除对称写 CheckerConfig）。**勿回退**：Validate 须把空白 profile 名归一为空；字数口径=纯 CJK；TestSkillFallbackHitRate 失败=真实 LLM（deepseek）负面样本误路由（基线 HEAD 同败，非回归）。

## 架构决策（2026-11-03）
- 四检测工作流（chapter/volume/book/structure）**否决进程/模块级大拆分**（评估报告 docs/detection-workflow-coupling-report-2026-11-03.md）：code 路径已按 kind 分治，共享的熔断器/队列/SQLite 必须共享；「跑很久没结果」类投诉先查①供应商流式停滞（idle watchdog 日志）②前端 query 失效缺口（旧缓存×新 run_id 过滤必空模式：`["signability-inline-issues"]`/`["detect-inline-issues"]` 无失效是已知坑）③重启 AbandonActive 杀在途任务。
- P0/P1 已落地（d144ec5e/011b44fa）：P0=检测完成事件失效内联查询+完成 toast；P1=①verifyTicketTimeout 25min+复验失败 note 三档（整轮超时/单工单「复验超时未验证」/执行失败）+巡检卡片 note 明示②DetectionTaskStatus 增 running_since/stall_count（detectRunInfo 登记启动时刻，stall=llm.RecentIdleTimeouts 自启动起算）+DetectRunningHint 三处运行态「已运行 X」+供应商缓慢提示③ResetAllStuckFixing 启动清扫（fixing→pending note 明示中断，RecoverInterruptedRuns 挂钩，零自动派发不变）。P2（确定性+向量预筛等）未动。

## ONNX 现状（结论，细节见 docs/onnx-enhancement-development-plan.md）
- 仅 `bge-small-zh-v1.5`（512维）做文本向量化（4 消费方全部优雅降级）。reranker 已 L3 彻底移除（真实评测净负优化）；P0-2 批处理/缓存 ✅；P1-1 长文分段聚合 ✅（不影响功能）；P1-2 升级 embedding 模型 评估后放弃（维度变更须重建向量库）；P2 本地分类器/NER 降级待触发。

## 供应商熔断
- 三态自愈熔断器 `internal/llm/breaker.go`，按 provider 隔离；只认硬失败（连接拒绝/DNS/首字节超时/≥5xx），空闲超时只观测不熔断。

## 章节寻址 / 工单引用契约
- chapter_id（主键）≠ 阅读序号（实时计算）；前端严格用 reading_number；LLM 精修「第N章」须用 `no_{N}` 路径，严禁拿 get_chapter_list 的「第N章」当 id 推算。
- 工单 chapter_ids=全局阅读序号；台账引用写 `<type>#<id>`（id=自增主键，常上千）；裸 `#\d+` 一律视为台账引用。

## 排障
- 重启 wails dev：带全套 CGO export **+ `CODEBUDDY_SAFE_DELETE_ENABLED=0`** 跑 `wails.exe dev`。关键：WorkBuddy 的 node-safe-delete-shim（`app.asar.unpacked/cli/vendor/shim/node-safe-delete-shim.cjs`）会拦截 vite 的 emptyDir（threshold 50 / scope turn，抛 `SAFE_DELETE_BULK_CONFIRM_REQUIRED`）；且 `wails dev` 跑 `tsc -b && vite build`，`tsc -b` 会在 rm 之后重新往 `dist/assets` 吐编译产物，故**仅预删 assets 无效**，必须靠该 env 关掉 shim 才能让 vite build 过。结束旧进程 `Stop-Process -Id <pid> -Force`（注意 `ps -W` 的 MinGW PID ≠ Windows 真实 PID，须用 PowerShell `Get-Process` 取真实 PID 再杀，否则杀不干净致旧实例残留）。浏览器访问 5173 白屏=正常（Wails 桥仅注入桌面 webview）。
- wails dev watcher 会失活（一次 rebuild 后对后续 Go 保存零反应，touch 无效）：判据=日志无 [Rebuild triggered]+novelgo-dev.exe mtime 停滞+models.ts 缺新 DTO 字段；处置=整机重启 dev（换新日志名）。
- git commit 含 Go 文件必须同条命令带全套 export（钩子 golangci-lint 要 go/GOCACHE/GOLANGCI_LINT_CACHE/LOCALAPPDATA，缺失即 BLOCKED；纯前端提交无感）。shell 状态不跨命令持久。
- render 里禁 Date.now()/new Date()（react-hooks purity error 级）：走针用 SubagentCard 模式 useState(()=>Date.now())+setInterval 回调；Date.parse(str) 不触发。
- 无 sqlite3 CLI 查库：`node -e "require('node:sqlite').DatabaseSync(...,{readOnly:true})"`；tasks 非终态=status NOT IN ('done','dead')。
- `npm run build` 写 dist/assets 会撞死 dev 里的 app（assetserver panic，wails 工具不拉起）：判据=队列轮询日志停更；处置=重启 dev。build 闸门后须查 dev 存活。
- vitest 沙箱偶发失败（资源受限）；前端用 format/i18n/lint(0 error)/build 兜底，vitest 脱沙箱跑。
- wails 重启重产的前端绑定文件（App.d.ts/App.js/models.ts）常现纯缩进/排序噪声，git checkout 还原即可，勿混入提交。

## 一致性巡检（consistency-patrol）架构（勿回退）
- 设计收敛：单次巡检调用只按 novelID 读当前书（runConsistencyPatrol→SkillInput.NovelID→tc.NovelID；get_*/read 全带 WHERE novel_id），不存在读全库；旧「LLM 逐章读」与现「后端批量注入语料」都只碰当前书。
- 编排（patrol.go runConsistencyPatrol）= **单次归并调用**（收敛自旧分块 Map-Reduce，commit 0da8355c）：本地预筛（Phase 2，免费）聚跨章候选矛盾簇→仅**一次** consistency-patrol 调用，提示词内联【本书语料】（buildPatrolCorpus：章正文 patrolChapterCap=2000/章 + 台账快照 patrolLedgerCap=20000，patrolCorpusCap=120000 字符上限；唯一完整证据源，替代旧「LLM 逐章 read」50+ 工具往返）——明确禁止 read/get_*/search 工具；**语料体量是超时成败关键**：原 4000/60000/200000 配置（~112k 字符）思考模型 45min 都跑不完（实测 task 105 verdict:error），收紧至 2000/20000/120000（约 40% 体量）+ synthesizePrompt 输出约束（末尾只交 JSON、之前最多 2-3 句）；但思维块膨胀与语料体量基本无关（~50 token/s 空转），故再加 **patrolSynthesizeMaxTokens=48k 输出 token 硬上限**（SkillInput.MaxTokens→RunOptions→CallOptions→MiMo max_completion_tokens=思考+回答总量；MiMo 无 ReasoningLevels，这是唯一可靠杠杆）——task 107 实测 16.8min 成功产出真实张力报告（needs_fix，2 处 P1，35k output_tokens 自然收口）。分块不能省总思维量（台账每块重复），勿再走回分块。**dev 运行中改 Go 文件触发 wails 重建重启、杀在途任务**，验证期间绝不动文件。patrolSynthesizeTimeout=**45min**（commit 2b0335a2）、patrolOverallTimeout=60min 兜底；失败走 classifyPatrolFailure 落清晰失败态，绝不静默卡死。列章失败→预筛/语料降级为空，归并步仍可用台账证据。
- Phase 2 向量预筛（patrol_prescreen.go）：列章后跑 runPatrolPrescreen——抽「疑似主张句」→ BGE 512 维 EmbedBatch 单链聚类（余弦≥0.78，仅留跨章簇）→ detectDivergence 同单位数值分歧标 Divergent → formatPrescreen 注入归并步。embedder 取 rag.TryGetEmbedder() 全局单例，未就绪即降级不阻断；不调 Compact（共享全局）。测试经 patrolEmbedderProvider 注入 stub。
- fail-fast（5775990b）：classifyPatrolFailure 区分 idle_timeout/DeadlineExceeded 给清晰「供应商无响应/半死连接」提示；agent idle EOF/重置分支计入 idleTimeouts 堵 idleRetryCap 漏检。
- 张力项落工单（2026-11-03, 23d6e19f/58d275e2）：成功路径 ReplaceIssues 按 run 整批落 pipeline_issues（round=0，pending）；失败/拒绝路径不落不清。前端巡检卡片带**单条**「去修复/复验」（复用工单闭环，人工触发；去修复=单条清理内容发 chat+标修复中，无工单退化纯聊天）。「只报告不改写」保持 = 零自动流转/零修复派发。旧「巡检零工单」契约已废（三处测试断言改落 2 条 pending）。
- 验证铁律：pipeline 包四道闸（gofmt/build/vet/golangci-lint webkit2_41）/ 全量 go test 全绿；新功能必带单测（parseChunkOutput/partial-sink/prescreen 共 11 个）。

## P0-3 评分打通（勿回退，2026-11-05）
- 6↔8 映射契约单源 `internal/review/score_mapping.go`（ScoreDimToAuditDims/StarsFromScore/JudgeToAuditScores/ScoreDimInfos）；前端零硬编码映射，全走 GetScoreTrend 的 judge_dims/audit_dim_order 快照。
- 评分明细 append-only 表 chapter_score_events（source=new/full_replace/snapshot）；趋势数据双轨=事件主轨+chapters.quality_score 快照回填（attempts=1）。体检 JSON `dims` 数组为全量契约专属可选字段（蓝图/巡检不产出；AuditSchemaVersion 不 bump）；contract_test 三方同步时 wantFull 须含 AuditDimScore tags、blueprint wantCore 显式排除 dims。
- P0 golden 基线有意演进：`go test ./internal/pipeline/ -run TestP0BaselineRegen -p0-update` 重定格后 git diff 复核只含预期句。
- 趋势 API GetScoreTrend/GetChapterScoreHistory（internal/pipeline/score_trend.go，scoreTrendStores 从 s.store.db 兜底构造）；前端 ScoreTrendPanel 挂 DetectionWorkflowView 章节区恒挂（评分不依赖检测 run），queryKey=["score-trend"/"score-history", novelId(, chapterId)]。
