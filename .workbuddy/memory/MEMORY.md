# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）合规派生 fork。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..." 声明。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`。仓库 `ourvps1688/NovelGo`（origin）；upstream=sigpanic/goink。

## 构建/环境（勿回退）
- 前端只做 index.css 皮肤 token；四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）；改前端须 build 后重启 wails dev。
- CGO：`export PATH="/c/Program Files/Go/bin:/c/tools/mingw64/bin:$PATH"; export CGO_ENABLED=1; export CGO_CFLAGS="-IC:/tools/sqlite3-headers"`（gcc 在 /c/tools/mingw64/bin，**不是** /c/msys64；lint 与 pre-commit 钩子也须带。bash 无 LocalAppData 时另须 `GOCACHE`+`GOLANGCI_LINT_CACHE`，否则 go/lint「build cache is required」启动即败）。golangci-lint 在 `/c/Users/Administrator/go/bin`。gofmt 须 LF 归一化后查（CRLF 误报）。`cmd | tail` 掩退出码用 `${PIPESTATUS[0]}`。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；PAT 在仓库根 github_pat.txt。钩子=scripts/hooks/pre-commit。commit&&push 链式偶显 nothing to commit 但已落盘→核 log 单独 push。
- 测试瞬态：真实 LLM E2E 偶发 FAIL，复跑绿即收口；无 key Skip（NOVELGO_EVAL_KEY）。npm 一律 `npm --prefix frontend run X`（Bash cwd 不跨调用）。

## 流水线
- S0–S6/G1/G1b/G3b/G4/G终，Orchestrator.Advance 唯一裁决。body issue→RewriteBacklog 回 S3；S5 分批续修复评，连败 3→AwaitingGate，idleTrip=10 熔断。自动重写 Rounds≤2 超限 rewrite_handoff。控制指令同步广播 pipeline:stage。

## 卷快照（internal/snapshot）
- 冻结=全量 series bible（10 模型+正文）；**restore 必须 raw SQL Exec**（gorm callback 对 SkipHooks 无效）。审计 error 阻断冻结；G_freeze=S4+S5 收口且 AuditReason=="volume"。注入【已冻结卷既定事实】（bible.go 单源）。
- RestoreStyle：只还原 voice_dimensions，事实层零触碰；冻结快照双层钉住（事实+风格同记录）。

## retcon
- 状态机 candidate→approved→editing→recorded→rolled_back（+rejected），迁移只经显式 API。裁决必附理由；BeginEdit 固化回滚点；Record 附 diff；Rollback 先 Restore 后落状态；终态前 Commit canon。目标卷须已冻结。**testhelpers 与 handler.go 同形装配全部钩子**。

## 巡检（3.3）
- consistency-patrol.md 只读张力报告；零污染机械保证（patrol_test 断言）；冻结后自动一次+control("patrol")；不自动立 retcon 案。

## P0 上下文装配器
- internal/contextasm：Facts→Voice→Intent 尾接；purpose 矩阵 rewrite 全注/audit·fix·patrol=Facts+Voice/chat=Voice。四纪律：有则注入/无则降级逐字一致[最高验收]/8KB 截断/宁缺勿错。PersonaMarker 禁改。golden=testdata/p0_baseline_*.txt（演进重定格）。

## P1 风格结构化（全销）
- internal/voice：voice_dimensions（(novel_id,dimension) Upsert，书级十维，不入 temporal）；枚举/metrics/source 单源禁改。双写=skill md 不动+voice_profile 并行落库。台账空→与 P0 逐字一致。voice 已入 snapshot registry。

## P2 文风进闸门（全销 #169–#179，WBS=p2-style-gate-implementation-wbs.md）
- 漂移=确定性比对（style.ComputeStats vs voice.MetricKey 八键）；无基线→零 issue。V 系：前缀 V、P1、fix_type=body、不进评分；合并 maxV+1。只报告不改写=零写入+isStyleExempt 豁免+全豁免走 RewriteBacklog。**立案只在 handoffRetconToUser 出口**。
- retcon kind 路由：Kind（fact 缺省/style）→Rollback style→RestoreStyle/其余全量；V 系立案带 style。**每加迁移同步 migrate 集成测试 done 步数断言**（当前 12）+历史输入断言。

