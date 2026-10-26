# NovelGo 创作流水线（C→A 演进）设计文档

> **⚠️ 历史设计文档**：本文写于流水线开工前，所述 S0–S6 流水线随后已按 C→A 路线落地（后续另有 R0–R4 重构，见 `docs/future/pipeline-core-redesign-proposal.md`）。文中对 identity 提示词步骤、review 定位、技能触发机制等的描述停留在当时快照，**现状以代码为准**，本文仅保留作实现对照。
> 状态：历史设计（已落地，保留作对照）
> 范围：把当前"散落的能力模块"接成"有序流水线"，用户只需在步间确认是否继续
> 决策：采用 **C→A 渐进** 路线；MVP 覆盖「重接已有节点+门 / 补宏观蓝图节点 / 补签约评估节点」三块；**不**做双模式切换（流水线即主创作入口）
> 关联文档：`docs/v3-system-design.md`、`internal/agent/DESIGN.md`、`internal/mcp_tools/DESIGN.md`

---

## 0. 一句话总结

当前 NovelGo 的"编排"是**软的**——整个创作流程只是主 agent system prompt 里的几步文字指令，靠 LLM 自己按顺序跑。能力模块其实拆得很好（孵化 / 蓝图 / 写作 / 评分门禁 / 状态同步 / 审稿 / 签约体检都已有），**真正缺的是一个把模块确定性串联起来、并在步间停下等用户确认的"编排层"**。本文档给出从 C（前端向导外壳）逐步演进到 A（后端确定性状态机）的完整方案。

---

## 1. 问题诊断（基于代码实情）

### 1.1 编排是软的
- 主 agent 的创作顺序写在 `internal/agentcfg/identity.go` 的 system prompt 第 1~7 步（起名/人设 → 大纲 → 写前质检 → 写章 → 写后质检 → 状态维护 → 启动 Review）。LLM 读到后"自行决定"下一步做什么。
- 这不是 bug，但**不可靠**：我们已实测到主 agent 在"写后维护"上陷入只读循环（重复 `get_chapter_list`），被 `isStuckLoop`（`internal/agent/safety.go`）腰斩（dev 日志 turn 39 / turn 41）。根因是"软编排"无强制顺序。

### 1.2 能力模块内容已齐，但「确定性触发」仍缺
经代码核查，目标能力**内容/工具层面均已存在**，本期几乎零新造。但需注意：标 `skill` 的三项（book-pitch / story-blueprint / signability-audit）目前只是**写在 markdown 里的创作方法论**（`internal/skill/builtin/*.md`），由 LLM 经 `read` 加载后照做（`internal/agentcfg/identity.go`）；`internal/skill/router.go` 只做「用户消息 → skill 名」的路由，**没有把 skill 作为「带工具的一次性 agent run」的程序化执行入口**。换言之：内容已就绪，但**作为流水线节点被确定性触发 / 传参 / 收产物的机制缺失**——这正是 §13 要补的。其余工具 / 子 agent 已是可直接调用的确定性能力。

| 能力 | 现状 | 证据 |
|---|---|---|
| 孵化新书 | `book-pitch` skill（.md 方法论）+ `BookPitchWorkflowView` 前端壳 | `internal/skill/builtin/book-pitch.md`；`frontend/src/views/WorkspaceView.tsx` |
| 宏观蓝图 | `story-blueprint` skill（.md 方法论，内容已就绪；**缺确定性触发入口**） | `internal/agentcfg/skill_catalog.go:54`；`internal/skill/regression_test.go:75`（"写个全书卷纲"→`story-blueprint`） |
| 逐章写作 | `edit` 工具（确定性） | `internal/mcp_tools/rw_tools.go` |
| 写后质检 / 评分门禁 | `edit` 内 `maybeScoreChapter`（确定性「门」范式） | `internal/mcp_tools/rw_tools.go:989`（低于阈值阻断重试，已是成熟的「门」范式） |
| 状态同步 | `statesync` 子 agent（确定性，写后 6s debounce 自动派发） | `internal/mcp_tools/rw_tools.go:907` `scheduleStateSync` / `:925` `runStateSync` |
| 章节审稿 | `review` 子 agent（确定性） | `internal/agentcfg/identity.go:291` `reviewAgentSystem1` |
| 签约体检 | `signability-audit` skill（.md 方法论，内容已就绪；**缺确定性触发入口**） | `internal/agentcfg/skill_catalog.go:54`；`internal/skill/regression_test.go:74`（"帮我评估签约潜力"→`signability-audit`） |

### 1.3 前端已有"工作流面板"骨架，但没接线
- `PanelId` 已含 `bookpitch` / `signability`（`frontend/src/types/panel.ts:21-22`）。
- `WorkspaceView` 有 `activeWorkflow` 状态（`:163`）与 ActivityBar「工作流」入口；`:162` 注释明确"后续新增工作流时，在此追加"。
- 但 `signability` 面板当前是半接占位（`:611` 渲染的是 `BookPitchWorkflowView`），且**面板之间没有顺序、没有确认闸**。这正是 C 期要补的。

