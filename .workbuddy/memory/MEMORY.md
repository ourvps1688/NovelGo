# NovelGo 长期备忘（勿回退；细节见 .workbuddy/memory/YYYY-MM-DD.md）

## 环境 / 闸门 / 提交
- 派生 sigpanic/goink（AGPL-3.0）：README/NOTICE/About 保留 "Based on Goink by 响滩 (sigpanic)"、"This is a modified version of Goink..."。模块 github.com/novelgo/novelgo；dev DB=build/bin/novel-agent.db；origin=ourvps1688/NovelGo。
- CGO：PATH += Go/bin、tools/mingw64/bin、go/bin；CGO_ENABLED=1；CGO_CFLAGS=-IC:/tools/sqlite3-headers；LOCALAPPDATA/GOCACHE/GOLANGCI_LINT_CACHE=...\go-build。后端四闸 `go build/vet/test -tags webkit2_41 ./... -skip '^TestRewriteVoiceABE2E$' -count=1` + `golangci-lint run --build-tags webkit2_41 ./...`。
- 前端**六闸**：format:check / i18n:check / theme:check / lint / build / vitest。前五用 `npm --prefix frontend`，**test 必须 `cd frontend && ./node_modules/.bin/vitest run --maxWorkers=2`**（根目录 npm --prefix 假红）。set -o pipefail；vite build `CODEBUDDY_SAFE_DELETE_ENABLED=0`；i18n 禁 JSX 中文硬编码；theme:check 禁 Tailwind 调色板色，一律语义类 text-primary/text-destructive/text-muted-foreground。lint 167 warning/0 error 是基线。
- push `GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；代码 + 单独 chore(memory) 双提交；pre-commit 需完整 Go 环境、禁 --no-verify；`rev-list --left-right --count origin/master...HEAD`=0 0；Wails 生成物噪声 `git checkout --`，新绑定行手工补回。纯 memory 提交触发 3 条 CI，代码提交 4 条。
- 无 sqlite3 CLI→node:sqlite；p0 golden `-run TestP0BaselineRegen -p0-update`；Windows 静态断言须归一 CRLF，禁加 `*.go text eol=lf`；CI 单平台红标先看 job 级结论（#415/#418/#435 外部偶发）。

## 总红线
- 检测四 kind 独立启动→发现项→人工确认→修复→显式复验；零 auto-advance/闸门/自动派发修复。禁 StartPipeline/PipelineWizard/SignabilityAuditView；S2 RunBlueprintTask 独立容器+显式按钮+无 useEffect。app 只 DTO+委托；internal 禁 import wails；DB=真相源。度量纯本地零上传。
- **所有修复都走 chat**：后台自动修复整链已删，技能 27→23，`issue_no_auto_fix_test.go` 静态锁；GateContext.FixRounds 恒 0；p0 golden 五构造器（fixPrompt/chapterRewritePrompt/blueprintFixPrompt/fixLogBlock/fixReceiptFormatLine）函数体逐字不动；`Finding.Status` 恒 pending。手工链 StartManualIssueFixTickets→Close/Cancel→VerifyIssueTickets；「去修复」=`useChatLaunchStore.getState().send(content)`（无 prefill/window event）。
- **数值勿回退**：`tensionActionCap=1.0` / `tensionShortRatioCap=0.4` / `structuralPeakAt=0.45`（#114 P3 真实语料重标定）；技法帽 2400/1500/9000；`openingSlopeMinRise=5.0`、`openingPressureMin=tensionFlatBelow`；七猫 1000–1500 字。
- 单源索引：signability-audit.md 八维 / featureKeys.ts / factory.ts / pipeline_issues 状态机；字数=`quality.ChapterWordsTarget`（platform_profile>platform_card>genre>default 2000）；word_count=纯汉字+英文词；处方=`quality.Prescription`+`AcceptanceMarker`；张力=按落盘正文现场 `BuildTensionCurve`，禁回退 DB 旧曲线。chapter_ids=阅读序号，chapter_id≠阅读序号。

## 里程碑（细节见每日日志）
- **#113** 章节检测只写 chapter_quality（无 issue_key）⇒闭环=卡片勾选→send 中央 chat→RunDetectionTask("chapter") 重检；`DetectionWorkflowClosure.test.tsx` 锁死。手动重检经 RecalcChapterL0FromDisk 按落盘正文重算 L0、不刷新 DB 曲线/不重复落 E 系工单。
- **#114** 处方/判定/摘要同源：FindingKeys=`scope|check|归一化 refs|序号`（禁含 ID/Desc/数字/Status）；finding_round 三态+RepeatCount、GuardRounds=3、PendingL1 沿用 PrevKeys；score_events.finding_keys 快照须**审批/落盘前**取，空快照禁反推「全部消掉」；skill_invocations 加 turn_id+finding_keys、SummarizeSkillInvocations 的 `Group("skill, channel")` 不动；llm_signal_daily 纯本地四信号；P3 独立事件 EvChapterDetectDone + `tryBeginDetect` LoadOrStore 防连点 + `invalidateChapterViews` 单源失效。
- **#115** 章节 L1 曾因后台空 `RunOptions{}` 缺 Model **从未真跑**（已由 `backgroundRunOptions()` 单源修复；失败/坏回执不再 upsertL1 覆盖旧结果）；chapter 超时 8min / `chapterCIMaxTurns=12`、超时发 ok=false/reason=timeout、取消静默；显式重判分 `app.RescoreChapter`（人工触发、绝不自动）；评分历史轴缺口=maybeScoreChapter 只对新建/full_replace 评分，局部改稿不补录；verdict fail/revise 是判官质量结论非程序错误。
- **#116** 写作治理：`quality.WritingConstraints()` 写前硬约束注入 novel_profile system（ChapterWords 之后、自由文本之前）；`openingSlopeOK(rise,peak)` 峰值/升幅双通路让冷开场达标；`dedupeOpeningFindings` 在 opening_hook 命中时抑制同轮 opening_slope；轮次保护三人工出口（localStorage 豁免/撤销/换思路 send）；`SummarizeSkillEffects` 纯只读聚合、前端如实标注「最近一轮近似」；写后自检按 finding 定向、零发现回落 4 技法；rewrite_method 假账本埋点已删并有回潮静态锁。技能正文禁另写判定数字。
- **#117** 只读调研 + P0 清理：孤儿口径修正——`pipeline:`/`detect:` 是设计内可回放子会话**绝不能删**，真孤儿仅 `sess_%`+空串（messages 19,388 / operation_log 15,740 / turn_commits 307）；已备份 `build/bin/novel-agent.db.bak-20261113`（417MiB），**主库零 DELETE**，清理 SQL 草案待二次确认。P0 代码：`text.TruncateRunes/ContainsAny` 单源（structure/plan_check.go 须用 textutil 别名）、`activity.DefaultStatDays=30`、`slash_command.go` 整块帽 break→continue + Debug 可观测、i18n Check 5 报告型孤儿 key（只 warn）、删双语各 10 个死文案、作废文档加头注。**修正勿再提**：DataDir 是活字段；l0RuleSweep/patrolPrompt/CountChunks/newServiceWithClient/AuditDimJudgeSources 是测试契约基座；set_auto 是协议兼容。「零生产引用」≠可删，须先查测试消费与跨包配置读取。
- 遗留约束：复验预算 120min/15min/2turns/10k、rejected 3 次→skipped；#106 CAS 状态；#109 对标书库下线（v173 DROP）；#107 UpsertPitchPreferences 唯一入口；#101/#104 安全审核双路径落可见 error；alive 三态仅 app 人工复活。

## dev
- 冷启动=全套 CGO export + `CODEBUDDY_SAFE_DELETE_ENABLED=0 wails dev`；**Go 改动不自动重载后端**，必须精确停进程冷启动；判据=exe mtime + `grep -a -c "<新符号>" novelgo-dev.exe` + 34115/5173 双 200 + FATAL=0（grep -c 无匹配本就输出 0，别加 `|| echo 0`）。exit 127=停进程预期信号。
- **dev 运行期间禁跑 go build / 删 novelgo-res.syso**（FATAL .rsrc merge failure）；wails watcher 会在 Go 文件变动时重生成 3 个绑定文件，须先停 dev → `git checkout --` → 手工补最小行 → 再跑闸门。
- 白屏真根因=SubagentCard `getAgentMeta` 未映射 statesync/chapterci 致 React19 卸载 #root（已修：类型放宽+🧩降级+双 ErrorBoundary）；浏览器直开 5173 白屏正常；render 禁 Date.now()/new Date()；ONNX 仅 bge-small-zh-v1.5。

## 当前状态 / 待办（2026-11-13 收口）
- 工作树干净、origin/master...HEAD=0 0、CI 全绿、dev 双 200。#116、#117 调研、#117 P0 清理、**#116 写作实测第一轮**均已完成；实测硬结论见 11-13 日志（活跃书 opening_slope 已 0 报、定向自检/效果面板零真实样本、22:55 写作卡在写前对齐）。
- **孤儿数据已清理（23:55 用户二次确认）**：先 `VACUUM INTO` 一致性备份 `build/bin/novel-agent.db.bak-20261113-2350`（sha256 `f06ab5bf…`），事务内删三表共 35,440 行（messages 19,388 / operation_log 15,745 / turn_commits 307），严格行数校验 + integrity_check=ok；VACUUM + WAL 截断后主库 418M→232M。`pipeline:`/`detect:` 子会话、活跃会话、按 novel_id 的 602 行孤儿全部保留。恢复=停 dev → 删 -wal/-shm → 备份覆盖主库 → 冷启动。
- **挂起**：①#116 真实写作闭环**第二轮**（新二进制 23:23 后零运行样本，需用户在 dev 点一次「重新检测本章」或「撰写下一章」再采集现场 L0/处方/自检定向/收口耗时/verdict）；②#117 P1 技能治理（写后自检 65.9% 字符压成按 check 的 300–500 rune 短处方、技能正文与 L0 双口径、chapter_ci_selfcheck 用 context.Background() 致 finding_keys 恒空效果面板测不到大头）；③#117 P2 架构（写后维护/评分装配双轨、主 chat MaxTurns=100 分档、前端三处轮询收口）；④#115 局部 patch 不评分（P1.5）、chat 直接输入修复 beat_gap 不自动注入处方。
