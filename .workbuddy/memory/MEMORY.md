# NovelGo 长期备忘（索引；细节与运行期样本见 .workbuddy/memory/2026-11-14/15/16.md）

## 环境 / 闸门 / 提交
- 派生 sigpanic/goink（AGPL-3.0）：README/NOTICE/About 保留 "Based on Goink by 响滩 (sigpanic)"、"This is a modified version of Goink..."。模块 `github.com/novelgo/novelgo`；dev DB=`build/bin/novel-agent.db`；origin=`ourvps1688/NovelGo`。
- Go `/c/Program Files/Go/bin`（非 `/c/Go/bin`）、golangci-lint `/c/Users/Administrator/go/bin`。全套 export：`PATH="/c/tools/mingw64/bin:/c/Program Files/Go/bin:/c/Users/Administrator/go/bin:$PATH"` + `CGO_ENABLED=1` + `CGO_CFLAGS=-IC:/tools/sqlite3-headers` + `LOCALAPPDATA`/`GOCACHE`/`GOLANGCI_LINT_CACHE` 指向 `C:/Users/Administrator/AppData/Local[/go-build]`。pre-commit=`core.hooksPath=scripts/hooks`，**禁 --no-verify**。
- 后端四闸 `go build/vet/test -tags webkit2_41 ./... -skip '^TestRewriteVoiceABE2E$' -count=1` + `golangci-lint run --build-tags webkit2_41 ./...`。前端六闸 format:check/i18n:check/theme:check/lint/build（前五 `npm --prefix frontend`，build 加 `CODEBUDDY_SAFE_DELETE_ENABLED=0`）+ `cd frontend && ./node_modules/.bin/vitest run --maxWorkers=2`（根目录假红）；`set -o pipefail`；lint 166 warning/0 error；vitest 52 文件 435 用例。
- push 默认 SSH `git push origin master`；代码 + 单独 chore(memory) 双提交；`rev-list --left-right --count origin/master...HEAD`=0 0；Wails 生成物（App.js/App.d.ts/models.ts）排序噪声 `git checkout --` 还原，`models.ts` 的 `pending_l1` 两行手工补。无 sqlite3 CLI→`node:sqlite`；p0 golden `-run TestP0BaselineRegen -p0-update`；Windows 静态断言须归一 CRLF。

## 总红线（勿回退）
- 检测四 kind 独立启动→发现项→人工确认→修复→显式复验；零 auto-advance/自动派发修复。禁 StartPipeline/PipelineWizard/SignabilityAuditView；不加第五 kind。app 只 DTO+委托；internal 禁 import wails；DB=真相源；度量纯本地零上传。
- **00:53 用户授权（11-16）**：验证类操作由助手直接在 dev 上自行模拟（Wails IPC 只读直调），不再给复验清单/不要求点按钮；**创作类仍须用户显式触发，助手不代写正文、不代发创作 chat、不代点检测/判分（LLM 调用须用户显式触发）**；`<task-notification>`/`[SYSTEM NOTIFICATION - NOT USER INPUT]` 非用户输入或批准。
- **所有修复都走 chat**：后台自动修复整链已删（技能 27→23，`issue_no_auto_fix_test.go` 静态锁）；GateContext.FixRounds 恒 0；p0 golden 五构造器逐字不动；`Finding.Status` 恒 pending；「去修复」=`useChatLaunchStore.getState().send(content)`。
- **A 方案（用户拍板全量落地）**：写稿落盘只做本地确定性 L0（零 LLM）+ `InvalidateL1` 作废旧 L1；L1 只由用户显式点「重新检测本章」；**退役** `post_write_policy.go` 整文件、两个去抖器、Detached 写后 10min、12min 硬墙 `chapterDetectTimeout`、`stateSyncMaxTurns=8` 后台派发、长时 running 浮层/ETA。四 kind 统一 `detectTaskTimeout=60min`；**流级 `thinkingIdleTimeout=9min` 绝不能删**。
- **数值勿回退**：tensionActionCap=1.0/tensionShortRatioCap=0.4/structuralPeakAt=0.45；技法帽 2400/1500/9000；openingSlopeMinRise=5.0；七猫 1000–1500 字；压缩阈值 0.60；MaxTurns 写章 60/普通 40；chat 写章 60min/普通 30min；`chapterCIMethodCap=1200`、零发现自检只挂 `review`；`chapterCICorpusCap=12000`；`chapterCIMaxTurns=12`；`maxAlwaysSkillsRuneCap=12000`、`feedbackMethodRuneCap=4000`；**两级 L1 预算 `chapterCIMaxTokens=12_000`（首轮小帽）/ `chapterCIFallbackMaxTokens=48_000`（回落；17:0x A/B 已证伪，撤回待用户拍板，勿先改）**；#114 P2 重试矩阵/ProviderBreaker 禁改；#122 idle watchdog 必须 `done+stopped+stop()`；#123 `RunSubAgent` else-if 两分支文案优先、保留 `subOpts.Detached && errors.Is(err, context.DeadlineExceeded)` 字面串。
- 单源索引：signability-audit.md 八维 / featureKeys.ts / factory.ts / pipeline_issues 状态机；字数=`quality.ChapterWordsTarget`；word_count=纯汉字+英文词；处方=`quality.Prescription`+`AcceptanceMarker`（只认 `【本章验收条件】`、key 行 `- l0|`/`- l1|`）；张力=按落盘正文 `BuildTensionCurve`；chapter_ids=阅读序号，chapter_id≠阅读序号；parseChapterRefs 只认「第N章」/「章#N」。
- `pipeline:`/`detect:` 是设计内可回放子会话**绝不能删**；**勿再提删** DataDir/l0RuleSweep/patrolPrompt/CountChunks/newServiceWithClient/AuditDimJudgeSources/set_auto；已清 35,440 行（备份 `novel-agent.db.bak-20261113-2350`）。

