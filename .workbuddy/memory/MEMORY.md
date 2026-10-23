# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）合规派生 fork。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..." 声明。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；N+G monogram。仓库 `ourvps1688/NovelGo`（origin）；upstream=sigpanic/goink。

## 构建/环境（勿回退）
- 前端只做 index.css 皮肤 token；四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）；改前端须 build 后重启 wails dev。
- CGO：`export PATH="/c/msys64/mingw64/bin:$PATH"; export CGO_CFLAGS="-IC:/tools/sqlite3-headers"`（lint 也须带）。golangci-lint 在 `/c/Users/Administrator/go/bin`。gofmt 须 LF 归一化后查（CRLF 误报）。`cmd | tail` 掩退出码用 `${PIPESTATUS[0]}`。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；PAT 在仓库根 github_pat.txt。钩子=scripts/hooks/pre-commit（gofmt+lint）。commit&&push 链式偶显 "nothing to commit" 但已落盘→核 log 后单独 push。
- 测试瞬态：真实 LLM E2E 偶发超时 FAIL，复跑绿即收口；无 key Skip（NOVELGO_EVAL_KEY）。

## 流水线
- S0–S6 / G1/G1b/G3b/G4/G终，Orchestrator.Advance 唯一裁决。body 级 issue→RewriteBacklog 回 S3；S5 分批+断点续修+复评，连败 3→AwaitingGate，idleTrip=10 熔断，issue 原子化。自动重写 RewriteAgent，Rounds≤2 超限 rewrite_handoff。控制指令须同步广播 pipeline:stage。

## 卷快照（internal/snapshot）
- 冻结=全量 series bible（10 模型+正文）；**restore 必须 raw SQL Exec**（gorm callback 对 SkipHooks 无效）。审计 error 阻断冻结；G_freeze=S4+S5 收口且 AuditReason=="volume"。提示词注入【已冻结卷既定事实】（bible.go 单源，坏 payload 返空）。
- RestoreStyle（P2 T4.1）：restoreOneTable 只还原 voice_dimensions，事实层零触碰；冻结快照双层钉住（事实+风格同记录，勿拆两条）。

## retcon
- 状态机 candidate→approved→editing→recorded→rolled_back（+rejected），迁移只经显式 API。裁决必附理由；BeginEdit 固化回滚点；Record 附 diff；Rollback 先 Restore 后落状态；Record/Rollback 终态前各 Commit canon。目标卷须已冻结。
- **testhelpers 必须与 handler.go 同形装配全部钩子**（漏装=nil panic）。

## 巡检（3.3）
- consistency-patrol.md 只读张力报告；零污染是机械保证（patrol_test 断言）；触发=冻结后自动一次+control("patrol")；不自动立 retcon 案。

## P0 上下文装配器
- internal/contextasm：Facts→Voice→Intent 尾接提示词；purpose 矩阵 rewrite 全注/audit·fix·patrol=Facts+Voice/chat=Voice。四纪律：有则注入/无则降级逐字一致[最高验收]/8KB 截断/宁缺勿错。PersonaMarker 禁改。golden=testdata/p0_baseline_*.txt（提示词演进须重定格并备案）。

## P1 风格结构化（全销）
- internal/voice：voice_dimensions（(novel_id,dimension) Upsert，书级十维，不入 temporal）；枚举/metrics/source 单源禁改。双写=skill md 不动+voice_profile 并行落库。voiceText=personaMindText+RenderLedger；台账空→与 P0 逐字一致。voice 已入 snapshot registry。

## P2 文风进闸门（已全销 #169–#179，WBS=p2-style-gate-implementation-wbs.md）
- 漂移=确定性比对（style.ComputeStats vs voice.MetricKey 八键，`|cur−base|>max(abs,rel×|base|)`）；无基线/坏数据→零 issue（最高验收）。V 系：ID 前缀 V 单源（D 归蓝图、T 归巡检、I 归常规）、Level=P1、fix_type=body、不进评分不翻 verdict；合并续编 maxV+1 防撞号。
- 只报告不改写=检测器零写入+isStyleExempt 派发豁免+全豁免 RewriteBacklog 手动交接（防死循环）。executeFix 四分区闭合（retcon/rewrite/pending/resolved 必居其一）。**persistRetconCandidates 立案只在 handoffRetconToUser 出口**（backlog 优先时不立案，既有语义）。
- retcon kind 路由：RetconCase.Kind（fact 缺省/style，单源）→Rollback style→RestoreStyle/fact 与存量空 kind→全量；V 系立案带 style；testhelpers 同形装配 SetStyleRestorer。**每加迁移须同步 migrate 集成测试的 done 总步数断言**（当前 12）并补 E2E 历史输入+断言（注释硬要求）。
- 双验收=p2_acceptance_test.go（进路由+派发豁免+立案 kind）×retcon 全链（风格回滚只动 voice 行）。前端零改动（UI 属 P3）；后续见 docs/future/ultimate-form-integration-plan.md。