### 1.4 结论
缺的不是"能力"，是"编排层 + 确认闸 + 产物传递"。这三者就是流水线的全部新增量。

---

## 2. 目标与原则

- **有序**：阶段按固定顺序推进，不依赖 LLM 记住下一步。
- **可确认**：每个关键节点停下，用户只做"继续 / 修改 / 终止"三选一。
- **产物驱动**：上一阶段产物作为下一阶段输入（如蓝图喂写作、签约体检读全本）。
- **最小新造**：全部复用现有 skill / 工具 / 子 agent。
- **渐进**：先 C（前端向导外壳，最快见效）后 A（后端状态机，终态确定性）。
- **章细纲前置（硬约束）**：**每章必须先写「章细纲」再写正文**，不可跳过。没有章细纲的 `edit` 在流水线内不允许推进——否则前后章逻辑、人设、伏笔会失序，作品崩坏。
- **台账实时维护（硬约束）**：**每次章节变更后 `statesync` 必须跑完并落库**（角色 / 地点 / 弧线 / 时间线 / 读者认知），方可进入下一章或闸门。台账一旦滞后，后续创作基于过期世界状态，小说会漏洞百出。这是流水线的不可跳过步骤，不只是"后台顺手做"。

---

## 3. 阶段图（全为现有能力接线）

```
[S1 孵化 book-pitch] ──G1──▶ [S2 蓝图(自动建卷/写卷纲) story-blueprint] ──G2──▶
   [S3 逐章循环: 写章细纲(强制) → edit → 评分门禁 → statesync(强制,必须落库) → review]   (从第一卷第1章起)
      ├─ 写完黄金三章(卷1前3章) ──▶ [S4a 签约评审 signability-audit]
      │                            └─▶ [S5 修复(卷纲/章细纲/正文/逻辑/台账)]
      │                                   └─▶ 复评(回到 S4a；通过则继续写／不通过继续修／用户可强制继续)
      ├─ 每5章 或 卷边界(取先到) ──▶ G3b 里程碑闸(确认继续)
      └─ 卷边界额外 ──▶ [S4b 签约复评 signability-audit] ─▶ S5 修复 ─▶ 复评
   …循环至完本…
   [S6 完本终检 signability-audit(全本)] ──G终──▶ 完成
```

> **两条硬约束（不可跳过）**：① **每章必须先写「章细纲」再写正文**——无章细纲不得推进 `edit`，否则逻辑/人设/伏笔失序；② **每次章节变更后 `statesync` 必须跑完并落库**才进下一章或闸门——台账滞后会让后续创作基于过期世界状态，小说漏洞百出。S4a / S4b / S6 均为 `signability-audit` skill（触发时机不同）；S5 修复为共享阶段，按评审产出的 issue list 修正多层级产物。S4 本身就是"门"——评审通过判定驱动 S5，不再单独设 G3a 确认闸（避免冗余点击）。

| 阶段 | 复用能力 | 闸门 | 产物（落入流水线上下文） |
|---|---|---|---|
| S1 孵化 | `book-pitch` skill + `BookPitchWorkflowView` | G1 确认书名/简介/卖点 | `novelgo.md` 元数据 + 一句话卖点 |
| S2 蓝图（**自动建卷/写卷纲**） | `story-blueprint` skill（一次性 agent run）+ `create_volume` / 写卷纲工具（**落库**） | G2 确认大纲/卷纲/立意/爽点地图 | 卷结构（Volumes 落库）+ `blueprint.md` + 各卷卷纲 |
| S3 逐章 | **写章细纲(强制前置)** → `edit` → `评分门禁` → **`statesync`(强制,必须落库)** → `review` 子 agent | 内部每章走评分门禁；**无章细纲不得 edit、台账未同步完不得进下一章/闸门**；卷1前3章完成后进 S4a；每5章/卷边界进 G3b | 章节正文 + 章细纲 + 评分 + 台账同步结果 + 审稿意见 |
| S4 签约评审（开篇/里程碑/完本触发） | `signability-audit` skill（一次性 agent run） | **自身即门**：产出 issue list + 通过/不通过判定，驱动 S5 | 签约体检报告 + issue list（编辑审稿口径/黄金三章/完本潜力/数据预期） |
| S5 修复（共享） | 复用 `edit` / 卷纲工具 / 章细纲 / `statesync` 复查 | 按 issue list 改完回到 S4 复评（通过则出循环／不通过继续修／用户可强制继续） | 修正后的卷纲/章细纲/正文/逻辑/台账 |
| S6 完本终检 | `signability-audit`(全本) | G终 完本确认 | 终检报告 |

> S4→S5→复评 是闭环：评审不过则进入修复，修复后复评，直到通过或用户强制继续——这正是"签约体检产物反哺大纲/正文/台账"的落地方式。闸门粒度见 §10。

