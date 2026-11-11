# NovelGo 长期备忘（勿回退；细节见 .workbuddy/memory/YYYY-MM-DD.md）

## 环境 / 提交
- 派生自 sigpanic/goink（AGPL-3.0）：README/NOTICE/About 保留 "Based on Goink by 响滩 (sigpanic)"、"This is a modified version of Goink..."。模块 github.com/novelgo/novelgo；dev DB=build/bin/novel-agent.db；origin=ourvps1688/NovelGo。
- CGO：PATH += `Program Files/Go/bin`, `tools/mingw64/bin`, `go/bin`；CGO_ENABLED=1；CGO_CFLAGS=-IC:/tools/sqlite3-headers；LOCALAPPDATA/GOCACHE/GOLANGCI_LINT_CACHE=C:\Users\Administrator\AppData\Local\go-build。go build/vet/test `-tags webkit2_41`；golangci-lint `--build-tags webkit2_41`。
- 前端闸 `npm --prefix frontend run format:check/i18n:check/lint/build/test`；set -o pipefail；vite build `CODEBUDDY_SAFE_DELETE_ENABLED=0`；i18n 禁 JSX 中文硬编码。
- push `GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；代码 + 单独 chore(memory) 双提交；`rev-list --left-right --count origin/master...HEAD`=0 0；Wails 生成物噪声 `git checkout --`（models.ts 只手工补字段）。
- 无 sqlite3 CLI→node:sqlite DatabaseSync；非终态 tasks=status NOT IN('done','dead')；p0 golden `-run TestP0BaselineRegen -p0-update`。

## 总纪律
- 检测四 kind 独立启动→发现项→人工确认→修复→显式复验；零 auto-advance/闸门/自动派发修复。禁 StartPipeline/PipelineWizard/SignabilityAuditView；S2 RunBlueprintTask 独立容器+显式按钮+无 useEffect，不加第五 kind。
- app 只 DTO+委托；internal 禁 import wails（event.Emitter）；DB=真相源。单源：signability-audit.md 八维（desc 禁回改前章）/featureKeys.ts/factory.ts/pipeline_issues 状态机；度量纯本地。
- **所有修复都走 chat**：后台自动修复整链已删（StartIssueFixTickets/issueFixActive/Mu/taskKindIssueFix/dispatchExecutorUnique/执行体/回执/漂移巡检/fix_reachability.go/复验在途护栏），技能 27→23，`issue_no_auto_fix_test.go` 静态锁回潮；GateContext.FixRounds 恒 0；p0 golden 五构造器（fixPrompt/chapterRewritePrompt/blueprintFixPrompt/fixLogBlock/fixReceiptFormatLine）**函数体逐字不动**。
- 手工链 StartManualIssueFixTickets(只置 fixing)→CloseIssueFixFixTickets(fixed)/CancelIssueFixTickets(pending)→VerifyIssueTickets；「去修复」= `useChatLaunchStore.getState().send(content)` 直接发送（无 prefill/window event）。
- 台账唯一约束 147b667c 自然键 find-or-create + v172；chapter_ids=阅读序号 1..N（台账/全书 []），chapter_id≠阅读序号；parseChapterRefs 只认「第N章」/「章#N」，裸 #\d+=台账引用。

## #113 章节闭环 / 字数单源
- 章节检测只写 chapter_quality（L0/L1 JSON 发现，无 run_id/issue_key/ticket_id），不落 pipeline_issues；工单面板需 IssueTicketRef ⇒ 无法直接挂修复按钮。
- 闭环=卡片勾选发现项→send 发中央 chat 人工改稿→`RunDetectionTask("chapter")` 重检；不新增工单/不改表/不派发后台执行体；`DetectionWorkflowClosure.test.tsx` 锁死。档案卡片不再以 st.last_run_at 为门槛。
- `quality.ChapterWordsTarget`（chapter_words.go + Store.ChapterWords）是单章字数唯一真相源：platform_profile>platform_card>genre_card>default(2000)；EffectiveLengthWindow/EffectiveTarget/SystemLine。接线：L0 三调用点(chapter_ci/propagation/l0_verify)、writing system(novel_profile/identity/BuildEffectiveConstraint)、判官(score.go+rw_tools)、ChapterQualityArchive.WordsTargetLabel；chapter_words_wiring_test.go 静态锁。七猫 1000–1500（opening-chapters.md 曾误写 1800）。
- 手动重检跑 L1 前经 app/handler.go `RecalcChapterL0FromDisk` 按落盘正文重算 L0（与写后链路共用 recalcChapterL0），不刷新张力曲线/不重复落 E 系工单，失败仅告警。
- **#105** word_count=纯汉字+英文词(text.ComputeStats)，不含标点/换行/@偏移/台账/任务卡/证据片段；字数类必走 tryL0Verify 本地确定性判定（CheckOpeningLength、parseChapterLengthWindow 取处方区间、过滤 RunL0Checks profile 口径）；不可读/无区间→回落 LLM。

## #114 验收处方单源 / 判定器同源（P0）
- 根因：chapter_quality(L0/L1) 与 chapter_score_events(前后对比) 双链独立，判官看不到 finding；Finding.Status 恒 pending、L0 整行覆盖无稳定 key；emotionScore 靠直白情绪词/感叹号，与去 AI 味数学互斥；开篇坡度公式「末−首」与文案「抬首段」反向。非供应商能力问题，技能只是次要放大器。
- **单源** `quality.Prescription`（prescription.go）：`AcceptanceMarker=【本章验收条件】`，阈值全引用检测侧常量；`BuildPrescriptions/RenderPrescriptions` 投影「判定式/实测/目标/位置/动作/避免」。档案 DTO `Prescriptions []string`（取 source=l0 基线曲线）→ 前端按 `[check]` 对齐勾选；判官 `ScoreChapterWithPrescriptions` 逐条核验；改稿 `buildAcceptanceGuard` 按标记确定性注入 system-reminder（5 条：逐条复述/只定点改/严禁 full_replace/不换数字/逐条回报），不经技能路由、不新增后台执行体。
- 判定器：tension.go 加结构信号第二通路（action_density/short_sentence_ratio，structuralPeakAt=0.40、cap=3.0），`beatPeakCount` 单源；reconcile.go 冲突预检给曲线类追加「禁堆情绪词/禁单纯扩写」；platform_checks/tension_checks 文案给可达成目标；MarkTensionCurve 与 beatGap 同源。
- 技法帽：核心 3 技法 2400 / 条件化 1500 / 整块 9000（writingBundleCoreSkillCap），勿回退到 1500 或 5000。
- 契约：prescription_test.go（show-not-tell 可达、阈值同源、冲突预检、坡度方向）、prescription_wiring_test.go（静态接线 + 零自动修复）、DetectionWorkflowClosure.test.tsx（处方字段/双语标记/models.ts）。p0 golden 五构造器零改动、Finding.Status 语义不动（P1 另加列，仍未写 fixed/skipped）。**P2 仍是方案未实施**：turn_id、供应商可观测；P1 已落地见下节。

## #114 P1 跨轮对齐 / 前后对比（勿回退）
- **身份单源** `quality.FindingKeys(l0,l1)` = `scope|check|归一化 LedgerRefs|同组序号`；绝不进 key：顺编 ID、Desc 全文、实测数字、Status/ChapterIDs。`DiffFindingKeys` 出 `resolved/persisted/new`。
- `chapter_quality.finding_round` JSON 列（AutoMigrate 加列、零版本化迁移）：`UpsertL0/UpsertL1` 覆盖前经 `withFindingRound`→`NextFindingRound` 写三态 + `RepeatCount`；`PendingL1` 让同轮 L1 收口沿用同一 `PrevKeys`（否则 L0 的 resolved 被吞）；同轮用 `carryRepeatCount` 只增删不累加，否则轮次保护永远触发不了。`FindingGuardRounds=3`→`GuardKeys`。旧档案空串按当前行作基线。
- `chapter_score_events.finding_keys` 快照列：必须在审批/落盘**之前**取（`findingKeysForChapter`→`pendingFindingKeys`→`persistChapterScore`），因 `ScheduleChapterCI` 之后档案已重算；`ScoreEventView.FindingKeys` 透出，相邻事件做集合差，**空快照禁止反推成「全部消掉」**。
- DTO `ChapterQualityArchive` 加 4 个基础类型字段（resolved/persisted/new_count + guard_keys）；`models.ts` 只手工补 10 行。前端两处 invalidate `["score-trend"]`/`["score-history"]`（此前零失效通道）。卡片三态 badge + guard 提示 + chat 消息「豁免/调整阈值/换思路」人工决策文本，仍零自动修复。
- **未做**：P1.5「局部 patch 也评分」（会让每次 search_replace 都调判官 LLM，与省 token 冲突）；P2。p0 golden 与 Finding.Status 不动。
- 契约：finding_key_test / finding_round_test / finding_round_store_test / finding_round_wiring_test（静态全链 + banned）/ DetectionWorkflowClosure.test.tsx P1 describe。vitest 默认 fork 并发在沙箱里会随机 worker 超时，`--maxWorkers=2` 全量干净通过（51 文件 / 414 用例）；`--minWorkers` 非 vitest4 参数。

## 复验 / 状态机
- VerifyIssueTickets 经 payload IssueKeys 透传、禁回扫 verifying；L0 快路径命中仍回落 LLM；重新 verifying 清 note；预算 120min/15min/2 turns/10k；rejected 3 次→skipped。
- RAG-Verify：SearchBatch 切片（章 topK=2+台账 #id 定点+窗口回退），单工单 4500/单批 12000 帽，批量 4+批间并发 2，工具零暴露/输出≤3 句；语料帽与书长无关；截断抢救 parseVerifyReceipt partial+salvageVerifyEntries。
- #106 UpdateIssueStatus 条件 CAS `WHERE status NOT IN(verified,dismissed,verifying)`；#100 ReplaceIssuesByPrefix 按「键+Area→Area 兜底」继承 carryIssueWorkflow + pre_verify_status/verifyRestoreStatus。

## 数据 / 边界
- **#109** 平台数据对账/对标书库整体下线（删 14 文件）；signability-audit.md 只删 dim2「对标书库摘要」句，八维定义/desc 未动、description/cues 禁再出现「对标」；**保留** platform_profile、review/score_mapping.go、quality/feedback.go、ScoreTrendPanel 的 review.JudgeAuditDimScore；存量表 migrate/v173（Destructive+PostSchemaStep+幂等）DROP。
- mcp_tools→pipeline 成环→反馈/情绪账落单走 SetFeedbackTicketLander/SetTensionTicketLander；issue.Dimension=台账维枚举≠八维键。alive：Alive *bool 三态、仅在世→已死单向；DeriveAliveFromChapter 正文派生（宁漏勿误）；app Alive *bool 唯一人工复活通道。
- #107 五类引导项落 preference_items，被 intentFromPitch + NovelProfile→FormatPreferences 消费；唯一写入口 UpsertPitchPreferences（全量 PUT/单事务/空串清除），禁逐条 CreatePreference；平台约束/创作约束 >80 rune 且≠本次值=手写纪律不触碰；平台名命中 preset 且无检查器配置行→自动武装 platform_profile（七猫→qimao）。
- #99 多卷书裸 new.md 拒绝并列卷清单、废 LastByNovel 静默默认；move_chapter target 0=未分卷。#101/#104 安全审核定位：语料单元同源回收、二分探针（control 先行/64 探针/8min 帽）、request_units.go 覆盖 prompt+本次 session 工具返回、detect.go 双路径落可见 error、硬错误禁静默降级。巡检单次归并+本地预筛+48k 帽。
- CheckerConfig 关键词全空=不检查；Validate 空白 profile 归一为空；E 系文案避开语义排除词；parseAudit 禁塞 LLM 原文进 Summary/Desc；idleRetryable 零字节空转不可重试；禁 reflect.Ptr（用 Pointer）；PreSchemaSteps 碰子表须 HasTable 守卫。

## 技能 / CI
- builtin 23、无 mode: always；写章必走 buildWritingSkillBundle（writingBundleSkills: reader-immersion/scene-beats/plot-hook），writingBundleBlockCap 5000→9000 勿回退；注入用 sk.Content/skill.Body、禁 sk.RawContent；notWhen 是 cue 单源（next 留 `notWhen:[动笔前]`）；chat:started 带 skills 唯一可见性通道。
- #111 P0：Skill 加 Mounts/InternalOnly/Budget/Deprecated；skill_invocations 走 migrate.go AutoMigrate（不写 v174）；activity.RecordSkillInvocation sink；health_test.go 静态闸零挂载点即失败（legacyPlaceholders 已清空）；state_sync 记 Skill="" 通道事件、汇总过滤空 skill；internalOnly 只禁 chat 路由/catalog；mountOwners 单源映射。
- P1：RouteResult.Redirects；chat 3,1+去重+12000 帽+capSkillRunes（manual next 不截断）；writing_skill_conditions.go 缺信息即不命中；chapterCISelfCheckSkills 空块逐字不变。P2：stateSyncSkills(collect+memory)、RunStateSyncForChapter 增 skillStore 两处、slashSubAgentDispatch /memory /review、editor_feedback.go 复用 RecordEditorFeedbackTool.Execute；S2 幂等**必须** GetRunningRunByMode（按 mode 精确查）。P3：WorkflowAutoArms/BuildSkillCatalog 说明性、BuildAlwaysSkillsContent 兼容层；docs/skill-wiring-contract-2026-11-09.md 的 27 技能表是下线前快照原样保留；TECHNICAL_STATUS_AND_GUARDRAILS.md §3 + AGENTS.md 是现值口径。
- CI：#415/#418 外部偶发（lint action 504 / npm ci 超时）；run_attempt 唯一重跑证据、重跑 3 次仍红=代码问题；fallback_bench 单跑为准；checks.yml lint-go 缺 build-tags 已知差异。Windows-only：静态断言读 .go 必须 readHealthSource 归一 CRLF、healthVarBlock 截窗口、stripGoLineComments；internal/pipeline 跨包 ../..；**禁加 `*.go text eol=lf`**；旧 SHA Re-run 洗不掉红标。

## dev
- 重启：全套 CGO export + `CODEBUDDY_SAFE_DELETE_ENABLED=0 wails dev`；杀进程 PowerShell Get-Process（循环变量禁 `$pid`）；判据 exe mtime+新文案符号+34115/5173 双端口（mtime≠最新代码，须核具体符号）；exit 127=停进程预期通知。
- 配置基线：wails.json `frontend:dev:serverUrl=http://127.0.0.1:5173` + vite.config.ts `server.host='127.0.0.1'`；浏览器直开 5173 白屏正常。
- **白屏真根因（CDP）**：SubagentCard.tsx `getAgentMeta(t)[agentType]` 只映射 memory/review、历史会话含 statesync/chapterci ⇒ meta.emoji TypeError ⇒ React19 卸载 #root；IPC/绑定仍正常（能调绑定≠界面在）。修复=类型放宽 string+未知 🧩 降级+双 ErrorBoundary。「双栈错配」「缺 serverUrl」推断已证伪，判据必须 WebView2 DOM/异常。CDP 需临时给 Wails 模块缓存 setupChromium 加 `--remote-debugging-port=9222 --remote-allow-origins=*` 并还原；WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS 无效。
- dev 改 Go 会杀在途巡检；idle watchdog 9m 既有噪声；Bash/Windows 代理两套；render 禁 Date.now()/new Date()；vitest 沙箱偶发以四道闸兜底；ONNX 仅 bge-small-zh-v1.5，reranker 已移除；breaker.go 三态只认硬失败。
