# NovelGo 项目长期备忘

## 项目性质
- NovelGo = sigpanic/goink（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0 + NOTICE 附加条款）的合规派生 fork。
- 硬性合规：必须保留 "Based on Goink by 响滩 (sigpanic) — https://github.com/sigpanic/goink" 与 "This is a modified version of Goink..." 声明（README/NOTICE/About 对话框），不得移除或暗示原作者背书。

## 关键约定
- 模块路径 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；小说元数据文件 `novelgo.md`。
- 品牌 mark：N+G 字母组合 monogram（用户指定，明确不要书的意象）；wails 生成类型名前缀 `novelgo_novelgo`。
- skills 仓库 `ourvps1688/NovelGo-skills`、主仓库 `ourvps1688/NovelGo`（已创建并推送，master/main 分支）。
- git 历史已 unshallow；`origin`=ourvps1688/NovelGo，`upstream`=sigpanic/goink。

## 签约级能力缺口（评估内置 skill 后的核心结论）
- 现有系统保证了"写得不差"（大纲先行+写后质检+状态维护+review agent+题材模板+AI味硬卡+LLM 6维评分门禁），但**完全没有任何一层保证"能签"**——全仓无 签约/编辑审稿口径/完本/追读/网文数据 概念（grep 证实 internal/ 与 frontend/src/ 均无）。
- review agent 仅查内部一致性（角色/情节/伏笔/读者认知/弧线），无平台编辑视角；review.ScoreChapter 6 维（字数/情节推进/情绪流爽点/人设一致/去AI味/禁忌）亦无签约维度。
- 12 内置 skill 仅承载"创作技法方法论"；题材/偏好层覆盖题材/开场抓手/商业化节奏。真正缺的三类"签约智能"：
  1. **签约/平台审稿口径**（编辑看什么、黄金三章过审、题材契合、人设记忆点、完本潜力、数据预期）——最高优先。
  2. **宏观故事蓝图/卷纲方法论**（立意→卷纲节奏地图→主支线编织→全局悬念→爽点地图→人物弧总览），现有仅 per-chapter 大纲，无蓝图方法论。
  3. **世界观/金手指/力量体系设计方法论**（自洽升级体系/金手指规则/卖点化），设定仅存储无设计法。
- 补充路径：手写"知识/方法论型"skill（pattern 蒸馏系统只能从本书正文归纳 struct/mental，**产不出外部平台知识**，故签约向 skill 必须手写）；可再用 pattern 提取器从用户提供的签约级范本蒸馏风格/套路 skill 互补。新 skill 建议 story-blueprint/worldbuilding-design 设 auto、signability-audit 设 manual，并充实空壳 `next` 让其引用蓝图+签约自检（纯内容改动）。

## 题材/偏好层（评估内置 skill 时易被漏看）
- 题材预设不在 12 个内置 skill 里，而在前端偏好层：`frontend/src/components/preference/genreTemplates.ts`（30+ 经语料校准的题材文风卡，覆盖 通用/男频/女频言情/悬疑/历史脑洞/幻想科幻/种田日常 7 组；每张含 开场抓手/冲突发动机/章尾钩子/禁止漂移 + 爽点密度/情绪曲线/字数门槛）。
- `GenreConstraintPicker.tsx` 把所选题材写为本书偏好（category=创作约束），经既有偏好注入管线每轮常驻生效，无需后端改动。
- `internal/agentcfg/identity.go` 已内置题材感知规则（爽文≥3500字/章、悬疑≥3000、爽点密度、爽文禁虐主/有仇不过夜、情绪流）。小说 `Genre` 字段由 `novel_state.go` 注入上下文。
- 结论：评估"签约向"覆盖度时，题材/赛道/平台调性 + 开场抓手 + 商业化节奏已由该层覆盖；12 内置 skill 仅承载"创作技法方法论"，缺的是 worldbuilding/金手指/力量体系设计 与 宏观大纲/卷纲 两套方法论 skill。

## UI 方向
- 用户认为当前前端"丑"，但**现阶段只做纯皮肤换肤**：仅改 `frontend/src/index.css` 颜色 token，不动组件/布局/功能入口。
- 审美参考 AI-NWA（ExplosiveCoderflome/AI-Novel-Writing-Assistant）。**已按用户提供的 AI-NWA 真实截图对版**：极简黑白灰"墨色"风（Notion/Vercel 挂）——亮色 primary 近黑 `oklch(0.24 0.005 260)`、暗色反转近白；白卡片 + 浅灰页底 + 中性细边框；**不是**彩色强调。语义色（tag/tool/danger/success）保留原样。
- 沙箱 Bash 外网被拦；AI-NWA 视觉以用户贴图为准（微信截图_20260813220038.png）。

