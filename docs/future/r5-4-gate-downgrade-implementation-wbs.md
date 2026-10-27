# R5-4「G 闸门降级」—— G 闸门改检测汇总视图、默认不卡推进 —— 分步实施计划细化（WBS）

> 承接 `pipeline-core-redesign-proposal.md` §10.5/§10.7，R5 迁移路线收官步（R5-1 工单化 / R5-2 摘除自动修复 / R5-3 检测任务独立入口 已销）。
> 目标：G1/G1b/G3b/G4/G终 从「硬性串行关卡」降级为「本阶段检测结论一览」（汇总质量档案 + 未决工单 + verdict）——不卡流程、是否推进由人决定。
> 顺序铁律：先落后端行为开关（G3b 弹闸退役 + auto_run 自动裁决退役）→ 前端汇总视图与文案 → 双验收收尾；每任务验收不过不进下一任务，一任务一提交一销账。
> **范围铁律**：R5 全程 **P0–P2 工件零触碰**（`internal/contextasm`/`internal/voice`/`internal/snapshot`/`internal/retcon` 不进本 WBS）；**降级逐字一致**；**只报告不拦截**；**自动检测节奏红线**（黄金三章首检/每 5 章与卷边界复检入口/完本终检/写后章节 CI）保留；**冻结/retcon 边界语义不变**；**PanelId 零删除**；**desktop-first**；**度量纯本地**。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 语义定案：不卡流程 + 汇总视图 + 人决定推进（三件套）

- **不卡流程**：检测/闸门不再打断或冻结用户的创作与检测活动——写作（chat/mcp 写章）、检测任务（四 kind）、工单操作（去修复/复验/驳回）全程可用；S1/S3 阶段常驻控件不再被闸门遮蔽。
- **汇总视图**：G 闸门不再以阻塞式警告卡形态出现，改为「检测汇总」视图 = verdict + 质量档案摘要 + 未决工单计数 + 回流/人工清单提示 + **建议动作按钮**（continue/modify/fix/abort/finish/audit_now 全保留，按钮语义不变 = ResumePipeline → Orchestrator.Advance 唯一裁决点）。
- **人决定推进**：run 的阶段推进（S2→S3、S4→S3/S5、S6→finish、复检触发）一律由用户显式动作驱动，机器不自动裁决——**auto_run 自动裁决分支退役**（「裁决默认交还用户」；自动 = 只自动触发检测节奏，不含自动推进）。

**解读定案（两读取舍，勿翻案）**：§10.5「不再阻塞流程，是否推进由人决定」并列成立的唯一解 = **「等人」≠「卡流程」**——run 可停在「结论就绪、等人定夺」态（awaiting_gate 语义重定位），但该停点不得阻塞任何其他活动、不得以硬卡形态逼答。考虑过「G4 收口自动回 S3（机器默认推进）」方案，被「是否推进由人决定」明文否决；「是否推进」指带裁决性质的阶段转移（进修复/完本/重生成蓝图/放弃），S3 写作循环本身不是裁决。

### 0.2 逐闸门处置表

| 闸门 | 现状 | R5-4 处置 |
|---|---|---|
| G3b·里程碑闸（每 5 章/卷边界复检提示） | S3 弹闸落 awaiting_gate（continue/audit_now/abort） | **弹闸退役**：S3 持续 running 不打断写作；里程碑提示改非阻塞呈现（stage note/活动区）；「进入体检」= S3 常驻显式按钮（audit_now 链路保留） |
| G3b·普通逐章闸（章间「已完成第 N 章」节奏闸） | S3 每次「检查进度」后弹闸落 awaiting_gate（continue/abort） | **弹闸退役**：S3 写作循环改纯按钮节奏（检查进度→S3 running +「已完成第 N 章」stage note，不再停闸）；continue/abort 决策仍在（控制条/汇总视图） |
| G1（蓝图确认）/ G1b（蓝图修复闸） | S2 弹闸硬卡（continue/modify/abort / fix/continue/abort） | 「等人决定」停点保留（蓝图是创作决策），呈现降级为汇总视图 + 建议动作；常驻控件并存不遮蔽 |
| G4（S4/S4b 签约体检） | S4 弹闸硬卡（continue/fix/abort）+ auto_run 自动裁决 | 停点保留（等人决定），汇总视图呈现 verdict+工单+档案；auto_run 自动裁决退役 |
| G终（S6 完本终检） | S6 弹闸硬卡（finish/fix/abort） | 停点保留（**完本必须人工确认、绝不自动完本红线不变**），汇总视图呈现 |

