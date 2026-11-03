# 四检测工作流（章/卷/书/结构）耦合调研与模块化拆分可行性报告

- 日期：2026-11-03
- 触发事件：结构巡检修复后，用户报「签约潜力工作流跑了很长时间也没有结果」，怀疑巡检改动导致，并提出是否应对四个检测工作流做模块化单独拆分、互不影响。
- 调查方式：纯代码路径调研 + 运行态取证（tasks / gate_objects / pipeline_issues / exec_run_stats / pipeline_runs 表、wails-dev 日志、git diff 范围）。**未改动任何代码。**

---

## 0. 结论先行（TL;DR）

1. **签约潜力工作流没有卡死，也没有坏。** 全书检测 run `7381e113`（task 114）于 17:29 启动、**17:52 成功出结果**：verdict=pass、score=30/40、「能签（偏勉强）」，18 条发现项（I1-I18）已全部落为工单。后端链路端到端正常。
2. **「很长时间没结果」有三个叠加根因**：① 单轮体检 skill 实际耗时 22.8 分钟（mimo-v2.6-flash 流式反复停滞，9 分钟空闲看门狗触发 10+ 次）；② **前端完成后不刷新**——签约评估卡的内联工单查询在检测完成后没有任何失效机制，且会用新 run_id 过滤陈旧缓存导致**结果区必然为空**（这才是"跑完没有任何反馈"的直接机制）；③ 16:57 重启 dev 时误杀了一个进行中的修复任务（副作用，见 §2.1）。
3. **与巡检结构修复无关。** 相关提交（`23d6e19f`/`58d275e2`/`3d058352`）diff 范围只覆盖 patrol 后端成功路径与巡检前端卡片；`ReplaceIssues` 的 run 级隔离有 17:52 的实证（书体检落 18 条时，巡检 run 的 T1-T4 工单完好无损）。
4. **四个工作流做"模块化大拆分"的必要性低，属于过度设计。** 代码路径事实上已按 kind 解耦（独立入口/任务类型/执行体/落库/事件）；真正共享的是**应当共享**的基础设施（LLM 熔断器、任务队列、SQLite）。本次事故的三个根因**没有一个**能靠拆分解决。建议改为三个定点修复（P0/P1），明确否决进程级拆分。

---

## 1. 问题陈述

> 「现在运检结构工作流修复好了，但是签约潜力工作流似乎又罢工了，运行了很长时间也没有结果。是不是修复巡检结构的时候改动了什么，导致签约工作流出了问题卡死了？现在是不是需要思考，对工作流中的四个工作流要进行单独的思考，需要对代码进行模块化的单独拆分，各自互不影响？」

拆成三个问题：

| # | 问题 | 结论 |
|---|------|------|
| Q1 | 签约潜力工作流是不是卡死了？巡检改动是不是肇因？ | 没卡死；不是肇因（§2） |
| Q2 | 四个工作流分别是什么状态、各自有什么特有风险？ | 逐个评估（§3.5） |
| Q3 | 是否需要模块化单独拆分做到互不影响？ | 有必要性的是定点修复，不是大拆分（§4）；方案分层给出（§5） |

---

## 2. Q1：「签约潜力卡死」事实还原

### 2.1 时间线（来自 tasks / exec_run_stats / gate_objects / 日志取证）

