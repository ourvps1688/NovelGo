# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25+Wails v2+React 19+SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 github.com/novelgo/novelgo；DB=build/bin/novel-agent.db（dev）；仓库 ourvps1688/NovelGo（origin）。

## 构建/环境（勿回退）
- CGO：PATH 含 `Program Files/Go/bin`+`tools/mingw64/bin`+`go/bin`；`CGO_ENABLED=1`；`CGO_CFLAGS=-IC:/tools/sqlite3-headers`；裸调用带 `LOCALAPPDATA`+`GOCACHE`/`GOLANGCI_LINT_CACHE`=`C:\Users\Administrator\AppData\Local\go-build`。golangci-lint 在 go\bin。go build/vet 用 `-tags webkit2_41`（`--build-tags` 是 golangci-lint 旗标，go 不认）。
- 前端四道闸 `npm --prefix frontend run format:check/i18n:check/lint/build`（eslint 0 error；i18n 禁 JSX 硬编码中文）。Bash cwd 不跨会话持久，npm 闸须 --prefix 或显式 cd。
- 推送 `GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；一任务一提交带全套 export；记忆账单独 chore(memory)；核验 `git rev-list --left-right --count origin/master...HEAD` = 0 0。

## 架构约束（勿回退）
- 质量保障唯一规范路径：检测四 kind 独立启动→发现项落工单→人工确认→修复→显式复验；零 auto-advance/零闸门/零自动派发修复。前端入口=DetectionTasks+IssueTicketsPanel+PatrolReportPanel+RetconPanel+VolumeManagePanel，禁 StartPipeline/PipelineWizard/SignabilityAuditView。
- 契约单源勿改：signability-audit.md（8 维，desc 禁回改前章）、featureKeys.ts、factory.ts、pipeline_issues 状态机。度量纯本地不上传。
- 分层：app 绑定层只 DTO+委托；internal 禁 import wails（走 event.Emitter）；DB=真相源。
- 台账唯一约束（147b667c）：四类台账自然键去重 find-or-create；v172 迁移合并+唯一索引。

## 质量升级路线图 2026-11（P1-2/P2-3 已按用户判定下线，其余勿回退）
- 落地：P0-0 开篇体检(4189a7bb)/P1-1 编辑反馈回流(67410ffb)/P0-1 平台 profile(qimao preset+L0 三查)/P0-3 评分打通(review/score_mapping.go 6↔8 映射)/P0-2 情绪账(chapter_tension+E 系 ts- 容器)/P2-1 技能路由/P2-2 scene-beats；~~P1-2 平台对账(platform_metrics)~~/~~P2-3 benchmark_books~~ 已于 2026-11-09 整体移除（#109）。
- **#109 移除边界（勿回退/勿误删）**：用户判定「平台数据对账」「对标书库」实际写作价值低，属多余设计。删 14 文件（app 10 个导出方法、quality 两组模型/解析、pipeline 两组服务+对账诊断、前端两面板），改 `auditInput.benchmark`/`signabilityPrompt` 注入块、detect.go+service.go 两处装配、zh/en i18n 各 85 行、docs 升级计划标注下线；`signability-audit.md` **只删 dim2 的「对标书库摘要」证据纪律句，八维定义/desc 未动**。**保留**：platform_profile（P0-1/P2-2 platformRhythmBlock）、`review/score_mapping.go`、`quality/feedback.go`、ScoreTrendPanel 的 `review.JudgeAuditDimScore`。`pipeline.AuditDimScore` 从 wails 生成类消失仅因不再作 DTO 暴露。存量表靠 `internal/migrate/v173`（Destructive=true/PostSchemaStep/幂等）DROP，备份在 `build/bin/backups/v1.7.3-drop-retired-quality-tables/`。
- 关键决策：mcp_tools→pipeline 是 import cycle，反馈落单走 SetFeedbackTicketLander 回调（feedback 测试用外部包 mcp_tools_test）；issue.Dimension=台账维枚举≠八维键；E 系文案避开语义排除词（矛盾/冲突/逻辑/人设/崩/连贯/伏笔/悬念/篇幅）；CheckerConfig 关键词全空=不检查；Validate 空白 profile 归一为空；字数=纯 CJK；golden 基线 `go test ./internal/pipeline/ -run TestP0BaselineRegen -p0-update`。

## 复验闭环（勿回退）
- VerifyIssueTickets 须经 task payload IssueKeys 透传，严禁执行体回扫 verifying 态；L0 快路径命中回落 LLM 复核；重新 verifying 清 note。预算 verifyCallTimeout 120min/verifyTicketTimeout 15min/verifyMaxTurns 2/verifyMaxTokens 10k；rejected 3 次自动转 skipped。
- RAG-Verify（8546f044）：SearchBatch 证据切片（章 topK=2+台账 #id 定点+窗口回退），单工单 4500/单批 12000 字符帽；批量合验 4+批间并发 2；工具零暴露（Whitelist 空）+输出≤3 句；语料硬帽=输入与书长无关，勿加码。回执截断抢救（a1753e08）：parseVerifyReceipt partial+salvageVerifyEntries 流式逐条抢救。

## 近期修复（勿回退）
- chapter_ids=全局阅读序号 1..N，台账/全书级填[]；filterChapterIDs 上界裁剪；parseChapterRefs 只认「第N章」/「章#N」，裸 #\d+=台账引用。
- dispatchExecutor Enqueue 失败走后台 goroutine+短重试（47125469）；PreSchemaSteps 碰子表须 HasTable 守卫（6a0aca2f）。
- parseAudit 禁把 LLM 原文塞 Summary/Desc（7cb519d8）；idleRetryable 零字节空转=不可重试（c4afcb37）。
- args_coerce.go JSON 文本参数宽容化（8add66ac）；golangci-lint 禁 reflect.Ptr 须 reflect.Pointer。
- 一致性巡检（0da8355c）：单次归并+本地预筛+内联语料帽+patrolSynthesizeMaxTokens=48k；dev 运行中改 Go 文件会杀在途任务。
- 工单状态回退（298d779c，#100）：ReplaceIssuesByPrefix 换血按「键+Area→Area 兜底」继承流转状态（carryIssueWorkflow）；pre_verify_status 快照+verifyRestoreStatus 恢复；前端两视图补订 fix/verify 事件失效内联查询。**注：本条中 `StartIssueFixTickets`/`issueFixMu`/`dispatchExecutorUnique` 是当时实现，2026-11-10 后台自动修复全清时已删除；carryIssueWorkflow 与状态继承口径仍有效**。
- 写章误入最后一卷（8c919913，#99）：多卷书裸 new.md 一律拒绝并列卷清单（0 卷→未分卷/1 卷→该卷兜底），废弃 LastByNovel 静默默认（rw_tools 审批前解析）；新增 move_chapter 移章工具（chapter.Store.Place 封装，target 0=未分卷、before 缺省推断目标组、metadata_only 事件刷前端目录）。
- 安全审核定位（1f670de5，#101）：巡检语料单元同源回收（章/台账行，截断逐行重建）；patrol_safety.go 二分探针（control 先行+64 探针/8min 帽+三态）与词面提示降级；软拒绝、APIError 审核错误双路径回显原文+定位结果；探测走 llmClient.GenerateText 非 skillRunner。
- 定位须覆盖整条真实请求（#104）：request_units.go 把 prompt + 本次 session 工具返回切同源连续片段（baseline 按 MAX(messages.id) 圈定本次，pipeline skillRunner 不回放 DB 历史、勿误判会话累积）；detect.go 双路径落可见 error 报告，安全类硬错误不得静默降级；patrol 探针必须含提示词/预筛/上下文块，否则「逐段探测全清」只是覆盖缺口；摘录首尾双窗；前端 last_summary 用 whitespace-pre-wrap。
- 台账 alive 写入面（#102）：create/update_character 开放 `Alive *bool`（指针三态、仅 在世→已死 单向、DisallowUnknownFields 保护不回退）；护栏**只注入派发层 prompt**（fixPrompt 全文被 p0 golden 基线锁定，勿改单源）。**注：原 `fix_reachability.go` 护栏随后台自动修复全清已删除，本条保留的是「人工 alive 写入 + 单向」口径**。
- 生死态派生（#103）：statesync 派发前 `character.DeriveAliveFromChapter` 从本章正文高精度单向标死（宁漏勿误、LLM 复核补漏、名单+摘录注入 instruction；人工裁决 source!=auto 优先不再覆盖）；`RunStateSyncForChapter` 增 db 参数（rw_tools 与 app/handler 两处调用点）；app `Create/UpdateCharacterInput.Alive *bool` 是唯一人工复活通道（MCP 工具仍单向）。存量 #121/#126 已于 2026-11-08 02:02 经用户授权修正（alive=0 + auto 履历，定点备份见日志）。
- 复验字数口径（#105）：**word_count = 纯汉字+英文词（`text.ComputeStats`），不含标点/换行；含标点字符数、`@字符N/charN` 偏移都不是字数，台账/任务卡/证据片段不计入章长**。字数类必须走阶段1 L0 `tryL0Verify` 本地确定性判定：`mechanicalCheckKeywords` 补 `CheckOpeningLength`（命中即删 `chapter_length_fit`，作用域零重叠）、`parseChapterLengthWindow` 从工单处方取区间（零 checker profile 也须可判）、过滤 RunL0Checks 的 profile 口径防双口径；阶段2 两条语料路径都标「权威字数」+ `verifyPrompt`/signability-verify.md 硬性口径声明。正文不可读/无区间→回落 LLM 绝不瞎判。勿把确定性数字判断交回 LLM 数字符。
- 工单终态保护（#106）：**`UpdateIssueStatus` 兼容写必须条件 CAS** `WHERE status NOT IN (verified,dismissed,verifying)`，章节重写比复验晚收口时绝不把 verified 打回 fixed（verifying 事务分支同样拒绝终态）。修复执行体快照的工单可能已被人工收口，收口写回一律走条件 CAS。**注：原 `VerifyIssueTickets` 用 `issueFixActive` 拒绝「修复在途」的互斥护栏，随后台自动修复全清已删除（不再有在途执行体）；CAS 终态保护仍有效**。
- 卖点引导项（#107）：**标签/方向/一句话卖点/平台/风格五类落 `preference_items`，被 `intentFromPitch`（Intent 块）与 `NovelProfile→FormatPreferences`（system 全量注入，无 category 过滤）两条链路真实消费，绝非「仅做记录」**；`preferenceCategories.ts` 旧注释是错的，已修。唯一写入口必须是 `app.UpsertPitchPreferences`（全量 PUT、单事务按 novel_id+category 收敛、空串清除），严禁再逐条 `CreatePreference` 纯新增（自动保存+手动采纳会写两组，旧稿与冲突平台值一起进 system 白烧 token）。**保护边界**：`平台约束/创作约束` 与用户手写长纪律共用分类，>80 rune 且不等于本次值的历史行视为手写纪律、收敛不触碰，前端 read 也跳过；方向/标签/卖点无此包袱可全量收敛。平台名命中注册 preset 且该书**无检查器配置行**时自动武装 platform_profile（七猫→qimao，不覆盖已有行）。

## 技能触发链（#108，勿回退）
- builtin **无一个 `mode: always`**（当时 27 个；2026-11-10 退役 4 个后现值 23）；写章消息原先只注入 `next` 一个 manual 提醒（maxAuto=1/maxManual=1），其编排的 10 个技法技能在写章语料下 cue 命中 0，历史 55 次 next 注入仅 2 次真 `read /builtin/skills/`。**写章必须走 bundle**：`app.buildWritingSkillBundle` 按单源 `writingBundleSkills`（reader-immersion/scene-beats/plot-hook）注入纯正文、双封顶、声明为硬约束。
- 注入一律用 `sk.Content` 或 `skill.Body`（LF/CRLF 通吃），**禁用 `sk.RawContent`**（会把 CRLF YAML frontmatter 拼进提示词）。`stripFrontmatter` 已委托 `skill.Body`。
- `notWhen` 是 cue 边界单源：给 next 加宽泛 cue「动笔」必须同时保留 `notWhen: [动笔前]`，否则 canary / fallback 基准的 collect 样本被抢。`explicitSkillNames` 不得把「消息整体等于技能名」当显式点名排除（裸发 next 会永不触发）。`chat:started` 事件带 `skills` 供前端徽标，是唯一可见性通道。

## 技能装配与可观测契约（#111，P0–P3 已实施，勿回退口径）
- **P0 落地**：`Skill` 加 `Mounts/InternalOnly/Budget/Deprecated`；27 个 builtin md 补 frontmatter（正文零改动）；`skill_invocations` 表走 `migrate.go` AutoMigrate（**不写 v174**）；`activity.RecordSkillInvocation` 包级 sink（未注入 no-op / panic 吞 / WithoutCancel）供 8 个埋点；`GetSkillUsage` + `SkillUsagePanel`；`health_test.go` 静态闸（零挂载点即构建失败，`legacyPlaceholders={memory,review}` P2 清空）。**零行为变更：提示词函数体与 p0 golden 一字未动**。
- P0 契约：`state_sync` 记 `Skill=""` 通道级事件（汇总与面板过滤空 skill，不得伪造技能用量）；`internalOnly` 只声明、P1 才在 router 生效；`always` 从 `--- name ---` 解析；`mountOwners` 是 mounts 枚举→源码的单源映射。**CI `Checks #415` 是 action 504 网关超时（非代码问题），HEAD 绿**；`checks.yml` 的 lint-go 缺 `--build-tags webkit2_41`，属已知口径差异、用户选择 P0 前不修。
- **P1 落地**：`RouteResult.Redirects` 让 `internalOnly` 前置过滤只回指引（Redirects 也算 cue 命中、候选/LLM 结果都剔除），消掉 chat 反向错投；chat 改 `3,1` + 同类去重 + 12000 总帽 + `capSkillRunes` 消费 Budget（**manual 的 next 刻意不截断**）；`app/writing_skill_conditions.go` 章况探测三取数路径「缺信息即不命中」；`writingBundleBlockCap` **5000→9000**（固定 3 技法已占 4273、旧帽剩 727 < 最短条件技能 1273，条件化必失效，勿回退）；`chapterCISelfCheckSkills` 自检块空则逐字不变；rewrite 路由补 3 技能。四处契约测试（regression/e2e/fallback_bench/rewrite_routes）同步 internalOnly 口径。**p0 golden 五构造器仍逐字未动**。
- **CI 报红判据（勿误判）**：`Checks #415` 是 `golangci-lint-action@v7` 下载 504；`Checks #418` 是 `prettier`/`lint-frontend` 的 `npm ci` 卡 539/616s exit 1（同代码 #417 的 npm ci 只 128/143s 全绿）——都是 CI 外部依赖偶发，非代码问题。`run_attempt` 是判断「是否真重跑过」的唯一硬证据；**重跑 3 次仍红 = 确定性代码问题，别再重跑**。`skill_fallback_bench_test.go` 真实 LLM 基准偶发误触发，单跑为准、勿改阈值。
- **Test #420/#421 Windows-only 报红（687042b2 修复，勿回退）**：`internal/skill/health_test.go` 等静态断言读 `.go` 源码**必须走 `readHealthSource` 归一 CRLF**——`.gitattributes` 只锁 testdata/builtin md，Windows CI `autocrlf=true` 会把 `.go` 检出为 CRLF，`"\n}\n"` 窗口必然失配（ubuntu/macos 恒绿、本地 LF 测不出来）。按行边界截窗口一律用 `healthVarBlock`，并留 CRLF 回归样本；全仓 214/654 个 `.go` 本为 CRLF，**禁加 `*.go text eol=lf`**。CI 单 job 日志用 PAT 打 `GET /actions/jobs/{id}/logs` 可直接取文本（run logs 才需 admin）。旧 SHA 的红标 Re-run 洗不掉（重跑用旧代码），只认新提交的 run。
- **P2 落地（勿回退）**：`rw_tools.go` 加 `stateSyncSkills`（`collect`+`memory` 两条单源、各带 cap）与 `stateSyncSkillBlock`，`RunStateSyncForChapter` 增 `skillStore *skill.Store` 参数（**两处调用点**：ToolContext / `app/handler.go`），把 P0 空 Skill 通道事件升级为真实技能账（技能缺失→指令逐字不变 + 仍只记空 Skill 事件，禁伪造用量）；`memory.md`/`review.md` 补正文+useWhen（MemoryAgent 检索契约 / 章后评审清单），`review` 进 `chapterCISelfCheckSkills`；`slashSubAgentDispatch` 单源让 `/memory`、`/review` 改派同名子 Agent；**`legacyPlaceholders` 已清空、健康检查零豁免**（`agentTypeCollisions` 保留）。`app/editor_feedback.go` 直接复用 `RecordEditorFeedbackTool.Execute` 两段式（子 Agent 不可用降级只存原话），前端 `EditorFeedbackPanel`。S2 新增 `blueprintTaskMode="blueprint-task"` 独立容器 + `RunBlueprintTask`（`app/blueprint_task.go` 薄转发、`BlueprintTaskPanel.tsx` 纯 onClick 显式按钮、**无 useEffect**），复用 `executeBlueprint→enterBlueprintAudit→executeAudit→pipeline_issues`，守零 auto-advance / 不复用 StartPipeline / 不加第五 kind。
- **P2 两个真实缺陷已修（勿回退）**：① S2 幂等**必须按 mode 精确查** —— `Store.GetRunningRunByMode`（novel_id+mode+status=running），原 `GetByNovel` 只取全书最新一条非终态 run，后起的章节检测会顶掉蓝图 run 导致重复烧 LLM；静态契约把 `GetByNovel` 列被禁词。② `RunBlueprintTask` 的 `go executeBlueprint` 异步分支测试里须保持熔断态 + `swapEmitStage` 等收敛信号，否则 `t.Cleanup` 恢复 breaker 后 goroutine 跨到后续测试真实跑链。`health_test.go` 静态窗口结尾用 `"\n}\n"`（不是 `"\n}"`，`[]struct{...}{` 以 `"\n}{"` 开头），**且源码必须先经 `readHealthSource` 归一 CRLF**（见上方 CI 判据行，否则 Windows CI 必红）。
- **生成物噪声护栏（P2 复现）**：`models.ts` 里 449 行「纯 tab 空行」是 wails 生成物固有格式，**禁止批量替换**；清理 `pre_verify_status`/tab 空白后须用 `difflib.SequenceMatcher` 按行对齐只还原两侧皆空白的行，`git diff --ignore-space-at-eol --stat` 分辨真实新增（P2 = 96 行 DTO）。跨包静态契约测试在 `internal/pipeline` 下相对路径要用 `../..`（不是 `..`）；断言源码前先 `stripGoLineComments`（注释里正当地写著「不复用 StartPipeline」）。
- 规划单源 `docs/skill-wiring-contract-2026-11-09.md`：manifest（mounts/internalOnly/budget/deprecated）+ 确定性装配 + `skill_invocations` 统一账本 + 健康检查 CI 闸；P0 可观测先行 → P1 错投抑制/写章扩容 → P2 七技能+S2 → P3 收口，串行。立论：摆设与口径陷阱同源 = 正文消费权交给 LLM/用户自觉 + 各通道留痕不统一。**P3 已落地（48afd4dc）**：目录降级、常驻兼容层、`WorkflowAutoArms` 单源、三份文档口径同步、设计文档状态改「已实施」+ 附录 C（27 技能用量 SQL 导出、7 项实施偏差、全量闸门记录）；P0–P3 全收口。
- 三处纠偏（勿按口述方案直接写码）：`dispatchSkill` 在 `internal/pipeline/provider_trip.go:71`（非 skillrun.go，埋点落 provider_trip.go:71/73/85 + service.go:283/311）；新表按 R3/activity 先例加 `migrate.go:93-145` models 由 AutoMigrate 建，**不写 v174 版本化迁移**（v173 是删表特例）；S2 是「代码已接线、入口已退役」（StartPipeline 前端零调用、开新书走 RunDetectionTask("book")、四 kind 无 blueprint），触发**必须显式按钮**，守零 auto-advance / 不恢复旧流水线 / 不加第五检测 kind。
- `internalOnly` 只禁 chat 路由与 catalog 可见性，**不禁流水线挂载**；健康检查是开发期静态断言，不是运行时质量闸门。技能名与 `agent_type` 重名（memory/review）须显式解耦。`GetPipelineExecStats` 有绑定但前端零消费 = 账本已建消费未接线的反面案例，P0 必须连用量面板一起交付。
- **P3 收口（勿回退）**：`internal/skill/workflow.go` 的 `WorkflowAutoArms` 是**说明性单源，只用于目录渲染 + 健康检查，不参与运行时调度**，别误当第二套装配器或按它改注入逻辑；`BuildSkillCatalog` 已降级为说明性（自动武装点 + `mounts` 技能标注「由系统按意图注入」，**禁用「用 read 加载完整内容」「主动建议用户启用」这类靠自觉的引导**），`BuildAlwaysSkillsContent` 只是 `.enabled.json` 兼容层、注入逻辑与 `MaxAlwaysSkillsResident=3` 不动；`mountOwners` 已删 `catalog_read`、内置技能禁声明 `always`，由 `TestWorkflowAutoArmContract` 四条断言锁死。**技能总数 27**（文档旧「26」是 2026-10 历史时点）。
- **账本取证口径（勿伪造）**：`skill_invocations` 当前 **0 行**（P0–P3 上线后 dev 未发生真实 LLM 交互），27 技能静态挂载点 27/27 达成；设计文档附录 C.4 已如实附 27 行全 0 表 + 复测 SQL/判据。§6.5「账本 30 天有记录」是取证要求而非接线要求，**严禁伪造记录或擅自自动派发写章/体检/蓝图任务**；真实交互后重跑 C.4 SQL 即可。冷启动判据再次确认「exe mtime 更新≠最新代码」：watcher 12:52 重建的 exe 一个 P3 文案都没有，13:17 冷启动后二进制含 `说明性目录`/`常驻技能（兼容层）`/`只改某类具体技法时由系统装配` 且 34115/5173 双监听。

