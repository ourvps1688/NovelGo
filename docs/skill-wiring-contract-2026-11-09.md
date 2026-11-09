# 技能装配与可观测契约（2026-11-09）

> 状态：**设计定稿，待实施**。本文只描述规划与开发计划，除本文外未改动任何代码。
> 关联：#108 写章技法 bundle、#109 下线平台数据对账/对标书库、#110 四个悬空口径清理。
> 取证：dev 库 `build/bin/novel-agent.db` 只读查询（messages 2026-10-11 ~ 11-09，
> exec_run_stats 10-25 ~ 11-08，chapter_quality 10-28 ~ 11-09）。
>
> **结论先行**：27 个内置技能出现「3 个零使用、4 个近乎零、3 个统计口径陷阱」，
> 根因不是技能写得不好，而是**技能正文的消费权被交给了 LLM 与用户的自觉**：
> 6 条正文通道里只有 3 条是 Go 确定性注入，其余靠 cue 碰运气、目录自读、斜杠、
> 勾选常驻；各通道留痕格式还不一致，于是「有没有用过」无法可靠回答。
> 摆设与口径陷阱同源，都指向**缺一份声明式装配契约 + 一本统一账**。
>
> 终极方案 = 把技能从「文档 + 目录」升级为「能力注册项」：
> **一张清单（manifest）+ 一个装配器（确定性注入）+ 一本账（skill_invocations）
> + 一道 CI 闸（零挂载点即构建失败）**。以下为完整规格与开发计划。
>
> **路径约定**：表格为紧凑起见省略仓库前缀
> （`service.go` / `pipeline/service.go` = `internal/pipeline/service.go`、
> `rw_tools.go` / `mcp_tools/rw_tools.go` = `internal/mcp_tools/rw_tools.go`、
> `components/skill/SkillMatrixView.tsx` = `frontend/src/components/skill/SkillMatrixView.tsx`、
> `detect.go`、`contract_test.go` 同理）；
> 完整仓库相对路径统一见附录 A，行号基于 2026-11-09 HEAD，实施时以函数名为准。
> 文中 `internal/skill/health_test.go`、`internal/activity/skill_invocation_test.go`、
> `frontend/src/components/skill/SkillUsagePanel.tsx` 为**规划待新建文件**，当前不存在属预期。

---

## 一、问题定义

### 1.1 实证底图

判定口径：**技能正文被真正加载进 LLM 上下文**才算「用过」——流水线派发、
chat 注入、常驻块、写章技法包、子 Agent 指令。只出现在技能目录
`<available_skills>`、代码注释或文档互引，一律不算。

| 分档 | 技能（可观测次数） | 判定 |
|---|---|---|
| 零使用（3） | `collect`、`memory`、`blueprint-fix` | 30 天内正文一次都没进过任何上下文 |
| 近乎零（4） | `blueprint-audit` 3、`review` 1、`worldbuilding-design` 1、`editor-feedback` 2 条存疑产物 | 多为 chat cue 偶发命中，无稳定机器侧消费 |
| 口径陷阱（3） | ① `chapter-ci` 不进 `exec_run_stats` ② `memory`/`review` 的 `agent_type` 是子 Agent 枚举、与技能同名异物 ③ 8 个重写技法的子 Agent 初始 prompt 不落库 | 不是 bug，是**留痕机制不统一**导致统计看不见 |

`chapter-ci` 实证：`chapter_quality.l1_status=checked` 64 次、
`agent_type='chapterci'` 消息 5876 条，是全项目最高频技能之一；
`memory`/`review` 的 `agent_type` 消息 1119 / 232 条与技能无关；
8 个重写技法经 `rewriteMethodBlock` 注入 `chapter-rewrite` 的 user prompt，
`pipeline_issues` 里 `reader_pov` 8 / `character` 5 / `locus_arc` 4 只是间接线索。

### 1.2 六条正文通道现状

| # | 通道 | 代码锚点（截至 2026-11-09 HEAD） | 是否 Go 确定性 | 30 天实证 | 留痕 |
|---|---|---|---|---|---|
| 1 | 流水线派发 | `internal/pipeline/provider_trip.go:71` `dispatchSkill` → `skillrun.go:125` `RunSkill` | 是 | `exec_run_stats` 378 条 / 6 个技能 | `activity.ExecStat` 结构完整 |
| 2 | 子 Agent 指令 | `mcp_tools/chapter_ci.go:311`、`feedback_tools.go:159`、`rw_tools.go:1017` | 是 | `chapter-ci` 高频；`statesync` 硬编码零技能 | 部分（`agent_type`），正文不落库 |
| 3 | 重写方法论 | `pipeline/rewrite_skills.go:77` `rewriteMethodBlock` | 是 | 8 个技能有映射，实际命中不可查 | **无** |
| 4 | chat cue 路由 | `app/slash_command.go:91` `buildAutoSkillInjectWithNames`（`:106` 传 `1,1`） | 概率 | 285 轮注入 / 3163 条 system-reminder | messages 模板字串 |
| 5 | 写章技法包 | `app/slash_command.go:149` `buildWritingSkillBundle`（`:72` 固定 3 个） | 是但固定 | `scene-beats` 7 次全来自 #108 后的包 | messages 模板字串 |
| 6 | catalog 自读 / slash / 常驻 | `agentcfg/skill_catalog.go`、`skill/enabled.go` | **否** | read 8 次 / 6 技能、slash 0 次、常驻仅 `book-pitch` 15 次 | 极弱 |