---

## 3.1 启动模式：新开 vs 续写（状态感知切入）

流水线必须支持两种起点，差异只在**入口阶段**与**台账初始化方式**，流程骨架相同。否则导入的存量小说（已 >3 章、台账可能缺失）直接进 S3 续写，会继承"台账滞后 → 漏洞百出"的风险，违反 §2 硬约束②。

### 3.1.1 新开模式（S1 起）
默认路径：S1 孵化 → S2 蓝图 → S3(黄金三章) → S4a → S5 → …（见 §3 阶段图）。

### 3.1.2 续写 / 导入模式（S0 导入对齐起）
适用：导入的现有小说、或已有章节（>3 章）的老书。这类书**未走完 S1/S2**，且台账可能缺失/失真。续写模式必须先做 **S0 导入对齐**，再切入公共循环：

1. **结构校验 / 归卷**：若不存在 Volumes，运行 `story-blueprint` 的"仅建卷"子能力，调用 **`create_volume`** 工具把现有内容归卷（不重新构思世界观，只结构化）。
2. **补章细纲（存量）**：为缺失章细纲的已有章节补写章细纲（一次性 best-effort）。存量章节无法改写过去，但后续续写需以章细纲为锚，满足"章细纲前置"的可持续性。
3. **全量台账重建**：对所有已有章节跑一次**全量 `statesync`**，建立/修复 角色/地点/弧线/时间线/读者认知 台账。这是硬约束②在续写场景的落地——**续写前世界状态必须准确**。
4. **现状体检（S4-entry）**：对已有全文跑 `signability-audit`，产出 issue list（存量问题：逻辑漏洞、人设不一致、伏笔未回收、节奏）。
5. **S5 存量修复**：按 issue list 修存量（卷纲/章细纲/正文/逻辑/台账）。
6. 完成后进入 **G3b 里程碑闸** 或直接进入 **S3 续写循环**（从下一章起）。

### 3.1.3 统一循环（两模式汇合）
S0 / S4a 之后汇入同一条公共循环：

```
S3 逐章(写章细纲→edit→评分门禁→statesync→review) → G3b(每5章/卷边界) → S4b(卷边界复评) → S5 → … → S6 完本终检
```

差异仅在于 S4 的**首次触发点**：新开 = 写完黄金三章（S4a）；续写 = 导入对齐后（S4-entry / 现状体检）。之后两者完全一致。

### 3.1.4 计数器与闸门（续写）
- G3b 的"每 5 章"基于**全书绝对章号**（`ch % 5 == 0`）或卷边界，向前推进时自然触发；老书在 ch20，则下次闸在 ch25 或下一卷边界。
- `statesync` 在续写模式同样**每章强制落库**（硬约束②）；S0 的全量重建只是初始化，不替代后续每章同步。

### 3.1.5 C 期 / A 期实现要点
- **C 期**：`PipelineWizard` 启动处提供"新开小说 / 续写导入"模式选择；选续写则首屏走 S0 对齐 UI。
- **A 期**：`Orchestrator` 依据 `pipeline_runs.mode` 与小说当前状态（章节数 / 是否存在 Volumes / 台账是否齐全）**自动决定入口阶段**——无章节从 S1 起，有章节 >3 且无台账则从 S0 起，已就绪则直接进 S3。

---

## 4. 数据模型

新增 `pipeline_runs` 表（持久化流水线状态，重启可续跑）：

```sql
CREATE TABLE pipeline_runs (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  novel_id     INTEGER NOT NULL,
  run_id       TEXT    NOT NULL,          -- 一次完整流水线运行的 UUID
  mode         TEXT    NOT NULL DEFAULT 'new',  -- 'new' 新开 | 'continue' 续写/导入
  start_chapter INTEGER,                 -- 续写模式的起始章号（导入对齐后从此章续写）
  current_stage TEXT   NOT NULL,          -- S0(仅续写) / S1..S4 / S3_loop
  status       TEXT   NOT NULL,          -- pending|running|paused|awaiting_gate|finished|aborted
  auto_run     INTEGER NOT NULL DEFAULT 0,-- 0 手动(每闸等用户) | 1 自动(自主连跑至下一闸)
  context_json TEXT,                     -- 累计产物 + 闸门决策日志 + 暂停/恢复点
  created_at   DATETIME,
  updated_at   DATETIME,
  UNIQUE(novel_id, run_id)
);
```

`context_json` 存：各阶段产物引用（文件路径 / 摘要）、闸门决策历史（gate_id + decision + feedback）、当前循环计数（已写章节数）、续写模式下的存量体检 issue list 与修复记录。

---

## 5. 事件协议（复用已建 wails 全局事件范式）

沿用刚落地的 `statesync:progress` 全局事件模式（`wails.EventsEmit` / 前端 `EventsOn`，见 `frontend/src/hooks/useStateSyncProgress.ts`）：

