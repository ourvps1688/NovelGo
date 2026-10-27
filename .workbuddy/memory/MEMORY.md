# NovelGo 项目长期备忘

## 项目性质
- sigpanic/goink 合规派生 fork（Go 1.25 + Wails v2 + React 19 + SQLite/ONNX，AGPL-3.0）。README/NOTICE/About 必须保留 "Based on Goink by 响滩 (sigpanic)" 与 "This is a modified version of Goink..."。
- 模块 `github.com/novelgo/novelgo`；数据目录 `~/NovelGo`；仓库 `ourvps1688/NovelGo`（origin），upstream=sigpanic/goink。

## 构建/环境（勿回退）
- 前端四道闸 `npm --prefix frontend run format:check / i18n:check / lint / build`（eslint 限 0 error）；改前端须 build 后重启 wails dev。
- CGO：`export PATH="/c/Program Files/Go/bin:/c/tools/mingw64/bin:$PATH"; export CGO_ENABLED=1; export CGO_CFLAGS="-IC:/tools/sqlite3-headers"`（gcc 在 /c/tools/mingw64/bin）。bash 无 LocalAppData 须带 GOCACHE+GOLANGCI_LINT_CACHE。golangci-lint 在 /c/Users/Administrator/go/bin。gofmt 须 LF 归一化后查。
- 推送：`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；commit&&push 链式偶显 nothing to commit 但已落盘→核 `git rev-list --left-right --count origin/master...HEAD` = `0 0`。PAT 在仓库根 github_pat.txt。钩子=scripts/hooks/pre-commit。
- 一任务一提交带全套 export；记忆账单独 `chore(memory)`。npm 一律 `npm --prefix frontend run X`（cwd 不跨调用）。
- 测试瞬态：真实 LLM E2E 偶发 FAIL 复跑绿即收口；无 key Skip（NOVELGO_EVAL_KEY）。

## 架构状态（核心约束，勿回退）
- **R5 检测核心已落地收官**（R0–R4 + R5-1/2/3/4 全销），是创作后质量保障的**唯一规范路径**：检测任务四 kind（chapter/volume/book/structure）独立启动、发现项落工单、人工确认→去修复→显式复验闭环、**零 auto-advance / 零闸门 / 零自动派发修复**。后端入口 `internal/pipeline/detect.go` RunDetectionTask。
- 旧 `Orchestrator` 流水线（S0–S6/G1/G1b/G3b/G4/GEnd）后端保留作 legacy，但**前端不得再走 StartPipeline / PipelineWizard / SignabilityAuditView**（S4/S5/S6 + G4 闸门硬卡）。前端规范入口 = DetectionTasks（四 kind 启动）+ IssueTicketsPanel（工单人工修复闭环）+ PatrolReportPanel + RetconPanel + VolumeManagePanel。
- 文案纪律：检测核心/检测任务「只跑检测不跑流水线」完全合法；零「唯一入口」宣称；pipeline 框仅为「写→检测」便捷壳（标注「编排预设」）。降级逐字一致是最高验收。
- 契约单源（勿改）：signability-audit.md=体检契约源；featureKeys.ts=三类 key 单源；factory.ts=PanelId 单源（零删除）；internal/structure 长程结构索引（恒 P1 只报告）；internal/quality chapter_quality 质量档案；pipeline_issues 工单九态。度量纯本地不上传（隐私红线）。

## 排障/坑（勿回退）
- LF 归一化只对本次改动文件做（全树 find+sed 误伤 50+ 文件）；读文件做逐字/正则断言的测试须 CRLF 归一化或钉 eol=lf。
- 重启 wails dev 要点：①`/c/Users/Administrator/go/bin/wails.exe dev`（带全套 Go/CGO export）；②重启前 `rm -rf frontend/dist/assets`（否则 vite emptyDir 触发 SAFE_DELETE_BULK_CONFIRM_REQUIRED 失败）；③结束旧进程用 PowerShell `Stop-Process -Id <pid> -Force`。wails watcher 会卡死不重建，修完须核验运行中构建（grep 二进制文案实证新代码进进程）。
- 队列测试「入队后断言不被执行」须先停/延后消费循环。MCP read 工具 start_line 越界=窗口平移收敛+note；破坏性参数才 required。grep 零回退口径：P0–P2 工件、theme token、desktop-first、PanelId 零删除。

## 当前任务（2026-10 末）：前端切到检测核心
- #257 新建 DetectionCorePanel（组合 IssueTicketsPanel+PatrolReportPanel+RetconPanel+VolumeManagePanel，react-query 取 GetPatrolReport + ListDetectionTaskStatuses；为 PatrolReportPanel 供 data/tick/onRun(structure 检测)）。
- #258 WorkspaceView 的 pipeline 面板切到 DetectionCorePanel（移除 PipelineWizard 导入与渲染分支）。
- #259 删旧流水线/闸门视图与孤立文件：PipelineWizard.tsx / SignabilityAuditView.tsx / PipelineControlBar.tsx / PipelineGateCard.tsx / BlueprintWorkflowView.tsx / usePipeline.ts / PipelineWizard.test.tsx。
- #260 SignabilityWorkflowView 改用 RunDetectionTask(novelId,book,null) 独立启动（移除 StartPipeline/GetPipelineState）。
- #261 PIPELINE_STAGES 移除 S4/S5/S4b/S6/GEnd（保留 S0/S1/G1/S2/S3/G3b）。
- #262 i18n（pipeline.title→检测核心，zh/en）+ featureKeys.test.tsx 移除 PipelineGateCard 引用。
- #263 四道闸 + vitest 验证（format:check/i18n:check/lint 0 error/build/vitest 全绿）→ 一任务一提交推送核 0 0。