| 时间 | 事件 | 证据 |
|------|------|------|
| 16:12 | issue_fix 任务 109 启动（run `1997a79d`，修 T3/T4 巡检工单） | tasks 表 id=109 |
| 16:57 | **重启 dev** → 启动恢复 `AbandonActive` 把 109 标记 dead「进程退出中断，任务由恢复流程重派」 | tasks 109；日志 `task_dispatch.go:62 count=1` |
| 17:05–17:20 | 两轮复验：T1/T2 复验通过（有回执）；**T4 复验在 900,023ms 撞 `verifyTicketTimeout=15min` 墙钟**，报 `context deadline exceeded`，按契约「标未确认留待下轮」 | exec_run_stats 225（error）；日志 `issue_tickets.go:572` |
| 17:07–17:38 | **mimo-v2.6-flash 流式反复停滞**，9 分钟空闲看门狗触发 10+ 次（`llm stream idle watchdog fired`） | wails-dev-1656.log |
| 17:29 | 全书检测（签约潜力）启动：task 114，run `7381e113`（detect:book） | tasks 114；pipeline_runs id=10 |
| 17:52 | **检测成功完成**：signability-audit skill 耗时 1,367,954ms（≈22.8 min），turns 7/24，status ok。审计结果 verdict=pass、score=30、「能签（偏勉强）…须先过一轮集中修复再投签」，18 条发现项 I1-I18 全部 P1 落为工单（round 8，fix_rounds=8） | exec_run_stats 226；gate_objects 206/207；pipeline_issues run `7381e113` 18 行 |

> 取证时（18:03）队列中零 running/queued 任务——不存在任何仍在跑的"卡死"任务。

### 2.2 三个根因

**根因 A：长耗时 + 供应商流式停滞（"很长时间"）**
单轮全书体检 skill 墙钟 22.8 分钟。期间 mimo-v2.6-flash 多次吐字后长时间静默，9 分钟空闲看门狗（`internal/llm/stream.go:77 thinkingIdleTimeout`）触发 10+ 次并重试。对用户而言就是「点了开始，转了二十几分钟圈」。这是**供应商稳定性问题**，与工作流代码结构无关。

**根因 B：前端完成后不刷新（"没有任何反馈"）——直接机制，最值得修**

签约潜力页 `SignabilityWorkflowView.tsx` 的内联发现项数据源：

```ts
// SignabilityWorkflowView.tsx:52-56
const { data: allIssues } = useQuery({
  queryKey: ["signability-inline-issues", novelId],
  queryFn: async () => (await ListIssueTickets(novelId)) ?? [],
  enabled: !!novelId && !!bookSt?.last_run_at,
});
// :57-61
const inlineIssues = useMemo(() => {
  const runId = bookSt?.last_run_id ?? "";
  return (allIssues ?? []).filter((r) => r.run_id === runId);
}, [allIssues, bookSt?.last_run_id]);
```

缺陷链条：

1. 该 query **全工程没有任何一处 invalidate**（grep 坐实），也没有 refetchInterval；配合全局 `staleTime 30s` + `refetchOnWindowFocus: false`（queryClient.ts），一次拉取后长期不更新。
2. 该视图**不订阅任何完成事件**。全局的 `useDetectionFeedback`（订阅 `pipeline:detect_done` 并失效 `detectionKeys.list`）**只挂在 `journey/DetectionTasks.tsx:65`**，签约潜力页收不到。
3. 状态区靠 3 秒轮询 `detectionKeys.list` 能拿到最终 verdict/score，`bookSt.last_run_id` 会翻成新 run——但 `allIssues` 还是旧缓存。**「旧缓存 × 新 run_id 过滤」交集必为空**：于是 17:52 落库的 18 条 I1-I18 工单在界面上一条都不显示，甚至比检测前更空。
4. 完成时无 toast、无任何提示——「跑完之后没有任何反馈」一字不差。

`DetectionWorkflowView.tsx:107-111` 的 `["detect-inline-issues", novelId]` 是同一模式的同一缺陷（仅 structure 分支补了 `pipeline:consistency_patrol` 事件刷新巡检报告——这正是用户感觉"运检结构修好了、签约潜力又罢工"的反差来源）。

**根因 C：16:57 重启 dev 误杀修复任务（副作用）**
`StartTaskQueue` 的 `AbandonActive` 把进程退出时残留的活跃任务统一标 dead 等待重派——这是设计意图，但它杀掉了 16:12 起在跑的 issue_fix 109（T3/T4 修复），需人工经 `RecoverInterruptedRuns` 重派。T4 后续复验又撞 15 分钟墙钟失败（`verifyTicketTimeout=15min` 对思考模型 + 停滞供应商偏紧，exec_run_stats 225 恰好 900,023ms）。与巡检改动无关，属于部署/运维操作的固有代价 + 既有已知的复验超时张力。