- `pipeline:stage` —— 阶段切换 / 进度
  ```json
  { "run_id": "...", "novel_id": 12, "stage": "S2", "status": "running", "note": "正在生成卷纲…" }
  ```
- `pipeline:gate` —— 弹确认卡
  ```json
  { "run_id": "...", "novel_id": 12, "stage": "S1", "gate_id": "G1",
    "summary": "书名《XX》/ 简介… / 卖点…",
    "options": ["continue", "revise", "stop"] }
  ```

前端据此渲染 `PipelineGateCard`；用户点击后调用后端 `ResumePipeline(run_id, decision, feedback)`。

### 运行控制：暂停 / 恢复 / 停止（随时开始与停止）

用户要求：自动化流程必须有**暂停 / 恢复 / 停止开关**，可随时开始与停止，且停止/暂停后状态可持久化、可续跑。

- **控制状态（扩展 `pipeline_runs.status`）**：`pending | running | paused | awaiting_gate | finished | aborted`。
  - 新增 `paused`：用户主动暂停或异常中断；保留 `current_stage` + `context_json`，可恢复。
  - `aborted`：用户停止；可借 `current_stage` 续跑（恢复即回到该阶段入口）。
- **用户控制指令**（前端控制条 → 后端命令通道，复用 wails 全局事件范式）：
  - `pipeline:control` 事件（前端 → 后端）：`{ "run_id": "...", "cmd": "start" | "pause" | "resume" | "stop" }`。
  - 后端 `ControlPipeline(run_id, cmd)` 落库并即时改变状态；A 期 `Orchestrator` 在「安全检查点」轮询该状态。
- **安全检查点（A 期 Orchestrator 必查）**：在以下节点前检查 `status==paused/aborted`，命中则挂起/退出并持久化：
  - 每个阶段切换前；
  - S3 每章「写章细纲 → `edit` → `statesync`」之间；
  - **S5 修复闭环的每一轮迭代前**（最易长时间自主运行，必须可中断）；
  - 「自动连跑 N 章」的每章之间。
- **安全停止/恢复（C 期）**：C 期本质用户点击驱动，天然可停（不点继续即停）。但 S5 修复是 LLM 多轮自主运行，需在 `PipelineWizard` 提供「停止」按钮 → 发 `pipeline:control stop`，等待当前 LLM 轮结束（不杀进程，置 `aborted`，下次从当前 stage 续）。「恢复」→ `ControlPipeline(run_id,"resume")` 把 `paused→running`，从 `current_stage`+`context_json` 重建现场继续。
- **运行模式（手动 vs 自动）**：`pipeline_runs.auto_run` 区分。
  - **手动模式（默认，C 期主用）**：到每个闸门停下等用户点「继续」；用户随时可「停止」。
  - **自动模式（A 期主用，可选）**：Orchestrator 自主连跑多阶段/多章，仅在 `pipeline:gate`（G1/G2/G3b/G4/G终）停下；运行中提供「暂停/停止」浮钮。
  - 模式可在运行时切换（切到手动即下一安全点停下）。

### 卷结构自动化：LLM 自动建卷 / 写卷纲（当前代码缺口）

**现状（代码实证）**：仓库仅有 `internal/volume/store.go` 的 `volume.NewStore().Place()` 内部方法，**未暴露为 MCP 工具**。卷目前只能在写章（`chapters/new.md` 且全书未建卷）时由 `rw_tools.go:314/359/457` 隐式创建「未分卷章节」。**没有任何供 LLM 调用的 `create_volume` 工具**——这正是 §3 S2「自动建卷/写卷纲落库」此前无法落地的根因。`story-blueprint` 想「自动建卷」却无手段。

卷纲文件 `volumes/id_{id}.md` 虽可用现有 write 工具写入（见 `rw_tools.go:36` 路径说明），但**卷记录本身无法由 LLM 创建**。

**新增能力：`create_volume` MCP 工具（PR2 硬前置）**

- 新建 `internal/mcp_tools/volume_tools.go`，在 `RegisterAllTools` 注册；并加入主 agent 工具白名单（**注意：子 agent 枚举仅 `memory`/`review`/`statesync`，`internal/mcp_tools/subagent_tools.go:12` 无 `blueprint` 子 agent**，故 blueprint 走「一次性 agent run」而非子 agent）+ `app/handler.go` 外部白名单（接法参考 `list_volumes`，commit 7b24a23）。
- 入参：
  ```go
  type CreateVolumeArgs struct {
      Name           string  `json:"name" validate:"required"`   // 卷名
      BeforeVolumeID *int64  `json:"before_volume_id"`           // 可选：插到某卷前；为空则追加末尾
      Outline        *string `json:"outline"`                    // 可选：建卷同时写入卷纲正文
  }
  ```