**只有 1/2/3/5 是 Go 侧确定性**。第 6 条完全靠模型自觉与用户自觉，
第 4 条靠 cue 猜测且 `maxAuto=1` 硬上限（一轮注入 >1 个 auto 发生 **0 次**）。
另有反向错投：`signability-fix` 在 chat 被 cue「修复」命中 **90 次**，
其正文却是 S5 流水线口径，占名额且烧 token。

### 1.3 一句话根因

> 系统把「哪个技能该进上下文」这个决定，交给了不保证执行的三方
> （LLM 自觉 read、用户自觉 slash/勾选、cue 字面碰巧命中），
> 并且没给这三条路统一的执行回执。于是既产生摆设，也产生统计黑洞。

---

## 二、目标架构

### 2.1 三层结构

```
① 能力清单 manifest          唯一事实源：mounts / internalOnly / budget / deprecated
        ↓ 约束
② 确定性装配                 chat 意图 · 写章技法包 · 流水线派发 · 子 Agent 与重写
        ↓ 每次注入写一条
③ 统一账本与守护             skill_invocations · 健康检查 CI 闸 · 前端用量面板
```

### 2.2 能力清单（manifest）

在 `internal/skill/types.go` 的 `Skill` 与 `SkillMeta` 增加 4 个 frontmatter 字段
（`parse.go` 已用 `gopkg.in/yaml.v3`，加 yaml/json tag 即可自动解析，`Meta()` 需同步透传）：

| 字段 | 类型 | 语义 |
|---|---|---|
| `mounts` | `[]string` | 合法挂载点枚举；为空且未标 `deprecated` → 健康检查失败 |
| `internalOnly` | `bool` | **仅约束 chat 路由与 catalog 可见性**，不约束流水线挂载；cue 命中时只回一行「请到检测/工单面板」指引，不注入正文 |
| `budget` | `int` | 该技能正文注入的 rune 上限；统一现有散落的 1500/4000/5000/12000 常量 |
| `deprecated` | `bool` | 显式占位/待下线；健康检查允许 `mounts` 为空，但必须列入排期 |

`mounts` 枚举（与代码消费点一一对应）：

| 枚举 | 消费点 |
|---|---|
| `chat_intent` | `app/slash_command.go:91` |
| `writing_bundle` | `app/slash_command.go:149` |
| `chapter_ci` | `mcp_tools/chapter_ci.go:311` |
| `state_sync` | `mcp_tools/rw_tools.go:1017` |
| `pipeline_dispatch` | `pipeline/provider_trip.go:71` |
| `rewrite_method` | `pipeline/rewrite_skills.go:77` |
| `pitch_flow` | `frontend/src/components/skill/BookPitchWorkflowView.tsx` |
| `blueprint_audit` | `pipeline/service.go:469` `enterBlueprintAudit` |
| `feedback` | `mcp_tools/feedback_tools.go:159` |
| `slash_only` | 高级入口；**不作为可用性指标** |

**不强行合并现有 5 处写死名单**（`writingBundleSkills`、
`auditDimSkillRoutes`/`issueDimSkillRoutes`、`skillAgentType` 映射、
`NewBookWizard.tsx:40` 的 `skills`、`identity.go` 硬写）。
它们各有业务语义与契约测试；改由**一致性测试**校验
「名单里的名字必须存在且声明了对应 mount」，先做到单源可校验，
再视情况逐步派生。这样不触碰 p0 golden 与契约单源。

frontmatter 目标样例（以 `memory.md` 为例，现状仅 8 行 frontmatter + 2 行正文）：

```yaml
---
name: memory
description: 依用户指令检索与归档跨章记忆
category: 系统
mode: manual
author: builtin
version: 2
internalOnly: true
mounts: [state_sync]
budget: 1200
useWhen:
  - intent: "回忆前文 / 归档本轮设定"
    cues: ["回忆", "前文说过", "整理记忆"]
---
```

### 2.3 确定性装配规则

