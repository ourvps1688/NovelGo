# NovelGo 项目长期备忘

## 项目性质与约定
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）合规派生 fork。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..." 声明，不得移除。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；品牌 N+G monogram。仓库 `ourvps1688/NovelGo`（origin）；upstream=sigpanic/goink。

## 构建/环境（勿回退）
- 前端只做 index.css 皮肤 token，不动组件/布局/入口。前端四道闸：`npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）。改前端须 build 后重启 wails dev 才生效。
- CGO：`export PATH="/c/msys64/mingw64/bin:$PATH"; export CGO_CFLAGS="-IC:/tools/sqlite3-headers"`（漏则缺 sqlite3.h）。golangci-lint 在 `/c/Users/Administrator/go/bin`。gofmt 需 LF 归一化后查（CRLF 全量误报）。`cmd | tail` 掩盖退出码用 `${PIPESTATUS[0]}`。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；PAT 在仓库根 github_pat.txt（.gitignore 不入库）。钩子=scripts/hooks/pre-commit（gofmt+lint）。Bash 用 `/d/workbuddyfiles/NovelGO` 绝对路径。
- 测试瞬态：真实 LLM E2E 高负载偶发 60s 整数超时 FAIL，单项+包复跑绿即收口勿当回归；无 key Skip（NOVELGO_EVAL_KEY）。

## 流水线（已落地）
- 阶段 S0–S6、闸门 G1/G1b/G3b/G4/G终，Orchestrator.Advance 唯一裁决。body 级 issue 不进 S5 批次→RewriteBacklog 回 S3 重写；S5 分批（P0 每批1/P1 每批2，MaxTurns=18）+断点续修+修复回执+复评。启动恢复僵尸 run→paused。
- S5 有界化：FixBatchFailures 连败 3→AwaitingGate 交裁决；fixProviderIdleTrip=10 供应商熔断；issue 原子化（>5 台账/>2 章必拆）。
- 自动重写回路：RewriteAgent+chapter-rewrite.md 整章 full_replace，Rounds≤autoRewriteCap=2，超限回落 paused+rewrite_handoff。控制指令必须同步广播 pipeline:stage（带 Note）。

## 卷快照（internal/snapshot）
- 口径=冻结时全量 series bible（10 模型+正文清单）；Freeze/Commit/Diff*/Restore；diff_ref=base:<id> 运行时算。**restore 必须 raw SQL Exec**（gorm callback 对 SkipHooks 无效；Exec 保时间戳零履历污染），整体回滚=全表 DELETE+逐行 INSERT。
- 台账审计 audit.go：error=结构矛盾（ref.dangling/enum/lifecycle/parent_cycle）阻断 Freeze；warning 只报告；审计走事务句柄。G_freeze=S4+S5 收口且 AuditReason=="volume" 自动 Freeze；中途体检（golden3/milestone）绝不冻结；完本 freezeRemainingVolumes 补冻。
- S4/S4b/S6/S5/S3 提示词注入【已冻结卷既定事实】（bible.go 单源渲染，24KB 截断，坏 payload 返空串）。

## retcon（3.1/3.2）
- 状态机 candidate→approved→editing→recorded→rolled_back（+rejected 驳回支），单向无环、迁移只经显式 API。裁决必附理由；BeginEdit 先固化回滚点快照；Record 附 diff 摘要；Rollback 先 Restore 后落状态。目标卷须已冻结。
- Record/Rollback 终态前各 Commit 一份 canon（LatestSnapshot 跟上，勿删）。三执行体 handler.go 注入（Commit/DiffAgainstCurrent+FormatDelta/Restore）；**testhelpers 必须同形装配三钩子**（漏装=nil panic）。

## 巡检（3.3）
- consistency-patrol.md 只读张力报告→GateContext.PatrolReport+事件 pipeline:consistency_patrol；只报告不改写是机械保证（patrol_test 断言零污染）；触发=冻结成功后自动一次+control("patrol")。不自动立 retcon 案。

## P0 上下文装配器
- internal/contextasm：ContextBlocks{Facts,Voice,Intent} 尾接提示词末尾，Facts→Voice→Intent；purpose 矩阵：rewrite 三块全注 / audit·fix·patrol=Facts+Voice / chat=Voice。四纪律：有则注入/无则降级逐字一致[最高验收]/预算截断 8KB/宁缺勿错。PersonaMarker 禁改。golden 基线 testdata/p0_baseline_*.txt。

## P1 风格结构化（账 #158–#168 全销）
- internal/voice：voice_dimensions（(novel_id,dimension) Upsert；书级十维；不入 temporal）。十维枚举/metrics 键/source 单源禁改。双写=skill md 与 style_profiles 路径一字不动+voice_profile toolcall 并行落库（未产出不落库）。
- voiceText=personaMindText（P0 闭包一字不动）+RenderLedger（render.go 单源固定序只渲染 content）；台账空→与 P0 逐字一致；读失败弃台账块。voice 入 snapshot registry 四机制自动覆盖（存量卷首个含 voice 快照 diff 全量新增=预期勿当 bug）。
- retcon Rollback 已可还原 voice 行（P2 风格回改铺路），A3 零触碰。三联验收 app/voice_acceptance_test.go 全绿。

## P2 文风进闸门（已立项，WBS=p2-style-gate-implementation-wbs.md，账 #169–#179）
- 漂移检测=确定性数值比对（style.ComputeStats vs voice.MetricKey 八键基线，阈值表单源保守初值）；无基线/坏数据→零 issue（降级第 5 条，最高验收）。
- V 系 issue：ID 前缀 V 单源（**D 归 G3 蓝图漂移**）、Level=P1、三枚举照常、fix_type=body。「只报告不改写」=检测器零写入+**自动派发豁免**（isStyleExempt，作者显式推进不受限）+verdict 不翻转；全豁免 RewriteBacklog 走手动交接收口防死循环。
- G_freeze 双快照=冻结快照双层钉住（同事务 registry 携带，**不拆两条记录**护 DiffToPrevious/uk）；RestoreStyle 按表只还原 voice_dimensions（书级行防误伤事实层）。
- retcon 风格回改=A3 零新流程+RetconCase.Kind（fact/style）+Rollback 按 kind 路由；testhelpers 同形装配钩子防 nil panic。前端零改动（UI 属 P3）。
- 后续 P3/P4 见 docs/future/ultimate-form-integration-plan.md。