## P3 IA 整合（全销 #180–#190，WBS=p3-ia-integration-implementation-wbs.md）
- factory map 单源 frontend/src/lib/factory.ts（四工厂，全键覆盖断言）；旅程 PanelId×3+默认 journey-create；16 条目留次级导航+三旅程面板直达条（quality 含 reader）。勿回退：PanelId 零删除、双速勿合并、P0–P2 工件零触碰、theme token、desktop-first。规划=ultimate-form-integration-plan.md。
- 叙案 i18n 单源 narrative.tagline 五落点；空态漏斗=GetVoiceLedger 全 false→重写入口 hint（失败/坏数据/同步抛错一律静默 false）+重写卡与吸收提取卡同语承接；WritingConfigTab 迁 journey-quality 设置区（组件零改动）、SettingsDialog 三 Tab。
- 双验收 p3-acceptance.test.tsx（喂给谁逐入口+旅程链路+PanelId 零删除回归）；WBS §T5.1 结论已记。门禁全绿（四道闸+theme:check+go build/test）；P3 全程 internal/·*.go 零触碰。vitest 必须 `cd /d/workbuddyfiles/NovelGO/frontend &&` 内联（cwd 不跨调用）。

## P4 可靠性与度量（全销 #191–#199，WBS=p4-reliability-metrics-implementation-wbs.md）
- 重试矩阵单源 internal/llm/retry_matrix.go（agent 重试协议零改动）；熔断单源 internal/pipeline/provider_trip.go：dispatchSkill 唯一派发口（8 派发点，源码测试锁定 `s.skillRunner(` 恰 1 处）、判定异常照常派发、S3 循环熔断整循环中断不刷屏。
- 长跑可见性纯前端：TurnSegment 时间戳派生（事件协议零改动）+SubagentCard 时钟/心跳、done/failed 停表定格、静默仅数值零弹窗、interval 卸载清理。
- 度量纯本地不上传（隐私红线，grep 断言）：feature_usage_daily（AutoMigrate 不动版本化迁移，done=12）+featureKeys.ts 单源三类 key（panel.*/journey.*/action.*）+trackFeature 失败静默；消费面 FeatureUsageSection 有度量才显示、queryErrorToast 静默口径白名单 feature-usage。
- 埋点映射备忘：重写=闸门 modify（聊天写/重写不可区分不计入）、体检=align_full/audit_now/finish_book、retcon 立案=ApproveRetconCase（立案在后端 handoff 出口）。降级逐字一致是最高验收；TestRewriteVoiceABE2E 为真实 LLM E2E 瞬态（复跑绿收口）。