| 挂载点 | 装配函数 | 触发条件 | 预算纪律 |
|---|---|---|---|
| `chat_intent` | `buildAutoSkillInjectWithNames` | 过滤 `internalOnly`；`RouteUserMessageSmart(...,1,1,...)` 改为 **auto ≤3 + 同类去重 + 单轮总 rune 帽** | 新帽先埋点观测 7 天再定值，不拍脑袋 |
| `writing_bundle` | `buildWritingSkillBundle` | 由固定 3 个改为按章况条件选（见 §3.3） | 单技能 1500 / 整块 **9000** rune 双封顶与「如实标注节选」。P1 实测：固定 3 技法已占 4273，旧 5000 帽只剩 727，条件技能最短 1273 必被 break → 条件化失效；故整块帽按 6×1500 同步扩容，单技能帽与「异常回退固定 3 个」不变 |
| `chapter_ci` | `buildChapterCIInstruction` | 追加写后自检技能正文块；**不动 `quality.L1ReceiptFormatLine()` 回执契约** | 非空才注入、空则逐字不变（复用现有降级写法 `:328-334`） |
| `state_sync` | `RunStateSyncForChapter` | 函数签名加 `skillStore`，把 `collect` 收集清单拼在硬编码指令前 | 技能缺失时退回现有硬编码指令，逐字不变 |
| `rewrite_method` | `rewriteMethodBlock` | 两张路由表补 `dialogue-subtext`/`deai-flavor`/`revision-pass` | 单技能 4000 / 整块 12000 双封顶、跨源去重、空块返回 `""` |
| `pipeline_dispatch` | `dispatchSkill` | 不变 | 不变 |

**catalog 降级**：`<available_skills>` 保留为说明性目录，但不再承担正文通道职责；
auto 技能在目录里标注「由系统按意图注入」。斜杠与 `.enabled.json` 保留兼容，
常驻改为「工作流自动武装」（开新书挂 `book-pitch`、写章挂技法包、体检挂 audit），
用户不必知道技能名。

### 2.4 统一执行账 `skill_invocations`

复用 `internal/activity/types.go:73` `ExecStat` 的成熟机制：
默认 sink（`activity/store.go:155` `AddExecStat`）、panic 吞掉、
`context.WithoutCancel`、**只记数字与标识不记正文、纯本地不上传**。

| 列 | 说明 |
|---|---|
| `skill` | builtin 技能名 |
| `channel` | `chat_intent` / `writing_bundle` / `chapter_ci` / `state_sync` / `pipeline_dispatch` / `rewrite_method` / `slash` / `always` / `catalog_read` |
| `mount` | 本次命中的挂载点 |
| `novel_id` / `run_id` / `chapter_id` / `session_id` | 归属（可空） |
| `runes` | 实际注入正文字符数 |
| `created_at` | 时间戳 |

6 个埋点位置：`app/slash_command.go:91`（chat）、`:149`（bundle）、
`pipeline/provider_trip.go:71`（dispatch）、`pipeline/rewrite_skills.go:77`（rewrite）、
`mcp_tools/chapter_ci.go:311` + `rw_tools.go:1017`（subagent）、
`agentcfg/skill_catalog.go`（常驻/slash）。

> **迁移方式纠偏**：新增表**不写 v174 版本化迁移**。按 `migrate/r3_task_gateobject_test.go`
> 与 `migrate/activity_test.go` 的先例，把 `&activity.SkillInvocation{}` 加入
> `internal/migrate/migrate.go:93-145` 的 `models` 列表，由 AutoMigrate 建表即可。
> `v173/drop_tables.go` 是「删表」特例（AutoMigrate 只增不减），不能当建表模板；
> 唯一显式 `CREATE TABLE` 的生产迁移是 `v161/writing.go:42-49`。

只读出口照 `app/activity.go:25` `GetFeatureUsage` / `:35` `GetPipelineExecStats`
的既有模式新增 `GetSkillUsage(days)`；DTO 放 `internal/activity/types.go`；
`wails generate module` 重生成绑定（说明见 `AGENTS.md:66`、`docs/rules/frontend-crud-patterns.md:96`）。
注意 `GetPipelineExecStats` 目前有绑定但**前端零消费**，是「账本已建、消费未接线」
的现成反面案例，本方案必须把面板一起交付。

一条 SQL 回答「某技能 30 天用了几次、走哪条通道」：

```sql
SELECT skill, channel, COUNT(*) AS n, MAX(created_at) AS last_seen
FROM skill_invocations
WHERE created_at >= datetime('now', '-30 days')
GROUP BY skill, channel ORDER BY n DESC;
```

### 2.5 健康检查 CI 闸

新增 `internal/skill/health_test.go`（**开发期静态断言，不是运行时质量闸门**；
不改变「检测四 kind 独立启动 → 工单 → 人工确认 → 修复 → 显式复验」的
质量保障唯一规范路径）：