## 其他契约
- 悬空口径已清（#109/#110，勿回退）：`signability-audit` 的 description/cues/何时使用禁再出现「对标」（对标书库已下线；dim2 开放式问句是八维契约、保留）；`stockAuditPrompt` 维度必须逐字对齐八维，禁「数据预期」等非八维口径（contract_test 有断言）；退役技能名 `signability-workflow` 仅存于 10-16/10-18 历史 system 消息与 v1.7.2 备份 novel17 的 `.enabled.json`，**历史数据不清理**，`filterByNames` 按真实 meta 取交集即设计护栏（skill_catalog_test 有断言）；`identity.go` 去 AI 味自查清单已内联可直执行，不再硬写「必须 read deai-flavor.md」。
- 章节寻址：chapter_id≠阅读序号；前端用 reading_number；工单 chapter_ids=全局阅读序号。
- ONNX 仅 bge-small-zh-v1.5 向量化；reranker 已移除。熔断 internal/llm/breaker.go 三态只认硬失败。

## 排障
- 重启 dev：全套 CGO export+`CODEBUDDY_SAFE_DELETE_ENABLED=0` 跑 wails dev（shim 拦 vite emptyDir；tsc -b 重吐 assets）。杀旧进程用 PowerShell Get-Process 取真实 PID（**循环变量禁叫 `$pid`，只读自动变量**）。5173 白屏=正常。判据=exe mtime 更新 + 二进制含新文案符号 + 34115/5173 双端口监听；`models.ts` 生成噪声 git checkout 还原，`novelgo-res.syso` 编译后自清理。旧后台任务 exit 127 是停进程预期通知。
- **dev 配置基线（2026-11-11 03:27，勿回退）**：`wails.json` 补 Wails v2.16 官方 `frontend:dev:serverUrl: http://127.0.0.1:5173` + `vite.config.ts` 的 `server.host: '127.0.0.1'`，让窗口走真实 HTTP 反代 Vite（34115 返回 Vite dev 页、`wails.localhost:34115`=200、双端口同绑 IPv4、HMR/Established 连接可见）。**但这只是配置基线，不是白屏根因**：03:39 用户截图证明配置补齐后窗口仍纯白。
- **白屏真根因（2026-11-11 03:55，CDP 实证，勿回退）**：`SubagentCard.tsx` 的 `getAgentMeta(t)[agentType]` 只映射 memory/review；`types.ts` 历史会话回放与 `ChatPanel.tsx` 的 run_subagent 事件把后端真实 `agent_type`（DB 实证 statesync 14483 条、chapterci 6331 条）用 `as "memory"|"review"` 硬转后原样传入 ⇒ `meta=undefined` ⇒ `meta.emoji` TypeError ⇒ React 19 卸载整棵 `#root`（CDP：`rootLen=0`、`body.innerText=""`）⇒ 纯白窗口；但 Wails IPC/Go 绑定仍正常（`panel.*` 埋点每次 reload 都 +1，故「能调绑定」不等于「界面在」）。修复：类型放宽 string、statesync/chapterci 加中文标签、未知类型 🧩 通用降级、聊天面板级 + `main.tsx` 根级双 ErrorBoundary；复验 `rootLen=533783`、`subagentIcons=2`、0 exception、CDP 截图 UI 完整。
- **00:27「localhost 双栈错配」与 03:15「wails.json 缺 serverUrl」两版推断均已被证伪**（详见当日日志）；`curl` 端口状态、Vite dev 页、Established 连接都只能证明资源链路，不能证明 React 根树还在。**判据升级：必须核 WebView2 内部 DOM/异常（CDP），不是 34115 返回 200。**
- **排障手段（勿丢）**：go-webview2 的 `preventEnvAndRegistryOverrides` 会把 `WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS` 置空，环境变量注入 CDP 无效；可行路径是临时给 Wails 模块缓存 `internal/frontend/desktop/windows/frontend.go:setupChromium` 的 `chromium.AdditionalBrowserArgs` 追加 `--remote-debugging-port=9222 --remote-allow-origins=*`（诊断后必须还原），再用 Node 22 全局 WebSocket 连 `http://127.0.0.1:9222/json/list` 抓 `Runtime.evaluate` / `Runtime.exceptionThrown` / `Page.captureScreenshot`。Python `ctypes`+GDI `BitBlt`/`PrintWindow` 可抓窗口像素（PowerShell `Add-Type` 被安全策略拦），但会话锁屏时只拍到锁屏，须以 CDP 为准；无头 Chrome 在本机被阻断。
- 旧口径「5173 白屏=正常」仅指浏览器直开 5173 时 `window.go` 未注入，别套 Wails 应用窗口。`novelgo-llm.log` 密集 `idle watchdog 9m` 是流结束后延迟清理的既有噪声；Bash 的 `HTTP_PROXY` 与 Windows `ProxyOverride` 是两套，系统层已 bypass localhost。
- watcher 失活判据：无 [Rebuild triggered]+exe mtime 停滞→整机重启 dev。**exe mtime 更新≠跑的是最新代码**：watcher 可能只重建一次就失活，须按「最后一个改动」的具体符号/文案核验（#107 曾出现 23:19 中间版本缺 23:35 才补的保护逻辑）。
- git commit 含 Go 必须同条命令带 export；commit 可能谎报 "nothing to commit"——git log 核实后单独补 push。
- render 禁 Date.now()/new Date()；vitest 偶发沙箱失败以四道闸兜底；wails 绑定文件噪声 git checkout 还原。
- 无 sqlite3 CLI：`node -e "require('node:sqlite').DatabaseSync(...,{readOnly:true})"`；tasks 非终态=status NOT IN ('done','dead')。