- 实现：调 `volume.NewStore(ctx).Place(...)` 创建卷记录（复用两阶段排序落库，`store.go:52`），若 `Outline` 非空则同步写 `volumes/id_{新卷id}.md`（复用现有文件 write 通道）。返回新卷 `id` + 卷纲路径。
- 只读性：`create_volume` 是**写工具**，**不计入 `safety.go` 的 `readOnlyTools`**——与 §2 硬约束一致（建卷是确定的结构写入，非探索性读）。

**在流水线中的使用点**

- **S2 蓝图（主用）**：`story-blueprint` agent 先产出卷划分方案，再**逐卷调用 `create_volume`（带 `outline`）** 落库 → 卷结构与卷纲同时就位；随后 S3 写章按 `VolumeID` 挂接。
- **S0 续写 / 归卷**（§3.1.2 第 1 步）：若导入书无 Volumes，`story-blueprint` 的「仅建卷」模式调用 `create_volume` 把现有内容结构化归卷（不重新构思世界观）。
- **S5 修复**：当修复发现需新增卷（如线性结构需拆卷）时，可调用 `create_volume` 补卷。

**配套（PR2 一并考虑）**：`update_volume`（重命名/移动，复用 `volume.NewStore().Update()/.Place()`），供 S2/S5 调整卷顺序与命名。

---

## 6. C 期方案（前端向导外壳，本期 MVP）

**定位**：不动底层编排，把现有面板包一层"引导模式"。

### 6.1 前端
- **`PipelineWizard`** 组件：左侧线性阶段进度条（S1→S2→S3→S4），高亮当前阶段；包裹现有每段 UI：
  - S1 → `BookPitchWorkflowView`
  - S2 → 新 `BlueprintWorkflowView`（调 `story-blueprint` skill，产物落 `blueprint.md`）
  - S3 → 现有章节编辑器 + 写章（评分门禁/状态同步已在 `edit` 内自动发生）
  - S4 → 新 `SignabilityWorkflowView`（调 `signability-audit` skill）
- **`PipelineGateCard`**：监听 `pipeline:gate`，渲染"继续 / 修改 / 终止"三按钮。
- 顺序逻辑**由前端 wizard 持有**（最省事、最快见效）。

### 6.2 后端（C 期最小化）
- `pipeline_runs` 表 + CRUD。
- `StartPipeline(novel_id)` / `GetPipelineState(novel_id)` / `ResumePipeline(run_id, decision, feedback)`。
- `runStateSync` 同款 `wails.EventsEmit` 发射 `pipeline:stage` / `pipeline:gate`。
- 各阶段的"实际执行"在 C 期仍走现有 UI / skill 调用路径，后端只管状态与事件。

### 6.3 C 期已知限制
- 仍依赖用户在向导内操作；若用户游离出向导面板，流水线状态可能与其实际进度脱节 → 需"从当前面板同步状态"的容错逻辑（C 期末可先做最简版：向导仅作引导，状态以实际产物为准）。

---

## 7. A 期方案（后端确定性状态机，终态）

**定位**：把顺序与闸门逻辑从前端下沉到后端，彻底根治"软编排卡死"。

### 7.1 新增 `internal/pipeline` 包
- `Stage` 枚举（S1..S4 + S3_loop）与阶段流转表（DAG / 线性 + 循环）。
- `Orchestrator`：
  - 持有当前 `run_id` 与 `current_stage`；
  - 按表推进，调用对应能力（skill 作为一次性 agent run 的 system prompt / 子 agent / `edit` 工具）；
  - 在闸门点 `emit pipeline:gate` 并 **阻塞等待 `ResumePipeline`**；
  - 把上阶段产物注入下阶段 agent 的上下文（确定性产物传递）。
- "按名字跑 skill"能力：以 skill 正文作 system prompt 的一次性 agent run。**实证参考**：`app/skill_audit_e2e_test.go` 已用 `llmClient.GenerateText` 裸调用（`sysPrompt = 方法论 + sk.Content`）把 signability-audit 正文直接作 system prompt 作用于样稿——证明「markdown 方法论 → 一次评估」可行；但**该测试无 MCP 工具、无 agent loop**，故它只验证了「内容可驱动一次 LLM 评估」，并**不等于**「skill 能作为带工具、带上下文、带产物的流水线节点原生运行」。把 skill 升级为流水线节点所需的确定性执行入口（显式 `RunSkillByName` / 一次性 agent run + 工具 + 产物回收）仍须新建，见 §13。

### 7.2 前端在 A 期退化
- 仅渲染 `pipeline:stage` 进度与 `pipeline:gate` 确认卡；不再持有顺序逻辑。
- 支持"自动跑到下一闸"模式（用户可放手让 Orchestrator 连跑多阶段，仅在闸门停下）。

---

## 8. 落地拆分（建议 5 个 PR）