### 2.3 「巡检改动导致」假说——证伪

| 证据 | 内容 |
|------|------|
| diff 范围 | `23d6e19f` = patrol.go 成功路径新增 `ReplaceIssues` 落工单 + 测试（structure 专属路径）；`58d275e2`/`3d058352` = 纯前端巡检卡片/工单按钮/i18n（DetectionWorkflowView 的 diff 仅注释 + `runId` prop）。**签约链路（detect.go / issue.go / SignabilityWorkflowView / signability-audit 技能）零改动。** |
| run 级隔离实证 | 17:52 书体检落 18 条工单（run `7381e113`）时，巡检 run `1997a79d` 的 T1-T4 四条工单完好（合计 novel 19 共 28 条 = 4+18）。`ReplaceIssues` 是 `DELETE WHERE novel_id AND run_id` 的 run 内替换，跨 run 不互踩。 |
| 后端结果 | run `7381e113` 完整走完检测→解析→落库，审计对象与工单齐备——链路没有坏，只是**没人来读结果**。 |

---

## 3. Q2：四工作流现状架构与耦合点

### 3.1 每 kind 的调用链（现状速查）

| kind | 队列/执行体 | 技能 | 落库 | 完成事件 | 结果呈现 |
|------|------------|------|------|----------|----------|
| chapter | **无队列**，裸 goroutine `dispatchChapterDetect` | chapter-ci | 章级质量档案（非工单） | `pipeline:detect_done` | 质量档案就地展开 |
| volume | `taskKindDetect` → `executeDetectAudit` → `runDetection` | signability-audit | `ReplaceIssues`（run 级） | `pipeline:detect_done` | SignabilityAssessmentCard 内联 |
| book | 同 volume | signability-audit | 同上 | `pipeline:detect_done` | SignabilityAssessmentCard 内联 |
| structure | `taskKindPatrol` → `runConsistencyPatrol`（分块 Map-Reduce） | consistency-patrol | patrol_report + `ReplaceIssues`（`23d6e19f` 新增） | `pipeline:consistency_patrol` | PatrolReportPanel（双源合并取数） |

### 3.2 真实共享面（耦合点清单）

| # | 共享资源 | 耦合性质 | 本次事故中的角色 |
|---|----------|----------|------------------|
| 1 | **LLM 供应商 + 熔断器**（`provider_trip.go`，trip 全线拦 `dispatchSkill`） | 设计性共享：熔断保护的单位就是供应商 | 根因 A 的载体：mimo 停滞拖慢所有工作流 |
| 2 | **任务队列 Runner**（`internal/task`：2min 租约、3s 轮询、无限并发、panic 恢复、`kind:run_id` 去重） | 无饥饿风险（maxConcur≤0 无限并发） | 无（114 任务正常调度） |
| 3 | **skillRunner 单缝**（`dispatchSkill` 是唯一调用点，测试强制） | 单缝=统一观测/熔断/统计 | 无 |
| 4 | **SQLite**（busy_timeout 5s、max_open_conns=8、"database is locked" 3 次重试） | 写竞争 | 无（已缓解） |
| 5 | **detect.go 超时变量**（`detectTaskTimeout=60min` 全 kind 共用；`verify*` 只归复验） | 全局 var，未按 kind 区分 | 间接：复验 15min 墙钟杀死 T4（根因 C 一部分） |
| 6 | **事件总线** | 事件名按 kind 命名空间化，无碰撞 | 前端只订阅了一半（根因 B） |
| 7 | **启动恢复 `AbandonActive`** | 全 kind 通杀（进程级，设计意图） | 根因 C：杀掉 issue_fix 109 |

### 3.3 已有的隔离机制（说明"拆分"其实已存在）