1. 27 个 builtin md 全部 `len(mounts) >= 1`，或显式 `deprecated: true` 且列入排期；
2. `useWhen` 非空、正文非空行数 ≥5（现状 `memory.md` 缺 useWhen 且正文 2 行、
   `review.md` 正文 2 行 → P0 先列入 `legacyPlaceholders` 临时豁免，P2 补正文后清空）；
3. 每个 `mounts` 枚举必须有代码消费点（维护 `mountOwners` 映射表由测试断言）；
4. 5 处写死名单里的技能名必须存在且声明了对应 mount；
5. 技能名与 `agentcfg.AgentType` 枚举重名必须显式登记（现状 `memory` / `review`）；
6. `internalOnly` 技能不得出现在 `writingBundleSkills` 等 chat 侧名单。

前端用量面板（`SkillMatrixView.tsx` 挂 `SkillUsagePanel.tsx`，
范式照 `profile/FeatureUsageSection.tsx` 或 `skill/ScoreTrendPanel.tsx`）
对「账本 30 天零记录」的技能标红——以后「哪些是摆设」系统自己报警。

---

## 三、逐技能归宿

### 3.1 七个待激活技能

| 技能 | 终局挂载 | 触发条件 / 关键动作 |
|---|---|---|
| `collect` | `state_sync`（`rw_tools.go:1017` 前置收集清单） | `internalOnly`；每次状态同步必走。真实用户「整理/汇总/梳理/全局」词频全 0，**不要**塞进 chat |
| `memory` | 补全正文 + `state_sync` 归档 + `/memory` 接 MemoryAgent | 与 `agent_type=memory` 显式解耦；设兜底：接入后验收仍 0 则按 `deprecated` 删除占位 |
| `blueprint-fix` | **修上游**，见 §4 | 有蓝图体检 → 才有蓝图工单 → `issue_tickets.go:329` 才等得到触发 |
| `blueprint-audit` | 同上 S2 链路 | 链路接通后自然高频，不另造 cue |
| `review` | 补成「章后评审清单」→ `chapter_ci` L2 | 与 `ReviewAgent`（232 条消息，`reviewAgentSystem1`）同名异物，必须显式登记或更名 |
| `worldbuilding-design` | `writing_bundle` 条件化 + 蓝图阶段对齐 | 新卷/新地图/新势力才注入，不每章塞 |
| `editor-feedback` | 前端显式录入面板 → `record_editor_feedback` 埋点 | 2 条无来源 `editor_feedbacks` 样本标注或清理；让「录入→解析→回流」可查 |

### 3.2 `internalOnly` 名单（抑制反向错投）

`signability-audit`、`signability-fix`、`signability-verify`、
`blueprint-audit`、`blueprint-fix`、`blueprint-drift-check`、
`consistency-patrol`、`chapter-ci`、`chapter-rewrite`、`editor-feedback`、
`collect`（改造后）。这些技能正文带 JSON 回执契约或流水线阶段口径，
投给 chat 主 Agent 会污染输出格式；已在机器侧正常工作的不必换通道。

保留 chat 可见：`next`（总调度）、`book-pitch`（开新书工作流）、
12 个 auto 技法类技能。

### 3.3 写章条件化扩容规则

`next.md` 阶段 0/1/2 声明 11 个专项技法，#108 后只兑现 3 个。
在 `writingBundleSkills` 之外新增 `writingSkillConditions`：

| 条件 | 追加技能 | 依据 |
|---|---|---|
| 每章兜底 | `reader-immersion` / `scene-beats` / `plot-hook` | 现有 `slash_command.go:72`，不动 |
| 阅读序号 ≤3 | `opening-chapters` | 黄金三章 / 平台 profile 开篇口径 |
| 任务卡含新卷/新地图/新势力 | `worldbuilding-design` | 设定首次登场章 |
| 任务卡含对白戏 | `dialogue-subtext` | 潜台词与对白推进 |
| 平台 profile 指名节奏要求 | `pacing-control` | qimao preset / L0 三查 |
| 用户消息含卡文/没思路 | `brainstorm-composer` | 构思辅助 |
| 写后（章 CI 通道） | `deai-flavor` / `revision-pass` / `dialogue-subtext` | 自检清单比塞进起草提示词更贴切 |

`story-blueprint` / `character-design` 归「开新书 / 蓝图写前对齐」，不是每章注入。
所有条件命中都受单技能/整块双封顶，且必须「非空才注入、空则逐字不变」。

### 3.4 三个口径陷阱的自动消解

- `chapter-ci`：`chapter_ci.go:311` 注入点直接写账，不再依赖 `exec_run_stats`；
- `memory`/`review` 同名：`skill` 字段与 `agent_type` 枚举解耦 + 健康检查第 5 条锁重名；
- 8 个重写技法：`rewriteMethodBlock` 注入时写 `skill + run + chapter + runes`，不写正文。

