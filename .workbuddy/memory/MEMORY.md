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
- 工单状态回退（298d779c，#100）：ReplaceIssuesByPrefix 换血按「键+Area→Area 兜底」继承流转状态（carryIssueWorkflow）；StartIssueFixTickets 幂等容忍+issueFixMu 互斥排空循环+dispatchExecutorUnique 防孤儿化；pre_verify_status 快照+verifyRestoreStatus 恢复；前端两视图补订 fix/verify 事件失效内联查询。
- 写章误入最后一卷（8c919913，#99）：多卷书裸 new.md 一律拒绝并列卷清单（0 卷→未分卷/1 卷→该卷兜底），废弃 LastByNovel 静默默认（rw_tools 审批前解析）；新增 move_chapter 移章工具（chapter.Store.Place 封装，target 0=未分卷、before 缺省推断目标组、metadata_only 事件刷前端目录）。
- 安全审核定位（1f670de5，#101）：巡检语料单元同源回收（章/台账行，截断逐行重建）；patrol_safety.go 二分探针（control 先行+64 探针/8min 帽+三态）与词面提示降级；软拒绝、APIError 审核错误双路径回显原文+定位结果；探测走 llmClient.GenerateText 非 skillRunner。
- 定位须覆盖整条真实请求（#104）：request_units.go 把 prompt + 本次 session 工具返回切同源连续片段（baseline 按 MAX(messages.id) 圈定本次，pipeline skillRunner 不回放 DB 历史、勿误判会话累积）；detect.go 双路径落可见 error 报告，安全类硬错误不得静默降级；patrol 探针必须含提示词/预筛/上下文块，否则「逐段探测全清」只是覆盖缺口；摘录首尾双窗；前端 last_summary 用 whitespace-pre-wrap。
- 台账 alive 写入面（#102）：create/update_character 开放 `Alive *bool`（指针三态、仅 在世→已死 单向、DisallowUnknownFields 保护不回退）；fix_reachability.go 派发前字段可达性预检+护栏**只注入派发层 prompt**（fixPrompt 全文被 p0 golden 基线锁定，勿改单源）。
- 生死态派生（#103）：statesync 派发前 `character.DeriveAliveFromChapter` 从本章正文高精度单向标死（宁漏勿误、LLM 复核补漏、名单+摘录注入 instruction；人工裁决 source!=auto 优先不再覆盖）；`RunStateSyncForChapter` 增 db 参数（rw_tools 与 app/handler 两处调用点）；app `Create/UpdateCharacterInput.Alive *bool` 是唯一人工复活通道（MCP 工具仍单向）。存量 #121/#126 已于 2026-11-08 02:02 经用户授权修正（alive=0 + auto 履历，定点备份见日志）。
- 复验字数口径（#105）：**word_count = 纯汉字+英文词（`text.ComputeStats`），不含标点/换行；含标点字符数、`@字符N/charN` 偏移都不是字数，台账/任务卡/证据片段不计入章长**。字数类必须走阶段1 L0 `tryL0Verify` 本地确定性判定：`mechanicalCheckKeywords` 补 `CheckOpeningLength`（命中即删 `chapter_length_fit`，作用域零重叠）、`parseChapterLengthWindow` 从工单处方取区间（零 checker profile 也须可判）、过滤 RunL0Checks 的 profile 口径防双口径；阶段2 两条语料路径都标「权威字数」+ `verifyPrompt`/signability-verify.md 硬性口径声明。正文不可读/无区间→回落 LLM 绝不瞎判。勿把确定性数字判断交回 LLM 数字符。
- 工单终态保护（#106）：**`UpdateIssueStatus` 兼容写必须条件 CAS** `WHERE status NOT IN (verified,dismissed,verifying)`，章节重写比复验晚收口时绝不把 verified 打回 fixed（verifying 事务分支同样拒绝终态）；`VerifyIssueTickets` 在写状态/派发前按 run 用 `issueFixActive` 拒绝修复在途的复验（避免读到未定稿正文 + 半截 verifying）。修复执行体快照的工单可能已被人工收口，收口写回一律走条件 CAS。
- 卖点引导项（#107）：**标签/方向/一句话卖点/平台/风格五类落 `preference_items`，被 `intentFromPitch`（Intent 块）与 `NovelProfile→FormatPreferences`（system 全量注入，无 category 过滤）两条链路真实消费，绝非「仅做记录」**；`preferenceCategories.ts` 旧注释是错的，已修。唯一写入口必须是 `app.UpsertPitchPreferences`（全量 PUT、单事务按 novel_id+category 收敛、空串清除），严禁再逐条 `CreatePreference` 纯新增（自动保存+手动采纳会写两组，旧稿与冲突平台值一起进 system 白烧 token）。**保护边界**：`平台约束/创作约束` 与用户手写长纪律共用分类，>80 rune 且不等于本次值的历史行视为手写纪律、收敛不触碰，前端 read 也跳过；方向/标签/卖点无此包袱可全量收敛。平台名命中注册 preset 且该书**无检查器配置行**时自动武装 platform_profile（七猫→qimao，不覆盖已有行）。