- **事件协议零改动**：GatePayload 形态与 `pipeline:gate` 通道不变（G3b 不再发 gate 事件属行为退役、非协议变更；G1/G1b/G4/G终 事件照发供汇总视图消费）。
- **状态机零改动**：`StatusAwaitingGate` 保留为「结论就绪、等人定夺」语义；`Orchestrator.Advance` 唯一裁决点不变；冻结（G_freeze）/retcon 边界零触碰。

### 0.3 自动检测节奏红线（§10.5b，勿回退）

- **保留**：写后章节 CI（ScheduleChapterCI）；黄金三章首检（enterAudit golden3）；每 5 章/卷边界复检**入口**（audit_now → enterAudit milestone/volume，advanceS3 decision 分支不动）；完本终检入口（finish_book → enterFinalAudit）。R5-4 只改「闸门呈现/裁决」，**绝不把自动触发节奏改纯手动**。
- **退役**：auto_run 的 S4 自动裁决（pass → Advance(continue) / fail → Advance(fix) + autoFixCap 轮上限）——裁决交还用户；autoFixCap 随之退役（仅 executeAudit 一处消费）。StartPipeline `autoRun` 参数与 AutoRun 字段**保留兼容**（零迁移），行为语义收窄注明。

### 0.4 跨 WBS 断言迁移（勿误伤，T1.1 内同步做）

- R5-3 T4.1 `TestR53Acceptance_AutoCadenceSourceLock` 钉了 `GateG3b` 在 orchestrator.go（「每 5 章里程碑复检闸不得退役」）——G3b 弹闸退役会使其变红。**迁移口径**：该断言改为「里程碑复检入口保留」（`enterAudit(ctx, run, ...)` 的 milestone/volume 触发链 + advanceS3 `case "audit_now"` 在），而非「GateG3b 常量在」；golden3/完本终检/写后 ScheduleChapterCI 三断言原样保留。
- R5-3 T4.1 `TestR53Acceptance_PresetKeepsGate` 断言 G4 闸门广播——G4 事件保留（汇总视图消费），断言原样成立。
- R5-3 `TestR53Acceptance_DetectIndependence` 检测-only 零闸门断言——语义不变，原样成立。
- 既有 G3b/auto_run 测试面（freeze_test 跨卷→audit_now、service_test G3b 两测、orchestrator_test GateBlockResume S3 段、issue_test AutoRunKeepsAuditContext、orchestrator_test AutoRunStopsAtGate、service_test BlueprintAuditAutoRunSkipped）：按新语义迁移（G3b 零弹闸断言 / auto_run 零自动裁决断言），**迁移后既有测试语义零回退**（凡改断言先 stash 验证 HEAD 上红绿归属）。

---

## 1. 后端：行为开关（G3b 弹闸退役 + auto_run 自动裁决退役）

### T1.1 后端行为开关（契约 0.2/0.3/0.4）

- **文件触点**：`internal/pipeline/orchestrator.go`（advanceS3 两道 G3b 闸、AutoRun skip 分支）、`internal/pipeline/service.go`（executeAudit auto_run 裁决分支、autoFixCap）、`internal/pipeline/r5_3_acceptance_test.go`（源码锁迁移）、受影响测试文件。
- **交付物**：
  1. advanceS3 里程碑块与普通逐章闸**不再落 awaiting_gate/不发 G3b**——S3 setS3Running 持续（写作循环改纯按钮节奏）；「已完成第 N 章」与里程碑提示改**非阻塞 stage note**（emitStage Note 既有字段，事件协议零改动）；advanceS3 `case "audit_now"`/`case "finish_book"` 决策分支零改动。
  2. executeAudit auto_run 自动裁决分支退役：检测收口统一「落 awaiting_gate + emitGateDecision」（裁决交还用户）；autoFixCap 常量随之退役。
  3. AutoRun skip 分支清理（行为已成默认）；AutoRun 字段/参数保留兼容，语义注释收窄为「历史参数，自动裁决已退役」。
  4. 契约 0.4 断言迁移 + G3b/auto_run 测试迁移（凡改断言先验 HEAD 红绿归属）。
- **验收**：①S3 到里程碑/逐章闸零 gate 事件、S3 持续 running（写作不打断）；②S4 检测收口零自动 Advance（run 落等人态 + G4 汇总事件照发）；③G1/G4/G终 停点与 Advance 裁决零改动；④自动节奏四触发点源码锁迁移后全绿；⑤`TestRewriteVoiceABE2E` 402 瞬态不作回归判据。
- **依赖**：无。**风险**：中——触及 S3 循环节奏与 auto_run 语义，须测试迁移（先 stash 验归属再改）。