三者都不需要专门修 bug，是统一账本的副产品。

### 3.5 共性根因对消表

| 根因 | 对消机制 |
|---|---|
| catalog 自读失效（8 次/30 天） | 降级为说明目录，正文只由 Go 注入 |
| slash 0 次 / 常驻仅 1 个 | 工作流自动武装；slash 保留为高级入口，不再当可用性指标 |
| `maxAuto=1` | 预算制（auto ≤3 + 总 rune 帽 + 同类去重） |
| `signability-fix` 90 次错投 | `internalOnly` 在 `RouteUserMessage` 前置过滤，只回指引导语 |
| 占位稿与同名异物 | 健康检查强制正文行数 / `useWhen` / `mounts` / 重名登记 |

---

## 四、S2 蓝图链路专项

### 4.1 代码证据：链路已接线，入口已退役

- `story-blueprint` → `service.go:412`（`executeBlueprint`）；
  `enterBlueprintAudit`（`:469`）置 `ctxt.AuditKind="blueprint"`；
  `executeAudit`（`:959`）在 `:1009-1011` 改派 `blueprint-audit`；
  蓝图工单修复在 `issue_tickets.go:329` 改派 `blueprint-fix`。
  **Go 代码完整，且有 `service_test.go` 的 `TestServiceBlueprintAuditPass` 等 4 个用例覆盖。**
- 但唯一能置 `AuditKind="blueprint"` 的入口是旧 `StartPipeline`
  （`app/pipeline.go:10`，注释「autoRun 历史参数，R5-4 自动裁决已退役」），
  而它**前端零组件调用**。
- 开新书明确绕开旧流水线：`NewBookWizard.tsx:190-198` 注释「走
  `RunDetectionTask(novelId,"book",null)`，不再走旧 `StartPipeline`」；
  `detect.go:214-219` 的 `DetectionKind*` 只有 chapter/volume/book/structure，
  **没有 blueprint**；`service.go:921-925` 反而把 `AuditKind` 复位为空。
- 历史设计文档亦标注「缺确定性触发」：`docs/pipeline-development-plan.md:22,138`、
  `docs/pipeline-orchestration-design.md:29,34`。

结论：`blueprint-fix` 零使用不是它自己有问题，是**上游整条 S2 链路从未在真实数据里跑过**。

### 4.2 硬约束（不可违反）

1. **零 auto-advance / 零闸门 / 零自动派发修复**（质量保障唯一规范路径）。
   S2 触发必须是**用户显式动作**，禁止建书后自动派发。
2. **不恢复 `StartPipeline` 的自动裁决链路**。
3. **不新增第五个检测 kind**：「检测四 kind 独立启动」是架构约束，
   不能为了 blueprint 往 `DetectionKind*` 里加枚举。
4. **不新增运行时质量闸门**：本文的健康检查是开发期静态断言。

### 4.3 主方案（推荐）：显式蓝图任务入口

- 在 `BookPitchWorkflowView.tsx:528` `await onCreated(n.id)` 之后
  （或 `NewBookWizard.tsx:197-198` 的 `onCreated` 之后）
  提供**显式按钮**「生成全书蓝图 / 蓝图体检」，用户点击才启动。
- 后端新增单次任务入口（不复用 `StartPipeline`），复用现有
  `executeBlueprint → enterBlueprintAudit → executeAudit` 与
  `dispatchExecutor` / task 记录，跑完落工单后由用户「去修复」显式触发
  `blueprint-fix`；全程无自动复评、无自动修复。
- 附一个「存量书补跑」入口，避免只对新书生效。

备选方案：若产品上确认不再需要 S2，则把
`story-blueprint` / `blueprint-audit` / `blueprint-fix` / `blueprint-drift-check`
统一标 `deprecated` 并下线对应代码与测试。取舍标准很简单：
**要么给它一个显式入口，要么给它一个明确的下线结论，不留「代码在、入口没了」的中间态。**

---

## 五、红线与不可回退约束

1. **p0 golden 五构造器函数体逐字不动**：`signabilityPrompt`（`service.go:2732`）、
   `finalAuditPrompt`（`:1749`）、`fixPrompt`（`:2838`）、
   `chapterRewritePrompt`（`:1599`）、`patrolPrompt`（`patrol.go:67`）。
   基线在 `internal/pipeline/testdata/p0_baseline_*_*.txt`，
   由 `prompt_blocks_test.go:43` `TestP0BaselineRegen`（须 `-p0-update`）与
   4 个 `TestPromptBlocks*MatchesBaseline` 逐字断言。
   只能扩「输入块」，不能改函数体；改后必须验证 baseline diff 为空。
