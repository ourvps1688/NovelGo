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