| PR | 内容 | 交付标志 |
|---|---|---|
| **PR1（先跑通闭环 + 运行控制）** | `pipeline_runs` 表（含 `paused`/`auto_run`）+ `pipeline:stage`/`pipeline:gate` 事件 + `pipeline:control` 命令通道 + `PipelineWizard` + `PipelineGateCard`；串 `S1 → G1 → S3 写章（单章含评分门禁+statesync）`；**含暂停/恢复/停止开关（控制条 + 安全检查点）** | 用户点"继续"即推进；运行中可随时暂停/停止/恢复，状态持久化可续跑 |
| **PR2** | 新增 `create_volume` MCP 工具（**PR2 硬前置，解决"LLM 自动建卷"缺口**）+ 接入 `story-blueprint` 节点（S2）+ **自动建卷/写卷纲落库** + G2 | 蓝图阶段可在向导内完成并确认，卷结构已由 LLM 经 `create_volume` 落库 |
| **PR3** | 接入 `signability-audit`（S4a，黄金三章后触发）+ **S5 修复闭环** + 复评 | 开篇即可签约评审→按 issue list 修复→复评，闭环跑通 |
| **PR4** | G3b 里程碑闸（每5章/卷边界）+ 卷边界 S4b 复评 + S5 修复 | 长线写作的多节点质量闸门就位 |
| **PR5（A 演进）** | 新增 `internal/pipeline` Orchestrator，把 PR1-4 的顺序/闸门/复评逻辑下沉；支持"自动跑到下一闸"；补 S6 完本终检 + G终 | 顺序不再依赖前端/LLM，确定性；完本有收口 |

---

## 9. 关于"不选双模式切换"

用户决策：**流水线即主创作入口**。自由聊天面板保留为"临时答疑 / 微调"工具（仍可随时调，不作为主线）。理由：双模式会让用户困惑于"该用哪个"，且流水线本就是要替代散落操作的。

---

## 10. 开放问题与风险

1. **G3 闸门粒度（已决议）**：**G3b 里程碑闸** = 每 5 章**或**每到卷边界，取先到者，用户确认继续（5 章为可配置默认间隔，避免把高潮章硬切开）。**黄金三章不再设单独确认闸**——卷1前3章写完直接进入 S4a 签约评审，S4 自身即"门"。S4 签约评审前置到开篇（卷1前3章后）触发，而非写在最后，更贴合真实网文"早签"逻辑；后续在卷边界再触发 S4b 复评。
2. **蓝图是否强制**：建议 S2 默认可跳过（轻小说/随写随改场景），但签约向创作强烈建议走；且 S2 需**真正建卷/写卷纲落库**（不只是产出 markdown）。
3. **签约体检产物如何反哺（已决议）**：评审报告不只是展示——它产出 **issue list**，直接驱动 **S5 修复**（覆盖卷纲/章细纲/正文/逻辑/台账），修复后回 S4 复评，形成 **S4→S5→复评** 闭环。C 期即落地该闭环（无需等到 A 期）。
4. **C 期状态漂移**：用户游离出向导时，需"以实际产物为准"的同步兜底。
5. **技能单跑 API**：A 期需要稳定的 `RunSkillByName`（一次性 agent run + skill 作 system prompt）。当前 skills 主要以"上下文合并"方式注入，需确认/补一个显式单跑入口（e2e 测试已有雏形）。
6. **重启续跑**：`pipeline_runs` 持久化需能从中断阶段恢复，而非从头；尤其 S5 修复中途中断也应能续。
7. **S5 修复范围与终止**：S5 覆盖"卷纲/章细纲/正文/逻辑/台账"多层，必须有明确终止条件（复评通过 / 用户强制继续），否则会无限循环；首次签约评审（S4a）优先级最高的是黄金三章正文+开篇 hook+台账。
8. **章细纲每章强制前置（已决议）**：用户明确——每章必须先写「章细纲」再写正文，流水线内不得跳过，否则前后章逻辑/人设/伏笔失序、作品崩坏。需保证 `edit` 在缺章细纲时不被推进（C 期向导层拦截；A 期 Orchestrator 前置校验）。
9. **台账实时维护（已决议）**：用户明确——台账（角色/地点/弧线/时间线/读者认知）必须每次章节变更后及时 `statesync` 落库，否则小说漏洞百出。注意：此前已从主 agent 内联循环移除"请执行维护"提醒、改为后端 `scheduleStateSync` 自动派发（commit 3d796e2）——本约束要求该自动派发**必须完成（含落库）才允许推进下一章/闸门**，不可因"后台异步"而被绕过；C 期向导应在 `statesync:progress=done` 前禁用"下一章"操作。
10. **续写 / 导入模式切入（已决议）**：流水线必须支持"新开"与"续写/导入"两种起点。续写模式新增 **S0 导入对齐**（结构校验/归卷 → 补存量章细纲 → 全量台账重建 → 现状体检 signability-audit → S5 存量修复），完成后汇入公共循环；差异仅在 S4 首次触发点（新开=写完黄金三章 S4a；续写=导入对齐后 S4-entry）。`pipeline_runs` 增加 `mode` / `start_chapter` 字段；A 期 Orchestrator 依据小说当前状态（章节数/Volumes 是否存在/台账是否齐全）自动决定入口阶段。续写模式同样遵守 §2 两条硬约束（章细纲前置、台账实时维护）。
11. **暂停 / 恢复 / 停止开关（已决议）**：自动化流程必须支持随时开始/暂停/恢复/停止。`pipeline_runs.status` 扩展 `paused` / `aborted`，新增 `auto_run` 列区分手动/自动模式；前端控制条经 `pipeline:control` 事件（cmd: start|pause|resume|stop）驱动后端 `ControlPipeline`。A 期 `Orchestrator` 在阶段切换、S3 各步之间、**S5 修复每轮迭代前**、自动连跑每章之间设「安全检查点」轮询状态，命中 `paused/aborted` 即挂起/退出并持久化；C 期在 S5 修复等多轮自主环节提供「停止」按钮（等待当前 LLM 轮结束，置 `aborted`，下次从 `current_stage` 续）。停止/暂停后均从 `current_stage`+`context_json` 重建现场续跑。
12. **LLM 自动建卷（已决议，当前代码缺口）**：现有代码**无 `create_volume` MCP 工具**，卷仅能在写章时隐式创建「未分卷章节」（见 `rw_tools.go` 多处调用 `volume.NewStore().Place()`）；故 S2「自动建卷/写卷纲落库」此前无法落地。PR2 须新增 `internal/mcp_tools/volume_tools.go` 暴露 `create_volume`（入参 `name` / `before_volume_id` / `outline`，实现复用 `volume.NewStore().Place()` + 同步写 `volumes/id_{id}.md`），并加入主 agent 工具白名单（**子 agent 枚举无 `blueprint`，故 story-blueprint 走一次性 agent run**）+ `app/handler.go` 外部白名单（参考 `list_volumes` 接法）。S2 由 `story-blueprint` agent 调 `create_volume` 逐卷落库；S0 归卷、S5 补卷亦用此工具。配套 `update_volume`（重命名/移动）PR2 一并考虑。