2. **八维契约不动**：`signability-audit.md:43-51 / :107 / :110-134`
   的维度、desc、dims 八 key、JSON 示例；`review/score_mapping.go` 6↔8 映射；
   `contract_test.go:345` `TestSkillMDExamplesMatchContract` 双向同步。
3. **`patrol.go` 不追加技能正文**：#101 安全审核定位依赖
   `patrol_corpus.go:72` `buildPatrolCorpusWithUnits` 与
   `patrol_safety.go:157` `localizeSafetyTriggers` 的同源语料单元，
   加块会让「哪一章/哪张台账哪一行」的探测失真。
4. `contextasm` / persona 文案不动；`quality.L1ReceiptFormatLine()` 回执契约不动。
5. **#109 保留项不误伤**：平台 profile（`platform_profile`、qimao preset、
   `platformRhythmBlock`）、`review/score_mapping.go`、`quality/feedback.go`
   与 `editor_feedbacks` 回流、情绪账与 ScoreTrendPanel。
6. 所有新增注入走「**非空才注入、空则逐字不变**」；
   `rewrite_skills.go:9-10,76` 与 `chapter_ci.go:10-11` 的缺省纪律必须保持。
7. 所有映射表扩充同步 `TestRewriteSkillRoutesContract`、
   `TestBuildAlwaysSkillsContent_*` 等契约测试；wails 绑定只能
   `wails generate module` 重生成，禁止手改（`AGENTS.md:66`）。

---

## 六、开发计划

### 6.0 总原则

- **按 P0 → P3 顺序，一期一条提交**，每期独立跑完整闸门再进下一期。
- P0 是零行为变更的「可观测先行」；没有账本之前不调注入策略，避免盲改后无法评估。
- 每期结束核 `origin/master...HEAD = 0 0`；记忆账单独 `chore(memory)` 提交。
- dev 二进制靠 watcher 重建不稳定（多轮实测只重建中间版本），
  涉及 Go 提示词改动的期次结束后**冷启动 `wails dev` 并核二进制符号**。

### 6.1 P0 — 可观测先行（零行为变更）

| 项 | 内容 |
|---|---|
| 目标 | 让「谁用了、走了哪条通道」变成一条 SQL 能答的问题 |
| 改动 | `internal/skill/types.go`（4 字段 + `Meta()` 透传）；27 个 builtin md 批量补 `mounts`/`internalOnly`/`budget`（**不补正文**）；`internal/activity/types.go` 新增 `SkillInvocation` + `TableName()`；`internal/activity/store.go` 新增 `AddSkillInvocation` / `SummarizeSkillInvocations`；`internal/migrate/migrate.go:93-145` models 加一行；6 个通道埋点；`app/activity.go` 新增 `GetSkillUsage(days)`；`wails generate module`；`frontend/src/components/skill/SkillUsagePanel.tsx` 挂 `SkillMatrixView.tsx`；zh/en i18n `skillMatrix.usage.*` |
| 测试 | 新建 `internal/skill/health_test.go`（含 `legacyPlaceholders = {"memory","review"}` 临时豁免）；`internal/activity/skill_invocation_test.go`；`frontend/.../SkillUsagePanel.test.tsx` |
| 验收 | 27 技能全部有 `mounts` 声明；6 通道各产生至少 1 条账本记录；面板能显示每个技能的通道/次数/最近一次；p0 baseline diff 为空 |
| 闸门 | `go build/vet -tags webkit2_41`、`golangci-lint --build-tags webkit2_41`、全量 `go test`；前端 `format:check`/`i18n:check`/`lint`/`build`/`test` |

### 6.2 P1 — 抑制错投 + 写章条件化扩容（收益最大）

| 项 | 内容 |
|---|---|
| 目标 | 消掉 90 次反向错投，兑现 `next.md` 声明的 11 个技法，写后自检落地 |
| 改动 | `internal/skill/router.go:25` 前置过滤 `internalOnly` + 指引导语；`app/slash_command.go:106` `1,1` → `3,1` + 单轮总 rune 帽；新增 `writingSkillConditions` 与章况解析；`mcp_tools/chapter_ci.go:311` 追加自检块（复用 `:328-334` 降级写法）；`pipeline/rewrite_skills.go:37,49` 补 3 个映射 |
| 测试 | 扩 `app/skill_writing_bundle_test.go`（条件命中/不命中/封顶/零叠加）、`internal/skill/router_test.go`（internalOnly）、`internal/mcp_tools/chapter_ci_test.go`（空块逐字不变）、`internal/pipeline/rewrite_skills_test.go`（`TestRewriteSkillRoutesContract` 同步） |
| 验收 | chat 侧 `signability-fix` 注入 = 0；写章轮次按章况注入 3~6 个技法；`chapterRewritePrompt` 的 baseline 逐字不变；账本显示每技能 runes p95 在预算内 |
| 闸门 | 全套 + 显式核 p0 baseline diff + 冷启动 dev 核符号 |

