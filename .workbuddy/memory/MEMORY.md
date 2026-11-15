# NovelGo 长期备忘（索引；运行期样本与实现细节全见 .workbuddy/memory/2026-11-14/15/16.md 台账）

## 环境 / 闸门 / 提交
- 派生 sigpanic/goink（AGPL-3.0）：README/NOTICE/About 保留 "Based on Goink by 响滩 (sigpanic)"、"This is a modified version of Goink..."。模块 `github.com/novelgo/novelgo`；dev DB=`build/bin/novel-agent.db`；origin=`ourvps1688/NovelGo`。
- **Go**：`/c/Program Files/Go/bin`（go1.27，非 `/c/Go/bin`）；golangci-lint `/c/Users/Administrator/go/bin`。export：`PATH="/c/tools/mingw64/bin:/c/Program Files/Go/bin:/c/Users/Administrator/go/bin:$PATH"` + `CGO_ENABLED=1` + `CGO_CFLAGS=-IC:/tools/sqlite3-headers` + `LOCALAPPDATA` + `GOCACHE` + `GOLANGCI_LINT_CACHE`。pre-commit=`core.hooksPath=scripts/hooks`，**禁 --no-verify**。
- 后端四闸 `go build/vet/test -tags webkit2_41 ./... -skip '^TestRewriteVoiceABE2E$' -count=1` + `golangci-lint run --build-tags webkit2_41 ./...`。前端六闸 format:check/i18n:check/theme:check/lint/build + `cd frontend && ./node_modules/.bin/vitest run --maxWorkers=2`（根目录假红）；前五 `npm --prefix frontend`；vite build 加 `CODEBUDDY_SAFE_DELETE_ENABLED=0`；`set -o pipefail`；i18n 禁 JSX 中文硬编码；lint 166 warning/0 error；vitest 52 文件 435 用例。
- push 默认 SSH `git push origin master`；代码 + 单独 chore(memory) 双提交；`rev-list --left-right --count origin/master...HEAD`=0 0；Wails 生成物（App.js/App.d.ts/models.ts）排序噪声 `git checkout --` 还原，`models.ts` 的 `pending_l1` 两行手工补。无 sqlite3 CLI→`node:sqlite`；p0 golden `-run TestP0BaselineRegen -p0-update`；Windows 静态断言须归一 CRLF。