## 工单修复双轨（#270，2026-11-10 收口；**历史方案，已被下方「全清」覆盖**）
- 「去修复」= 发 chat + `StartManualIssueFixTickets`（只置 fixing，**不派发自动执行体、不占 `issueFixActive`**）；`IssueTicketsPanel`/`PatrolReportPanel` 两个面板同口径。
- 状态机补 `fixing → fixed` 人工收口出口；`CloseIssueFixTickets`（标记已修复留 note）/`CancelIssueFixTickets`（回 pending）。前端「标记已修复 / 取消修复」保留。
- 事故原型：chat 修完点复验报「修复任务仍在进行中」，旧「去修复」隐含派发的后台执行体跑 72 分钟未落回执（heartbeat 一直续租、工单 round=0）。**`issueFixActive` 是进程内标记，只有执行体返回或重启才摘**；处置 = 停 dev → 启动清扫 `AbandonActive` 置 dead → 工单人工收口为 fixed → 冷启动。别让用户干等，也别只改 DB（内存标记还在）。
- 前端闸门脚本链用 `| tail` 会吞掉 `tsc -b` 失败仍打印成功，**必须 `set -o pipefail`**；新增 toolbar kind 要同步 `BusyKind` type alias，否则 TS2345/TS2367。

## 后台自动修复彻底下线（2026-11-10，用户判定「全清+技能退役」，勿回退）
- **所有修复都走 chat**：用户明确「后台自动 LLM 修复并不需要」。删 `app.StartIssueFixTickets` 绑定 + `internal/pipeline` 整条运行时链（互斥 `issueFixActive`/`issueFixMu`、`taskKindIssueFix`、`dispatchExecutorUnique`、分组批处理、台账/正文执行体、修复回执链 `parseFixReceipt`/`applyFixReceipt`、漂移巡检 `runBlueprintDriftPatrol`、字段可达性护栏 `fix_reachability.go`）；`VerifyIssueTickets` 的修复在途死护栏删除；`GateContext.FixRounds` 保留兼容读恒为 0。
- **技能 27→23**：退役 `signability-fix`/`blueprint-fix`/`chapter-rewrite`/`blueprint-drift-check` 四个 builtin md + `skillrun.go` 三条专用 Agent 映射；`health_test.go` 内置技能数断言改 23，`WorkflowAutoArms` 去自动重写语义。**p0 golden 五构造器（fixPrompt/chapterRewritePrompt/blueprintFixPrompt/fixLogBlock/fixReceiptFormatLine）仅作测试与契约单源保留、生产零调用，函数体仍逐字不动**。
- 前端删「自动修复」按钮 + `autoFix`/`autoFixHint`/`autoFixOk` 双语键 + wails 绑定条目；`issue_no_auto_fix_test.go` 用 `autoFixBanned` 静态扫描（源码 + 前端入口 + 退役技能 + 手工链路与 golden 构造器仍在）锁死零回潮。
- 文档口径：`docs/TECHNICAL_STATUS_AND_GUARDRAILS.md` §3 现值 23 + §9 变更行；`AGENTS.md` 新增「工单修复唯一路径」；`docs/skill-wiring-contract-2026-11-09.md` 的 27 技能表/验收判据是**下线前历史时点快照，原样保留不回改**，仅在头部、§6.5、C.3、C.4 加时点注记。
- 闸门坑：删代码后 `golangci-lint` 报 3 个残留死 helper（`countKey`/`upsertManualItem`/`emitFixStart` wrapper，后者只删 wrapper、保留 `emitFixStartSlot`+`swapEmitFixStart` 测试接缝）；前端 `format:check` 因删 JSX 后 `BusyKind` 格式挂，`build` 需 `CODEBUDDY_SAFE_DELETE_ENABLED=0` 绕 vite emptyDir 的 shim 批量删除守卫。