### 6.3 P2 — 收掉 7 个技能 + S2 上游

| 项 | 内容 |
|---|---|
| 目标 | 3 个零使用 + 4 个近乎零全部有确定性挂载与账本记录 |
| 改动 | §4.3 显式蓝图任务入口（app 新方法 + 前端按钮 + 存量补跑）；`rw_tools.go:1017` 加 `skillStore` 并注入 `collect`；`memory.md` 补 `useWhen` + 正文 + `/memory` 接 MemoryAgent；`review.md` 补「章后评审清单」并挂 `chapter_ci`；`worldbuilding-design` 条件化已随 P1 落地；`editor-feedback` 前端录入面板 + 埋点；**清空 `legacyPlaceholders` 豁免** |
| 测试 | 蓝图链路端到端（显式启动→体检→工单→`blueprint-fix` 派发→账本）；statesync 技能缺失降级逐字不变；`memory`/`review` 重名解耦断言 |
| 验收 | 27 技能零使用 = 0；`blueprint-fix` 至少 1 次真实派发；健康检查零豁免；无自动派发/自动复评 |
| 闸门 | 全套 + 冷启动 dev + 真实 dev 库跑一次显式蓝图流程取证 |

### 6.4 P3 — 收口与文档

| 项 | 内容 |
|---|---|
| 改动 | `agentcfg/skill_catalog.go` 目录降级为说明性（auto 标注「由系统按意图注入」）；常驻改工作流自动武装（保留 `.enabled.json` 兼容，守住 `MaxAlwaysSkillsResident=3`）；`docs/TECHNICAL_STATUS_AND_GUARDRAILS.md` / `AGENTS.md` 技能口径更新（历史文档写「26 个」为旧时点，当前 27 个）；本文状态改为「已实施」并补实测数据 |
| 验收 | 全量回归；一次 SQL 导出 27 技能用量表附在本文末尾 |

### 6.5 验收硬指标

1. 27 个技能零使用 = **0**；每个技能 ≥1 个确定性挂载点且账本 30 天有记录。
2. chat 反向错投 = **0**；单轮注入 ≤3 个 auto 且有 rune 预算。
3. 一条 SQL 可答任意技能的用量/通道/最近一次。
4. p0 golden 与八维契约**零变化**；#109 保留项零回归。
5. `go build` / `go vet -tags webkit2_41` / 全量 `go test -tags webkit2_41 ./...` /
   `golangci-lint run --build-tags webkit2_41` 全绿；
   前端 `format:check` / `i18n:check` / `lint`(0 error) / `build` / `test` 全绿。

### 6.6 风险与回退

| 风险 | 对策 |
|---|---|
| 提示词膨胀、注意力稀释 | 所有新增走 rune 帽；P1 上线后看账本 7 天 runes p95 再定帽值；异常可单条开关回退到固定 3 个技法 |
| p0 golden 被意外改动 | 只扩输入块不动函数体；每期显式检查 baseline diff；改基线须 `-p0-update` 并复核 |
| S2 触发变成长耗时自动任务 | 默认关闭、纯显式按钮；复用现有 task/dispatchExecutor，不引入 auto-advance |
| 健康检查把历史占位稿卡死 | P0 用 `legacyPlaceholders` 临时豁免，P2 补正文后清空，不留永久后门 |
| wails 重生成带回历史噪声 | 生成后核 diff，精确剔除 `pre_verify_status` 等历史字段噪声，禁止整文件手改 |
| dev watcher 只重建中间版本 | 每期结束核二进制符号；不符则冷启动 |

### 6.7 提交纪律

- 代码/文档任务一条 `feat|refactor|fix` 提交，记忆账单独 `chore(memory)`；
- 含 Go 的提交命令必须带全套 CGO export
  （`PATH` 含 Go/mingw64/go/bin、`CGO_ENABLED=1`、
  `CGO_CFLAGS=-IC:/tools/sqlite3-headers`、`LOCALAPPDATA`/`GOCACHE`/`GOLANGCI_LINT_CACHE`）；
- 推送 `GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master`；
- commit 可能谎报「nothing to commit」，须 `git log` 核实后补推；
- 每期末核 `git rev-list --left-right --count origin/master...HEAD` = `0 0`，工作区干净。

---

## 七、数据口径与局限

- `messages` 2026-10-11 ~ 11-09（38721 条，技能注入 285 轮 / 3163 条 system-reminder 行）；
  `exec_run_stats` 10-25 ~ 11-08（378 条，只覆盖 `dispatchSkill` 的 6 个实际跑过的技能）；
  `chapter_quality` 10-28 ~ 11-09。**10-25 之前的流水线执行无留痕表**，
  所以「从未」严格说是「可观测窗口内从未」——这正是 P0 要先补账本的原因。