- **数据隔离**：`ReplaceIssues` run 级替换（17:52 实证）；`pipeline_issues.run_id`、`pipeline_runs.mode=detect:<kind>` 容器、`gate_objects.run_id` 全部 run 内聚。
- **调度隔离**：每 kind 独立 task kind（detect/patrol/issue_fix/issue_verify）+ `kind:run_id` 活跃任务去重合并。
- **状态隔离**：`beginDetect(kind, scope)` 登记表按 kind+scope 键控，取消/超时互不牵连（`detectCancelFns`）。
- **前端隔离**：每 kind 独立视图与 query key（`signability-inline-issues` / `detect-inline-issues` / `patrolKeys.report` / `chapter-quality-archive`），无 key 碰撞。
- **执行体隔离**：chapter 自有 runner、structure 自有 `runConsistencyPatrol`、volume/book 共享 `runDetection` 核心（两者本来就是同一契约的两个作用域）。

### 3.4 真正的风险耦合（按风险排序，均非本次肇因）

1. **熔断共享**：一个工作流反复硬失败会 trip 供应商 → 其余三个全被拦（"A 拖垮 B"唯一真实的代码通道）。但这正是熔断的设计目的——保护供应商不被打爆；拆掉它得不偿失。
2. **供应商吞吐共享**：并发长任务抢 rate limit，一起变慢。进程拆分无济于事（配额在供应商侧）。
3. **重启通杀**：`AbandonActive` 全 kind 生效。任何工作流重启时在跑任务全部中断——进程级拆分也躲不开（除非常驻多进程）。
4. **SQLite 写竞争**：已有重试 + 连接池，残余风险低。

### 3.5 四个工作流逐一评估（"单独的思考"）

| kind | 现状健康度 | 特有风险 | 需要独立模块？ |
|------|-----------|----------|----------------|
| **chapter** | 健康。走质量档案，不落工单，逻辑最轻 | **唯一不走队列的 kind**（裸 goroutine）：无法统一租约/心跳/恢复，进程退出静默丢失——与整体架构不一致 | 不需要拆包；**建议并入任务队列**（P2，一致性收益） |
| **volume** | 健康。与 book 共享核心，作用域多了 volume 存在性校验 | 无特有风险 | 不需要 |
| **book（签约潜力）** | 后端健康（本次 22.8min 成功出结果）；**前端反馈缺口是它的痛点** | 单轮最重（全书语料），对供应商停滞最敏感；60min 硬上限下 23min 属正常偏慢 | 不需要拆包；需要的是 §5-P0 前端修复 + 运行中可观测性 |
| **structure（巡检）** | 刚修完，最完善（分块 Map-Reduce、部分沉淀、落工单、事件刷新齐全） | 45min 归并超时预算是它独有的（已按需放宽） | 不需要；它的超时差异用「预算配置化」即可表达 |

---

## 4. Q3：模块化拆分的必要性评估（核心问题）

### 4.1 逐一验证：拆分能解决本次事故吗？

| 根因 | 模块化/进程拆分能否解决 | 说明 |
|------|------------------------|------|
| A. 供应商流式停滞 + 23min 长耗时 | **否** | 配额与稳定性在供应商侧；拆分后各模块仍打同一个 mimo，甚至更难统一熔断保护 |
| B. 前端完成后不刷新 | **否** | 纯 React Query 缓存失效问题，与后端模块边界正交 |
| C. 重启杀任务 + 复验超时 | **否** | 进程生命周期与超时预算问题；拆分要么照样被杀（同进程），要么付出多进程常驻的代价 |

**三个根因全部与"模块边界"无关 → 大拆分对本次事故收益为零。**

### 4.2 反对大拆分的论据

