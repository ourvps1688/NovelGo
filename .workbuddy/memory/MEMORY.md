# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0 + NOTICE）的合规派生 fork。硬性合规：README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..." 声明，不得移除或暗示原作者背书。

## 关键约定
- 模块路径 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；品牌 N+G monogram（不要书的意象）。
- 仓库 `ourvps1688/NovelGo`（origin，SSH `git@github.com:ourvps1688/NovelGo.git`）；skills 仓 `ourvps1688/NovelGo-skills`。git 历史已 unshallow，`upstream`=sigpanic/goink。

## UI / 构建环境
- 前端只做纯皮肤换肤（index.css 颜色 token），不动组件/布局/功能入口。
- 前端硬闸门（提交前本地必跑）：`npx prettier --check "src/**/*.{ts,tsx}"` + `npm run i18n:check`（硬编码中文必须进 i18n）。eslint 限 0 error。
- 改前端源码后必须 `npm run build` 并重启 `wails dev` 才在 webview 生效（dist 为构建产物，非 vite 实时源码）。
- CGO 构建：PATH 含 mingw64，设 `CGO_CFLAGS=-IC:/tools/sqlite3-headers`。go 在 `C:/Program Files/Go/bin`，wails CLI v2.16.0（go.mod 锁定 v2.16.0）。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`。PAT 在仓库根 `github_pat.txt`（.gitignore，绝不入库）。

## 流水线状态机（已落地）
- 阶段 S0/S1/S2/S3/S4/S4b/S5/S6，闸门 G1/G1b/G3b/G4/G终；`Orchestrator.Advance` 唯一裁决。
- S5 修复闭环：分批（P0 每批1/P1 每批2，每批独立会话 MaxTurns=18）+ 断点续修（`FixStates: fixed/skipped/rewrite`）+ 修复回执（`parseFixReceipt`/`applyFixReceipt`）+ 复评（`finishFixAndReaudit`）。
- 正文级 issue（`fix_type=body`）不进 S5 批次，显式路由到 `RewriteBacklog`（FixStates=rewrite），须回 S3 重写。
- 启动恢复：`RecoverInterruptedRuns` 把 running 僵尸置 paused；`ControlPipeline("resume")` 按阶段重派发。

## S5「完成但零修复」死循环修复（2026-10-21）
- 根因：当体检问题全部为 body 级时，`executeFix` 路由到 RewriteBacklog 后 pending 空 → 误入 `finishFixAndReaudit` → 广播 `ok:true`（"修复完成"）+ 复评 → 复评复报同一批 body 问题 → 死循环。用户看到"修复完成"却无有效修复。另：台账批次曾因 MaxTurns=12 在 LLM 供应商流式挂起（idle watchdog 取消）时耗尽而失败。
- 修复（手动交接版 commit c878cc6b）：新增 `finishFixHandoffRewrite`——pending 空且存在 RewriteBacklog 时，广播带 `rewrite_handoff` 标记的 fix_done、将 S5 置 paused 并提示"已转交章节重写，请在写作页重写后重新复评"，**不再复评**；fix MaxTurns 12→18。前端 FixProgressView 新增"已转交章节重写(N)"状态 + RewriteBacklogPanel，i18n 加 `fixProgress.rewriteHandoff`。测试 `TestServiceFixHandoffRewriteWhenOnlyBodyIssues`。

## 自动 S3 章节重写回路（2026-10-21，commit cce0973b）
- 在手动交接基础上，把"转交 S3"升级为**流水线自动执行**：S5 交回时自动派发重写 agent 逐章重改，再自动复评闭环（达标自动出清单）。
- 新增 `RewriteAgent` 类型（复用 FixAgent 白名单：整章覆写 `edit` + statesync 写工具）；内置技能 `internal/skill/builtin/chapter-rewrite.md`（整章 `full_replace`，过 `maybeScoreChapter` 质量门禁）。`GateContext.RewriteRounds` 限制重写→复评循环（上限 `autoRewriteCap=2`）。`EvRewriteProgress` 逐章广播进度。
- `finishFixHandoffRewrite` 现在：pending 空且有 RewriteBacklog → 串行派发重写 agent、广播 progress、成功/无定位章节/达上限则自动重跑 `executeAudit` 闭合「重写→复评」；达上限或章节不可定位则回落手动交接（paused + rewrite_handoff）作安全阀。前端 `RewriteProgressView` 展示逐章进度，i18n 加 `rewriteProgress.*`。测试 `TestServiceAutoRewriteDispatch`。
- 已知环境限制：LLM 供应商流式偶发挂起（`llm stream idle watchdog fired` 刷屏）属供应商问题非应用 bug；并发 statesync 子 Agent 可能长时间运行（"日志 LLM 仍在跑"常因此），非阻断。

## 控制指令与阶段同步约定（2026-10-21，commit acfa90d5）
- pause/resume/stop 等 ControlPipeline 指令必须同步广播 `pipeline:stage` 事件（带 Note），否则侧栏阶段条（只订阅 stage 事件）会与工作区向导不一致。
- S5 修复进度视图（FixProgressView）在 running 与 paused 态都渲染；控制指令失败必须 toastError 可见，禁止静默吞错。
- 交互阶段（S0/S1/S2 生成/S3）resume 后不自动派发，后端广播「由界面交互推进」note，向导在「当前阶段」下展示。
- 阶段条组件（Rail/Bar）收到 finished/aborted 的 stage 事件即隐藏；GetByNovel 取最新非终态 run，注意清理僵尸 run 避免旧 run 顶替。

## S5 修复有界化（2026-10-21，待 push）
- 杜绝无限重试三道闸：`GateContext.FixBatchFailures` 同 run 内批次连续失败计数，达 `fixBatchFailureCap=3` 转 `StatusAwaitingGate`（Options=[continue,abort]）交用户裁决，不再原样重派；派发前 `fixProviderIdleTrip=10`（近 10 分钟 idle 超时 ≥10）供应商熔断不派发；批次成功清零计数。
- 体检端强制 issue 原子化（>5 台账或 >2 章必须拆分，禁 meta-issue）；修复端 `fixPrompt`/技能加「超限主动 skip」护栏。
- 前端：fix_done(ok:false) 把残留 fixing→pending（usePipeline.ts）+ FixProgressView 视图层兜底（ok===false||!running 时 fixing 按 pending 展示）。

## 读者 lifecycle 时间戳（WBS 2.7 已落地，2026-10-22）
- R2 不加新列：起点（埋设/形成/揭示）→planted_chapter_id，终点（回收/纠正）→revealed_chapter_id；known 单点生命周期不设回收章。状态机校验单源 `internal/reader/lifecycle.go`（ValidateLifecycle/Update；Update 仅触碰 type/planted/revealed 才执法，护存量数据）。采集双通道=S4 四事件缺口检测（signability-audit.md）+ 作者确认（前端「标记回收」）。
- 本 worktree 钩子是 `scripts/hooks/pre-commit`（core.hooksPath，仅 gofmt+golangci-lint）；`.githooks/pre-commit` 是 CI 镜像版（含 fe build/lint/test）未启用。前端 vitest 存量失败 2 处（ContentPanel monaco/jsdom 缺 queryCommandSupported、WorkspaceView novel-edit-dialog testid 失配）不阻塞提交。

## 卷快照机制（WBS 2.2 已落地，2026-10-22）
- 采集口径=**冻结时刻全量 series bible**（§7「不断增长的快照即系列圣经」递增全量），非 volume_id 过滤（台账 volume_id 从未填充，全 NULL）。范围=§0.4 冻结清单 10 模型 + 正文清单（只读）。新包 `internal/snapshot`（volume 不能 import chapter，独立包）：Freeze（unfrozen→frozen 同事务提交快照，幂等）/Commit/Diff*/Restore。
- diff_ref=`base:<snapshot_id>` 对象键（基准=同卷上一版本，diff 运行时算，不物化文件）；版本=max(volumes.snapshot_version, 行最大版本)+1，uk_volume_snapshot 冲突重试 3 次。
- **restore 必须 raw SQL Exec**：2.6 temporal 回调与 oplog 回调是全局 gorm callback，`SkipHooks` 拦不住（回调只查 Schema 有无 ChangedAt）；Exec 才不触发回调链——保原时间戳、零履历污染。整体回滚=registry 全表 DELETE+逐行 INSERT（schema 反射取值）。
- 冻结接线：app.SetVolumeFrozen(true)→snapshots.Freeze；2.5 G_freeze 闸门后续直接复用。diff/restore 的 app API 留给 3.2 retcon 流程。

## 台账审计（WBS 2.4 已落地，2026-10-22，commit 450d90b2）
- 冻结前机械校验圣经自洽，防垃圾进垃圾出。严重度分界（勿回退）：error=结构性矛盾（ref.dangling/enum.invalid/lifecycle 违规/locations.parent_cycle）**阻断 Freeze**（`*LedgerInconsistentError` 带完整问题清单）；warning=可疑但合法（unlocated/resolved_without_chapter/duplicate_name/self_loop/gap_incomplete）只报告。语义张力（G7 规则）属 S4/阶段 3，不在机械审计。
- `internal/snapshot/audit.go`：AuditLedger 只读 10 表，纯函数 auditLedger 无 IO；lifecycle 复用 `internal/reader/lifecycle.go` 单源。**审计必须走事务句柄**（auditLedgerIn(ctx,tx,…)），s.db 多连接下读到另一份库态。
- unlocated 警告只给「基础检查通过、仅缺定位」的行，error 行不叠加噪声。app 层 AuditSeriesBible 暴露审计。

## G_freeze 自动闸门（WBS 2.5 已落地，2026-10-22，commit c855dba9）
- 触发=卷级 scoped run 过 S4+S5 收口（G4 continue）且 `AuditReason=="volume"`（卷边界复评）→ 自动 snapshots.Freeze（含 2.4 审计闸门）。中途体检（golden3/milestone）收口**绝不冻结**；仍等于当前卷的目标不冻。
- 卷边界复评定界=**刚完结的前一卷**（completedVolumeScope）做全卷收尾体检，过审即冻结；中途体检仍扫当前卷。audit_now 于跨卷时 reason=volume（原一律 milestone 已修）。完本 G终 finish → freezeRemainingVolumes 补冻剩余卷（每卷有 committed snapshot）。
- advanceS5 兜底复评沿用 ctxt.AuditReason（勿按 AuditStage 推导，会误触冻结）。冻结失败不阻断收口（note 提示，卷保持可写）。volumeFreezer 经 SetVolumeFreezer 由 app 注入。

## S4/S5 依据冻结快照校验（WBS 2.3 已落地，2026-10-22，commit e5362b9b，闭 G5）
- S4/S4b/S6/S5 修复/S3 重写四类提示词尾部注入【已冻结卷既定事实】块（快照出处+前向依据句）；无快照空串不注入=与旧行为逐字一致。冲突改本卷适配，不得回改冻结卷。
- 渲染单源 internal/snapshot/bible.go（LatestSnapshot→EstablishedFactsText→BibleText）；24KB 预算按表粒度截断（截断≠事实不存在）；坏 payload 返回空串宁不注入。pipeline 经 SetBibleProvider 注入解耦，出错降级。
- prompt 构造器统一 bibleText 尾参（signability/finalAudit/fix/chapterRewrite）。技能 md 同口径（audit/fix 各补 bullet）。
- **阶段 2 已全部完成**（2.1/2.8/2.6/2.7/2.2/2.4/2.5/2.3）。

## retcon 审批闸门（WBS 3.1 已落地，2026-10-22，commit d4336d84）
- internal/retcon 新包：retcon_cases 表 + 状态机 candidate→approved→editing→recorded→rolled_back（candidate→rejected 驳回支）；单向无环、终态封死、**迁移只能经显式 API**（retcon 必须显式审批，非循环自动，测试机械验证无环）。
- 闸门规则：裁决（Approve/Reject）必附理由；BeginEdit 必先固化回滚点（快照 id）；Record 必附差异摘要；Rollback 必先接 RestorerFunc 且先还原后落状态；目标卷须已冻结（发起/批准守卫，含中途解冻）。
- pipeline：handoffRetconToUser 沉淀 RetconBacklog→candidate（同 issue 未终态幂等去重）；SetRetconGate 注入，nil 降级。app API：List/Get/Create/Approve/RejectRetconCase。signability-audit.md 有 retcon_candidate 判定问句（宁前向勿 retcon）。
- 下步：3.2 受控编辑留痕 ✅（commit 3d0b1894）：BeginControlledEdit 固化回滚点快照→editing 窗口→RecordWithDiff（回滚点 vs 当前差异摘要留痕）→Rollback（先 Restore→再固化 canon→才落终态）。**Record/Rollback 终态前各 Commit 一份 canon**，让 LatestSnapshot（既定事实注入源）跟上 retcon 后 canon（关键，勿删）。snapshot.FormatDelta 渲染留痕摘要；app：BeginRetconEdit/RecordRetconEdit/RollbackRetconCase/GetRetconEditDiff。**testhelpers 必须与 handler.go 同形装配 retcons 三钩子**（漏装=nil receiver panic）。下步：3.3 一致性巡检（张力报告 agent，只报告不改写）→ 阶段 4 前端。