## 总红线（勿回退）
- 检测四 kind 独立启动→发现项→人工确认→修复→显式复验；零 auto-advance/自动派发修复。禁 StartPipeline/PipelineWizard/SignabilityAuditView；不加第五 kind。app 只 DTO+委托；internal 禁 import wails；DB=真相源；度量纯本地零上传。
- **00:53 用户授权（11-16）**：验证类操作由助手直接在 dev 上自行模拟（Wails IPC 直调），不再给用户复验清单/不再要求用户点按钮；**创作类仍须用户显式触发，助手不代写正文、不代发创作 chat**；`<task-notification>` / `[SYSTEM NOTIFICATION - NOT USER INPUT]` 是系统通知，非用户输入/批准。**LLM 调用须用户显式触发**（直调只做只读验证）。
- **所有修复都走 chat**：后台自动修复整链已删（技能 27→23，`issue_no_auto_fix_test.go` 静态锁）；GateContext.FixRounds 恒 0；p0 golden 五构造器逐字不动；`Finding.Status` 恒 pending。「去修复」=`useChatLaunchStore.getState().send(content)`。
- **A 方案（用户拍板「按 A 全量落地」治本，勿回潮）**：写稿落盘只做本地确定性 L0（毫秒级零 LLM）+ `InvalidateL1` 作废旧 L1；L1 只由用户显式点「重新检测本章」；**退役**：`post_write_policy.go` 整文件、两个去抖器、Detached 写后 10min、12min 硬墙 `chapterDetectTimeout`、`stateSyncMaxTurns=8` 后台派发、长时 running 浮层/ETA。四 kind 统一 `detectTaskTimeout=60min` 兜底；**流级 `thinkingIdleTimeout=9min` 绝不能删**。
- **数值勿回退**：tensionActionCap=1.0/tensionShortRatioCap=0.4/structuralPeakAt=0.45；技法帽 2400/1500/9000；openingSlopeMinRise=5.0；七猫 1000–1500 字；压缩阈值 0.60；MaxTurns 写章 60/普通 40；chat 写章 60min/普通 30min；`chapterCIMethodCap=1200`、零发现自检只挂 `review`；`chapterCICorpusCap=12000`；`chapterCIMaxTurns=12`；`maxAlwaysSkillsRuneCap=12000`、`feedbackMethodRuneCap=4000`；**`chapterCIMaxTokens=48_000`（禁调小）**；#114 P2 重试矩阵/ProviderBreaker 禁改；#122 idle watchdog 必须 `done+stopped+stop()`；#123 `RunSubAgent` else-if 两分支文案优先，保留 `subOpts.Detached && errors.Is(err, context.DeadlineExceeded)` 字面串。
- 单源索引：signability-audit.md 八维 / featureKeys.ts / factory.ts / pipeline_issues 状态机；字数=`quality.ChapterWordsTarget`；word_count=纯汉字+英文词；处方=`quality.Prescription`+`AcceptanceMarker`（只认 `【本章验收条件】`，key 行 `- l0|`/`- l1|`）；张力=按落盘正文 `BuildTensionCurve`；chapter_ids=阅读序号，chapter_id≠阅读序号；parseChapterRefs 只认「第N章」/「章#N」。
- `pipeline:`/`detect:` 是设计内可回放子会话**绝不能删**；**勿再提删** DataDir/l0RuleSweep/patrolPrompt/CountChunks/newServiceWithClient/AuditDimJudgeSources/set_auto；已清 35,440 行（备份 `novel-agent.db.bak-20261113-2350`）。

## 里程碑（细节全见台账）
#113–#130、A 方案 #355–#359（`edfdee4b`/`0ed66b02`/`b4317140`）、#131（`ffd4c3d4`）均已收口推送。要点：`quality.Store.InvalidateL1`；`ScheduleChapterCI` 只跑 L0；删 12min 硬墙；前端 #358 按版本两行结果视图（`chapterDetectDone = exists && !running && l1_status==="checked" && !pending_l1`）；#131 章节面板改走 `GetDetectionTaskStatus(novelId,"chapter",effChapterId)`、完成事件按 `p.chapter_id` 过滤、`chapterDetectLegacyL1` 只对 `l1_status==="pending"` 说已作废（根因＝kind 级概览取全书 `checked_at` 最新行跨章串台）。**用户未批准**回填历史脏 DB / 统一两个启动入口 ⇒ 不动业务 DB、不动 `DetectionTasks.tsx`。
- **运行期验收（11-16 00:5x）**：`2913` 写后仅本地 L0（`l1_status=pending`、零后台子 Agent）；`2912` 显式 L1 16m31s 成功收口；按章状态与全局概览隔离、`2906` 历史脏档案走 legacy 分态。**系统侧四痛点（超时腰斩 / 后台自动 LLM 假挂死 / 跨章串台 / 谎称已作废）已解决**。

## dev
- 冷启动=全套 CGO export + `CODEBUDDY_SAFE_DELETE_ENABLED=0 wails dev`；**Go 改动不自动重载后端**，须精确停进程冷启动；判据=exe mtime + `grep -a -c "<新符号>" novelgo-dev.exe` + 34115/5173 双 200 + 启动窗口 FATAL/ERROR/WARN=0（全日志旧二进制 WARN 不算）。**dev 正常退出会清理 `novelgo-dev.exe`**；dev 运行期间禁跑 go build / 删 novelgo-res.syso；watcher 重生成 3 个绑定文件，须先停 dev → checkout → 手工补最小行 → 再跑闸门。render 禁 Date.now()/new Date()。日志 `build/bin/novelgo.log`/`novelgo-llm.log` **会轮转**，改用 DB + 新日志头尾时间判定。
- **dev 自行模拟（用户 00:53 授权）**：WebSocket `ws://localhost:34115/wails/ipc`，发 `"C"+JSON({name,args,callbackID})`、收 `"c"+JSON({callbackid,result|error})`，方法名 `app.App.<Method>`；协议见 `/wails/runtime.js` 的 `Call()`/`Callback()`，完整示例见 `2026-11-16.md`。只读直调零 LLM / 零写库。