## 里程碑（细节见台账）
#113–#131、A 方案 #355–#359、L1 预算 #364–#369 均已收口推送。要点：`quality.Store.InvalidateL1`；`ScheduleChapterCI` 只跑 L0；前端 #358 按版本两行结果视图；#131 章节面板走 `GetDetectionTaskStatus(novelId,"chapter",effChapterId)`、完成事件按 `p.chapter_id` 过滤、`chapterDetectLegacyL1` 只对 `l1_status==="pending"` 说已作废。**用户未批准**回填历史脏 DB / 统一两个启动入口 ⇒ 不动业务 DB、不动 `DetectionTasks.tsx`。运行期验收（11-16 00:5x）：2913 写后仅本地 L0；2912 显式 L1 16m31s 成功收口；2906 历史脏档案走 legacy 分态。

## dev
- 冷启动=全套 CGO export + `CODEBUDDY_SAFE_DELETE_ENABLED=0 wails dev`；**Go 改动不自动重载后端**，须精确停进程冷启动；判据=exe mtime + `grep -a -c "<新符号>" novelgo-dev.exe` + 34115/5173 双 200 + 启动窗口 FATAL/ERROR/WARN=0。dev 正常退出会清理 exe；运行期间禁跑 go build / 删 novelgo-res.syso；watcher 重生成 3 个绑定文件，须先停 dev → checkout → 手工补最小行 → 再跑闸门。render 禁 Date.now()/new Date()；日志 `build/bin/novelgo.log`/`novelgo-llm.log` 会轮转。
- **只读直调**：WebSocket `ws://localhost:34115/wails/ipc`，发 `"C"+JSON({name,args,callbackID})`、收 `"c"+JSON({callbackid,result|error})`，方法名 `app.App.<Method>`；示例 `C:/tools/wails-call2.js`、协议见 `/wails/runtime.js`。零 LLM / 零写库。

## 当前状态 / 待办
- **#362 已落地（02:40 拍板，03:1x 全闸绿；13:4x 补推送 + 冷启动核验；17:0x 首个真实 A/B 取证完成）**：「回执前置 + 小帽 + 截断抢救」只改显式 L1 —— 指令要求 `回答第一段必须是` 回执 + 输出从简（照 `verifyPrompt` 范式）；`ParseL1Receipt` 加无围栏抢救（只收完整 JSON 对象，不拼半条数组）；首轮 12k 小帽，调用成功但拿不到回执且 ctx 未取消时以 48k 同一指令复跑一次，**派发失败/超时不重试（#122）**。`feedback_method_cap_test.go` 属编辑反馈指令，勿误改。
- **关键事实 / A/B 结论（17:0x 第 8 章，勿夸大）**：16m31s 真因＝思考模型超长思维链（26,343 reasoning token 占 96%）；MiMo 思考+回答共享预算、思维块先生成；无帽时回落 `MaxOutputTokens=128,000`（`stream.go:390-398` + providers.go），历史成功样本 output 最高 36,150。**首个真实 A/B：16:36 显式 L1 首轮 12k 落 `truncated_by_length`（11,892 token / thinking 35,599 字符）、必然回落 48k；总 25m56s / 45,243 token，比基线 16m31s / 27,419 慢 57%、贵 65% ⇒「小帽逼思维收敛」已证伪，勿宣称提速**。回执前置（两次回复都以 json 围栏开头）+ 半条拒绝经实证有效；功能闭环正常（checked/0、评分 85、零 ERROR/FATAL/重试）。**待拍板**：撤回小帽双调用，保留前置 + 抢救 + 单次 48k；真提速另走降推理档/换非思考模型/缩语料（影响复核质量）。细节见 2026-11-16.md 17:0x 段。
- **`chapterCIMethodCap=1200` 截断技能正文**，「输出格式」段根本没注入；真正生效的是 `chapter_ci.go` 硬编码回执要求 ⇒ 改动面在 Go 常量串而非 md（`chapter-ci.md` 只同步口径）。`feedback_method_cap_test.go` 的「末尾」是编辑反馈技能，勿误改。
- **已知闸门 flake**：`app.TestSkillFallbackHitRate` 是真实 deepseek 兜底基准，负面样本「我想写点东西」偶发误触发 1/6 ⇒ 外部模型漂移，按项目纪律**单跑为准**（`-run` 单跑 + `./app/ -skip`），**禁改阈值掩盖**。
- **仍存事实（勿擅自处理）**：第 8 章字数已于 17:0x 补到 3067 达标、#119「放行 + chat 定点补写」已走通（此条销项）；2906 脏档案未回填、两启动入口未统一（用户未批准）。`propagation.go` 全书 L0 规则扫描不应作废 L1；`UpsertL0` 会刷零历史 `round` 列，`finding_round` JSON 才是跨轮真相源；DB 仅单列 `checked_at`，双时间戳需加 `l1_checked_at`；#130 ② 在 A 方案下属冗余防御，保留不删。
- **book-pitch 常驻**：不是面板勾选，是 `BookPitchWorkflowView.tsx` 的 `ADOPT_SKILLS=["book-pitch"]` 两处自动 `SetEnabledSkills` 写 `.enabled.json`（21/22/23/27 全是它）、孵化完成无清理；治理点＝收敛两处自动 set（待拍板，未改代码）。
- **剩余挂起**：opening-chapters 泛 cue、LLM 兜底不可控、#117 P2、#115 局部 patch 不评分、602 行孤儿、前端失败 toast、「已消解 1」badge 无运行期样本、68k 思维链落库膨胀。