- 各通道次数是不同量纲的合计（派发次数 / 注入轮次 / 消息条数），
  仅作分档参考，不可直接相加比较；P0 后统一为 `skill_invocations` 行数。
- `editor_feedbacks` 2 条 `parse_status=parsed` 产物缺 `record_editor_feedback`
  调用留痕与 `operation_log` 写入，判为「执行存疑」，未武断归零。
- 本文所有行号基于 2026-11-09 HEAD（含 `f23519f8` / `36429c33` /
  `43c00ced` / `c1d817b0`）；实施时若行号漂移，以函数名为准。

---

## 附录 A：精确锚点索引

| 主题 | 路径:行号 |
|---|---|
| 技能元数据 / 解析 | `internal/skill/types.go:33,61`；`parse.go:43,94`；`builtin.go:8-12` |
| cue 路由 | `internal/skill/router.go:25,66,123`；`app/slash_command.go:91,106,149,72` |
| 常驻 / 目录 | `internal/agentcfg/skill_catalog.go:11,139`；`internal/skill/enabled.go` |
| 流水线派发 | `internal/pipeline/provider_trip.go:71`；`skillrun.go:35,125`；`service.go:283,311,208-210` |
| 重写方法论 | `internal/pipeline/rewrite_skills.go:37,49,77,129,138` |
| 子 Agent 指令 | `internal/mcp_tools/chapter_ci.go:311,328-334`；`feedback_tools.go:159,170`；`rw_tools.go:1017,1023-1038` |
| 账本模型 / 写入 | `internal/activity/types.go:73,86`；`activity/store.go:155,174`；`migrate/migrate.go:93-145` |
| Wails 只读出口 | `app/activity.go:25,35`；`frontend/.../wailsjs/go/app/App.js:317,361`；`models.ts:1-48` |
| 前端挂点 | `components/skill/SkillMatrixView.tsx`；`components/profile/FeatureUsageSection.tsx`；`components/skill/ScoreTrendPanel.tsx` |
| S2 链路 | `pipeline/service.go:404,412,469,959,1009-1011,921-925`；`issue_tickets.go:329`；`app/pipeline.go:10`；`NewBookWizard.tsx:190-198`；`BookPitchWorkflowView.tsx:528` |
| p0 基线 | `internal/pipeline/prompt_blocks_test.go:43,73-94`；`testdata/p0_baseline_*_*.txt` |
| #101 定位单元 | `internal/pipeline/patrol_corpus.go:72`；`patrol_safety.go:157,335` |
| 八维契约 | `internal/skill/builtin/signability-audit.md:43-51,107,110-134` |

## 附录 B：现有契约测试清单（改动时须同步）

| 文件 | 关键用例 |
|---|---|
| `internal/pipeline/rewrite_skills_test.go` | `TestRewriteSkillRoutesContract`、`TestRewriteMethodBlock`、`TestChapterRewritePromptMethodBlock` |
| `internal/pipeline/contract_test.go` | `TestPromptSchemaSingleSource`、`TestSkillMDExamplesMatchContract`、`TestAuditDimsFormatLineSingleSource` |
| `internal/pipeline/prompt_blocks_test.go` | `TestP0BaselineRegen`、`TestPromptBlocks*MatchesBaseline` |
| `internal/pipeline/execstat_test.go` | `TestDispatchSkillRecordsExecStat`、`TestDispatchSkillDefaultSinkWritesExecRunStats` |
| `internal/pipeline/provider_trip_test.go` | `TestSkillRunnerDispatchOnlyViaTripGuard`、`TestProviderTripSkipsDispatchS2Blueprint` |
| `internal/skill/router_test.go` / `regression_test.go` / `parse_test.go` | `TestRouteUserMessage_MaxCap`、`TestCuePrecisionRegression`、`TestParseFrontmatter_WithFields` |
| `internal/agentcfg/skill_catalog_test.go` | `TestBuildAlwaysSkillsContent_CapsResidentCount`、`TestBuildSkillCatalog_RoutingTableFromUseWhen` |
| `app/skill_writing_bundle_test.go` | `TestBuildAutoSkillInject_WritingBundle`、`TestWritingCueBoundary`、`TestSkillBodyCRLF` |
| `internal/mcp_tools/chapter_ci_test.go` | `TestRunChapterCIReceiptLanded`、`TestRunChapterCIDegradesToSkipped` |
| `internal/migrate/activity_test.go` / `r3_task_gateobject_test.go` | AutoMigrate 建表回归（P0 建 `skill_invocations` 的模板） |
