# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）合规派生 fork。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..." 声明。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`。仓库 `ourvps1688/NovelGo`（origin）；upstream=sigpanic/goink。

## 构建/环境（勿回退）
- 前端只做 index.css 皮肤 token；四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）；改前端须 build 后重启 wails dev。
- CGO：`export PATH="/c/msys64/mingw64/bin:$PATH"; export CGO_CFLAGS="-IC:/tools/sqlite3-headers"`（lint 也须带）。golangci-lint 在 `/c/Users/Administrator/go/bin`。gofmt 须 LF 归一化后查（CRLF 误报）。`cmd | tail` 掩退出码用 `${PIPESTATUS[0]}`。
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
- 提案 docs/future/pipeline-core-redesign-proposal.md（未排期部分：R0 观测→R1 修复工单化→R2 章节 CI→R2b 全书补强→R3 任务队列→R4 双编排合一）；核心诊断 F1 全书批审 O(n²)/F2 修复乘法/F3 发现滞后/F4 状态袋+裸 goroutine/F5 LLM 文本驱动控制流/F6 双编排器。
- **已落地：章纲任务卡四件套**（钩子/爽点/人设时刻/推进）= timeline.ChapterTaskCard 表（一章一卡 unique，Validate 四件套 TrimSpace 非空缺一拒收）+ MCP `upsert_chapter_task_card`/`get_chapter_task_card` + get_timeline 注入（缺失立卡提示只推不拦）+ identity 立卡前置/写后对卡核验 + review/signability/fix 白名单（blueprint 不接）。规划数据不进 snapshot。AutoMigrate 加表 done=12 不动。