1. **"拆开"已经存在**：入口、任务类型、执行体、落库、事件全部按 kind 分治（§3.3）；剩下共享的是熔断器、队列、SQLite——这些**恰恰必须共享**（熔断的保护单位是供应商；队列的价值在统一租约/恢复/去重）。
2. **Wails 单进程桌面应用**：进程级拆分意味着 IPC、事件桥、启动恢复、打包体积全部重做，成本极高，而桌面场景的部署/升级复杂度会翻倍。
3. **无回归证据**：本次事故不是跨 kind 干扰（§2.3 实证）；历史上四 kind 也从未互相污染过数据（run 级隔离始终成立）。
4. **过度设计风险**：为一个不存在的故障模式引入四套模块边界，后续每个契约改动（如 ReplaceIssues 语义）要改四处。

### 4.3 诚实列出：有限拆分/分化的合理诉求

- **超时预算差异化**（chapter 轻量 vs structure 45min vs verify 15min）——用 **per-kind 预算配置** 解决，不需要包拆分。
- **可观测性归属**（"这次变慢是哪个 kind 拖的？"）——用 per-kind 执行统计/熔断观测解决。
- **chapter 裸 goroutine 的架构不一致**——并入队列即可，方向是"更统一"而非"更拆分"。

### 4.4 结论

> **大拆分必要性：低（不建议）。定点修复必要性：高（立即）。**
> 需要"单独思考"的不是模块边界，而是**每个 kind 的反馈闭环与预算**——本次用户感知到的"罢工"，恰恰是签约页缺了结构页刚补上的那种"完成即反馈"。

---

## 5. 解决方案（分层，均为建议，未实施）

### P0 止血（必做，小改动，直接消除"没反馈"）

| 编号 | 改动 | 位置 | 效果 |
|------|------|------|------|
| P0-A | 签约页/检测卡订阅 `pipeline:detect_done`（复用 `useDetectionFeedback` 或自挂 `EventsOn`），完成后 invalidate `["signability-inline-issues"]` / `["detect-inline-issues"]`（顺带 `["chapter-quality-archive"]`）并弹完成 toast | `SignabilityWorkflowView.tsx` / `DetectionWorkflowView.tsx` | 修掉「旧缓存×新 run_id=必空」缺陷；跑完立即见结果——本次事故的直接痛点 |
| P0-B | 完成提示露出：verdict/score 变化时给一次性反馈（toast 或卡片高亮） | 同上 | "跑完没反馈"的体验面 |

### P1 加固（建议，防复发）

| 编号 | 改动 | 说明 |
|------|------|------|
| P1-C | 复验超时分层：`verifyTicketTimeout=15min` 对思考模型+停滞供应商偏紧（T4 死于 900,023ms）→ 上调至 20–25min，或改「空闲看门狗兜死连接 + 软预算告警」两段式；「超时未验证」须在 UI 明示（note 已有，前端露出） | 超时纪律注意维持 > `thinkingIdleTimeout=9min` |
| P1-D | 运行中可观测性：检测卡显示已运行时长；空闲看门狗频繁触发时提示「供应商响应缓慢，已自动重试 N 次」 | 直接缓解"跑了很久没结果"焦虑 |
| P1-E | dead 任务可见 + 一键重派：`RecoverInterruptedRuns` 已有后端，补用户入口；**并补 `fixing` 态启动清扫**——现行 `ResetAllStuckVerifying` 只复位 `verifying` 残留，进程中断残留的 `fixing` 工单（2026-11-04 00:14 dev 退出实况：T3/T4 卡死「修复中」）无任何清扫，会永久显示修复中 | 消除根因 C 类事故的静默性 |

### P2 可选重构（不推荐现在做）

| 编号 | 改动 | 判定 |
|------|------|------|
| P2-F | per-kind 超时预算配置化（timeout profile 替代全局 var） | 可做，收益中等，改动面小 |
| P2-G | per-kind 执行统计/熔断观测 | 可做，先看 P1-D 是否已够 |
| P2-H | chapter 并入任务队列（去掉裸 goroutine） | 方向正确（更统一），排后 |
| P2-I | **进程/模块级大拆分** | **否决**（§4.2） |