---

## 11. 验收标准

- [ ] 用户从书架/工作流入口启动流水线，看到线性阶段进度（S1→S2→S3→S4→S5→S6）。
- [ ] 完成 S1→G1→S2→G2→S3(黄金三章)→S4a→S5(修复)→复评→G3b(每5章/卷边界)→S4b→S5→…→S6→G终，全程只需点"继续/修改/终止"。
- [ ] **签约评审→修复→复评闭环**生效：评审不过即进入 S5 按 issue list 修复，修复后复评，直到通过或用户强制继续。
- [ ] S2 真正完成**自动建卷/写卷纲落库**（不仅是 markdown）。
- [ ] **章细纲每章强制前置**：S3 每章缺章细纲时 `edit` 不可推进（C 期向导拦截 / A 期 Orchestrator 前置校验）。
- [ ] **台账实时维护**：每次章节变更后 `statesync` 必须落库完成，在此之前下一章/闸门操作被禁用，不得因"后台异步"被绕过。
- [ ] **续写 / 导入模式**：选"续写"时走 S0 导入对齐（归卷→补章细纲→全量台账重建→现状体检→S5 存量修复），完成后汇入公共循环；A 期 Orchestrator 能按小说当前状态自动选入口（无章节从 S1 / 有章节>3且无台账从 S0 / 已就绪直接 S3）。
- [ ] 每个阶段产物正确落入下一阶段上下文（蓝图喂写作、签约体检读全本、issue list 驱动修复）。
- [ ] 流水线状态持久化，重启应用后可续跑（含 S5 修复中途中断）。
- [ ] **暂停 / 恢复 / 停止开关**：运行中可随时暂停/停止，状态持久化；恢复后从 `current_stage`+`context_json` 续跑；A 期在 S5 修复每轮迭代前等安全检查点可被打断，不丢进度。
- [ ] **LLM 自动建卷**：S2 由 `story-blueprint` agent 调新增的 `create_volume` 工具逐卷落库（卷记录 + 卷纲 `volumes/id_{id}.md` 同时就位），而非仅产出 markdown；S0 归卷、S5 补卷亦能经 `create_volume` 建卷。
- [ ] 不再依赖 LLM 记住"下一步该干嘛"（C：前端强制；A：后端强制）。
- [ ] 全部门禁通过：prettier / i18n:check / eslint(0 error) / build / go build；Go 预提交钩子（gofmt + golangci-lint）通过。

---

## 12. 附录：关键代码定位（便于开工）