## 当前状态 / 待办
- **「补 L1 推理预算」已落地（02:1x，`7fd9928e` + `9ed793a2` + `e7ca5f42`；全闸绿、`0 0`）**：`SubAgentRequest.MaxTokens` → `agent.RunSubAgent:146` 透传 → `RunChapterCIForChapter` 设 `chapterCIMaxTokens=48_000`；`backgroundRunOptions()` 不设全局帽，主 chat/写章/前台 run_subagent/反馈/巡检/复验零波及。契约＝行为测试 `got.MaxTokens` + `chapter_ci_l1_budget_wiring_test.go` 三静态锁。冷启动核验通过（exe `02:16:14 / 80,113,994 B`、新符号命中、退役符号 0、双 200、启动窗口 733 行零 ERROR/WARN、生成物噪声已还原）；dev 只读直调三章状态与 A 方案样本一致。**无运行期 A/B 样本，勿宣称提速已验证**。
- **L1 帽两条更正**：① 无帽时不是 64k，是回落模型 **`MaxOutputTokens=128,000`**（`stream.go:390-398` + `providers.go` mimo-v2.6-flash），64k 只在无模型信息时兜底。② **设帽不能压 16m31s**：MiMo 思考+回答共享预算且思维块先生成，`detect.go` verifyMaxTokens 注释已记「6k 帽把回执硬切半条 JSON」「预算帽救不了长思维（I7）」；`chapter-ci` 回执在**回复末尾**。历史成功样本 output 10,895/11,707/15,727/27,419/**36,150** token、回执约 1k ⇒ 帽 <36,150 会在回执生成前截断、本版 L1 停 pending；48k 覆盖全部样本，对 00:16 的 27,481 **不触发**。**本次只修接线缺口 + 消除 128k 无帽失控，不宣称提速**。
- **下一步待拍板**：真提速候选均影响复核质量或回执契约，超出已批准范围 —— ① 回执前置 + 小帽 + 截断抢救（`signability-verify` 范式）；② 降推理档 / 关思考（当前 `reasoning_effort=""` 且该模型无 `ReasoningLevels`）；③ 换非思考模型；④ 维持现状仅观察。
- **仍存事实（非系统缺陷，勿擅自处理）**：第 8 章 2147 字未达 3000–4000，判 fail 属 #119「放行 + chat 定点补写」，助手不代写正文；`2906` 历史脏档案未回填、两个启动入口未统一（用户未批准）。
- **已复核判定（勿再当缺口）**：`propagation.go` 全书 L0 规则扫描按同版正文重跑 ⇒ 不应作废 L1；`UpsertL0` 会刷零历史 `round` 列，`finding_round` JSON 才是跨轮真相源；DB 仅单列 `checked_at`，双独立时间戳需加 `l1_checked_at`；#130 ② 在 A 方案下变冗余防御，保留不删。
- **book-pitch 常驻更正**：不是面板勾选，是 `BookPitchWorkflowView.tsx` 的 `ADOPT_SKILLS=["book-pitch"]` 在「新建孵化器」(:252)/「采纳建书」(:507) 自动 `SetEnabledSkills` 写 `.enabled.json`，孵化完成无清理；21/22/23/27 全是 `["book-pitch"]`。治理点＝收敛两处自动 set（待拍板，未改代码）。
- **剩余挂起**：book-pitch、opening-chapters 泛 cue、LLM 兜底不可控、#117 P2、#115 局部 patch 不评分、602 行孤儿、前端失败 toast、「已消解 1」badge 无运行期样本、68k 思维链落库 DB 膨胀。