## 环境注意
- **前端提交前的 CI 硬闸门（本地必跑）**：`npx prettier --check "src/**/*.{ts,tsx}"` 与 `npm run i18n:check`（后者会扫描源文件硬编码中文，中文文案必须进 i18n locales）。仅跑 tsc/vite/vitest 不够——Checks #41 曾因此报红。eslint CI 不限 warnings 数（0 error 即过）。
- **wailsjs 绑定以 wails 自动生成产物为准（convertValues 约定）**：`wails dev`/build 每次重新生成绑定；v2.16 生成器给含嵌套结构体的类（如 ChatInput、SaveAuthorAliasesInput、GetAuthorAliasesResult）加 `convertValues` 方法，**对象字面量直接传参会 TS2345**。调用点必须用 `app.X.createFrom({...})` 构造实例，且 models 导入需为值导入（非 `import type`）才能用 createFrom。手写绑定仅为临时占位，dev 拉起后以重新生成结果为准入库。
- **沙箱已装 MinGW/gcc**：winlibs GCC 16.2.0 (UCRT,x86_64) 在 `C:/tools/mingw64/bin`；`go` 在 `C:/Program Files/Go/bin`、`wails` 在 `C:/Users/Administrator/go/bin`（均已在 PATH）。wails dev 已跑通（见每日日志）。
- **Windows 构建前置**：CGO 编译需 `sqlite3.h` → 设 `CGO_CFLAGS=-IC:/tools/sqlite3-headers`（该目录由 mattn/go-sqlite3@v1.14.44 的 `sqlite3-binding.h` 复制而来，详 cross-platform-build.md §7）。构建命令须显式 `export PATH="/c/tools/mingw64/bin:$PATH"`（新 shell 不继承 User PATH）。
- 本地 ONNX 向量检索为可选能力，缺 `onnxruntime.dll`+`build/bin/models/vocab.txt` 时仅该功能不可用，不影响主程序。
- 仓库 autocrlf=true 检出为 CRLF，`gofmt -l` 会全量误报，勿据此改格式。
- **前端改动需重建 dist 才在 webview 生效**：`wails dev` 启动时跑 `frontend:build`（npm run build → tsc -b && vite build）生成 `frontend/dist`，webview 加载的是 dist 构建产物（带 hash 的固定文件名，如 `index-*.js`），**不是** Vite 实时源码；后台 `npm run dev` 仅服务浏览器预览。故改前端源码后，必须 `npm run build` 并重启 `wails dev`（旧的 webview 不会热更到新 dist，表现为"界面没刷新/没加载"）。后台 vite 实时 HMR 只影响独立浏览器预览，不影响 wails 桌面窗口。
- **wails 版本自动对齐**：本机 wails CLI = v2.16.0（`C:/Users/Administrator/go/bin/wails`，Sep 19 安装）。`wails dev` 启动会把 go.mod 的 wails 版本自动改写为 CLI 版本。仓库 baseline 是 v2.12.0，但工作区会不断被 dev 改回 v2.16.0——回退 2.12 无意义。**结论：正式把项目锁定到 v2.16.0**（go mod tidy 后提交 go.mod/go.sum），该升级对业务代码零影响（仅用 pkg/runtime 基础 API，v2.12→v2.16 签名未变，go build 通过）。
- **Git 推送凭证（收口用）**：`origin` = `git@github.com:ourvps1688/NovelGo.git`（SSH）。SSH config 在 `C:\Users\Administrator\.ssh\config`（Host github.com → ssh.github.com:443，IdentityFile `id_ed25519_novel2all`，IdentitiesOnly）。PAT 在仓库根 `github_pat.txt`，**已被 .gitignore 忽略，绝不入库**。推送/拉取走 SSH 即可，命令显式 `GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`。预提交钩子含 gofmt + golangci-lint，提交前若报 gofmt 先 `gofmt -w` 对应文件再提交。

## 流水线状态机（PR5 已落地 2026-10-20，commit 6ce39e7e）
- `internal/pipeline/orchestrator.go` 为唯一流转源（`Orchestrator.Advance`）；`service.go` 的 `ResumePipeline` 已退化为薄壳（`AppendGateDecision` + `Advance`）。
- S6 完本终检 + G终（`finish`→`finished`、`fix`→S5 复评、`abort`→aborted）已跑通；`auto_run` 一键连跑至首个闸门/安全检查点停（G1、G终仍人工确认，受 `autoFixCap=3` 防无限循环）。
- S0 入口自动判定已接入 `StartPipeline`（无章→S1 / 有章无台账→S0 / 已就绪→S3）。**S0 对齐补丁（2026-10-20，commit 待推送）**：采用「方案 A + 全量对齐按钮」——进 S3 同时后台 `executeS0Align`（建默认卷挂 `volume_id` + 批量 `statesync` 回填台账，避免导入即烧额度），并叠加按需「全量对齐」按钮（`ControlPipeline("align_full")` → `runFullAlign`：对齐 + `runStockAudit` 现状体检 + `emitStockAudit`）。`pipeline` 与 `mcp_tools`/`agent` 零环，`SetStateSyncRunner` bridge 由 `app/handler.go` 注入（`agent.RunSubAgent` 闭包）。详见 plan §6.5。
- **启动恢复机制（2026-10-20）**：pipeline 执行体是内存 goroutine + DB 状态，应用重启会产生「DB=running 但无人执行」的僵尸运行。`RecoverInterruptedRuns()`（OnStartup 调用，幂等）把 running 僵尸置 paused + `GateContext.Interrupted` 标记；`ControlPipeline("resume")` 检测到标记后按阶段重派发执行体（S4/S4b→executeAudit、S5→executeFix、S6→executeFinalAudit，均幂等重入有状态守卫），派发后清标记。`set_auto` 只翻 DB 标志、永不派发执行体——不要指望一键连跑能救活僵尸运行。S5 前端 `FixProgressView` 初始态为 idle（「等待修复启动」），不是 done。
- **S5 分批修复 + Dispatching（2026-10-20，fbebe631）**：`executeFix` 按 `planFixBatches` 分批（P0 每批 1 条、P1 每批 2 条），每批独立会话 + 独立 12 turns 预算（防止 MaxTurns 耗尽整轮失败）；每批成功落 `GateContext.FixStates[issueID]="fixed"` 断点，恢复/续跑跳过已修项，全部完成后 `finishFixAndReaudit` 复评（复评才是修复到位的裁决）。体检/修复/终检失败一律落 paused（不再停 running）；`GateContext.Dispatching` 标记执行体存活，resume → `redispatchIfNeeded` 据此区分软暂停与需重派发；用户暂停在批次边界守卫生效。fix_start 事件带 batch_index/batch_total/done_ids，前端显示批次进度 + 已修复徽章 + 恢复提示。