---

## 2. 前端：检测汇总视图 + 复检入口

### T2.1 GateCard → 检测汇总视图（非阻塞）+ S3 复检入口（契约 0.1/0.2）

- **文件触点**：`frontend/src/components/pipeline/PipelineGateCard.tsx`（改造为汇总视图卡）、`PipelineWizard.tsx`（控件并存、S3 补复检按钮）、`SignabilityAuditView.tsx`/`BlueprintWorkflowView.tsx`（gate 渲染口径）、i18n。
- **交付物**：
  1. 检测汇总视图卡：verdict 徽标 + summary + **未决工单计数**（复用既有 `ListIssueTickets` 查询，零新视图）+ 回流/人工清单提示 + **建议动作按钮**（全决策按钮保留、语义不变；视觉从警告硬卡→中性信息卡，`PipelineGateCard` 对外 props 契约尽量零改动）。
  2. **常驻控件并存**：gate 存在时 S1 continue / S3 检查进度/完本/全量对齐按钮不再被遮蔽（`!gate &&` 条件退役，改并存渲染）。
  3. S3 常驻「进入体检/复检」按钮（`resume("audit_now")`，G3b 退役后的复检入口；埋点沿用 `action.audit`/gate 决策 key 白名单口径）。
- **验收**：①gate 态下常驻控件可点（不遮蔽）；②汇总视图含 verdict + 未决工单计数；③PanelId 零删除（只加不删）；④i18n 双语（i18n:check 过）；⑤theme token（theme:check 过）；⑥desktop-first。
- **依赖**：T1.1。**风险**：低——呈现层改造 + 一个常驻按钮。

### T2.2 文案诚实化（§10.2 话语，双语）

- **文件触点**：i18n zh-CN/en（`pipeline.gate.*`、里程碑提示、S3 复检按钮）、必要处组件文案。
- **交付物**：话语统一为「检测完毕，发现 N 项」（§10.2）——去掉「请确认是否继续或进入修复闭环」类硬卡逼答话术，改「建议：…」；里程碑提示改非阻塞话术（「建议进入体检复检」而非「请进入签约体检」）；完本按钮提示保留人工确认语气。**诚实红线：零逼答/零「必须」话术**，测试 import 双语 JSON 断言锁死（PipelineWizard.test 的 T3.1 诚实断言模式）。
- **验收**：i18n:check 过；诚实红线 JSON 断言绿。
- **依赖**：T2.1。**风险**：低——纯文案。

---

## 3. 验收收尾

### T3.1 双验收 + 全量门禁 + 提交

- **验收 A（不卡流程）**：G3b 零弹闸零 gate 事件（S3 里程碑/逐章节奏持续 running + stage note）；gate 呈现态写作/检测/工单入口可用（前端断言常驻控件不遮蔽）；auto_run 自动裁决退役（S4 收口零自动 Advance）。
- **验收 B（人决定 + 勿回退）**：G1/G4/G终 仍等人决定（awaiting_gate 停点 + Advance 唯一裁决点零改动）；冻结/retcon 边界零触碰（grep 断言）；自动节奏四触发点源码锁迁移后绿；PanelId 零删除；P0–P2 工件零触碰（grep 断言）。
- **门禁**：Go 四道闸（gofmt 改动文件 LF 归一化 / go build / go test 全量 / golangci-lint 走包路径 0 issue）+ 前端五道（format:check / i18n:check / lint 0 error / build / theme:check）+ vitest 全量。`TestRewriteVoiceABE2E` 402 外部瞬态不作回归判据。
- **提交**：一任务一提交（feat(pipeline) R5-4）+ chore(memory) 记忆账；commit 带全套 export；推送核验 AHEAD=0。
- **依赖**：T1.1–T2.2。**风险**：低。

---

## 4. 待办衔接

- R5 收官：提案 §4.0 终态图修正落地——「创作人驱动：写（S0–S3 预设或单章）→ 检测任务（章节 CI / 卷体检 / 书体检 / 巡检，可任意单独启动）→ 质量档案 + 发现项工单 → 人工修复（显式触发）→ 显式复验 → 回到检测」。
- 勿回退红线汇总：P0–P2 零触碰 / 降级逐字一致 / 自动检测节奏保留（不改纯手动）/ 冻结·retcon 边界不变 / PanelId 零删除 / 度量纯本地 / desktop-first / 事件协议与 GatePayload 形态零改动。