## 流水线重构提案 + 章纲任务卡（2026-10-25）
- 提案 docs/future/pipeline-core-redesign-proposal.md（**R0–R4 全销收官**，F1–F6 全根治）；核心诊断 F1 全书批审 O(n²)/F2 修复乘法/F3 发现滞后/F4 状态袋+裸 goroutine/F5 LLM 文本驱动控制流/F6 双编排器。
- **已落地：R2b 全销（R2b-1..9，§8.2/8.3/8.4/8.5/9.2/9.4）**：internal/structure 长程结构索引层（S 系恒 P1 只报告）=伏笔 lifecycle 三告警+pinned_invariants（*bool 三态防 GORM 零值跳写）+题材卡机制化 novel_checker_configs+章纲写前对账 L1.5（坑表取数失败整项跳过）+人名/称呼/死人出镜+战力境界数值序+ledger_provenance 双向对账（§8.3）；§8.4 修复传播=structure.DependencyClosure（DepEdge 五源边装配/L: 履历种子/闭包>8 升级 L2 不硬修）+executeVerify 传播块+l0RuleSweep 全书扫描兜底；§8.5 L2 三层输入=质量档案全序列升非蓝图常驻+PickKeyChapters/RenderKeyChapterBlock（未修复发现章+黄金三章，goldenN<=0=不挑）+signabilityPrompt auditInput struct（milestone=focused 三层输入不逐章通读；volume/final/golden3=编辑部通读逐字同旧文）；§9.4 命中率度量=action.alert_raised.{l2,l1,struct}+confirmed/dismissed.l2（曝光口径，挂 ReplaceIssues/fix 回执/structureBlock/l0RuleSweep）+IssueHitStats dimension 聚合（校准只取数不改行为）+featureKeys.ts 单源 5 key。
- **已落地：章纲任务卡四件套**（钩子/爽点/人设时刻/推进）= timeline.ChapterTaskCard 表（一章一卡 unique，Validate 四件套 TrimSpace 非空缺一拒收）+ MCP `upsert_chapter_task_card`/`get_chapter_task_card` + get_timeline 注入（缺失立卡提示只推不拦）+ identity 立卡前置/写后对卡核验 + review/signability/fix 白名单（blueprint 不接）。规划数据不进 snapshot。AutoMigrate 加表 done=12 不动。
- **已落地：R0 观测**（提案例行第一步）= activity `exec_run_stats`（耗时/turn/状态/归属 run，纯本地零内容）+ dispatchSkill 单点埋测（观测失败零影响）+ SummarizeExecStats/GetPipelineExecStats 聚合取数。
- **已落地：R1 修复工单化三步（R1a/R1b/R1c）**：R1a=issue 落表 pipeline_issues+chapter_ids 结构化契约（F5 根治，正则降兜底）；R1b=作用域分组并行+FixLog 修复日志共享（F2 根治）；**R1c=修复后 L1 增量复验（executeVerify 只验 FixStates=fixed 及修过作用域，verifyReceiptFormatLine 契约单源+signability-verify 技能，verified 断点态不整体清 FixStates/FixLog）+ skip 即转人工工单（ManualBacklog 连原因→G4 summary，全 skip 不复验落闸，F2 尾部根治）+ VerifyPending 断点防中断误重置；全量复评仅存 volume/final/蓝图/S6**。R1 已全销（含 #210 emit* 发射器注入原子化：emitterSlot[T] 泛型槽+swapEmitX，-race 零竞争，测试卫生）。
- **已落地：R2 章节 CI 四步（R2-1..R2-4 全销）**：R2-1=internal/quality 质量档案表 chapter_quality（(novel_id,chapter_id) 唯一，UpsertL0/UpsertL1 分阶段互不覆盖，"[]"=已查零发现≠空串未查，不加 changed_at）；R2-2=RunL0Checks 零 LLM 纯函数（字数/章尾钩子/AI 味词表/文风漂移全复用既有单源，C 系恒 P1 只报告）；R2-3=chapter-ci.md+ChapterCIAgent 三件套+L1 回执契约（quality/contract.go 锁 skill md）+ScheduleChapterCI 写后钩子（L0 同步+L1 异步 10s 去抖，失败→skipped 零提示）+破 import 环（SchemaOf 下放 internal/schemaof）；R2-4=RenderLedgerBlock 档案汇总块进 milestone L2 输入（signabilityPrompt 加 ledgerBlock，空块逐字同旧文）+并行档案汇总快审记 verdict（quickAuditFormatLine 契约，裁决仍归旧全量通读）+异同记 action.audit_quick_agree/diff（只记 key），降级零提示全路径。
- **已落地：任务卡补充闭环** = ①大纲审批×任务卡联动核验（outlineTaskCardLink 三态：有卡=审批载荷带四件套+审批卡渲染+对卡核验提示 / 缺卡=立卡提示 / 整本零卡=零打扰；只报告不拦截）②ChapterID 回填钩子（chapter.SetCreatedHook 进程级单例→timeline.BindChapterID 只填未绑定、同事务原子、创建回滚随滚；Place 移动不触发）。
- **已落地：R3 任务队列（R3a–R3d 全销）**：R3a=internal/task 租约队列（四态+attempt 围栏令牌+心跳续租）；R3b=8 执行体迁队列 dispatchExecutor（16 裸 goroutine 收口，队列不可用 legacy 逐字降级）；R3c=Interrupted/Dispatching 退役（存活判据=HasActiveForRun 租约语义）；R3d=GateContext 瘦身（9 大对象表化 gate_objects，**Store 层收口拆合**：写拆键+瘦 blob 同事务/整袋替换键缺即清，读合并回胖视图（表行优先 blob 兜底），40 marshal 点与前端 API 零改动；MigrateGateObjects 启动幂等表化）；迁移集成测试 r3_task_gateobject_test 五点黑盒验收补齐（done 仍 12）。
- **已落地：R4 双编排合一（F6 根治）**：next.md v3 收缩为写前对齐+写中技法（阶段 0/1/2），阶段 3 写后质检/阶段 4 周期体检文案退役→定位声明机制指路（机器承担：章节 CI 每章、里程碑签约体检，「不自调度」）；强制语全删（cues/name/总调度标题零改动，regression_test 锁 cues、skill_router_test 锁「总调度」）。**identity.go 第 5 步强制门禁语同步退役**（「先不要汇报完成…全部通过才继续」与「不通过不准进下一章」同一套门禁，只删技能文案=强制语换处存活）→「写后自查（建议）」+机器质检为权威记录，清单保留；第 7 步「启动 Review」留裁决项**已销（3709e807）**：降频重定位「编辑审读（按需）」（触发=重大修订后/批量收口/用户点名，逐章事实检查归章节 CI、状态维护归 statesync），reviewAgentSystem1 删失实「可调用部分 update 工具」句+TestReviewAgentPromptReadOnlyContract 锁提示词/白名单一致；备案未动=reader-immersion.md:75 措辞、timeline/storyarc types.go 过期注释。
- 勿回退坑：LF 归一化只对本次改动文件做（全树 find+sed 会误伤 50+ 无关文件；差集 checkout 还原）。读文件做逐字/正则断言的测试必须 CRLF 归一化或钉 eol=lf（Windows CI autocrlf 隐性变量；.gitattributes 已钉 testdata+skill md）。队列测试「入队后断言不被执行」须先停/延后消费循环（10ms 轮询节奏会合法领取执行，断言的是轮询时机而非语义）。