- 主 agent 软编排指令：`internal/agentcfg/identity.go`（第 1~7 步）
- 评分门禁范式：`internal/mcp_tools/rw_tools.go:989` `maybeScoreChapter`（函数定义）；调用点 `:432`；门禁注释 `:429`；`:515` 落库
- 状态同步自动派发：`internal/mcp_tools/rw_tools.go:907` `scheduleStateSync` / `:925` `runStateSync`（含 `statesync:progress` 事件发射，可照搬）
- 子 agent 类型枚举：`internal/mcp_tools/subagent_tools.go:12` `AgentType`（`memory`/`review`/`statesync`）
- 前端工作流骨架：`frontend/src/views/WorkspaceView.tsx:163` `activeWorkflow`；`:162` 注释；`:611` 半接占位；`frontend/src/types/panel.ts:21-22`
- skill 路由证据：`internal/agentcfg/skill_catalog.go:54`；`internal/skill/regression_test.go:74-75`
- 全局事件范式：`frontend/src/hooks/useStateSyncProgress.ts` + `internal/mcp_tools/rw_tools.go` `runStateSync`
- **卷创建缺口（LLM 自动建卷）**：`internal/volume/store.go:52` `Volume.Place`（内部两阶段建卷，未暴露 MCP）；写章隐式建「未分卷章节」调用点 `internal/mcp_tools/rw_tools.go:314/359/457`；卷纲文件 write 通道见 `:36`；**需新增 `internal/mcp_tools/volume_tools.go` 暴露 `create_volume`**（参考 `list_volumes` 接法 commit 7b24a23）
- **运行控制**：`pipeline_runs.status` 扩展 `paused`/`aborted` + `auto_run` 列（§4）；前端 `pipeline:control` 事件驱动后端 `ControlPipeline`；A 期 `Orchestrator` 安全检查点（§5.1）
- 既有设计文档：`docs/v3-system-design.md`、`internal/agent/DESIGN.md`、`internal/mcp_tools/DESIGN.md`

---

## 13. skill 代码化策略（审计结论：编排必代码化，内容不写死）

审计后关于「流水线中的 skill 是否需要代码化」的定论如下。**核心区分**：代码化的是「编排与触发机制」，不是「创作内容」。

### 13.1 必须代码化的部分（确定性、可复用、无 LLM 自由裁量）
1. **编排层（A 期 Orchestrator，#1 优先级）**：阶段顺序、闸门（G1/G2/G3b/G4/G终）、S4→S5→复评闭环、产物传递——全部下沉为后端确定性状态机。**这是根治「软编排卡死」的唯一手段，不能依赖 LLM 记住下一步。**
2. **skill 调用机制（封装为确定性节点）**：当前 skill 只是 .md，由 LLM `read` 后照做、`router.go` 只做消息路由，**没有带工具 / 带上下文 / 带产物回收的程序化执行入口**（§1.2、§7.1 已实证）。必须新建其一：
   - **方案甲（推荐，对齐 review/statesync 范式）**：把 `story-blueprint` / `signability-audit` / `book-pitch` 提升为**原生子 agent**——像 `run_subagent` 那样内置 const 硬编码 system prompt + 受控工具白名单 + 确定性产物回收；但**子 agent 枚举当前仅 `memory`/`review`/`statesync`**，新增 `blueprint`/`signability` 须扩展 `internal/mcp_tools/subagent_tools.go:12` 的 `AgentType`。
   - **方案乙（最小改动）**：新增一个 `run_skill(name)` MCP 工具，后端按名字加载 `internal/skill/builtin/<name>.md` 作一次性 agent run 的 system prompt，并挂载指定工具白名单、回收产物。复用 `app/skill_audit_e2e_test.go` 的「markdown → 一次性 LLM 评估」范式，但补上工具与产物环节。
   - 二者都达成同一目标：**让 skill 成为可被 Orchestrator 确定性触发的节点**，而非靠 LLM 自觉调用。
3. **运行控制与持久化**（§4 / §5）：`pipeline_runs` 状态机、暂停/恢复/停止安全检查点、事件协议——纯后端代码。

### 13.2 绝不写死的部分（必须保留 LLM 自适应）
1. **创意生成内容**：book-pitch 的卖点/人设构思、逐章正文写作、S5 修复的具体改写——这些依赖 LLM 对上下文的即时判断，**硬编码会丧失适应性**，应保留为「LLM + skill 方法论 + 工具」的开放执行。
2. **skill 正文（方法论）**：保持 .md 可读性，由作者/用户迭代优化；代码只负责「加载它、喂工具、收产物」，不把方法论固化进 Go 常量（除 review/statesync 那种已成熟的「系统提示」范式外，蓝图/签约类不照搬）。

### 13.3 落地归属
- 编排层与运行控制 → 随 PR1 / PR5（A 期 Orchestrator）落地。
- skill 调用机制（方案甲或乙）→ 建议作为独立 PR 前置，最晚不晚于 PR2（S2 蓝图需确定性调 `story-blueprint`）与 PR3（S4 需确定性调 `signability-audit`）。
- 创意内容与 skill 正文 → 不进入代码化范围，仅通过 prompt / .md 迭代。