## 技能触发链（#108，勿回退）
- 27 个 builtin **无一个 `mode: always`**；写章消息原先只注入 `next` 一个 manual 提醒（maxAuto=1/maxManual=1），其编排的 10 个技法技能在写章语料下 cue 命中 0，历史 55 次 next 注入仅 2 次真 `read /builtin/skills/`。**写章必须走 bundle**：`app.buildWritingSkillBundle` 按单源 `writingBundleSkills`（reader-immersion/scene-beats/plot-hook）注入纯正文、双封顶、声明为硬约束。
- 注入一律用 `sk.Content` 或 `skill.Body`（LF/CRLF 通吃），**禁用 `sk.RawContent`**（会把 CRLF YAML frontmatter 拼进提示词）。`stripFrontmatter` 已委托 `skill.Body`。
- `notWhen` 是 cue 边界单源：给 next 加宽泛 cue「动笔」必须同时保留 `notWhen: [动笔前]`，否则 canary / fallback 基准的 collect 样本被抢。`explicitSkillNames` 不得把「消息整体等于技能名」当显式点名排除（裸发 next 会永不触发）。`chat:started` 事件带 `skills` 供前端徽标，是唯一可见性通道。

## 其他契约
- 悬空口径已清（#109/#110，勿回退）：`signability-audit` 的 description/cues/何时使用禁再出现「对标」（对标书库已下线；dim2 开放式问句是八维契约、保留）；`stockAuditPrompt` 维度必须逐字对齐八维，禁「数据预期」等非八维口径（contract_test 有断言）；退役技能名 `signability-workflow` 仅存于 10-16/10-18 历史 system 消息与 v1.7.2 备份 novel17 的 `.enabled.json`，**历史数据不清理**，`filterByNames` 按真实 meta 取交集即设计护栏（skill_catalog_test 有断言）；`identity.go` 去 AI 味自查清单已内联可直执行，不再硬写「必须 read deai-flavor.md」。
- 章节寻址：chapter_id≠阅读序号；前端用 reading_number；工单 chapter_ids=全局阅读序号。
- ONNX 仅 bge-small-zh-v1.5 向量化；reranker 已移除。熔断 internal/llm/breaker.go 三态只认硬失败。

## 排障
- 重启 dev：全套 CGO export+`CODEBUDDY_SAFE_DELETE_ENABLED=0` 跑 wails dev（shim 拦 vite emptyDir；tsc -b 重吐 assets）。杀旧进程用 PowerShell Get-Process 取真实 PID（**循环变量禁叫 `$pid`，只读自动变量**）。5173 白屏=正常。判据=exe mtime 更新 + 二进制含新文案符号 + 34115/5173 双端口监听；`models.ts` 生成噪声 git checkout 还原，`novelgo-res.syso` 编译后自清理。旧后台任务 exit 127 是停进程预期通知。
- watcher 失活判据：无 [Rebuild triggered]+exe mtime 停滞→整机重启 dev。**exe mtime 更新≠跑的是最新代码**：watcher 可能只重建一次就失活，须按「最后一个改动」的具体符号/文案核验（#107 曾出现 23:19 中间版本缺 23:35 才补的保护逻辑）。
- git commit 含 Go 必须同条命令带 export；commit 可能谎报 "nothing to commit"——git log 核实后单独补 push。
- render 禁 Date.now()/new Date()；vitest 偶发沙箱失败以四道闸兜底；wails 绑定文件噪声 git checkout 还原。
- 无 sqlite3 CLI：`node -e "require('node:sqlite').DatabaseSync(...,{readOnly:true})"`；tasks 非终态=status NOT IN ('done','dead')。