---

## 6. 可行性评估

| 项 | 改动面 | 工作量 | 风险 | 收益 | 可行性 |
|----|--------|--------|------|------|--------|
| P0-A/B 前端反馈闭环 | 2 个视图组件 + hook 复用 | 小（数小时级，含测试） | 低（纯前端） | **高**：直接消灭本次痛点 | **立即可行** |
| P1-C 复验超时 | detect.go 变量 + 单测 | 小 | 低（有 `>9min` 纪律注释护住） | 中高 | 可行 |
| P1-D 运行可观测 | 前端状态卡 + 后端心跳透出 | 中 | 低 | 中 | 可行 |
| P1-E dead 重派入口 | 前端按钮 + 已有后端接口 | 小 | 低 | 中 | 可行 |
| P2-F/G/H | 后端局部重构 | 中 | 中（回归面在检测链路） | 中 | 可行但排后 |
| P2-I 大拆分 | 全栈重做（IPC/恢复/事件桥/打包） | **极大** | **高** | **≈0**（对本次事故） | **不建议** |

---

## 7. 建议行动顺序

1. **P0-A/B**（前端反馈闭环）——一次小改动，把"跑完没反馈"直接修掉；
2. **P1-D**（运行中可观测）+ **P1-C**（复验超时）——消灭"跑了很久"的焦虑与 T4 类超时；
3. **P1-E**（dead 任务重派入口）；
4. **P2-F/H** 视后续实际痛点再议；
5. **明确不做**：进程/模块级大拆分（P2-I）。

---

## 附录 A：证据索引

| 结论 | 证据来源 |
|------|----------|
| run `7381e113` 17:52 成功出结果 | tasks 114；exec_run_stats 226（signability-audit 1,367,954ms, ok）；gate_objects 206/207；pipeline_runs id=10 |
| verdict=pass / score=30 / 18 条发现项 | gate_objects 206（audit）；pipeline_issues run `7381e113` 18 行（I1-I18，全 P1） |
| mimo 流式停滞 | wails-dev-1656.log：`llm stream idle watchdog fired` 10+ 次（17:07–17:38） |
| 前端不刷新 | `SignabilityWorkflowView.tsx:52-61`（无失效/无事件订阅）；`DetectionWorkflowView.tsx:107-116`（同模式）；`useDetectionFeedback` 仅挂 `DetectionTasks.tsx:65`；queryClient `staleTime 30s / refetchOnWindowFocus:false` |
| 巡检改动无关 | `23d6e19f`/`58d275e2`/`3d058352` diff 范围；`ReplaceIssues` run 级隔离实证（4+18=28 条互不覆盖） |
| 16:57 重启杀任务 109 | tasks 109 dead「进程退出中断，任务由恢复流程重派」；日志 `task_dispatch.go:62` |
| T4 复验 15min 超时 | exec_run_stats 225（900,023ms, error）；日志 `issue_tickets.go:572`；`detect.go:214 verifyTicketTimeout=15min` |
| 超时变量现状 | `detect.go:193/201/214/219/225`（60min/120min/15min/12 turns/escalate 3）；`stream.go:77`（idle 9min） |

## 附录 B：共享 vs 隔离速查

```
必须共享（拆了有害）            已按 kind 隔离（无需再拆）
├─ LLM 熔断器（保护单位=供应商） ├─ 入口 RunDetectionTask(kind, scope)
├─ 任务队列（租约/恢复/去重）    ├─ task kind（detect/patrol/issue_fix/issue_verify）
├─ skillRunner 单缝（观测统一）  ├─ 执行体（chapterCI / runDetection / runConsistencyPatrol）
├─ SQLite（连接池+锁重试）      ├─ 落库（ReplaceIssues run 级 / 质量档案 / patrol_report）
└─ 事件总线（按 kind 命名空间）  ├─ 检测容器 detect:<kind> + beginDetect(kind,scope)
                                └─ 前端 query key 各自独立
```
